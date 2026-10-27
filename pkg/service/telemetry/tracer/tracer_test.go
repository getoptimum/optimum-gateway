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

func TestPeerTopicTracerTraceJoinLeaveAndUnsubscribe(t *testing.T) {
	topic := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"
	peerID := peer.ID("peer-1")

	tr := tracer.NewPeerTopicTracer(logger.NewAppSLogger(logger.Debug), &mockForkManager{})

	tr.Trace(&pubsubpb.TraceEvent{
		Type:   pubsubpb.TraceEvent_JOIN.Enum(),
		PeerID: []byte(peerID),
		Join:   &pubsubpb.TraceEvent_Join{Topic: new(topic)},
	})
	tr.Trace(&pubsubpb.TraceEvent{
		Type:   pubsubpb.TraceEvent_JOIN.Enum(),
		PeerID: []byte(peerID),
		Join:   &pubsubpb.TraceEvent_Join{Topic: new(topic)},
	})

	require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())

	tr.Trace(&pubsubpb.TraceEvent{
		Type:   pubsubpb.TraceEvent_LEAVE.Enum(),
		PeerID: []byte(peerID),
		Leave:  &pubsubpb.TraceEvent_Leave{Topic: new(topic)},
	})

	require.Empty(t, tr.GetDiscoveredTopics())
}

func TestPeerTopicTracerTraceGraftAndPruneUseEventPeerID(t *testing.T) {
	l := logger.NewAppSLogger(logger.Debug)
	t.Run("supported fork digest is discovered", func(t *testing.T) {
		topic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-2")
		forks := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}

		tr := tracer.NewPeerTopicTracer(l, forks)

		tr.Trace(&pubsubpb.TraceEvent{
			Type:  pubsubpb.TraceEvent_GRAFT.Enum(),
			Graft: &pubsubpb.TraceEvent_Graft{PeerID: []byte(peerID), Topic: new(topic)},
		})

		require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())

		tr.Trace(&pubsubpb.TraceEvent{
			Type:  pubsubpb.TraceEvent_PRUNE.Enum(),
			Prune: &pubsubpb.TraceEvent_Prune{PeerID: []byte(peerID), Topic: new(topic)},
		})

		require.Empty(t, tr.GetDiscoveredTopics())
	})

	t.Run("unsupported fork digest is not observed", func(t *testing.T) {
		topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-3")
		forks := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}

		tr := tracer.NewPeerTopicTracer(l, forks)
		tr.Trace(&pubsubpb.TraceEvent{
			Type:  pubsubpb.TraceEvent_GRAFT.Enum(),
			Graft: &pubsubpb.TraceEvent_Graft{PeerID: []byte(peerID), Topic: new(topic)},
		})

		require.Equal(t, []string{topic}, tr.GetDiscoveredTopics())
	})
	t.Run("trace rpc event", func(t *testing.T) {
		subscriptionTopic := "/eth2/c6ecb76c/beacon_attestation_9/ssz_snappy"
		messageTopic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
		peerID := peer.ID("peer-4")

		mockFrkMgr := &mockForkManager{
			supportedDigests: map[string]bool{"c6ecb76c": true},
		}
		tr := tracer.NewPeerTopicTracer(l, mockFrkMgr)

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
