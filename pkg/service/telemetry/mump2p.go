package telemetry

import (
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
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
	mpPeersPerProtocol = commonmetrics.NewGaugeVec("peers_per_protocol", "mump2p", "Number of mumP2P peers per protocol", []string{labelProtocol})

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

	meshMu sync.RWMutex
	mesh   map[string]map[peer.ID]struct{}
}

func (g *MumP2PCollector) OnNewOutboundStream(_ peer.ID, _ protocol.ID) {}

func (g *MumP2PCollector) OnClosedOutboundStream(id peer.ID) {
	g.removeMeshPeer(id)
}

func NewMumP2PCollector() *MumP2PCollector {
	return &MumP2PCollector{
		peers: syncx.NewRWMap[peer.ID, protocol.ID](),
		mesh:  make(map[string]map[peer.ID]struct{}),
	}
}

func (g *MumP2PCollector) AddPeer(id peer.ID, protoID protocol.ID) {
	g.peers.Store(id, protoID)
	if MetricsEnabled() {
		mpPeersPerProtocol.WithLabelValues(string(protoID)).Inc()
		mpTotalPeers.WithLabelValues().Inc()
	}
}

func (g *MumP2PCollector) RemovePeer(id peer.ID) {
	g.removeMeshPeer(id)
	protoID, ok := g.peers.Load(id)
	if !ok {
		return
	}
	g.peers.Delete(id)
	if MetricsEnabled() {
		mpPeersPerProtocol.WithLabelValues(string(protoID)).Dec()
		mpTotalPeers.WithLabelValues().Dec()
	}
}

func (g *MumP2PCollector) Join(string) {}
func (g *MumP2PCollector) Leave(topic string) {
	g.meshMu.Lock()
	delete(g.mesh, topic)
	g.meshMu.Unlock()
}

func (g *MumP2PCollector) Graft(id peer.ID, topic string) {
	g.meshMu.Lock()
	if g.mesh[topic] == nil {
		g.mesh[topic] = make(map[peer.ID]struct{})
	}
	g.mesh[topic][id] = struct{}{}
	g.meshMu.Unlock()
}

func (g *MumP2PCollector) Prune(id peer.ID, topic string) {
	g.meshMu.Lock()
	if peers := g.mesh[topic]; peers != nil {
		delete(peers, id)
		if len(peers) == 0 {
			delete(g.mesh, topic)
		}
	}
	g.meshMu.Unlock()
}

// MeshPeers returns a snapshot of the peers currently in the topic mesh.
func (g *MumP2PCollector) MeshPeers(topic string) []peer.ID {
	g.meshMu.RLock()
	defer g.meshMu.RUnlock()

	peers := make([]peer.ID, 0, len(g.mesh[topic]))
	for id := range g.mesh[topic] {
		peers = append(peers, id)
	}
	return peers
}

func (g *MumP2PCollector) removeMeshPeer(id peer.ID) {
	g.meshMu.Lock()
	defer g.meshMu.Unlock()

	for topic, peers := range g.mesh {
		delete(peers, id)
		if len(peers) == 0 {
			delete(g.mesh, topic)
		}
	}
}

func (g *MumP2PCollector) ValidateMessage(msg *pubsub.Message) {
	if !MetricsEnabled() || msg.Topic == nil {
		return
	}
	mpReceivedMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	mpReceivedMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

func (g *MumP2PCollector) DeliverMessage(msg *pubsub.Message) {
	if !MetricsEnabled() || msg.Topic == nil {
		return
	}
	mpDeliveredMessagesBytes.WithLabelValues(*msg.Topic).Add(float64(len(msg.Data)))
	mpDeliveredMessagesCount.WithLabelValues(*msg.Topic).Inc()
}

func (g *MumP2PCollector) RejectMessage(msg *pubsub.Message, reason string) {
	if !MetricsEnabled() {
		return
	}
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
	if !MetricsEnabled() || msg.Topic == nil {
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
