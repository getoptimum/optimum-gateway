package tracer

import (
	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// Shard kind labels. mump2p's symbol vocabulary (helpful/redundant/inconsistent/
// unnecessary) is mapped onto the existing label values so dashboards built on the
// previous engine keep resolving.
const (
	shardKindNew         = "new"
	shardKindDuplicate   = "duplicate"
	shardKindUnhelpful   = "unhelpful"
	shardKindUnnecessary = "unnecessary"
)

// HandleMumP2PTrace decodes a raw mump2p trace event and records its meaningful attributes
// (topic, bytes, reject reason, shard kind, RPC direction) as Prometheus metrics. Which
// events arrive is controlled by OPT_TRACE_MESH / OPT_TRACE_RPC / OPT_TRACE_SHARD (the node
// only broadcasts enabled categories), so this handler dispatches whatever it receives.
func HandleMumP2PTrace(evt *tracepb.TraceEvent) error {
	if evt == nil {
		return nil
	}
	switch e := evt.GetEvent().(type) {
	// message lifecycle
	case *tracepb.TraceEvent_PublishMessage:
		telemetry.TraceMessage("publish", e.PublishMessage.GetTopic())
	case *tracepb.TraceEvent_DeliverMessage:
		telemetry.TraceMessage("deliver", e.DeliverMessage.GetTopic())
	case *tracepb.TraceEvent_DuplicateMessage:
		telemetry.TraceMessage("duplicate", e.DuplicateMessage.GetTopic())
	case *tracepb.TraceEvent_RejectMessage:
		telemetry.TraceMessage("reject", e.RejectMessage.GetTopic())
		telemetry.TraceMessageReject(e.RejectMessage.GetTopic(), e.RejectMessage.GetReason())

	// RLNC symbols
	case *tracepb.TraceEvent_HelpfulSymbol:
		telemetry.TraceShard(shardKindNew, len(e.HelpfulSymbol.GetCoefficients()))
		telemetry.AddTotalShardCount()
	case *tracepb.TraceEvent_RedundantSymbol:
		telemetry.TraceShard(shardKindDuplicate, len(e.RedundantSymbol.GetCoefficients()))
		telemetry.AddDuplicateShardCount()
	case *tracepb.TraceEvent_InconsistentSymbol:
		telemetry.TraceShard(shardKindUnhelpful, len(e.InconsistentSymbol.GetCoefficients()))
		telemetry.AddUnhelpfulShardCount()
	case *tracepb.TraceEvent_UnnecessarySymbol:
		telemetry.TraceShard(shardKindUnnecessary, len(e.UnnecessarySymbol.GetCoefficients()))
		telemetry.AddUnnecessaryShardCount()

	// RPC traffic
	case *tracepb.TraceEvent_RecvRpc:
		telemetry.TraceRPC("recv", e.RecvRpc.GetLength())
	case *tracepb.TraceEvent_SendRpc:
		telemetry.TraceRPC("send", e.SendRpc.GetLength())
	case *tracepb.TraceEvent_DropRpc:
		telemetry.TraceRPC("drop", 0) // DropRPC carries no length

	// mesh topology
	case *tracepb.TraceEvent_AddPeer:
		telemetry.TraceMesh("add_peer", "")
	case *tracepb.TraceEvent_RemovePeer:
		telemetry.TraceMesh("remove_peer", "")
	case *tracepb.TraceEvent_Join:
		telemetry.TraceMesh("join", e.Join.GetTopic())
	case *tracepb.TraceEvent_Leave:
		telemetry.TraceMesh("leave", e.Leave.GetTopic())
	case *tracepb.TraceEvent_Graft:
		telemetry.TraceMesh("graft", e.Graft.GetTopic())
	case *tracepb.TraceEvent_Prune:
		telemetry.TraceMesh("prune", e.Prune.GetTopic())
	}
	return nil
}
