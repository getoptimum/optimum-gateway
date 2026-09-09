package auth_token_test

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/enrollment"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// joinKeyCfg switches the rig's config from the legacy api_key to a join key.
// The two are mutually exclusive, so api_key must be cleared.
func joinKeyCfg(t *testing.T, rig *test_utils.AuthTestRig, dir string) *config.AppConfig {
	t.Helper()
	cfg := rig.AppCfg(t)
	cfg.APIKey = ""
	cfg.JoinKey = "ojk_test_secret"
	cfg.EnrollCredDir = dir
	require.NoError(t, cfg.Validate())
	return cfg
}

// restartOf models the same node booting again: a fresh config that keeps the
// node's mumP2P identity, which the enrolled credential is bound to.
func restartOf(t *testing.T, rig *test_utils.AuthTestRig, prev *config.AppConfig) *config.AppConfig {
	t.Helper()
	cfg := joinKeyCfg(t, rig, prev.EnrollCredDir)
	cfg.IdentityMumP2PDir = prev.IdentityMumP2PDir
	return cfg
}

func TestEnrollmentGrant_EnrollsThenMintsWithClientAssertion(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	dir := t.TempDir()

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, dir))
	require.NoError(t, err)
	require.True(t, m.IsEnabled())
	require.Equal(t, "ag_test", m.ClientID(), "the enrolled client_id is what we mint as")
	require.EqualValues(t, 1, rig.EnrollCalls.Load())

	tok, err := m.Token(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	// The mint request must use the RFC 7523 grant, not the legacy secret.
	payload := rig.MintPayload()
	require.NotEmpty(t, payload["client_assertion"])
	require.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", payload["client_assertion_type"])
	require.Empty(t, payload["api_key"], "no shared secret may cross the wire after enrollment")
	require.Equal(t, rig.DefaultPeerID, payload["peer_id"])

	// The assertion must verify against the key we registered, addressed to the
	// token endpoint and issued as the client_id.
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(payload["client_assertion"], claims,
		func(*jwt.Token) (any, error) { return test_utils.PublicKeyFromJWK(t, rig.EnrolledKey()), nil },
		jwt.WithValidMethods([]string{"ES256"}),
	)
	require.NoError(t, err, "the mint assertion must verify against the enrolled public key")
	require.Equal(t, "ag_test", claims["sub"])
	require.Equal(t, "ag_test", claims["iss"])
	require.Equal(t, rig.ServerURL()+enrollment.TokenPath, claims["aud"],
		"aud is derived from the auth issuer, not the mint URL")
	require.Equal(t, rig.DefaultPeerID, claims["peer_id"],
		"peer_id inside the signature is what binds the token to this node")
}

// A trailing slash separates the two derivations; without it, taking aud from
// RemoteAuthURL or from the normalized issuer looks identical.
func TestEnrollmentGrant_AudienceIsNormalized(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := joinKeyCfg(t, rig, t.TempDir())
	cfg.RemoteAuthURL = rig.ServerURL() + "/"

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(rig.MintPayload()["client_assertion"], claims,
		func(*jwt.Token) (any, error) { return test_utils.PublicKeyFromJWK(t, rig.EnrolledKey()), nil })
	require.NoError(t, err)
	require.Equal(t, rig.ServerURL()+enrollment.TokenPath, claims["aud"],
		"a trailing slash on remote_auth_url must not produce a doubled slash in aud")
}

// A payload fixed at construction would mint once and fail every refresh after,
// since an assertion may live at most 120s.
func TestEnrollmentGrant_SignsAFreshAssertionPerMint(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	dir := t.TempDir()

	// Two mints on ONE manager. Comparing two boots would pass even with the
	// payload fixed at construction, which is the regression this guards.
	cfg := joinKeyCfg(t, rig, dir)
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.NoError(t, m.MintForTest(t.Context()))
	firstAssertion := rig.MintPayload()["client_assertion"]
	require.NotEmpty(t, firstAssertion)

	require.NoError(t, m.MintForTest(t.Context()))

	require.NotEqual(t, firstAssertion, rig.MintPayload()["client_assertion"],
		"each mint must carry a freshly signed assertion")

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(rig.MintPayload()["client_assertion"], claims,
		func(*jwt.Token) (any, error) { return test_utils.PublicKeyFromJWK(t, rig.EnrolledKey()), nil })
	require.NoError(t, err)
	iat, exp := int64(claims["iat"].(float64)), int64(claims["exp"].(float64))
	require.LessOrEqual(t, exp-iat, int64(120), "optimum-auth rejects a longer-lived assertion")
}

