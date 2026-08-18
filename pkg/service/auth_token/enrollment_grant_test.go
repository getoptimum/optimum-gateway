package auth_token_test

import (
	"errors"
	"testing"

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
	payload := rig.LastMintPayload
	require.NotEmpty(t, payload["client_assertion"])
	require.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", payload["client_assertion_type"])
	require.Empty(t, payload["api_key"], "no shared secret may cross the wire after enrollment")
	require.Equal(t, rig.DefaultPeerID, payload["peer_id"])

	// The assertion must verify against the key we registered, addressed to the
	// token endpoint and issued as the client_id.
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(payload["client_assertion"], claims,
		func(*jwt.Token) (any, error) { return test_utils.PublicKeyFromJWK(t, rig.EnrolledJWK), nil },
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

// The assertion must be built per mint, not once at construction: optimum-auth
// caps exp - iat at 120s, so a payload fixed at boot would mint successfully once
// and then fail every refresh three hours later. Two managers sharing one
// credential mint separately, and their assertions must differ.
// The audience must come from the normalized issuer, not from RemoteAuthURL as
// configured. A trailing slash is the case that separates them: optimum-auth builds
// its expected audience from SIGNER_ISSUER, so a doubled slash fails verification.
// Without this, deriving aud either way looks identical and the property is untested.
func TestEnrollmentGrant_AudienceIsNormalized(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := joinKeyCfg(t, rig, t.TempDir())
	cfg.RemoteAuthURL = rig.ServerURL() + "/"

	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(rig.LastMintPayload["client_assertion"], claims,
		func(*jwt.Token) (any, error) { return test_utils.PublicKeyFromJWK(t, rig.EnrolledJWK), nil })
	require.NoError(t, err)
	require.Equal(t, rig.ServerURL()+enrollment.TokenPath, claims["aud"],
		"a trailing slash on remote_auth_url must not produce a doubled slash in aud")
}

func TestEnrollmentGrant_SignsAFreshAssertionPerMint(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	dir := t.TempDir()

	first, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, dir))
	require.NoError(t, err)
	_, err = first.Token(t.Context())
	require.NoError(t, err)
	firstAssertion := rig.LastMintPayload["client_assertion"]
	require.NotEmpty(t, firstAssertion)

	second, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, dir))
	require.NoError(t, err)
	_, err = second.Token(t.Context())
	require.NoError(t, err)

	require.NotEqual(t, firstAssertion, rig.LastMintPayload["client_assertion"],
		"each mint must carry a freshly signed assertion")

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(rig.LastMintPayload["client_assertion"], claims,
		func(*jwt.Token) (any, error) { return test_utils.PublicKeyFromJWK(t, rig.EnrolledJWK), nil })
	require.NoError(t, err)
	iat, exp := int64(claims["iat"].(float64)), int64(claims["exp"].(float64))
	require.LessOrEqual(t, exp-iat, int64(120), "optimum-auth rejects a longer-lived assertion")
}

// The enrollment label is unique per org among live credentials, so sending the
// placeholder gateway_id from every unconfigured node would fail the second
// gateway's enrollment with a constraint violation the endpoint reports as an
// opaque "invalid join key".
func TestEnrollmentGrant_OmitsThePlaceholderLabel(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := joinKeyCfg(t, rig, t.TempDir())
	cfg.GatewayID = config.DefaultGatewayID

	_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.Empty(t, rig.EnrolledLabel, "an unconfigured node must not claim a shared label")
}

func TestEnrollmentGrant_SendsAConfiguredLabel(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	cfg := joinKeyCfg(t, rig, t.TempDir())
	cfg.GatewayID = "optimum-dev-hoodi-spot-us-central-hermes-2"

	_, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
	require.NoError(t, err)
	require.Equal(t, "optimum-dev-hoodi-spot-us-central-hermes-2", rig.EnrolledLabel)
}

func TestEnrollmentGrant_ReusesCredentialAcrossRestarts(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	dir := t.TempDir()

	first, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, dir))
	require.NoError(t, err)

	second, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, dir))
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

	require.Equal(t, "test-key", rig.LastMintPayload["api_key"])
	require.Empty(t, rig.LastMintPayload["client_assertion"])
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

// A 401 means different things on the two grants. For a shared secret it is a dead
// key. For an assertion it is every verification failure, including one that
// expired in flight because the host clock drifted, so retiring the node on it
// would turn a two-minute NTP slip into a gateway that serves a stale token until
// expiry and then fails every handshake until someone restarts it.
func TestMintErrorIsTerminal(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)

	legacy, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	enrolled, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), joinKeyCfg(t, rig, t.TempDir()))
	require.NoError(t, err)

	for _, err := range []error{auth_token.ErrUnknownKey, auth_token.ErrKeyRevoked, auth_token.ErrKeySuspended} {
		require.True(t, legacy.MintErrorIsTerminal(err), "%v must retire the legacy api_key path", err)
		require.False(t, enrolled.MintErrorIsTerminal(err), "%v must not retire an enrolled gateway", err)
	}
	require.False(t, legacy.MintErrorIsTerminal(errors.New("connection reset")))
	require.False(t, enrolled.MintErrorIsTerminal(errors.New("connection reset")))
}
