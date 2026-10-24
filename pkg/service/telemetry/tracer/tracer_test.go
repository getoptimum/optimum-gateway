package tracer_test

import (
	"testing"

	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
)

type mockForkManager struct {
	supportedDigests map[string]bool
}

func (m *mockForkManager) CheckForkSupported(forkDigest string) bool {
	return m.supportedDigests[forkDigest]
}

func TestPeerTopicTracerTracksRPCSubscriptions(t *testing.T) {
	topic := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	peerID := peer.ID("peer-1")

	tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{}, nil)

	tr.Trace(subscriptionEvent(peerID, topic, true))
	tr.Trace(subscriptionEvent(peerID, topic, true))

	require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())

	tr.Trace(subscriptionEvent(peerID, topic, false))

	require.Empty(t, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topic}, tr.GetAndEraseUnsubscribeTopics())
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())

	tr.Trace(subscriptionEvent(peerID, "/eth2/c6ecb76c/beacon_attestation_18/ssz_snappy", false))
	require.Empty(t, tr.GetDiscoveredTopics())
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())
}

func TestPeerTopicTracerIgnoresLocalAndMeshLifecycleEvents(t *testing.T) {
	topic := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	meshTopic := "/eth2/c6ecb76c/beacon_attestation_18/ssz_snappy"
	peerID := peer.ID("peer-1")
	tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{}, nil)

	tr.Trace(&pubsubpb.TraceEvent{
		Type:   pubsubpb.TraceEvent_JOIN.Enum(),
		PeerID: []byte(peerID),
		Join:   &pubsubpb.TraceEvent_Join{Topic: new(topic)},
	})
	tr.Trace(&pubsubpb.TraceEvent{
		Type:  pubsubpb.TraceEvent_GRAFT.Enum(),
		Graft: &pubsubpb.TraceEvent_Graft{PeerID: []byte(peerID), Topic: new(meshTopic)},
	})
	require.Empty(t, tr.GetDiscoveredTopics())

	tr.Trace(subscriptionEvent(peerID, topic, true))
	tr.Trace(&pubsubpb.TraceEvent{
		Type:  pubsubpb.TraceEvent_PRUNE.Enum(),
		Prune: &pubsubpb.TraceEvent_Prune{PeerID: []byte(peerID), Topic: new(topic)},
	})
	tr.Trace(&pubsubpb.TraceEvent{
		Type:   pubsubpb.TraceEvent_LEAVE.Enum(),
		PeerID: []byte(peerID),
		Leave:  &pubsubpb.TraceEvent_Leave{Topic: new(topic)},
	})

	require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())
	tr.Trace(subscriptionEvent(peerID, topic, false))
	require.Empty(t, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topic}, tr.GetAndEraseUnsubscribeTopics())
}

func TestPeerTopicTracerSignalsSupportedTopicDiscovery(t *testing.T) {
	l := logger.NewAppSLogger(logger.Debug)
	t.Run("supported topic signals only on first discovery", func(t *testing.T) {
		futureTopic := "/eth2/ddeeff00/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-2")
		forks := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true, "ddeeff00": true},
		}

		topicDiscovered := make(chan struct{}, 1)
		tr := tracer.NewPeerTopicTracer(l, forks, topicDiscovered)

		tr.Trace(subscriptionEvent(peerID, futureTopic, true))

		require.Equal(t, []string{futureTopic}, tr.GetDiscoveredTopics())
		require.Len(t, topicDiscovered, 1)
		<-topicDiscovered

		secondPeerID := peer.ID("peer-3")
		tr.Trace(subscriptionEvent(secondPeerID, futureTopic, true))
		require.Empty(t, topicDiscovered)

		tr.Trace(subscriptionEvent(peerID, futureTopic, false))
		require.Equal(t, []string{futureTopic}, tr.GetDiscoveredTopics())

		tr.Trace(subscriptionEvent(secondPeerID, futureTopic, false))

		require.Empty(t, tr.GetDiscoveredTopics())
		require.Equal(t, []string{futureTopic}, tr.GetAndEraseUnsubscribeTopics())
	})

	t.Run("unsupported fork digest is not signaled", func(t *testing.T) {
		topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-3")
		forks := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}

		topicDiscovered := make(chan struct{}, 1)
		tr := tracer.NewPeerTopicTracer(l, forks, topicDiscovered)
		tr.Trace(subscriptionEvent(peerID, topic, true))

		require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())
		require.Empty(t, topicDiscovered)
	})
	t.Run("trace rpc event", func(t *testing.T) {
		subscriptionTopic := "/eth2/c6ecb76c/beacon_attestation_9/ssz_snappy"
		messageTopic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-4")

		mockFrkMgr := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}
		tr := tracer.NewPeerTopicTracer(l, mockFrkMgr, nil)

		tr.Trace(&pubsubpb.TraceEvent{
			Type: pubsubpb.TraceEvent_RECV_RPC.Enum(),
			RecvRPC: &pubsubpb.TraceEvent_RecvRPC{
				ReceivedFrom: []byte(peerID),
				Meta: &pubsubpb.TraceEvent_RPCMeta{
					Subscription: []*pubsubpb.TraceEvent_SubMeta{
						{
							Subscribe: new(true),
							Topic:     new(subscriptionTopic),
						},
						nil,
						{
							Subscribe: new(true),
						},
					},
					Messages: []*pubsubpb.TraceEvent_MessageMeta{
						{
							Topic: new(messageTopic),
						},
						nil,
						{},
					},
				},
			},
		})

		require.Equal(t, []string{subscriptionTopic}, tr.GetDiscoveredTopics())

		tr.Trace(&pubsubpb.TraceEvent{
			Type: pubsubpb.TraceEvent_SEND_RPC.Enum(),
			SendRPC: &pubsubpb.TraceEvent_SendRPC{
				SendTo: []byte(peerID),
				Meta: &pubsubpb.TraceEvent_RPCMeta{
					Messages: []*pubsubpb.TraceEvent_MessageMeta{
						{
							Topic: new(messageTopic),
						},
						nil,
						{},
					},
				},
			},
		})

		require.Equal(t, []string{subscriptionTopic}, tr.GetDiscoveredTopics())
	})
}

