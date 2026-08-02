package routes_test

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/routes"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	gateway "github.com/getoptimum/optimum-gateway/pkg/service/gossipsub-gateway"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestAppRouter(t *testing.T) {
	// given
	cnt := test_utils.GetClean(t)
	test_utils.SpawnLocalDeps(t)
	cfg := test_utils.GetTestConfig(t)

	srvAuth, err := auth_token.New(cnt.Ctx, cnt.Log, cfg)
	require.NoError(t, err)
	srvAuth.Start(cnt.Ctx)
	_, err = srvAuth.Token(t.Context())
	require.NoError(t, err)
	require.NoError(t, cfg.InitRuntime(cnt.Ctx, cnt.Log, srvAuth.Chain().String(), cfg.GatewayID, cfg.GatewayType, cfg.OrgID))

	srvMessageRouter, err := message_router.NewService(cnt.Ctx, cfg, cnt.Log, srvAuth)
	require.NoError(t, err)
	srvGateway, err := gateway.NewService(
		cnt.Ctx,
		cnt.Log,
		cfg,
		srvMessageRouter,
		srvAuth,
		gateway.WithMumP2PNodeOptions(test_utils.TestNodeOptions()...),
	)
	require.NoError(t, err)
	require.NoError(t, srvGateway.Run())

	port := test_utils.GetFreePortT(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	appRouter := routes.NewAppRouter(cnt.Log, srvGateway, cfg, addr)
	t.Cleanup(func() {
		require.NoError(t, appRouter.Stop())
	})
	go func() {
		require.NoError(t, appRouter.Run(cnt.Ctx))
	}()

	// when
	healthURL := fmt.Sprintf("http://%s/health", addr)
	require.Eventually(t, func() bool {
		_, code, errR := net.GetCurl[any](cnt.Ctx, healthURL, nil)
		return errR == nil && code == http.StatusServiceUnavailable
	}, 10*time.Second, 500*time.Millisecond)

	// then
	type testResp struct {
		PeerID           string         `json:"peer_id"`
		CLHealth         int64          `json:"cl_health"`
		MumP2PHealth     int64          `json:"mump2p_health"`
		Version          string         `json:"version"`
		CommitHash       string         `json:"commit_hash"`
		GatewayID        string         `json:"gateway_id"`
		GatewayClusterID string         `json:"gateway_cluster_id"`
		RLNCConfig       map[string]any `json:"rlnc_config"`
	}
	res, code, errR := net.GetCurl[testResp](cnt.Ctx, fmt.Sprintf("http://%s/api/v1/self_info", addr), nil)
	require.NoError(t, errR)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, int64(0), res.CLHealth)
	require.Equal(t, int64(0), res.MumP2PHealth)
	require.Equal(t, cfg.GatewayID, res.GatewayID)
	require.Equal(t, cfg.GatewayClusterID, res.GatewayClusterID)
	id, err := identity.ExtractIdentityFromDir(cfg.IdentityLibP2PDir)
	require.NoError(t, err)
	require.Equal(t, id.ID.String(), res.PeerID)

	// The benchmark harness hashes rlnc_config whole to decide whether the fleet
	// agrees on its configuration, so the key set is part of the contract.
	require.Equal(t, []string{
		"forward_shard_threshold",
		"max_shard_size",
		"publisher_shard_multiplier",
		"random_message_size_bytes",
		"rlnc_shard_factor",
	}, slices.Sorted(maps.Keys(res.RLNCConfig)))

	served := cfg.GetDCRotator().Get()
	require.InDelta(t, float64(served.RandomMessageSize), res.RLNCConfig["random_message_size_bytes"], 0)
	require.InDelta(t, float64(served.ShardFactor), res.RLNCConfig["rlnc_shard_factor"], 0)
	require.InDelta(t, float64(served.PublisherShardMultiplier), res.RLNCConfig["publisher_shard_multiplier"], 1e-6)
	require.InDelta(t, float64(served.ForwardShardThreshold), res.RLNCConfig["forward_shard_threshold"], 1e-6)

	// 64 is the protocol default and nothing in the gateway sets MaxShardSize, so
	// this pins the value a run codes at until someone deliberately changes it.
	require.InDelta(t, 64, res.RLNCConfig["max_shard_size"], 0)
}
