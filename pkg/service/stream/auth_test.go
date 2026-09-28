package stream_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/stream"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestJWKSAuthenticator_AcceptsStreamToken(t *testing.T) {
	m, rig := newAuthManager(t)
	_, err := m.Token(t.Context())
	require.NoError(t, err)
	auth := stream.NewConsumerAuthenticator(m, true)

	tok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudStream}
		c.Subject = "as_stream-key-1"
		c.ChainID = "mainnet" // stream auth has no chain gate (ADR-0011)
		c.OperatorID = m.OperatorID()
	})

	sub, err := auth.Authenticate(tok)
	require.NoError(t, err)
	require.Equal(t, "as_stream-key-1", sub)
}

func TestJWKSAuthenticator_RejectsNilAuthManager(t *testing.T) {
	auth := stream.NewConsumerAuthenticator(nil, true)

	_, err := auth.Authenticate("any-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "verifier unavailable")
}

func TestJWKSAuthenticator_RejectsWrongAudience(t *testing.T) {
	m, rig := newAuthManager(t)
	auth := stream.NewConsumerAuthenticator(m, true)

	tok := rig.MustSignToken(t, rig.PrivateKey, nil) // aud=p2p

	_, err := auth.Authenticate(tok)
	require.Error(t, err)
}

func TestJWKSAuthenticator_RejectsMissingOperator(t *testing.T) {
	m, rig := newAuthManager(t)
	_, err := m.Token(t.Context())
	require.NoError(t, err)
	auth := stream.NewConsumerAuthenticator(m, true)

	tok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudStream}
		c.OperatorID = ""
	})

	_, err = auth.Authenticate(tok)
	require.Error(t, err)
	require.Contains(t, err.Error(), "operator mismatch")
}

func TestJWKSAuthenticator_RejectsDifferentOperator(t *testing.T) {
	m, rig := newAuthManager(t)
	_, err := m.Token(t.Context())
	require.NoError(t, err)
	auth := stream.NewConsumerAuthenticator(m, true)

	tok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudStream}
		c.OperatorID = "op-other"
	})

	_, err = auth.Authenticate(tok)
	require.Error(t, err)
	require.Contains(t, err.Error(), "operator mismatch")
}

func TestJWKSAuthenticator_AcceptsStreamTokenWhenGatewayOperatorUnknown(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	rig.OperatorID = ""
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)
	require.Empty(t, m.OperatorID())
	auth := stream.NewConsumerAuthenticator(m, true)

	tok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudStream}
	})

	sub, err := auth.Authenticate(tok)
	require.NoError(t, err)
	require.Equal(t, "gw-test", sub)
}

func TestJWKSAuthenticator_RejectsExpiredToken(t *testing.T) {
	m, rig := newAuthManager(t)
	auth := stream.NewConsumerAuthenticator(m, true)

	tok := rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudStream}
		c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	})

	_, err := auth.Authenticate(tok)
	require.Error(t, err)
}

func TestAllowAllAuthenticator_ReturnsDevSubject(t *testing.T) {
	auth := stream.NewConsumerAuthenticator(nil, false)

	sub, err := auth.Authenticate("")
	require.NoError(t, err)
	require.Equal(t, "dev-local", sub)
}

// newAuthManager builds an enabled auth manager; VerifyStreamToken only needs
// the JWKS verifier, so no mint round-trip is required.
func newAuthManager(t *testing.T) (*auth_token.Service, *test_utils.AuthTestRig) {
	t.Helper()
	rig := test_utils.NewAuthTestRig(t)
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	return m, rig
}