func TestPeerTopicTracerRemovePeerIfDisconnected(t *testing.T) {
	topicA := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	topicB := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	peerA := peer.ID("peer-a")
	peerB := peer.ID("peer-b")

	tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{
		supportedDigests: map[string]bool{"c6ecb76c": true},
	}, nil)

	tr.Trace(subscriptionEvent(peerA, topicA, true))
	tr.Trace(subscriptionEvent(peerB, topicA, true))
	tr.Trace(subscriptionEvent(peerA, topicB, true))

	tr.RemovePeerIfDisconnected(peerA, func() bool { return true })
	require.Equal(t, []string{topicA}, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topicB}, tr.GetAndEraseUnsubscribeTopics())

	tr.RemovePeerIfDisconnected(peerB, func() bool { return false })
	require.Equal(t, []string{topicA}, tr.GetDiscoveredTopics())
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())

	tr.RemovePeerIfDisconnected(peerB, func() bool { return true })
	require.Empty(t, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topicA}, tr.GetAndEraseUnsubscribeTopics())

	tr.RemovePeerIfDisconnected(peerB, func() bool { return true })
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())

	tr.RemovePeerIfDisconnected(peerB, nil)
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())
}

func TestPeerTopicTracerRechecksQueuedUnsubscribes(t *testing.T) {
	topic := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	peerID := peer.ID("peer-1")

	t.Run("resubscribed topic is skipped", func(t *testing.T) {
		tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{}, nil)

		tr.Trace(subscriptionEvent(peerID, topic, true))
		tr.Trace(subscriptionEvent(peerID, topic, false))
		tr.Trace(subscriptionEvent(peerID, topic, true))

		require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())
		require.Empty(t, tr.GetAndEraseUnsubscribeTopics())
	})

	t.Run("repeated empty transitions are deduplicated", func(t *testing.T) {
		tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{}, nil)

		tr.Trace(subscriptionEvent(peerID, topic, true))
		tr.Trace(subscriptionEvent(peerID, topic, false))
		tr.Trace(subscriptionEvent(peerID, topic, true))
		tr.Trace(subscriptionEvent(peerID, topic, false))

		require.Equal(t, []string{topic}, tr.GetAndEraseUnsubscribeTopics())
	})
}

func subscriptionEvent(peerID peer.ID, topic string, subscribe bool) *pubsubpb.TraceEvent {
	return &pubsubpb.TraceEvent{
		Type: pubsubpb.TraceEvent_RECV_RPC.Enum(),
		RecvRPC: &pubsubpb.TraceEvent_RecvRPC{
			ReceivedFrom: []byte(peerID),
			Meta: &pubsubpb.TraceEvent_RPCMeta{
				Subscription: []*pubsubpb.TraceEvent_SubMeta{{
					Subscribe: new(subscribe),
					Topic:     new(topic),
				}},
			},
		},
	}
}
