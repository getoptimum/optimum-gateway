package tracer

import (
	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// HandleMumP2PTrace decodes a raw mump2p trace event and records its meaningful attributes
// (topic, bytes, reject reason, shard kind, RPC direction) as Prometheus metrics. Which
// events arrive is controlled by OPT_TRACE_MESH / OPT_TRACE_RPC / OPT_TRACE_SHARD (the node
// only broadcasts enabled categories), so this handler dispatches whatever it receives.
func HandleMumP2PTrace(evt *tracepb.TraceEvent) error {
	if evt == nil || evt.GetEvent() == nil {
		return nil
	}
	switch event := evt.GetEvent().(type) {
	// message lifecycle
	case *tracepb.TraceEvent_PublishMessage:
		telemetry.TraceMessage("publish", event.PublishMessage.GetTopic())
	case *tracepb.TraceEvent_DeliverMessage:
		telemetry.TraceMessage("deliver", event.DeliverMessage.GetTopic())
	case *tracepb.TraceEvent_DuplicateMessage:
		telemetry.TraceMessage("duplicate", event.DuplicateMessage.GetTopic())
	case *tracepb.TraceEvent_RejectMessage:
		rm := event.RejectMessage
		telemetry.TraceMessage("reject", rm.GetTopic())
		telemetry.TraceMessageReject(rm.GetTopic(), rm.GetReason())

	// RLNC shards
	case *tracepb.TraceEvent_HelpfulSymbol:
		telemetry.TraceShard("new", len(event.HelpfulSymbol.GetCoefficients()))
	case *tracepb.TraceEvent_RedundantSymbol:
		telemetry.TraceShard("duplicate", len(event.RedundantSymbol.GetCoefficients()))
	case *tracepb.TraceEvent_InconsistentSymbol:
		telemetry.TraceShard("unhelpful", len(event.InconsistentSymbol.GetCoefficients()))
	case *tracepb.TraceEvent_UnnecessarySymbol:
		telemetry.TraceShard("unnecessary", len(event.UnnecessarySymbol.GetCoefficients()))

	// RPC traffic
	case *tracepb.TraceEvent_RecvRpc:
		telemetry.TraceRPC("recv", event.RecvRpc.GetLength())
	case *tracepb.TraceEvent_SendRpc:
		telemetry.TraceRPC("send", event.SendRpc.GetLength())
	case *tracepb.TraceEvent_DropRpc:
		telemetry.TraceRPC("drop", 0) // DropRPC carries no length

	// mesh topology
	case *tracepb.TraceEvent_AddPeer:
		telemetry.TraceMesh("add_peer", "")
	case *tracepb.TraceEvent_RemovePeer:
		telemetry.TraceMesh("remove_peer", "")
	case *tracepb.TraceEvent_Join:
		telemetry.TraceMesh("join", event.Join.GetTopic())
	case *tracepb.TraceEvent_Leave:
		telemetry.TraceMesh("leave", event.Leave.GetTopic())
	case *tracepb.TraceEvent_Graft:
		telemetry.TraceMesh("graft", event.Graft.GetTopic())
	case *tracepb.TraceEvent_Prune:
		telemetry.TraceMesh("prune", event.Prune.GetTopic())
	}
	return nil
}
