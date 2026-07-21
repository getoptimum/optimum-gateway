package gossipsub_gateway

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/version"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestHandshakeBuilder(t *testing.T) {
	t.Run("BuildsHandshakeWithClusterAndJWT", func(t *testing.T) {
		// given
		srv, rig := newHandshakeTestService(t, nil)

		// when
		got, ok := srv.handshakeBuilder().(*Handshake)
		require.True(t, ok)

		// then
		require.Equal(t, srv.cfg.GatewayClusterID, got.ClusterID)
		require.NotEmpty(t, got.JWTToken)
		require.Equal(t, int32(1), rig.Calls.Load())
	})

	t.Run("ReturnsEmptyTokenWhenMintFails", func(t *testing.T) {
		// given
		srv, rig := newHandshakeTestService(t, func(rig *test_utils.AuthTestRig, _ *config.AppConfig) {
			rig.ResponseStatus = http.StatusUnauthorized
		})

		// when
		got, ok := srv.handshakeBuilder().(*Handshake)
		require.True(t, ok)

		// then
		require.Equal(t, srv.cfg.GatewayClusterID, got.ClusterID)
		require.Empty(t, got.JWTToken)
		require.Equal(t, int32(1), rig.Calls.Load())
	})
}

func TestHandshakeHandler(t *testing.T) {
	// given
	srv, rig := newHandshakeTestService(t, nil)
	expectedPeerID := mustPeerID(t, rig.DefaultPeerID)
	t.Run("AcceptsValidHandshake", func(t *testing.T) {
		// when
		payload := mustMarshalHandshake(t, srv.handshakeBuilder().(*Handshake))

		// then
		require.NoError(t, srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewReader(payload))))
	})

	t.Run("ReturnsDecodeErrorForInvalidJSON", func(t *testing.T) {
		// when, then
		require.Error(t, srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewBufferString("{"))))
	})

	t.Run("RejectsMismatchedCluster", func(t *testing.T) {
		// given
		handshake := srv.handshakeBuilder().(*Handshake)
		handshake.ClusterID = "different-cluster"

		// when
		err := srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, handshake))))

		// then
		require.EqualError(t, err, "invalid cluster ID: different-cluster")
	})

	t.Run("RejectsInvalidToken", func(t *testing.T) {
		// given
		handshake := srv.handshakeBuilder().(*Handshake)
		handshake.JWTToken = "not-a-jwt"

		// when, then
		require.Error(t, srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, handshake)))))
	})

	t.Run("RejectsExpiredToken", func(t *testing.T) {
		// given
		_ = srv.handshakeBuilder()
		handshake := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, rig.PrivateKey, func(claims *jwks_verifier.Claims) {
			now := time.Now()
			claims.IssuedAt = jwt.NewNumericDate(now.Add(-2 * time.Hour))
			claims.ExpiresAt = jwt.NewNumericDate(now.Add(-1 * time.Hour))
		}), version.GetCommitHash())

		// when, then
		require.Equal(t, handshake.CommitHash, version.GetCommitHash())
		require.Error(t, srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, handshake)))))
	})

	t.Run("RejectsTokenSignedWithAnotherKey", func(t *testing.T) {
		// given
		_ = srv.handshakeBuilder()
		otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		handshake := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, otherKey, nil), version.GetCommitHash())

		// when, then
		require.Equal(t, handshake.CommitHash, version.GetCommitHash())
		require.Error(t, srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, handshake)))))
	})

	t.Run("RejectsPeerIDMismatch", func(t *testing.T) {
		// given
		handshake := srv.handshakeBuilder().(*Handshake)
		otherPeerID := mustPeerIDFromIdentityDir(t, t.TempDir())

		// when
		err := srv.handshakeHandler(otherPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, handshake))))

		// then
		require.EqualError(t, err, "peer ID mismatch: expected "+otherPeerID.String()+", got "+expectedPeerID.String())
	})

	t.Run("AcceptsHandshakeWhenAuthDisabled", func(t *testing.T) {
		disabled, _ := newHandshakeTestService(t, func(_ *test_utils.AuthTestRig, cfg *config.AppConfig) {
			cfg.EnableAuth = false
		})
		handshake := NewHandshake(disabled.cfg.GatewayClusterID, "", version.GetCommitHash())
		otherPeerID := mustPeerIDFromIdentityDir(t, t.TempDir())

		require.NoError(t, disabled.handshakeHandler(otherPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, handshake)))))
	})

	t.Run("ChainID", func(t *testing.T) {
		_ = srv.handshakeBuilder()
		cases := map[string]struct {
			chainID string
			wantErr string
		}{
			"accepts hoodi":   {chainID: "hoodi"},
			"accepts 560048":  {chainID: "560048"},
			"rejects mainnet": {chainID: "mainnet", wantErr: "chain mismatch"},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				h := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
					c.ChainID = tc.chainID
				}), version.GetCommitHash())
				err := srv.handshakeHandler(expectedPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))
				if tc.wantErr == "" {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				require.Equal(t, h.CommitHash, version.GetCommitHash())
				require.Contains(t, err.Error(), tc.wantErr)
			})
		}
	})
}

