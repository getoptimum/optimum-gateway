package gossipsub_gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// prepare seeds the stub before any service polls it.
func newGateway(t *testing.T, prepare ...func(*test_utils.LocalBootstrapServer)) (*Service, *test_utils.LocalBootstrapServer) {
	t.Helper()
	return newGatewayOfType(t, commonentities.GatewayTypePartner, prepare...)
}

func newGatewayOfType(t *testing.T, gwType commonentities.GatewayType, prepare ...func(*test_utils.LocalBootstrapServer)) (*Service, *test_utils.LocalBootstrapServer) {
	t.Helper()

	cnt := test_utils.GetClean(t)
	rig := test_utils.NewAuthTestRig(t, test_utils.WithClaimModifier(func(claims *jwks_verifier.Claims) {
		claims.Type = gwType
	}))
	bootstrap := test_utils.NewLocalBootstrapServerWithRig(t, rig)
	bootstrap.SetForkResponse(map[string]any{
		"chain_id":    "hoodi",
		"fork_digest": "deadbeef",
		"future_fork": "DDEEFF00",
	})
	for _, p := range prepare {
		p(bootstrap)
	}
	cfg := rig.AppCfg(t)
	cfg.RemoteBootstrapURL = bootstrap.URL()
	srvAuth, err := auth_token.New(t.Context(), cnt.Log, cfg)
	require.NoError(t, err)
	_, err = srvAuth.Token(t.Context())
	require.NoError(t, err)

	srvMessageRouter, err := message_router.NewService(cnt.Ctx, cfg, cnt.Log, srvAuth)
	require.NoError(t, err)
	srv, err := NewService(cnt.Ctx, cnt.Log, cfg, srvMessageRouter, srvAuth)
	require.NoError(t, err)
	require.Equal(t, "deadbeef", srv.GetForkDigestManager().ActiveDigest())

	return srv, bootstrap
}
