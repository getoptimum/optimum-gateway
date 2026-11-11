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
)

var (
	readOnly     = PeerCapability{CanPublish: false}
	fullPublish  = PeerCapability{CanPublish: true}
	testPeerTTL  = 15 * time.Second
	testInterval = 15 * time.Second
)

// newCapabilityNode builds a Node with only the state the admission path touches.
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
		peerCapabilities: syncx.NewRWMap[peer.ID, PeerCapability](),
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

func TestReadmitDoesNotPromoteStreamPeerOnReconnect(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)

	n.markHandshakeValid(peerID, readOnly)

	state, ok := n.getPeerState(peerID)
	require.True(t, ok)
	require.Equal(t, entities.PeerStateHandshakeValid, state)

	for range 3 {
		require.False(t, n.readmitCapability(n.log, peerID).CanPublish)
	}
}

func TestReadmitPreservesPublishCapability(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)

	n.markHandshakeValid(peerID, fullPublish)

	require.True(t, n.readmitCapability(n.log, peerID).CanPublish)
}

func TestReadmitFailsClosedOnCacheMiss(t *testing.T) {
	n := newCapabilityNode(t)

	require.False(t, n.readmitCapability(n.log, newTestPeerID(t)).CanPublish)
}

func TestDisconnectClearsCachedCapability(t *testing.T) {
	n := newCapabilityNode(t)
	peerID := newTestPeerID(t)
	n.markHandshakeValid(peerID, fullPublish)

	n.disconnectPeer(peerID)

	_, ok := n.peerCapabilities.Load(peerID)
	require.False(t, ok)
	_, approved := n.peersApprovedMap.Load(peerID)
	require.False(t, approved)
}
