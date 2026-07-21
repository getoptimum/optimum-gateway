package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// mump2p trace metrics. These capture the meaningful attributes carried by each decoded
// trace event (topic, bytes, reject reason, shard kind, RPC direction) rather than a flat
// per-category event count, so dashboards can show per-topic flow, RLNC efficiency and
// bandwidth. All labels are bounded: topics are reduced to a class via utils.SimplifyTopic,
// and per-peer / per-message identifiers are deliberately NOT labels.
var (
	// message lifecycle by event (publish|deliver|duplicate|reject) and topic class.
	traceMessagesTotal *prometheus.CounterVec
	// rejected messages by topic class and validation reason.
	traceMessageRejectsTotal *prometheus.CounterVec
	// RLNC shard events by kind (new|duplicate|unhelpful|unnecessary). duplicate/unhelpful/
	// unnecessary are the "wasted" shards — the ratio to new is the RLNC efficiency signal.
	traceShardsTotal *prometheus.CounterVec
	// RLNC shard coefficient bytes by kind (payload proxy).
	traceShardBytesTotal *prometheus.CounterVec
	// RPC events by direction (recv|send|drop).
	traceRPCTotal *prometheus.CounterVec
	// RPC bytes moved by direction (recv|send) — wire throughput proxy.
	traceRPCBytesTotal *prometheus.CounterVec
	// mesh-topology churn by event (add_peer|remove_peer|join|leave|graft|prune) and topic class.
	traceMeshTotal *prometheus.CounterVec
)

func initMump2pTraceMetrics() {
	traceMessagesTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_messages_total", subsystem,
		"mump2p message-lifecycle trace events by event (publish|deliver|duplicate|reject) and topic class.",
		[]string{"event", labelTopic},
	)
	traceMessageRejectsTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_message_rejects_total", subsystem,
		"mump2p rejected messages by topic class and validation reason.",
		[]string{labelTopic, "reason"},
	)
	traceShardsTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_shards_total", subsystem,
		"mump2p RLNC shard trace events by kind (new|duplicate|unhelpful|unnecessary).",
		[]string{"kind"},
	)
	traceShardBytesTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_shard_bytes_total", subsystem,
		"mump2p RLNC shard coefficient bytes by kind.",
		[]string{"kind"},
	)
	traceRPCTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_rpc_total", subsystem,
		"mump2p RPC trace events by direction (recv|send|drop).",
		[]string{labelDirection},
	)
	traceRPCBytesTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_rpc_bytes_total", subsystem,
		"mump2p RPC bytes by direction (recv|send).",
		[]string{labelDirection},
	)
	traceMeshTotal = commonmetrics.NewCounterVec(
		"mump2p_trace_mesh_total", subsystem,
		"mump2p mesh-topology trace events by event and topic class.",
		[]string{"event", labelTopic},
	)
}

// TraceMessage records a message-lifecycle event (publish|deliver|duplicate|reject) on a topic.
func TraceMessage(event, topic string) {
	if !enabledMetrics || event == "" {
		return
	}
	traceMessagesTotal.WithLabelValues(event, topicClass(topic)).Inc()
}

// TraceMessageReject records a rejected message with its validation reason.
func TraceMessageReject(topic, reason string) {
	if !enabledMetrics {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	traceMessageRejectsTotal.WithLabelValues(topicClass(topic), reason).Inc()
}

// TraceShard records an RLNC shard event and its coefficient byte size.
func TraceShard(kind string, coefficientBytes int) {
	if !enabledMetrics || kind == "" {
		return
	}
	traceShardsTotal.WithLabelValues(kind).Inc()
	if coefficientBytes > 0 {
		traceShardBytesTotal.WithLabelValues(kind).Add(float64(coefficientBytes))
	}
}

// TraceRPC records an RPC event and the bytes it carried (0 for drop, which has no length).
func TraceRPC(direction string, bytes uint64) {
	if !enabledMetrics || direction == "" {
		return
	}
	traceRPCTotal.WithLabelValues(direction).Inc()
	if bytes > 0 {
		traceRPCBytesTotal.WithLabelValues(direction).Add(float64(bytes))
	}
}

// TraceMesh records a mesh-topology event, optionally scoped to a topic (empty for peer add/remove).
func TraceMesh(event, topic string) {
	if !enabledMetrics || event == "" {
		return
	}
	traceMeshTotal.WithLabelValues(event, topicClass(topic)).Inc()
}

// topicClass reduces a full topic to a bounded class label; "none" for topic-less events.
func topicClass(topic string) string {
	if topic == "" {
		return "none"
	}
	return utils.SimplifyTopic(topic)
}
