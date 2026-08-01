package telemetry

import (
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func mumMsg(topic string, data []byte) *pubsub.Message {
	m := &pubsub.Message{Message: &pb.Message{Data: data}}
	if topic != "" {
		m.Topic = &topic
	}
	return m
}

func mump2pMetric(name string) string {
	return testMetricsNamespace + "_mump2p_" + name
}

func counterVal(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	return metricByLabels(t, reg, name, labels).GetCounter().GetValue()
}

func gaugeVal(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	return metricByLabels(t, reg, name, labels).GetGauge().GetValue()
}

func TestMumP2PCollector(t *testing.T) {
	reg := initTestMetricsRegistry(t, initMumP2PMetrics)
	c := NewMumP2PCollector()

	const proto = "/optimum/1.0.0"
	const topic = "beacon_block"
	const attTopic = "beacon_attestation_1"
	topicLabels := map[string]string{labelTopic: topic}
	attLabels := map[string]string{labelTopic: attTopic}
	peerA := peer.ID("peer-a")

	c.AddPeer(peerA, proto)
	c.AddPeer(peer.ID("peer-b"), proto)
	require.Equal(t, float64(2), gaugeVal(t, reg, mump2pMetric("total_peers"), map[string]string{}))
	require.Equal(t, float64(2), gaugeVal(t, reg, mump2pMetric("peers_per_protocol"), map[string]string{labelProtocol: proto}))

	c.RemovePeer(peerA)
	c.RemovePeer(peer.ID("unknown")) // no-op; must not underflow gauge
	require.Equal(t, float64(1), gaugeVal(t, reg, mump2pMetric("total_peers"), map[string]string{}))

	c.ValidateMessage(mumMsg(topic, []byte("abcd")))
	c.DuplicateMessage(mumMsg(topic, []byte("ef")))
	c.DeliverMessage(mumMsg(topic, []byte("xyz")))
	for _, fn := range []func(*pubsub.Message){c.ValidateMessage, c.DeliverMessage, c.DuplicateMessage} {
		fn(mumMsg("", []byte("ignored")))
	}
	require.Equal(t, float64(6), counterVal(t, reg, mump2pMetric("received_messages_bytes"), topicLabels))
	require.Equal(t, float64(2), counterVal(t, reg, mump2pMetric("received_messages_count"), topicLabels))
	require.Equal(t, float64(3), counterVal(t, reg, mump2pMetric("delivered_messages_bytes"), topicLabels))
	require.Equal(t, float64(1), counterVal(t, reg, mump2pMetric("delivered_messages_count"), topicLabels))

	rejectMsg := mumMsg(attTopic, nil)
	c.RejectMessage(rejectMsg, pubsub.RejectValidationThrottled)
	c.RejectMessage(rejectMsg, pubsub.RejectValidationQueueFull)
	c.RejectMessage(rejectMsg, "other")
	c.RejectMessage(mumMsg("", nil), "other")
	require.Equal(t, float64(1), counterVal(t, reg, mump2pMetric("dropped_throttled_total"), attLabels))
	require.Equal(t, float64(1), counterVal(t, reg, mump2pMetric("dropped_queue_full_total"), attLabels))
	require.Equal(t, float64(1), counterVal(t, reg, mump2pMetric("dropped_rejected_total"), attLabels))
	require.Equal(t, float64(1), counterVal(t, reg, mump2pMetric("dropped_rejected_total"), map[string]string{labelTopic: ""}))

	require.NotPanics(t, func() {
		c.Join("t")
		c.Leave("t")
		c.Graft(peerA, "t")
		c.Prune(peerA, "t")
		c.ThrottlePeer(peerA)
		c.RecvRPC(nil)
		c.SendRPC(nil, peerA)
		c.DropRPC(nil, peerA)
		c.UndeliverableMessage(nil)
		c.OnNewOutboundStream(peerA, proto)
		c.OnClosedOutboundStream(peerA)
	})

	AddTotalShardCount()
	AddDuplicateShardCount()
	AddUnnecessaryShardCount()
	AddUnhelpfulShardCount()
	for _, name := range []string{
		"shards_total", "shards_duplicate_total", "shards_unnecessary_total", "shards_unhelpful_total",
	} {
		require.Equal(t, float64(1), counterVal(t, reg, mump2pMetric(name), map[string]string{}))
	}
}
