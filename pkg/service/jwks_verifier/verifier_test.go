package jwks_verifier_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
)

// jwksFixture wraps a freshly-generated ES256 keypair together with an
// httptest.Server that serves the JWKS at /.well-known/jwks.json
// (the suffix jwks_verifier.New appends to AppConfig.RemoteAuthURL).
// Tests set appCfg.RemoteAuthURL to the server's base URL.
type jwksFixture struct {
	priv     *ecdsa.PrivateKey
	server   *httptest.Server
	baseURL  string // serves as the JWT issuer claim AND AppConfig.RemoteAuthURL
	diskPath string // disk-cache path for the JWKS doc (per-test t.TempDir())
}

// appCfg returns an AppConfig pointing the verifier at this fixture's JWKS
// endpoint with a per-test disk cache path.
func (f *jwksFixture) appCfg() *config.AppConfig {
	return &config.AppConfig{
		RemoteAuthURL:          f.baseURL,
		JWKSCachePath:          f.diskPath,
		JWKSRefreshIntervalSec: 3600,
	}
}

func newJWKSFixture(t *testing.T) *jwksFixture {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	jwksDoc, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "EC",
			"crv": "P-256",
			"kid": "test-key",
			"alg": "ES256",
			"use": "sig",
			"x":   base64.RawURLEncoding.EncodeToString(priv.X.Bytes()),
			"y":   base64.RawURLEncoding.EncodeToString(priv.Y.Bytes()),
		}},
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksDoc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &jwksFixture{
		priv:     priv,
		server:   srv,
		baseURL:  srv.URL,
		diskPath: filepath.Join(t.TempDir(), "jwks.json"),
	}
}

// sign produces an ES256-signed JWT with the test key. Default claims:
// iss=baseURL, sub="gw-test", exp=+1h, scope_version=1, type=partner,
// chain_id=hoodi. Mods can override any of them.
func (f *jwksFixture) sign(t *testing.T, mods ...func(*jwks_verifier.Claims)) string {
	t.Helper()
	now := time.Now()
	claims := jwks_verifier.Claims{
		ScopeVersion: 1,
		Type:         "partner",
		ChainID:      "hoodi",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    f.baseURL,
			Subject:   "gw-test",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			Audience:  jwt.ClaimStrings{jwks_verifier.AudP2P},
		},
	}
	for _, m := range mods {
		m(&claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = "test-key"
	signed, err := tok.SignedString(f.priv)
	require.NoError(t, err)
	return signed
}

func TestNew_RequiresAppConfig(t *testing.T) {
	_, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "AppConfig")
}

func TestNew_RequiresRemoteAuthURL(t *testing.T) {
	_, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), &config.AppConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "RemoteAuthURL")
}

func TestVerify_RejectsEmptyAudience(t *testing.T) {
	f := newJWKSFixture(t)
	v, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), f.appCfg())
	require.NoError(t, err)

	for _, aud := range []string{"", "   "} {
		_, err := v.Verify(f.sign(t), aud)
		require.ErrorIs(t, err, jwks_verifier.ErrInvalidToken)
		require.Contains(t, err.Error(), "expected audience missing")
	}
}

func TestVerify_HappyPath(t *testing.T) {
	f := newJWKSFixture(t)
	v, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), f.appCfg())
	require.NoError(t, err)

	token := f.sign(t, func(c *jwks_verifier.Claims) {
		c.ClusterIDs = []string{"optimum_hoodi_v0_3", "optimum_hoodi_v0_2"}
	})
	claims, err := v.Verify(token, jwks_verifier.AudP2P)
	require.NoError(t, err)
	require.Equal(t, commonentities.GatewayTypePartner, claims.Type)
	require.Equal(t, "hoodi", claims.ChainID)
	require.Equal(t, "gw-test", claims.Subject)
	require.Equal(t, int64(1), claims.ScopeVersion)
	require.Equal(t, []string{"optimum_hoodi_v0_3", "optimum_hoodi_v0_2"}, claims.ClusterIDs)
}

