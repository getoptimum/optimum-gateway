package gossipsub_gateway_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestSetupLibP2PHost_AllowsAllowlistedInboundPeer(t *testing.T) {
	cnt := test_utils.GetClean(t)
	test_utils.SpawnLocalDeps(t)

	clANodeKey, keyADir := test_utils.GenerateIdentity(t)
	clAPort := test_utils.GetFreePortT(t)
	t.Setenv("OPT_DIRECT_CL_PEERS", fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", clAPort, clANodeKey.ID.String()))
	gatewayNode, _ := getGatewayNode(cnt.Ctx, t, cnt.Log, "base_node", true)
	require.NoError(t, gatewayNode.Run())

	clNodeA := test_utils.SpawnLocalCLLibP2PNode(cnt.Ctx, t, gatewayNode.GetHostInfo(), clAPort, keyADir)
	clNodeB := test_utils.SpawnLocalCLLibP2PNode(cnt.Ctx, t, gatewayNode.GetHostInfo(), test_utils.GetFreePortT(t), t.TempDir())
	require.NoError(t, clNodeB.Host.Connect(cnt.Ctx, gatewayNode.GetHostInfo()))
	require.Eventually(t, func() bool {
		totalPeers, _, allPeerIDs, _ := gatewayNode.GetLibP2PPeers()
		if totalPeers < 1 {
			return false
		}
		return totalPeers == 1 && allPeerIDs[0] == clNodeA.Host.ID().String()
	}, 1*time.Minute, 5*time.Second)

	require.Equal(t, 1, len(clNodeA.Host.Network().ConnsToPeer(gatewayNode.GetHostInfo().ID)))
	require.Equal(t, 0, len(clNodeB.Host.Network().Peers()))

	directPeers := gatewayNode.GetDirectLibP2PPeers()
	require.Contains(t, directPeers, clNodeA.Host.ID().String())
	require.NotContains(t, directPeers, clNodeB.Host.ID().String())
}

func TestSetupLibP2PHost_DisallowsNonAllowlistedInboundPeer(t *testing.T) {
	cnt := test_utils.GetClean(t)
	test_utils.SpawnLocalDeps(t)

	clANodeKey, keyADir := test_utils.GenerateIdentity(t)
	clAPort := test_utils.GetFreePortT(t)
	t.Setenv("OPT_DIRECT_CL_PEERS", fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", clAPort, clANodeKey.ID.String()))
	gatewayNode, _ := getGatewayNode(cnt.Ctx, t, cnt.Log, "disallow_node", true)
	require.NoError(t, gatewayNode.Run())

	clNodeA := test_utils.SpawnLocalCLLibP2PNode(cnt.Ctx, t, gatewayNode.GetHostInfo(), clAPort, keyADir)
	clNodeB := test_utils.SpawnLocalCLLibP2PNode(cnt.Ctx, t, gatewayNode.GetHostInfo(), test_utils.GetFreePortT(t), t.TempDir())

	// when: allowlisted peer (clNodeA) connects
	require.NoError(t, clNodeA.Host.Connect(cnt.Ctx, gatewayNode.GetHostInfo()))

	// verify allowlisted peer appears in direct peers
	require.Eventually(t, func() bool {
		directPeers := gatewayNode.GetDirectLibP2PPeers()
		_, exists := directPeers[clNodeA.Host.ID().String()]
		return exists
	}, 30*time.Second, 5*time.Second, "allowlisted peer should appear in direct peers")

	// when: non-allowlisted peer (clNodeB) attempts connection
	require.NoError(t, clNodeB.Host.Connect(cnt.Ctx, gatewayNode.GetHostInfo()))

	// then: verify that clNodeB is disconnected from gateway and NOT in direct peers
	require.Eventually(t, func() bool {
		// Non-allowlisted peer should have no connections to gateway
		return len(clNodeB.Host.Network().ConnsToPeer(gatewayNode.GetHostInfo().ID)) == 0
	}, 1*time.Minute, 5*time.Second)

	directPeers := gatewayNode.GetDirectLibP2PPeers()
	require.NotContains(t, directPeers, clNodeB.Host.ID().String())

	// verify allowlisted peer is still in direct peers
	require.Contains(t, directPeers, clNodeA.Host.ID().String())
}

func TestSetupLibP2PHost_DisconnectionCleansupDirectPeerState(t *testing.T) {
	cnt := test_utils.GetClean(t)
	test_utils.SpawnLocalDeps(t)

	clNodeKey, keyDir := test_utils.GenerateIdentity(t)
	clNodePort := test_utils.GetFreePortT(t)
	t.Setenv("OPT_DIRECT_CL_PEERS", fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", clNodePort, clNodeKey.ID.String()))
	gatewayNode, _ := getGatewayNode(cnt.Ctx, t, cnt.Log, "disconnect_node", true)
	require.NoError(t, gatewayNode.Run())

	// spawn an allowlisted peer
	clNode := test_utils.SpawnLocalCLLibP2PNode(cnt.Ctx, t, gatewayNode.GetHostInfo(), clNodePort, keyDir)
	require.NoError(t, clNode.Host.Connect(cnt.Ctx, gatewayNode.GetHostInfo()))
	require.Eventually(t, func() bool {
		directPeers := gatewayNode.GetDirectLibP2PPeers()
		_, exists := directPeers[clNode.Host.ID().String()]
		return exists
	}, 30*time.Second, 5*time.Second, "peer should appear in direct peers after connection")

	peerIDStr := clNode.Host.ID().String()

	// when: peer disconnects by closing its host
	require.NoError(t, clNode.Host.Close())

	// verify peer is removed from direct peers after disconnection
	require.Eventually(t, func() bool {
		directPeers := gatewayNode.GetDirectLibP2PPeers()
		_, exists := directPeers[peerIDStr]
		return !exists
	}, 30*time.Second, 5*time.Second, "peer should be removed from direct peers after disconnect")

	require.Eventually(t, func() bool {
		_, _, allPeerIDs, _ := gatewayNode.GetLibP2PPeers()
		return !slices.Contains(allPeerIDs, peerIDStr)
	}, 30*time.Second, 5*time.Second, "peer should be removed from network peers after disconnect")
}
