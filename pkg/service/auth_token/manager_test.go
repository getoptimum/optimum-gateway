package auth_token_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const accessTokenKey = "access_token"

func TestNew(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)

	t.Run("RequiresLogger", func(t *testing.T) {
		_, err := auth_token.New(t.Context(), nil, rig.AppCfg(t))
		require.Error(t, err)
		require.Contains(t, err.Error(), "log")
	})
	t.Run("RequiresAppConfig", func(t *testing.T) {
		_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "AppConfig")
	})
	t.Run("NoAPIKey_ReturnsDisabledManager", func(t *testing.T) {
		cfg := rig.AppCfg(t)
		cfg.APIKey = ""
		m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.NoError(t, err)
		require.NotNil(t, m, "New must always return a non-nil Manager")
		require.False(t, m.IsEnabled(), "empty APIKey must produce a disabled Manager")
	})
	t.Run("RequiresRemoteAuthURL", func(t *testing.T) {
		cfg := rig.AppCfg(t)
		cfg.RemoteAuthURL = ""
		_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "RemoteAuthURL")
	})
	t.Run("AuthDisabled_ReturnsDisabledManager", func(t *testing.T) {
		cfg := rig.AppCfg(t)
		cfg.EnableAuth = false
		m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.NoError(t, err)
		require.NotNil(t, m, "New must always return a non-nil Manager — disabled state is on the Manager itself")
		require.False(t, m.IsEnabled(), "EnableAuth=false must produce a disabled Manager")
	})
}

func TestHasValidToken_FalseBeforeMint(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	require.False(t, m.HasValidToken(), "no mint yet → not valid")
}

func TestToken_MintsOnce_UsesCacheAfter(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	rig.ValidatorIndexes = []uint64{42, 1337}

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)

	// First call mints; second call serves from cache. The mint server's
	// call counter is the proof: must stay at 1.
	tok1, err := m.Token(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, tok1)

	tok2, err := m.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, tok1, tok2)
	require.Equal(t, int32(1), rig.Calls.Load(), "second Token() must hit cache, not mint server")

	require.True(t, m.HasValidToken())
	require.Equal(t, "hoodi", m.Chain().String())
	c := m.OwnClaims()
	require.NotNil(t, c)
	require.Equal(t, commonentities.GatewayTypePartner, c.Type)
	require.Equal(t, "gw-test", c.Subject)
	require.Equal(t, "op-test", m.OperatorID())
	require.Equal(t, []uint64{42, 1337}, m.ValidatorIndexes())
}

func TestMint_ErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantPart string // substring match when wantErr is nil-but-error-expected
	}{
		{"401 → ErrUnknownKey", http.StatusUnauthorized, "", auth_token.ErrUnknownKey, ""},
		{"403 key_revoked", http.StatusForbidden, `{"error":"key_revoked"}`, auth_token.ErrKeyRevoked, ""},
		{"403 key_suspended", http.StatusForbidden, `{"error":"key_suspended"}`, auth_token.ErrKeySuspended, ""},
		{"403 unknown reason", http.StatusForbidden, `{"error":"something_else"}`, nil, "403 from mint"},
		{"500 transient", http.StatusInternalServerError, `{"error":"transient"}`, nil, "mint returned 500"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := test_utils.NewAuthTestRig(t)
			rig.ResponseStatus = tc.status
			rig.ResponseBody = []byte(tc.body)

			m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
			require.NoError(t, err)
			_, err = m.Token(context.Background())
			require.Error(t, err)
			if tc.wantErr != nil {
				require.True(t, errors.Is(err, tc.wantErr), "want %v, got %v", tc.wantErr, err)
			} else {
				require.Contains(t, err.Error(), tc.wantPart)
			}
			require.False(t, m.HasValidToken(), "failed mint must not set HasValidToken")
		})
	}
}

func TestChain_NormalizesNumericChainID(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	rig.ClaimMod = func(c *jwks_verifier.Claims) { c.ChainID = "560048" }

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "hoodi", m.Chain().String(), "560048 must normalize to hoodi")
}

