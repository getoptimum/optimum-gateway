package mump2p_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commontest "github.com/getoptimum/optimum-common/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p/udpsession"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func speaksSessionProtocol(node mump2p.Engine) bool {
	return slices.Contains(node.GetHost().Mux().Protocols(), udpsession.ProtocolID)
}

// TestDatagramDisabledByDefault proves the feature flag is off and total: with
// no datagram section the node binds no UDP socket, answers no session protocol,
// and hands out no keys.
func TestDatagramDisabledByDefault(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_datagram_off"

	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())

	require.False(t, speaksSessionProtocol(nodeA))

	test_utils.ConnectNodes(cnt.Ctx, t, nodeA, nodeB)

	require.Eventually(t, func() bool {
		return asNode(t, nodeA).HandshakeVerified(nodeB.GetHostInfo().ID)
	}, 10*time.Second, 50*time.Millisecond)

	// A verified handshake is necessary for a session but never sufficient: with
	// the flag off there is nothing to establish into.
	_, ok := asNode(t, nodeA).DatagramSessionExpiry(nodeB.GetHostInfo().ID)
	require.False(t, ok)
}

// datagramNode builds a node with the datagram data plane on, bound to an
// ephemeral loopback port.
func datagramNode(t *testing.T, cnt *test_utils.Container, clusterID string) mump2p.Engine {
	t.Helper()

	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, commontest.GetFreePortT(t), nil)
	cfg.DatagramEnable = true
	cfg.DatagramListenAddr = "127.0.0.1:0"

	return test_utils.NewTestNodeWithCfg(cnt.Ctx, t, cnt.Log, t.TempDir(), cfg)
}

// TestDatagramSessionFollowsTheHandshake proves the wiring end to end: with the
// flag on, a verified handshake is what produces a session, and losing the
// connection is what destroys it. Establishment is never skipped, so a peer
// cannot carry a key across a reconnect.
func TestDatagramSessionFollowsTheHandshake(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_datagram_on"

	nodeA := datagramNode(t, cnt, clusterID)
	nodeB := datagramNode(t, cnt, clusterID)

	require.True(t, speaksSessionProtocol(nodeA))
	require.True(t, speaksSessionProtocol(nodeB))

	test_utils.ConnectNodes(cnt.Ctx, t, nodeA, nodeB)

	require.Eventually(t, func() bool {
		_, okA := asNode(t, nodeA).DatagramSessionExpiry(nodeB.GetHostInfo().ID)
		_, okB := asNode(t, nodeB).DatagramSessionExpiry(nodeA.GetHostInfo().ID)

		return okA && okB
	}, 15*time.Second, 50*time.Millisecond, "both sides must key a session off the handshake")

	// The default handshake carries no credential, so the default lifetime is the
	// only ceiling.
	expiry, ok := asNode(t, nodeA).DatagramSessionExpiry(nodeB.GetHostInfo().ID)
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(udpsession.DefaultMaxLifetime), expiry, time.Minute)

	require.NoError(t, nodeB.GetHost().Network().ClosePeer(nodeA.GetHostInfo().ID))

	require.Eventually(t, func() bool {
		_, held := asNode(t, nodeA).DatagramSessionExpiry(nodeB.GetHostInfo().ID)

		return !held
	}, 10*time.Second, 50*time.Millisecond, "a session must not survive its connection")
}
