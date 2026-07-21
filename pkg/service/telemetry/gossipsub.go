package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

var (
	gsMumP2PMessagesPerTopic *prometheus.CounterVec
	gsCLMessagesPerTopic     *prometheus.CounterVec

	gsLibP2PGossipsubRPCMessagesPerTopicClass *prometheus.CounterVec

	// aggregationIncludedPerTopic how many messages included in emitted protobuf, by topic
	aggregationIncludedPerTopic *prometheus.CounterVec
	// aggregationIncludedTotal total messages included across all topics
	aggregationIncludedTotal prometheus.Counter

	// hop tracking totals (label-free to avoid cardinality explosion from dynamic peer IDs)
	mumP2PMessagesFromUpstreamTotal prometheus.Counter
	mumP2PMessagesFromOriginTotal   prometheus.Counter
	libP2PMessagesFromUpstreamTotal prometheus.Counter

	// block first-seen race metrics
	blocksFirstSeenMumP2P prometheus.Counter
	blocksFirstSeenLibP2P prometheus.Counter
	blockArrivalMumP2PMs  prometheus.Histogram
	blockArrivalLibP2PMs  prometheus.Histogram

	lastBlockReceivedTimestamp prometheus.Gauge

	knownValidatorsTotal prometheus.Gauge
)

func initGatewayMetrics() {
	gsMumP2PMessagesPerTopic = commonmetrics.NewCounterVec(
		"mump2p_published_messages_per_topic_total",
		subsystem,
		"Number of messages published to mump2p per topic",
		[]string{labelTopic},
	)
	gsCLMessagesPerTopic = commonmetrics.NewCounterVec(
		"cl_published_messages_per_topic_total",
		subsystem,
		"Number of messages published to CL per topic",
		[]string{labelTopic},
	)
	gsLibP2PGossipsubRPCMessagesPerTopicClass = commonmetrics.NewCounterVec(
		"libp2p_gossipsub_rpc_messages_total",
		subsystem,
		"CL libp2p GossipSub RPC messages by topic, peer_id, direction (recv|send).",
		[]string{labelTopic, "peer_id", labelDirection},
	)
	aggregationIncludedTotal = commonmetrics.NewCounter(
		"aggregation_included_total",
		subsystem,
		"Total number of messages included in emitted protobufs",
	)
	aggregationIncludedPerTopic = commonmetrics.NewCounterVec(
		"aggregation_included_per_topic_total",
		subsystem,
		"Number of messages included in emitted protobufs per topic",
		[]string{labelTopic},
	)

	mumP2PMessagesFromUpstreamTotal = commonmetrics.NewCounter(
		"mump2p_messages_from_upstream_total",
		subsystem,
		"Total mump2p messages received from upstream peers",
	)
	mumP2PMessagesFromOriginTotal = commonmetrics.NewCounter(
		"mump2p_messages_from_origin_total",
		subsystem,
		"Total mump2p messages received from origin gateways",
	)
	libP2PMessagesFromUpstreamTotal = commonmetrics.NewCounter(
		"libp2p_messages_from_upstream_total",
		subsystem,
		"Total libp2p beacon blocks received from CL peers",
	)

	// block first-seen race: which transport wins per slot
	blocksFirstSeenMumP2P = commonmetrics.NewCounter(
		"blocks_first_seen_mump2p_total",
		subsystem,
		"Number of beacon blocks where mump2p delivered before libp2p",
	)
	blocksFirstSeenLibP2P = commonmetrics.NewCounter(
		"blocks_first_seen_libp2p_total",
		subsystem,
		"Number of beacon blocks where libp2p delivered before or equal to mump2p",
	)
	blockArrivalMumP2PMs = commonmetrics.NewSimpleHistogram(
		"block_arrival_mump2p_ms",
		subsystem,
		"Beacon block arrival latency via mump2p relative to slot start (ms)",
		[]float64{50, 100, 150, 200, 300, 500, 750, 1000, 2000, 5000},
	)
	blockArrivalLibP2PMs = commonmetrics.NewSimpleHistogram(
		"block_arrival_libp2p_ms",
		subsystem,
		"Beacon block arrival latency via libp2p relative to slot start (ms)",
		[]float64{50, 100, 150, 200, 300, 500, 750, 1000, 2000, 5000},
	)
	lastBlockReceivedTimestamp = commonmetrics.NewGaugeVec(
		"last_block_received_timestamp",
		subsystem,
		"Unix timestamp of last beacon block received from any source",
		nil,
	).WithLabelValues()

	knownValidatorsTotal = commonmetrics.NewGaugeVec(
		"known_validators_total",
		subsystem,
		"Number of validator indices synced from bootstrap",
		nil,
	).WithLabelValues()
}