// A shared placeholder label would fail the second gateway's enrollment on the
// per-org unique index, now reported as a 409 label_conflict.
func TestEnrollmentGrant_OmitsThePlaceholderLabel(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := joinKeyCfg(t, rig, t.TempDir())
	cfg.GatewayID = config.DefaultGatewayID

	_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.Empty(t, rig.EnrolledLabelValue(), "an unconfigured node must not claim a shared label")
}

func TestEnrollmentGrant_SendsAConfiguredLabel(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := joinKeyCfg(t, rig, t.TempDir())
	cfg.GatewayID = "optimum-dev-hoodi-spot-us-central-hermes-2"

	_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.Equal(t, "optimum-dev-hoodi-spot-us-central-hermes-2", rig.EnrolledLabelValue())
}

func TestEnrollmentGrant_ReusesCredentialAcrossRestarts(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	dir := t.TempDir()

	cfg := joinKeyCfg(t, rig, dir)
	first, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)

	second, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), restartOf(t, rig, cfg))
	require.NoError(t, err)

	require.Equal(t, first.ClientID(), second.ClientID())
	require.EqualValues(t, 1, rig.EnrollCalls.Load(),
		"a restart must reuse the on-disk credential, not burn another join-key use")
}

func TestEnrollmentGrant_RejectedJoinKeyFailsConstruction(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	rig.EnrollStatus = 401
	rig.EnrollBody = []byte(`{"error":"invalid_enrollment"}`)

	_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, t.TempDir()))
	require.Error(t, err, "a rejected join key must fail boot, not degrade to a disabled manager")
	require.ErrorIs(t, err, enrollment.ErrInvalidEnrollment)
}

func TestEnrollmentGrant_LegacyAPIKeyPathUnchanged(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)

	_, err = m.Token(t.Context())
	require.NoError(t, err)

	require.Equal(t, "test-key", rig.MintPayload()["api_key"])
	require.Empty(t, rig.MintPayload()["client_assertion"])
	require.Empty(t, m.ClientID())
	require.EqualValues(t, 0, rig.EnrollCalls.Load(), "the legacy path must never touch the enroll endpoint")
}

func TestConfig_RejectsBothCredentials(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	cfg.JoinKey = "ojk_test_secret" // api_key is already set by the rig
	require.Error(t, cfg.Validate(), "a half-migrated host must fail loudly, not pick one silently")
}

func TestNoCredential_ReturnsDisabledManager(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := rig.AppCfg(t)
	cfg.APIKey = ""
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.False(t, m.IsEnabled())
	require.EqualValues(t, 0, rig.EnrollCalls.Load())
}

func TestMintErrorIsTerminal(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	legacy, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	enrolled, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, t.TempDir()))
	require.NoError(t, err)

	// A 403 names the credential; it is terminal whichever grant asked.
	for _, e := range []error{auth_token.ErrKeyRevoked, auth_token.ErrKeySuspended} {
		require.True(t, legacy.MintErrorIsTerminal(e), "%v is an explicit 403", e)
		require.True(t, enrolled.MintErrorIsTerminal(e),
			"%v is an explicit 403; retrying a revoked credential forever helps nobody", e)
	}

	// A 401 is opaque. On the assertion path it is also an assertion that expired
	// in flight, so a clock slip must not cost a restart.
	require.True(t, legacy.MintErrorIsTerminal(auth_token.ErrUnknownKey))
	require.False(t, enrolled.MintErrorIsTerminal(auth_token.ErrUnknownKey))

	require.False(t, legacy.MintErrorIsTerminal(errors.New("connection reset")))
	require.False(t, enrolled.MintErrorIsTerminal(errors.New("connection reset")))
}

func TestRetryBackoffClimbsToACeiling(t *testing.T) {
	// A failed refresh must retry sooner than the next full interval: another 3h
	// sleep can land after the cached 6h token has expired.
	got := []time.Duration{}
	d := time.Duration(0)
	for range 8 {
		d = auth_token.NextRetryBackoff(d)
		got = append(got, d)
	}
	require.Equal(t, time.Minute, got[0])
	require.Equal(t, 30*time.Minute, got[len(got)-1])
	for _, d := range got {
		require.Less(t, d, 155*time.Minute, "must be well inside the refresh interval")
	}
}
