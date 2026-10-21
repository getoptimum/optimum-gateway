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
	observedDigests  []string
}

func (m *mockForkManager) CheckForkSupported(forkDigest string) bool {
	return m.supportedDigests[forkDigest]
}

func (m *mockForkManager) SetObservedDigest(forkDigest string) {
	m.observedDigests = append(m.observedDigests, forkDigest)
}

func TestPeerTopicTracerTracksRPCSubscriptions(t *testing.T) {
	topic := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	peerID := peer.ID("peer-1")

	var emptied []string
	tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{}, func(topic string) {
		emptied = append(emptied, topic)
	})

	tr.Trace(subscriptionEvent(peerID, topic, true))
	tr.Trace(subscriptionEvent(peerID, topic, true))

	require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())

	tr.Trace(subscriptionEvent(peerID, topic, false))

	require.Empty(t, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topic}, emptied)
	require.Equal(t, []string{topic}, tr.GetAndEraseUnsubscribeTopics())
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())

	tr.Trace(subscriptionEvent(peerID, "/eth2/c6ecb76c/beacon_attestation_18/ssz_snappy", false))
	require.Empty(t, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topic}, emptied)
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

func TestPeerTopicTracerTracksForkDigestFromSubscriptions(t *testing.T) {
	l := logger.NewAppSLogger(logger.Debug)
	t.Run("supported fork digest is observed", func(t *testing.T) {
		topic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-2")
		forks := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}

		var emptied []string
		tr := tracer.NewPeerTopicTracer(l, forks, func(topic string) {
			emptied = append(emptied, topic)
		})

		tr.Trace(subscriptionEvent(peerID, topic, true))

		require.Equal(t, []string{"c6ecb76c"}, forks.observedDigests)
		require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())

		tr.Trace(subscriptionEvent(peerID, topic, false))

		require.Empty(t, tr.GetDiscoveredTopics())
		require.Equal(t, []string{topic}, emptied)
	})

	t.Run("unsupported fork digest is not observed", func(t *testing.T) {
		topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-3")
		forks := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}

		tr := tracer.NewPeerTopicTracer(l, forks, nil)
		tr.Trace(subscriptionEvent(peerID, topic, true))

		require.Empty(t, forks.observedDigests)
		require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())
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
		require.Equal(t, []string{"c6ecb76c"}, mockFrkMgr.observedDigests)

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

func TestPeerTopicTracerRemovePeer(t *testing.T) {
	topicA := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	topicB := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	peerA := peer.ID("peer-a")
	peerB := peer.ID("peer-b")

	var emptied []string
	tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{
		supportedDigests: map[string]bool{"c6ecb76c": true},
	}, func(topic string) {
		emptied = append(emptied, topic)
	})

	tr.Trace(subscriptionEvent(peerA, topicA, true))
	tr.Trace(subscriptionEvent(peerB, topicA, true))
	tr.Trace(subscriptionEvent(peerA, topicB, true))

	tr.RemovePeer(peerA)
	require.Equal(t, []string{topicA}, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topicB}, emptied)
	require.Equal(t, []string{topicB}, tr.GetAndEraseUnsubscribeTopics())

	tr.RemovePeer(peerB)
	require.Empty(t, tr.GetDiscoveredTopics())
	require.Equal(t, []string{topicB, topicA}, emptied)
	require.Equal(t, []string{topicA}, tr.GetAndEraseUnsubscribeTopics())

	tr.RemovePeer(peerB)
	require.Empty(t, tr.GetAndEraseUnsubscribeTopics())
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
