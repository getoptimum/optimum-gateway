package auth_token_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const (
	// helperOperatorID is deliberately distinct from the rig's, so a response that
	// came through the helper is identifiable in a failure.
	helperOperatorID = "op-helper"
	operatorIDKey    = "operator_id"
)

// localMint stands in for a credential helper: it answers the mint path and records
// what the gateway sent it.
type localMint struct {
	*httptest.Server
	calls atomic.Int32
	body  atomic.Pointer[map[string]string]
	// servicesToken overrides the aud=services token in the reply when set.
	servicesToken string
}

// newLocalMint replies with the token produced by reply, which lets a test choose
// between a genuinely issued token and one the gateway must reject.
func newLocalMint(t *testing.T, reply func(peerID string) string) *localMint {
	t.Helper()
	lm := &localMint{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/token", func(w http.ResponseWriter, r *http.Request) {
		lm.calls.Add(1)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		lm.body.Store(&payload)
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"access_token": reply(payload["peer_id"]),
			"token_type":   "Bearer",
			"expires_in":   3600,
			operatorIDKey:  helperOperatorID,
		}
		if lm.servicesToken != "" {
			body["services_token"] = lm.servicesToken
		}
		require.NoError(t, json.NewEncoder(w).Encode(body))
	})
	lm.Server = httptest.NewServer(mux)
	t.Cleanup(lm.Close)
	return lm
}

func TestAuthTokenURL_MintsLocallyAndStillTrustsTheIssuer(t *testing.T) {
	// Given a helper serving the mint path, when the gateway mints, then it must call
	// the helper rather than the issuer, and must accept the token it gets back.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	helper := newLocalMint(t, func(string) string {
		return rig.MustSignToken(t, rig.PrivateKey, nil)
	})
	cfg.APIKey = ""
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.True(t, m.IsEnabled(), "a helper is a credential, so auth must be enabled")

	tok, err := m.Token(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	require.Equal(t, int32(1), helper.calls.Load(), "the mint must go to the helper")
	require.Zero(t, rig.Calls.Load(), "the mint must not go to the issuer")
}

func TestAuthTokenURL_DoesNotMoveTheTrustedIssuer(t *testing.T) {
	// Given a token that is validly signed and correct in every respect except that it
	// names the helper as its issuer, when the gateway mints, then it must be rejected.
	// Signing with the issuer's own key isolates the issuer check as the only thing
	// that can reject it.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)

	var helper *localMint
	helper = newLocalMint(t, func(peerID string) string {
		now := time.Now()
		claims := jwks_verifier.Claims{GatewayClaims: commonentities.GatewayClaims{
			ScopeVersion: 1,
			Type:         "partner",
			ChainID:      "hoodi",
			CNF:          &commonentities.GatewayConfirmation{PeerID: peerID},
			RegisteredClaims: jwt.RegisteredClaims{
				// The helper's own URL, which is exactly what must not be trusted.
				Issuer:    helper.URL,
				Subject:   "gw-test",
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
				Audience:  jwt.ClaimStrings{jwks_verifier.AudP2P},
			},
		}}
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		tok.Header["kid"] = "test-key"
		signed, signErr := tok.SignedString(rig.PrivateKey)
		require.NoError(t, signErr)
		return signed
	})
	cfg.APIKey = ""
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)

	_, err = m.Token(t.Context())

	require.Error(t, err, "a token issued by the helper must not verify")
	require.ErrorContains(t, err, "issuer", "the issuer check must be what rejects it")
	require.False(t, m.HasValidToken())
}

func TestAuthTokenURL_ShieldsTheSharedSecret(t *testing.T) {
	// Given both credentials configured, when the gateway mints through the helper,
	// then the shared secret must not be forwarded to it.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	helper := newLocalMint(t, func(string) string {
		return rig.MustSignToken(t, rig.PrivateKey, nil)
	})
	cfg.APIKey = "ogw_live_should_not_travel"
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)

	sent := helper.body.Load()
	require.NotNil(t, sent)
	require.NotContains(t, *sent, "api_key", "the shared secret must stay in the gateway")
	require.NotEmpty(t, (*sent)["peer_id"], "the helper needs the peer ID to bind the token")
}

func TestMintPayloadUnchangedWithoutAHelper(t *testing.T) {
	// Given no helper, when the gateway mints, then it must send the shared secret to
	// the issuer exactly as it does today. This is the behavior every existing
	// deployment depends on, so it is asserted positively, not by omission.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)

	require.Equal(t, int32(1), rig.Calls.Load(), "the mint must go to the issuer")
	sent := rig.LastMintPayload.Load()
	require.NotNil(t, sent)
	require.Equal(t, cfg.APIKey, (*sent)["api_key"], "the api key must still be sent")
	require.NotEmpty(t, (*sent)["peer_id"])
}

