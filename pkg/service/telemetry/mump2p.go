package telemetry

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	pubsub "github.com/getoptimum/optimum-p2p/optimum-pubsub"
)

var (
	mpTotalPeers       *prometheus.GaugeVec
	mpPeersPerProtocol *prometheus.GaugeVec

	// Topic-labeled traffic metrics for mumP2P
	mpDeliveredMessagesBytes *prometheus.CounterVec
	mpReceivedMessagesBytes  *prometheus.CounterVec
	mpDeliveredMessagesCount *prometheus.CounterVec
	mpReceivedMessagesCount  *prometheus.CounterVec
	mpQueueFullCount         *prometheus.CounterVec
	mpThrottledCount         *prometheus.CounterVec
	mpRejectedCount          *prometheus.CounterVec

	// Shard counters
	totalShardsCount      *prometheus.CounterVec
	duplicateShardsCount  *prometheus.CounterVec
	unnecessaryShardCount *prometheus.CounterVec
	unhelpfulShardCount   *prometheus.CounterVec
)

// initMumP2PMetrics uses subsystem "mump2p"
func initMumP2PMetrics() {
	mpTotalPeers = commonmetrics.NewGaugeVec("total_peers", "mump2p", "Total number of mumP2P peers", nil)
	mpPeersPerProtocol = commonmetrics.NewGaugeVec("peers_per_protocol", "mump2p", "Number of mumP2P peers per protocol", []string{"protocol"})

	mpDeliveredMessagesBytes = commonmetrics.NewCounterVec("delivered_messages_bytes", "mump2p", "Delivered payload bytes", []string{labelTopic})
	mpReceivedMessagesBytes = commonmetrics.NewCounterVec("received_messages_bytes", "mump2p", "Received payload bytes", []string{labelTopic})
	mpDeliveredMessagesCount = commonmetrics.NewCounterVec("delivered_messages_count", "mump2p", "Delivered messages", []string{labelTopic})
	mpReceivedMessagesCount = commonmetrics.NewCounterVec("received_messages_count", "mump2p", "Received messages", []string{labelTopic})
	mpQueueFullCount = commonmetrics.NewCounterVec("dropped_queue_full_total", "mump2p", "Dropped messages (queue full)", []string{labelTopic})
	mpThrottledCount = commonmetrics.NewCounterVec("dropped_throttled_total", "mump2p", "Dropped messages (throttled)", []string{labelTopic})
	mpRejectedCount = commonmetrics.NewCounterVec("dropped_rejected_total", "mump2p", "Dropped messages (other)", []string{labelTopic})

	// Shard-level counters (unlabeled)
	totalShardsCount = commonmetrics.NewCounterVec("shards_total", "mump2p", "Total shards processed", nil)
	duplicateShardsCount = commonmetrics.NewCounterVec("shards_duplicate_total", "mump2p", "Duplicate shards", nil)
	unnecessaryShardCount = commonmetrics.NewCounterVec("shards_unnecessary_total", "mump2p", "Unnecessary shards", nil)
	unhelpfulShardCount = commonmetrics.NewCounterVec("shards_unhelpful_total", "mump2p", "Unhelpful shards", nil)
}

// MumP2PCollector implements pubsub.RawTracer for Optimum's mump2p pubsub.
type MumP2PCollector struct {
	peers *syncx.RWMap[peer.ID, protocol.ID]
}

func NewMumP2PCollector() *MumP2PCollector {
	return &MumP2PCollector{
		peers: syncx.NewRWMap[peer.ID, protocol.ID](),
	}
}

func (g *MumP2PCollector) AddPeer(id peer.ID, protoID protocol.ID) {
	g.peers.Store(id, protoID)
	mpPeersPerProtocol.WithLabelValues(string(protoID)).Inc()
	mpTotalPeers.WithLabelValues().Inc()
}

func (g *MumP2PCollector) RemovePeer(id peer.ID) {
	protoID, ok := g.peers.Load(id)
	if !ok {
		return
	}
	g.peers.Delete(id)
	mpPeersPerProtocol.WithLabelValues(string(protoID)).Dec()
	mpTotalPeers.WithLabelValues().Dec()
}

func (g *MumP2PCollector) Join(string)           {}
func (g *MumP2PCollector) Leave(string)          {}
func (g *MumP2PCollector) Graft(peer.ID, string) {}
func (g *MumP2PCollector) Prune(peer.ID, string) {}

func (g *MumP2PCollector) ValidateMessage(msg *pubsub.Message) {
	if msg.Topic == nil {
		return
	}
	mpReceivedMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	mpReceivedMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

func (g *MumP2PCollector) DeliverMessage(msg *pubsub.Message) {
	if msg.Topic == nil {
		return
	}
	mpDeliveredMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	mpDeliveredMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

func (g *MumP2PCollector) RejectMessage(msg *pubsub.Message, reason string) {
	topic := ""
	if msg.Topic != nil {
		topic = *msg.Topic
	}
	switch reason {
	case pubsub.RejectValidationThrottled:
		mpThrottledCount.WithLabelValues(topic).Inc()
	case pubsub.RejectValidationQueueFull:
		mpQueueFullCount.WithLabelValues(topic).Inc()
	default:
		mpRejectedCount.WithLabelValues(topic).Inc()
	}
}

func (g *MumP2PCollector) DuplicateMessage(msg *pubsub.Message) {
	if msg.Topic == nil {
		return
	}
	mpReceivedMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	mpReceivedMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

func (g *MumP2PCollector) ThrottlePeer(peer.ID)                 {}
func (g *MumP2PCollector) RecvRPC(*pubsub.RPC)                  {}
func (g *MumP2PCollector) SendRPC(*pubsub.RPC, peer.ID)         {}
func (g *MumP2PCollector) DropRPC(*pubsub.RPC, peer.ID)         {}
func (g *MumP2PCollector) UndeliverableMessage(*pubsub.Message) {}

// Shard helpers
func AddTotalShardCount() {
	if MetricsEnabled() {
		totalShardsCount.WithLabelValues().Inc()
	}
}

func AddDuplicateShardCount() {
	if MetricsEnabled() {
		duplicateShardsCount.WithLabelValues().Inc()
	}
}

func AddUnnecessaryShardCount() {
	if MetricsEnabled() {
		unnecessaryShardCount.WithLabelValues().Inc()
	}
}

func AddUnhelpfulShardCount() {
	if MetricsEnabled() {
		unhelpfulShardCount.WithLabelValues().Inc()
	}
}