func MumP2PTotalMessagesInc(topic string) {
	if !enabledMetrics {
		return
	}
	gsMumP2PMessagesPerTopic.WithLabelValues(topic).Inc()
}

func CLTotalMessagesInc(topic string) {
	if !enabledMetrics {
		return
	}
	gsCLMessagesPerTopic.WithLabelValues(topic).Inc()
}

func LibP2PGossipsubRPCMessagesInc(topic, pid, direction string) {
	if !enabledMetrics || topic == "" || pid == "" || direction == "" {
		return
	}
	compressedTopic := utils.SimplifyTopic(topic)
	gsLibP2PGossipsubRPCMessagesPerTopicClass.WithLabelValues(
		compressedTopic,
		utils.ShrinkPeerID(pid),
		direction,
	).Inc()
}

func IncreaseAggregationIncluded(topic string, val int) {
	if enabledMetrics {
		aggregationIncludedTotal.Add(float64(val))
		aggregationIncludedPerTopic.WithLabelValues(topic).Add(float64(val))
	}
}

func IncBlocksFirstSeen(source entities.Source) {
	if !enabledMetrics {
		return
	}
	switch source {
	case entities.SourceMumP2P:
		blocksFirstSeenMumP2P.Inc()
	case entities.SourceLibP2P:
		blocksFirstSeenLibP2P.Inc()
	}
}

// RecordBlockPathArrival records hop counters and slot-relative arrival for one beacon block path.
func RecordBlockPathArrival(mumP2P bool, recvAt, slotStartMs int64, originGatewayID, upstreamPeerID string) {
	if !enabledMetrics {
		return
	}
	if mumP2P {
		RecordMumP2PHopInfo(originGatewayID, upstreamPeerID)
		ObserveMumP2PArrivalLatency(recvAt - slotStartMs)
		return
	}
	RecordLibP2PHopInfo(upstreamPeerID)
	ObserveLibP2PArrivalLatency(recvAt - slotStartMs)
}

func ObserveMumP2PArrivalLatency(ms int64) {
	if enabledMetrics {
		blockArrivalMumP2PMs.Observe(float64(ms))
	}
}

func ObserveLibP2PArrivalLatency(ms int64) {
	if enabledMetrics {
		blockArrivalLibP2PMs.Observe(float64(ms))
	}
}

func SetLastBlockReceivedTimestamp(unixSec float64) {
	if enabledMetrics {
		lastBlockReceivedTimestamp.Set(unixSec)
	}
}

func SetKnownValidatorsTotal(count int) {
	if enabledMetrics {
		knownValidatorsTotal.Set(float64(count))
	}
}

// RecordLibP2PHopInfo increments the total libp2p upstream message counter.
func RecordLibP2PHopInfo(ethUpstreamPeerID string) {
	if !enabledMetrics || ethUpstreamPeerID == "" {
		return
	}
	libP2PMessagesFromUpstreamTotal.Inc()
}

// RecordMumP2PHopInfo increments the total mump2p upstream/origin message counters.
func RecordMumP2PHopInfo(originGatewayID, upstreamPeerID string) {
	if !enabledMetrics {
		return
	}
	if originGatewayID != "" {
		mumP2PMessagesFromOriginTotal.Inc()
	}
	if upstreamPeerID != "" {
		mumP2PMessagesFromUpstreamTotal.Inc()
	}
}
