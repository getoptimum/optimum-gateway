package mum_p2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
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

func TestInspectIncomingRPCKeepsSubscriptions(t *testing.T) {
	n := newCapabilityNode(t)
	publisher := newTestPeerID(t)
	reader := newTestPeerID(t)
	stranger := newTestPeerID(t)
	n.markHandshakeValid(publisher, fullPublish)
	n.markHandshakeValid(reader, readOnly)

	for _, tc := range []struct {
		name string
		id   peer.ID
		keep bool
	}{
		{name: "publisher keeps the payload", id: publisher, keep: true},
		{name: "read-only peer loses the payload", id: reader},
		{name: "unapproved peer loses the payload", id: stranger},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := incomingRPC()
			require.NoError(t, n.inspectIncomingRPC(tc.id, rpc))
			require.Len(t, rpc.Subscriptions, 1)
			require.NotNil(t, rpc.Control)
			if tc.keep {
				require.NotNil(t, rpc.Publish)
				require.NotNil(t, rpc.Partial)
				return
			}
			require.Nil(t, rpc.Publish)
			require.Nil(t, rpc.Partial)
		})
	}

	require.NoError(t, n.inspectIncomingRPC(publisher, nil))

	n.peerCapabilities.Delete(publisher)
	rpc := incomingRPC()
	require.NoError(t, n.inspectIncomingRPC(publisher, rpc))
	require.Nil(t, rpc.Publish)
	require.Nil(t, rpc.Partial)
	require.Len(t, rpc.Subscriptions, 1)
}

func incomingRPC() *pubsub.RPC {
	return &pubsub.RPC{
		Subscriptions: []*pb.RPC_SubOpts{{}},
		Publish:       []*pb.Message{{Data: []byte("hello")}},
		Partial:       &pb.PartialMessagesExtension{},
		Control:       &pb.ControlMessage{},
	}
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
