package test_utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
)

// AuthTestRig provides a self-contained mint + JWKS environment for manager test.
type AuthTestRig struct {
	PrivateKey    *ecdsa.PrivateKey
	server        *httptest.Server
	diskPath      string // per-test JWKS disk-cache path, populated in newAuthTestRig
	DefaultPeerID string
	Calls         atomic.Int32
	// ClaimMod mutates the claims before signing on each mint (optional).
	ClaimMod func(*jwks_verifier.Claims)

	// ValidatorIndexes appears in the mint response.
	ValidatorIndexes []uint64
	// OperatorID appears in the mint response body (not the JWT).
	OperatorID string
	// ResponseStatus / ResponseBody override the mint reply for error-path tests.
	ResponseStatus int
	ResponseBody   []byte
}

// AppCfg returns a minimal AppConfig the Manager will accept (auth enabled,
// API key set, JWKS cache path wired to a temp file).
func (r *AuthTestRig) AppCfg(t *testing.T) *config.AppConfig {
	t.Helper()

	identityDir := t.TempDir()
	_, err := identity.EnsureIdentity(identityDir)
	require.NoError(t, err)
	identityKey, err := identity.ExtractIdentityFromDir(identityDir)
	require.NoError(t, err)
	r.DefaultPeerID = identityKey.ID.String()

	cfg := &config.AppConfig{
		APIKey:                 "test-key",
		RemoteAuthURL:          r.server.URL,
		EnableAuth:             true,
		JWKSCachePath:          r.diskPath,
		JWKSRefreshIntervalSec: 3600,
		IdentityMumP2PDir:      identityDir,
		IdentityLibP2PDir:      t.TempDir(),
		AgentLibP2PPort:        33212,
		AgentMumP2PPort:        33213,
		TelemetryPort:          48123,
		GatewayClusterID:       "test-cluster",
		TelemetryEnable:        true,
		PropagationEnabledRaw:  true, // match the yaml-loaded test configs
	}
	cfg.InitDerived()
	require.NoError(t, cfg.Validate())
	return cfg
}

func (r *AuthTestRig) MustSignToken(t *testing.T, key *ecdsa.PrivateKey, modify func(*jwks_verifier.Claims)) string {
	t.Helper()

	now := time.Now()
	claims := jwks_verifier.Claims{
		ScopeVersion: 1,
		Type:         "partner",
		ChainID:      "hoodi",
		CNF: jwks_verifier.Confirmation{
			PeerID: r.DefaultPeerID,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    r.server.URL,
			Subject:   "gw-test",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			Audience:  jwt.ClaimStrings{jwks_verifier.AudP2P},
		},
	}
	// Apply the rig-wide ClaimMod first (parity with the mint stub), then the
	// per-call modify last so an explicit override wins (e.g. cluster nil/mismatch cases).
	if r.ClaimMod != nil {
		r.ClaimMod(&claims)
	}
	if modify != nil {
		modify(&claims)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

type Option func(*AuthTestRig)

func WithClaimModifier(claimMod func(*jwks_verifier.Claims)) Option {
	return func(r *AuthTestRig) {
		r.ClaimMod = claimMod
	}
}

// SetClusterIDs makes every minted token carry the given cluster_ids claim (#707),
// so handshakes against a gateway in one of those clusters pass the membership check.
func (r *AuthTestRig) SetClusterIDs(ids ...string) {
	r.ClaimMod = func(c *jwks_verifier.Claims) { c.ClusterIDs = ids }
}

// NewAuthTestRig returns a stub auth-mint server + ES256 keypair for tests.
// Optional functional options may modify the rig before it is returned.
func NewAuthTestRig(t *testing.T, opts ...Option) *AuthTestRig {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	rig := &AuthTestRig{PrivateKey: privateKey, OperatorID: "op-test"}

	jwksDoc, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "EC",
			"crv": "P-256",
			"kid": "test-key",
			"alg": "ES256",
			"use": "sig",
			"x":   base64.RawURLEncoding.EncodeToString(privateKey.X.Bytes()),
			"y":   base64.RawURLEncoding.EncodeToString(privateKey.Y.Bytes()),
		}},
	})
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jwksDoc)
	})
	mux.HandleFunc("/api/v1/auth/token", func(w http.ResponseWriter, req *http.Request) {
		rig.Calls.Add(1)
		payloadBytes, errR := io.ReadAll(req.Body)
		require.NoError(t, errR)
		var payload map[string]string
		require.NoError(t, json.Unmarshal(payloadBytes, &payload))

		if rig.ResponseStatus != 0 || rig.ResponseBody != nil {
			status := rig.ResponseStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if rig.ResponseBody != nil {
				_, _ = w.Write(rig.ResponseBody)
			}
			return
		}
		now := time.Now()
		claims := jwks_verifier.Claims{
			ScopeVersion: 1,
			Type:         "partner",
			ChainID:      "hoodi",
			CNF: jwks_verifier.Confirmation{
				PeerID: payload["peer_id"],
			},
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    rig.server.URL,
				Subject:   "gw-test",
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
				Audience:  jwt.ClaimStrings{jwks_verifier.AudP2P},
			},
		}
		if claims.CNF.PeerID == "" {
			claims.CNF.PeerID = rig.DefaultPeerID
		}
		if rig.ClaimMod != nil {
			rig.ClaimMod(&claims)
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		tok.Header["kid"] = "test-key"
		signed, err := tok.SignedString(privateKey)
		require.NoError(t, err)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      signed,
			"token_type":        "Bearer",
			"expires_in":        3600,
			"operator_id":       rig.OperatorID,
			"validator_indexes": rig.ValidatorIndexes,
		})
	})
	rig.server = httptest.NewServer(mux)
	t.Cleanup(rig.server.Close)
	rig.diskPath = filepath.Join(t.TempDir(), "jwks.json")
	for _, opt := range opts {
		opt(rig)
	}
	return rig
}
