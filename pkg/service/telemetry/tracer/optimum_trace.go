package tracer

import (
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	pboptimum "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

// HandleMumP2PTrace decodes a raw mump2p trace event and records its meaningful attributes
// (topic, bytes, reject reason, shard kind, RPC direction) as Prometheus metrics. Which
// events arrive is controlled by OPT_TRACE_MESH / OPT_TRACE_RPC / OPT_TRACE_SHARD (the node
// only broadcasts enabled categories), so this handler dispatches whatever it receives.
func HandleMumP2PTrace(evt *pboptimum.TraceEvent) error {
	if evt == nil || evt.Type == nil {
		return nil
	}
	switch evt.GetType() {
	// message lifecycle
	case pboptimum.TraceEvent_PUBLISH_MESSAGE:
		telemetry.TraceMessage("publish", evt.GetPublishMessage().GetTopic())
	case pboptimum.TraceEvent_DELIVER_MESSAGE:
		telemetry.TraceMessage("deliver", evt.GetDeliverMessage().GetTopic())
	case pboptimum.TraceEvent_DUPLICATE_MESSAGE:
		telemetry.TraceMessage("duplicate", evt.GetDuplicateMessage().GetTopic())
	case pboptimum.TraceEvent_REJECT_MESSAGE:
		rm := evt.GetRejectMessage()
		telemetry.TraceMessage("reject", rm.GetTopic())
		telemetry.TraceMessageReject(rm.GetTopic(), rm.GetReason())

	// RLNC shards
	case pboptimum.TraceEvent_NEW_SHARD:
		telemetry.TraceShard("new", len(evt.GetNewShard().GetCoefficients()))
	case pboptimum.TraceEvent_DUPLICATE_SHARD:
		telemetry.TraceShard("duplicate", len(evt.GetDuplicateShard().GetCoefficients()))
	case pboptimum.TraceEvent_UNHELPFUL_SHARD:
		telemetry.TraceShard("unhelpful", len(evt.GetUnhelpfulShard().GetCoefficients()))
	case pboptimum.TraceEvent_UNNECESSARY_SHARD:
		telemetry.TraceShard("unnecessary", len(evt.GetUnnecessaryShard().GetCoefficients()))

	// RPC traffic
	case pboptimum.TraceEvent_RECV_RPC:
		telemetry.TraceRPC("recv", evt.GetRecvRPC().GetLength())
	case pboptimum.TraceEvent_SEND_RPC:
		telemetry.TraceRPC("send", evt.GetSendRPC().GetLength())
	case pboptimum.TraceEvent_DROP_RPC:
		telemetry.TraceRPC("drop", 0) // DropRPC carries no length

	// mesh topology
	case pboptimum.TraceEvent_ADD_PEER:
		telemetry.TraceMesh("add_peer", "")
	case pboptimum.TraceEvent_REMOVE_PEER:
		telemetry.TraceMesh("remove_peer", "")
	case pboptimum.TraceEvent_JOIN:
		telemetry.TraceMesh("join", evt.GetJoin().GetTopic())
	case pboptimum.TraceEvent_LEAVE:
		telemetry.TraceMesh("leave", evt.GetLeave().GetTopic())
	case pboptimum.TraceEvent_GRAFT:
		telemetry.TraceMesh("graft", evt.GetGraft().GetTopic())
	case pboptimum.TraceEvent_PRUNE:
		telemetry.TraceMesh("prune", evt.GetPrune().GetTopic())
	}
	return nil
}