// Pins the property at the handshakeHandler boundary: the verifier is
// always invoked with AudP2P, so any token without aud=p2p fails the
// handshake. wantErr substrings are the jwt/v5 messages bubbled up
// through jwks_verifier — stable across our wrap chain.
func TestHandshakeHandler_Audience(t *testing.T) {
	srv, rig := newHandshakeTestService(t, nil)
	peerID := mustPeerID(t, rig.DefaultPeerID)
	_ = srv.handshakeBuilder()

	cases := map[string]struct {
		aud     jwt.ClaimStrings
		wantErr string
	}{
		"accepts p2p":         {aud: jwt.ClaimStrings{"p2p"}},
		"rejects missing aud": {aud: nil, wantErr: "aud claim is required"},
		"rejects services":    {aud: jwt.ClaimStrings{"services"}, wantErr: "invalid audience"},
		"rejects other":       {aud: jwt.ClaimStrings{"something"}, wantErr: "invalid audience"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
				c.Audience = tc.aud
			}), version.GetCommitHash())
			err := srv.handshakeHandler(peerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Cluster binding (#707): the gateway's own cluster must be a member of the token's
// cluster_ids claim; missing or non-member both reject.
func TestHandshakeHandler_Cluster(t *testing.T) {
	srv, rig := newHandshakeTestService(t, nil)
	_ = srv.handshakeBuilder() // prime our own claims so the chain check passes
	peerID := mustPeerID(t, rig.DefaultPeerID)
	cases := map[string]struct {
		clusterIDs []string
		wantErr    string
	}{
		"accepts member":        {clusterIDs: []string{"optimum_hoodi_v0_3"}},
		"accepts member in set": {clusterIDs: []string{"optimum_mainnet_v0_1", "optimum_hoodi_v0_3"}},
		"rejects missing":       {clusterIDs: nil, wantErr: "missing cluster claim"},
		"rejects non-member":    {clusterIDs: []string{"optimum_mainnet_v0_1"}, wantErr: "cluster not authorized"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
				c.ClusterIDs = tc.clusterIDs
			}), version.GetCommitHash())
			err := srv.handshakeHandler(peerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func newHandshakeTestService(
	t *testing.T,
	configure func(rig *test_utils.AuthTestRig, cfg *config.AppConfig),
) (*Service, *test_utils.AuthTestRig) {
	t.Helper()

	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	cfg.GatewayClusterID = "optimum_hoodi_v0_3"
	// Minted handshake tokens must carry a cluster_ids claim covering the gateway's
	// cluster, since the handshake now enforces membership unconditionally.
	rig.SetClusterIDs(cfg.GatewayClusterID)
	if configure != nil {
		configure(rig, cfg)
	}

	log := logger.NewAppSLogger(logger.Debug)
	authMgr, err := auth_token.New(t.Context(), log, cfg)
	require.NoError(t, err)

	return &Service{
		ctx:     t.Context(),
		cfg:     cfg,
		log:     log,
		authMgr: authMgr,
	}, rig
}

func mustMarshalHandshake(t *testing.T, handshake *Handshake) []byte {
	t.Helper()

	payload, err := json.Marshal(handshake)
	require.NoError(t, err)
	return payload
}

func mustPeerID(t *testing.T, raw string) peer.ID {
	t.Helper()

	peerID, err := peer.Decode(raw)
	require.NoError(t, err)
	return peerID
}

func mustPeerIDFromIdentityDir(t *testing.T, identityDir string) peer.ID {
	t.Helper()

	_, err := identity.EnsureIdentity(identityDir)
	require.NoError(t, err)
	identityKey, err := identity.ExtractIdentityFromDir(identityDir)
	require.NoError(t, err)
	return identityKey.ID
}