// TestDisabledManager_MethodsAreNoOps locks in the contract that every
// public Manager method degrades to a zero-value / no-op when the Manager
// is disabled (the state New returns for EnableAuth=false or empty
// APIKey). Callers rely on this so they don't have to special-case the
// disabled path everywhere; only the router's token gate needs IsEnabled().
func TestDisabledManager_MethodsAreNoOps(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	cfg.EnableAuth = false
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.False(t, m.IsEnabled())

	tok, err := m.Token(context.Background())
	require.NoError(t, err)
	require.Empty(t, tok, "disabled Manager.Token must not attempt to mint")

	st, err := m.ServicesToken(context.Background())
	require.NoError(t, err)
	require.Empty(t, st, "disabled Manager.ServicesToken must not attempt to mint")

	ht, err := m.HandshakeToken(context.Background())
	require.NoError(t, err)
	require.Empty(t, ht, "disabled Manager.HandshakeToken must not attempt to mint")

	require.Nil(t, m.OwnClaims())
	require.Empty(t, m.Chain())
	require.Empty(t, m.OperatorID())
	require.Equal(t, []uint64{}, m.ValidatorIndexes())
	require.False(t, m.HasValidToken())

	// Start must be a no-op — no refresh goroutine, no panic, no mint server hits.
	m.Start(context.Background())
	require.Equal(t, int32(0), rig.Calls.Load(), "disabled Manager.Start must not hit the mint server")
}

func TestEmptyGettersBeforeMint(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	require.Empty(t, m.Chain())
	require.Nil(t, m.OwnClaims())
	require.Empty(t, m.OperatorID())
	require.Equal(t, []uint64{}, m.ValidatorIndexes())
	require.False(t, m.HasValidToken())
}

func TestVerifyToken_ChainID(t *testing.T) {
	m, rig := newMintedManager(t, nil)
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
			tok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
				c.ChainID = tc.chainID
			})
			claims, err := m.VerifyToken(tok)
			if tc.wantErr == "" {
				require.NotNil(t, claims)
				require.NoError(t, err)
				return
			}
			require.Nil(t, claims)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestMint_RejectsTokenThatFailsLocalVerify(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rig.ResponseStatus = http.StatusOK
	rig.ResponseBody, err = json.Marshal(map[string]string{
		accessTokenKey: rig.MustSignToken(t, otherKey, nil),
	})
	require.NoError(t, err)

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed local verify")
	require.False(t, m.HasValidToken())
}

// ServicesToken returns the aud=services token from the mint response, distinct
// from the handshake token returned by Token/HandshakeToken.
func TestServicesToken_PreferredWhenPresent(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	accessTok := rig.MustSignToken(t, rig.PrivateKey, nil)
	servicesTok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.ScopeVersion = 2
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudServices}
	})
	require.NotEqual(t, accessTok, servicesTok)

	var err error
	rig.ResponseStatus = http.StatusOK
	rig.ResponseBody, err = json.Marshal(map[string]any{
		accessTokenKey:   accessTok,
		"services_token": servicesTok,
		"operator_id":    "op-test",
	})
	require.NoError(t, err)

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)

	gotHandshake, err := m.HandshakeToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, accessTok, gotHandshake)

	gotServices, err := m.ServicesToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, servicesTok, gotServices)
}

// Symmetric to TestServicesToken_PreferredWhenPresent: call ServicesToken
// FIRST, which exercises the mint-first branch of Service.ServicesToken
// (the path where ServicesToken triggers the initial mint rather than
// reading a cache primed by HandshakeToken). A subsequent HandshakeToken
// call must reuse the same mint result rather than re-minting.
func TestServicesToken_FirstCallMintsAndPrimesHandshake(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	accessTok := rig.MustSignToken(t, rig.PrivateKey, nil)
	servicesTok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.ScopeVersion = 2
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudServices}
	})
	require.NotEqual(t, accessTok, servicesTok)

	var err error
	rig.ResponseStatus = http.StatusOK
	rig.ResponseBody, err = json.Marshal(map[string]any{
		accessTokenKey:   accessTok,
		"services_token": servicesTok,
		"operator_id":    "op-test",
	})
	require.NoError(t, err)

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)

	gotServices, err := m.ServicesToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, servicesTok, gotServices)

	gotHandshake, err := m.HandshakeToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, accessTok, gotHandshake)

	require.Equal(t, int32(1), rig.Calls.Load(),
		"ServicesToken-first must mint once and prime the cache for HandshakeToken; no re-mint")
}

