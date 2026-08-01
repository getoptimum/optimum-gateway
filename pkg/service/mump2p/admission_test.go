package mump2p_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// asNode reaches the admission surface, which the Engine interface deliberately
// does not carry: it is the node's own security state, not a pub/sub operation.
func asNode(t *testing.T, e mump2p.Engine) *mump2p.Node {
	t.Helper()

	n, ok := e.(*mump2p.Node)
	require.True(t, ok, "expected a *mump2p.Node")

	return n
}

// TestNodeDeniesAdmissionBeforeHandshake proves the mesh is default-deny: a peer
// the node has never handshaken with is not admitted, so it is never staged.
func TestNodeDeniesAdmissionBeforeHandshake(t *testing.T) {
	cnt := test_utils.GetClean(t)

	node := asNode(t, test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_admission_default", t.TempDir()))
	stranger := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_admission_default", t.TempDir())

	require.False(t, node.HandshakeVerified(stranger.GetHostInfo().ID))
}

// TestNodeAdmitsPeerOnVerifiedHandshake proves the admit-and-restage half: a peer
// skipped while unauthorized is re-queued once its handshake verifies, which is
// the only thing that gets it into a mesh.
func TestNodeAdmitsPeerOnVerifiedHandshake(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_admission_verified"
	const topic = "beacon_attestation_11"

	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())

	test_utils.ConnectNodes(cnt.Ctx, t, nodeA, nodeB)
	require.NoError(t, nodeA.SubscribeTopic(topic))
	require.NoError(t, nodeB.SubscribeTopic(topic))

	require.Eventually(t, func() bool {
		return asNode(t, nodeA).HandshakeVerified(nodeB.GetHostInfo().ID) &&
			asNode(t, nodeB).HandshakeVerified(nodeA.GetHostInfo().ID)
	}, 10*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		return len(nodeA.GetMeshPeers(topic)) == 1 && len(nodeB.GetMeshPeers(topic)) == 1
	}, 10*time.Second, 100*time.Millisecond)
}

// TestNodeClearsAdmissionOnDisconnect proves the allow set is connection scoped:
// an entry that outlived the connection would admit a peer on a later connection
// that never authenticated.
func TestNodeClearsAdmissionOnDisconnect(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_admission_scope"

	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())

	test_utils.ConnectNodes(cnt.Ctx, t, nodeA, nodeB)

	require.Eventually(t, func() bool {
		return asNode(t, nodeA).HandshakeVerified(nodeB.GetHostInfo().ID)
	}, 10*time.Second, 50*time.Millisecond)

	require.NoError(t, nodeB.GetHost().Network().ClosePeer(nodeA.GetHostInfo().ID))

	require.Eventually(t, func() bool {
		return !asNode(t, nodeA).HandshakeVerified(nodeB.GetHostInfo().ID)
	}, 10*time.Second, 50*time.Millisecond)
}
