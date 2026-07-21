package gossipsub_gateway_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	gateway "github.com/getoptimum/optimum-gateway/pkg/service/gossipsub-gateway"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// TestGatewayHandshakeIntegration: 3 valid gateways (shared JWKS) mesh (mumPeers == 2);
// 1 unknown-signer gateway (separate rig) stays isolated (mumPeers == 0).
// Proves multi-node mump2p JWT gating only - per-reason rejection is in handshake_test.go.
func TestGatewayHandshakeIntegration(t *testing.T) {
	cnt := test_utils.GetClean(t)

	validRig := test_utils.NewAuthTestRig(t)
	// getGatewayNodeWithRig builds gateways in cluster optimum_hoodi_v0_2; the minted
	// tokens must authorize that cluster for the mesh handshake to pass (#707).
	validRig.SetClusterIDs("optimum_hoodi_v0_2")
	unknownRig := test_utils.NewAuthTestRig(t)

	bootstrap := test_utils.NewLocalBootstrapServerWithRig(t, validRig)
	bootstrap.SetForkResponse(map[string]any{
		"chain_id":    "hoodi",
		"fork_digest": "c6ecb76c",
		"future_fork": "deadbeef",
	})

	const validCount = 3
	validGateways := make([]*gateway.Service, 0, validCount)
	for i := range validCount {
		gw := getGatewayNodeWithRig(cnt.Ctx, t, cnt.Log, fmt.Sprintf("valid%d", i), validRig, bootstrap.URL())
		require.NoError(t, gw.Run())
		validGateways = append(validGateways, gw)
	}
	unknownGateway := getGatewayNodeWithRig(cnt.Ctx, t, cnt.Log, "unknown", unknownRig, bootstrap.URL())
	require.NoError(t, unknownGateway.Run())

	require.Eventually(t, func() bool {
		for _, gw := range validGateways {
			mumPeers, _, _, _ := gw.GetMumP2PPeers()
			if mumPeers != validCount-1 {
				return false
			}
		}
		mumPeers, _, _, _ := unknownGateway.GetMumP2PPeers()
		return mumPeers == 0
	}, 40*time.Second, 1*time.Second)

	// Unknown-signer must stay excluded once the valid mesh is steady.
	require.Never(t, func() bool {
		mumPeers, _, _, _ := unknownGateway.GetMumP2PPeers()
		return mumPeers > 0
	}, 5*time.Second, 1*time.Second)
}

// getGatewayNodeWithRig builds a gateway from an explicit rig + bootstrap (no shared env vars).
func getGatewayNodeWithRig(
	ctx context.Context,
	t *testing.T,
	l logger.AppLogger,
	nodeID string,
	rig *test_utils.AuthTestRig,
	bootstrapURL string,
) *gateway.Service {
	t.Helper()

	rigCfg := rig.AppCfg(t)
	cfg := test_utils.LoadTestConfig(t, &config.AppConfig{
		LogLevel:               string(logger.Debug),
		IdentityLibP2PDir:      t.TempDir(),
		IdentityMumP2PDir:      t.TempDir(),
		AgentLibP2PPort:        test_utils.GetFreePortT(t),
		AgentMumP2PPort:        test_utils.GetFreePortT(t),
		GatewayClusterID:       "optimum_hoodi_v0_2",
		GatewayID:              "local_test_srv_" + nodeID,
		TelemetryPort:          48123,
		TelemetryEnable:        true,
		PropagationEnabledRaw:  true,
		RemoteBootstrapURL:     bootstrapURL,
		JWKSCachePath:          filepath.Join(t.TempDir(), "jwks.json"),
		JWKSRefreshIntervalSec: 3600,
		APIKey:                 rigCfg.APIKey,
		EnableAuth:             true,
		RemoteAuthURL:          rigCfg.RemoteAuthURL,
	})
	return spawnGateway(ctx, t, l, nodeID, cfg, true)
}
