package mum_p2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	pubsub "github.com/getoptimum/optimum-p2p/optimum-pubsub"
)

var (
	readOnly     = pubsub.PeerCapability{CanPublish: false}
	fullPublish  = pubsub.PeerCapability{CanPublish: true}
	testPeerTTL  = 15 * time.Second
	testInterval = 15 * time.Second
)

// newCapabilityNode builds a Node with only the state the admission path touches. ps is
// left nil on purpose: these tests pin the cached state that drives
// AllowPeerWithCapability rather than spinning up a full PubSub stack.
func newCapabilityNode(t *testing.T) *Node {
	t.Helper()

	h, err := libp2p.New(libp2p.NoListenAddrs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	return &Node{
		log:              logger.NewAppSLogger(logger.Debug),
		host:             h,
		peersMap:         syncx.NewTTLMap[peer.ID, entities.PeerState](testPeerTTL, testInterval),
		peersApprovedMap: syncx.NewRWMap[peer.ID, struct{}](),
		peerCapabilities: syncx.NewRWMap[peer.ID, pubsub.PeerCapability](),
	}
}

func newTestPeerID(t *testing.T) peer.ID {
	t.Helper()

	dir := t.TempDir()
	_, err := identity.EnsureIdentity(dir)
	require.NoError(t, err)
	identityKey, err := identity.ExtractIdentityFromDir(dir)
	require.NoError(t, err)
	return identityKey.ID
}

// A verified handshake caches the capability alongside the approval, so the two always
// describe the same peer.
func TestMarkHandshakeValidCachesCapability(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)

	n.markHandshakeValid(peerID, readOnly)

	got, ok := n.peerCapabilities.Load(peerID)
	require.True(t, ok)
	require.False(t, got.CanPublish)

	_, approved := n.peersApprovedMap.Load(peerID)
	require.True(t, approved)
}

// THE regression this change exists to prevent: a stream peer reconnects, the handshake is
// skipped because its state is still valid, and the re-admission must reuse the cached
// read-only capability instead of silently promoting it to publisher.
func TestReadmitDoesNotPromoteStreamPeerOnReconnect(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)

	n.markHandshakeValid(peerID, readOnly)

	// The reconnect branch is taken only while the peer still reads as handshake-valid.
	state, ok := n.getPeerState(peerID)
	require.True(t, ok)
	require.Equal(t, entities.PeerStateHandshakeValid, state)

	// Repeated reconnects must never accumulate rights.
	for range 3 {
		require.False(t, n.readmitCapability(n.log, peerID).CanPublish)
	}
}

// A publishing peer keeps its rights across the same path.
func TestReadmitPreservesPublishCapability(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)

	n.markHandshakeValid(peerID, fullPublish)

	require.True(t, n.readmitCapability(n.log, peerID).CanPublish)
}

// An unknown peer has no proven capability, so re-admission fails CLOSED.
func TestReadmitFailsClosedOnCacheMiss(t *testing.T) {
	n := newCapabilityNode(t)

	require.False(t, n.readmitCapability(n.log, newTestPeerID(t)).CanPublish)
}

// Disconnecting drops the cached capability with the approval, so a later handshake
// cannot inherit stale rights.
func TestDisconnectClearsCachedCapability(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)
	n.markHandshakeValid(peerID, fullPublish)

	n.disconnectPeer(peerID)

	_, ok := n.peerCapabilities.Load(peerID)
	require.False(t, ok)
	_, approved := n.peersApprovedMap.Load(peerID)
	require.False(t, approved)

	// And the fail-closed default applies from here on.
	require.False(t, n.readmitCapability(n.log, peerID).CanPublish)
}

// The capability cache must have no TTL of its own. peersMap expires after 15s while the
// approval does not; if the capability expired with it, a still-admitted read-only peer
// would be re-admitted as a publisher.
func TestCapabilityCacheOutlivesPeerStateTTL(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)
	n.markHandshakeValid(peerID, readOnly)

	// Simulate TTL expiry of the peer-state map only.
	n.peersMap.Delete(peerID)

	got, ok := n.peerCapabilities.Load(peerID)
	require.True(t, ok)
	require.False(t, got.CanPublish)
}