// When the mint response omits services_token (pre-split auth), ServicesToken
// falls back to the handshake token so centralized pushes keep working.
func TestServicesToken_FallsBackToHandshakeWhenAbsent(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t) // default response carries no services_token

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)

	handshake, err := m.HandshakeToken(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, handshake)

	services, err := m.ServicesToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, handshake, services, "absent services_token must fall back to the handshake token")
	require.Equal(t, int32(1), rig.Calls.Load(), "fallback must reuse the cached handshake token, not re-mint")
}

// A present-but-invalid services_token (fails local verify) is dropped; pushes
// fall back to the handshake token and the mint as a whole still succeeds.
func TestServicesToken_FallsBackWhenServicesTokenInvalid(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	accessTok := rig.MustSignToken(t, rig.PrivateKey, nil)
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	badServicesTok := rig.MustSignToken(t, otherKey, nil) // signed by an untrusted key

	rig.ResponseStatus = http.StatusOK
	rig.ResponseBody, err = json.Marshal(map[string]any{
		accessTokenKey:   accessTok,
		"services_token": badServicesTok,
		"operator_id":    "op-test",
	})
	require.NoError(t, err)

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)

	handshake, err := m.HandshakeToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, accessTok, handshake)
	require.True(t, m.HasValidToken(), "an invalid services token must not fail the whole mint")

	services, err := m.ServicesToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, accessTok, services, "invalid services token falls back to the handshake token")
}

func newMintedManager(t *testing.T, configure func(*test_utils.AuthTestRig)) (*auth_token.Service, *test_utils.AuthTestRig) {
	t.Helper()
	rig := test_utils.NewAuthTestRig(t)
	if configure != nil {
		configure(rig)
	}
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(context.Background())
	require.NoError(t, err)
	return m, rig
}

func TestGatewayLabels_FromServicesClaims(t *testing.T) {
	m, _ := newMintedManager(t, func(rig *test_utils.AuthTestRig) {
		accessTok := rig.MustSignToken(t, rig.PrivateKey, nil)
		servicesTok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
			c.Audience = jwt.ClaimStrings{jwks_verifier.AudServices}
			c.Label = "mainnet-sarca7-1"
			c.Region = "Europe (West)"
			c.ConsensusClient = "Lighthouse"
			// HostingProvider + DVT left empty → omitted.
		})
		body, err := json.Marshal(map[string]any{
			accessTokenKey:   accessTok,
			"services_token": servicesTok,
			"operator_id":    "op-test",
		})
		require.NoError(t, err)
		rig.ResponseStatus = http.StatusOK
		rig.ResponseBody = body
	})
	require.Equal(t, map[string]string{
		"gateway_label":    "mainnet-sarca7-1",
		"region":           "Europe (West)",
		"consensus_client": "Lighthouse",
	}, m.GatewayLabels())
}

func TestGatewayLabels_NilWhenNoServicesToken(t *testing.T) {
	m, _ := newMintedManager(t, func(rig *test_utils.AuthTestRig) {
		accessTok := rig.MustSignToken(t, rig.PrivateKey, nil)
		body, err := json.Marshal(map[string]any{
			accessTokenKey: accessTok,
			"operator_id":  "op-test",
		})
		require.NoError(t, err)
		rig.ResponseStatus = http.StatusOK
		rig.ResponseBody = body
	})
	require.Nil(t, m.GatewayLabels())
}