// TestVerify_FailureModes collapses every rejection reason (empty,
// garbage, wrong issuer, expired, missing/whitespace sub) into the same
// ErrInvalidToken sentinel — callers should switch on it without parsing.
func TestVerify_FailureModes(t *testing.T) {
	f := newJWKSFixture(t)
	v, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), f.appCfg())
	require.NoError(t, err)

	cases := map[string]string{
		"empty token":   "",
		"garbage token": "not.a.jwt",
		"wrong issuer":  f.sign(t, func(c *jwks_verifier.Claims) { c.Issuer = "https://attacker.test" }),
		"already expired": f.sign(t, func(c *jwks_verifier.Claims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			c.IssuedAt = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
		}),
		"empty subject":      f.sign(t, func(c *jwks_verifier.Claims) { c.Subject = "" }),
		"whitespace subject": f.sign(t, func(c *jwks_verifier.Claims) { c.Subject = "   \t  " }),
		"wrong audience":     f.sign(t, func(c *jwks_verifier.Claims) { c.Audience = jwt.ClaimStrings{jwks_verifier.AudServices} }),
	}

	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			_, err = v.Verify(tok, jwks_verifier.AudP2P)
			require.Error(t, err)
		})
	}
}

// TestVerify_WrongSigningAlg defends against alg-confusion attacks (e.g.
// attacker submits HS256 using the public key as the HMAC secret). The
// Verifier pins ES256, so HS256 must be rejected at the alg check before
// signature verification.
func TestVerify_WrongSigningAlg(t *testing.T) {
	f := newJWKSFixture(t)
	v, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), f.appCfg())
	require.NoError(t, err)

	claims := jwks_verifier.Claims{
		Type:    "partner",
		ChainID: "hoodi",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    f.baseURL,
			Subject:   "gw-test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			Audience:  jwt.ClaimStrings{jwks_verifier.AudP2P},
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte("any-secret"))
	require.NoError(t, err)

	_, err = v.Verify(signed, jwks_verifier.AudP2P)
	require.Error(t, err)
}

// TestVerify_TTLBound enforces the 6h-max-lifetime cap. A JWT that the
// upstream auth service is documented never to mint must still be
// rejected gateway-side as defense-in-depth against issuer mis-config.
func TestVerify_TTLBound(t *testing.T) {
	f := newJWKSFixture(t)
	v, err := jwks_verifier.New(t.Context(), logger.NewAppSLogger(logger.Debug), f.appCfg())
	require.NoError(t, err)

	now := time.Now()

	t.Run("exactly 6h accepted", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now)
			c.ExpiresAt = jwt.NewNumericDate(now.Add(6 * time.Hour))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.NoError(t, err)
	})

	t.Run("6h plus tiny skew accepted", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now)
			c.ExpiresAt = jwt.NewNumericDate(now.Add(6*time.Hour + 30*time.Second))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.NoError(t, err)
	})

	// Exact boundary lock: 6h + the documented 60s skew buffer is the
	// last accepted value; one second over rejects. If anyone tightens
	// or loosens the buffer, these tests force a deliberate update.
	t.Run("6h + 60s exactly accepted (boundary)", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now)
			c.ExpiresAt = jwt.NewNumericDate(now.Add(6*time.Hour + 60*time.Second))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.NoError(t, err)
	})

	t.Run("6h + 61s rejected (boundary)", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now)
			c.ExpiresAt = jwt.NewNumericDate(now.Add(6*time.Hour + 61*time.Second))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.Error(t, err)
	})

	t.Run("well over 6h rejected", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now)
			c.ExpiresAt = jwt.NewNumericDate(now.Add(24 * time.Hour))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.Error(t, err)
	})

	t.Run("missing iat rejected", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = nil
			c.ExpiresAt = jwt.NewNumericDate(now.Add(time.Hour))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.Error(t, err)
	})

	t.Run("iat in the far future rejected", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now.Add(10 * time.Minute))
			c.ExpiresAt = jwt.NewNumericDate(now.Add(time.Hour))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.Error(t, err)
	})

	t.Run("iat in the near future tolerated by clock skew", func(t *testing.T) {
		tok := f.sign(t, func(c *jwks_verifier.Claims) {
			c.IssuedAt = jwt.NewNumericDate(now.Add(10 * time.Second))
			c.ExpiresAt = jwt.NewNumericDate(now.Add(time.Hour))
		})
		_, err = v.Verify(tok, jwks_verifier.AudP2P)
		require.NoError(t, err)
	})
}

// silence unused warning — big.Int is referenced indirectly via ecdsa.
var _ = big.NewInt
