package mump2p_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestBootstrap(t *testing.T) {
	// given
	cnt := test_utils.GetClean(t)
	clusterID := "über_cluster"
	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	require.NoError(t, nodeA.Start())
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	require.NoError(t, nodeB.Start())
	bootstrapPeers := []string{
		fmt.Sprintf("%s/p2p/%s", nodeA.GetHost().Addrs()[0].String(), nodeA.GetHostInfo().ID.String()),
		fmt.Sprintf("%s/p2p/%s", nodeB.GetHost().Addrs()[0].String(), nodeB.GetHostInfo().ID.String()),
	}

	// when
	cfgC := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, test_utils.GetFreePortT(t), bootstrapPeers)
	cfgD := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, test_utils.GetFreePortT(t), bootstrapPeers)
	cfgE := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, test_utils.GetFreePortT(t), bootstrapPeers)
	nodeC := test_utils.NewTestNodeWithCfg(cnt.Ctx, t, cnt.Log, t.TempDir(), cfgC)
	nodeD := test_utils.NewTestNodeWithCfg(cnt.Ctx, t, cnt.Log, t.TempDir(), cfgD)
	nodeE := test_utils.NewTestNodeWithCfg(cnt.Ctx, t, cnt.Log, t.TempDir(), cfgE)
	require.NoError(t, nodeC.Start())
	require.NoError(t, nodeD.Start())
	require.NoError(t, nodeE.Start())

	// then
	require.Eventually(t, func() bool {
		totalPeersA, _ := nodeA.CountConnectedPeers()
		totalPeersB, _ := nodeB.CountConnectedPeers()
		totalPeersC, _ := nodeC.CountConnectedPeers()
		totalPeersD, _ := nodeD.CountConnectedPeers()
		totalPeersE, _ := nodeE.CountConnectedPeers()
		return totalPeersA == 4 && totalPeersB == 4 && totalPeersC == 4 && totalPeersD == 4 && totalPeersE == 4
	}, 42*time.Second, 1*time.Second)
}