func TestMintRejectsATokenBoundToAnotherGateway(t *testing.T) {
	// Given a helper that returns a validly issued token for a different peer, when the
	// gateway mints, then it must refuse rather than adopt that gateway's identity for
	// its own labels and metrics.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	const otherPeer = "16Uiu2HAmSomeOtherGatewayEntirely"
	helper := newLocalMint(t, func(string) string {
		return rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
			c.CNF = &commonentities.GatewayConfirmation{PeerID: otherPeer}
		})
	})
	cfg.APIKey = ""
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)

	_, err = m.Token(t.Context())

	require.Error(t, err)
	require.ErrorContains(t, err, "bound to peer")
	require.False(t, m.HasValidToken())
	require.Empty(t, m.OperatorID(), "nothing from the rejected response may be adopted")
}

func TestMintRejectsATokenWithNoPeerBinding(t *testing.T) {
	// Given a validly issued token carrying no cnf at all, when the gateway mints, then
	// it must refuse: an unbound token cannot complete a handshake, and accepting it
	// would let the gateway wear another gateway's sub and operator id in its labels.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	helper := newLocalMint(t, func(string) string {
		return rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
			c.CNF = nil
			c.Subject = "gw-someone-else"
		})
	})
	cfg.APIKey = ""
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)

	_, err = m.Token(t.Context())

	require.Error(t, err)
	require.ErrorContains(t, err, "bound to peer")
	require.False(t, m.HasValidToken())
	require.Empty(t, m.OperatorID())
	require.Nil(t, m.OwnClaims(), "no claims from an unbound token may be adopted")
}

func TestForeignServicesTokenIsNotUsedForPushes(t *testing.T) {
	// Given a correctly bound handshake token and a services token bound to another
	// peer, when the gateway mints, then it must fall back rather than authenticate its
	// centralized pushes and label its metrics as another gateway.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	helper := newLocalMint(t, func(peerID string) string {
		return rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
			c.CNF = &commonentities.GatewayConfirmation{PeerID: peerID}
		})
	})
	// Replace the response so the services token names a different peer.
	helper.servicesToken = rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudServices}
		c.CNF = &commonentities.GatewayConfirmation{PeerID: "16Uiu2HAmSomeOtherGateway"}
		c.Label = "not-ours"
	})
	cfg.APIKey = ""
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	tok, err := m.Token(t.Context())
	require.NoError(t, err, "the handshake token is fine, so the mint must succeed")

	svc, err := m.ServicesToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, tok, svc, "pushes must fall back to the handshake token")
	require.Nil(t, m.GatewayLabels(), "a foreign services token must not supply our labels")
}

func TestMintAcceptsATokenBoundToThisGateway(t *testing.T) {
	// Given a helper returning a token bound to this gateway's own peer, when it mints,
	// then it must be accepted: the binding check must not reject the real case.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	helper := newLocalMint(t, func(peerID string) string {
		return rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
			c.CNF = &commonentities.GatewayConfirmation{PeerID: peerID}
		})
	})
	cfg.APIKey = ""
	cfg.AuthTokenURL = helper.URL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)

	_, err = m.Token(t.Context())

	require.NoError(t, err)
	require.True(t, m.HasValidToken())
	require.Equal(t, helperOperatorID, m.OperatorID(),
		"the whole response body, not just the token, comes from the helper")
}

func TestHelperNotListeningIsDiagnosable(t *testing.T) {
	// Given a configured helper that is not there, when the gateway mints, then the
	// failure must name the address it tried, since this is the headline failure mode.
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	dead := newLocalMint(t, func(string) string { return "" })
	deadURL := dead.URL
	dead.Close()
	cfg.APIKey = ""
	cfg.AuthTokenURL = deadURL

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)

	_, err = m.Token(t.Context())

	require.Error(t, err)
	require.ErrorContains(t, err, deadURL, "the error must name the endpoint it tried")
	require.False(t, m.HasValidToken())
}

func TestIsEnabledAcrossCredentialCombinations(t *testing.T) {
	// Given every credential combination, when a Manager is built, then IsEnabled must
	// keep its existing meaning for configurations that exist today.
	rig := test_utils.NewAuthTestRig(t)
	cases := []struct {
		name string
		mod  func(*config.AppConfig, string)
		want bool
	}{
		{"api key only, as today", func(c *config.AppConfig, _ string) {
			c.APIKey, c.AuthTokenURL = "test-key", ""
		}, true},
		{"helper only", func(c *config.AppConfig, url string) {
			c.APIKey, c.AuthTokenURL = "", url
		}, true},
		{"both", func(c *config.AppConfig, url string) {
			c.APIKey, c.AuthTokenURL = "test-key", url
		}, true},
		{"neither", func(c *config.AppConfig, _ string) {
			c.APIKey, c.AuthTokenURL = "", ""
		}, false},
		{"auth disabled outranks a helper", func(c *config.AppConfig, url string) {
			c.APIKey, c.AuthTokenURL, c.EnableAuth = "", url, false
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			helper := newLocalMint(t, func(string) string {
				return rig.MustSignToken(t, rig.PrivateKey, nil)
			})
			cfg := rig.AppCfg(t)
			tc.mod(cfg, helper.URL)

			m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)

			require.NoError(t, err)
			require.NotNil(t, m)
			require.Equal(t, tc.want, m.IsEnabled())
		})
	}
}
