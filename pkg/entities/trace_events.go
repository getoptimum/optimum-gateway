package entities

import (
	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
)

// TraceCategories selects which mump2p trace events are fanned out to
// RegisterListener consumers. Shard metrics always run regardless of these; the
// categories only gate the raw fan-out. All default false (no fan-out at all).
type TraceCategories struct {
	// Mesh covers peer and topic membership plus mesh grafting. Moderate frequency.
	Mesh bool
	// RPC covers RPC traffic (recv/send/drop). HIGH FREQUENCY firehose.
	RPC bool
	// Shard covers the message lifecycle plus the RLNC symbol coding/decoding events.
	Shard bool
}

// NewTraceCategories builds the category selection from the three config flags.
func NewTraceCategories(mesh, rpc, shard bool) TraceCategories {
	return TraceCategories{Mesh: mesh, RPC: rpc, Shard: shard}
}

// Any reports whether at least one category is enabled.
func (c TraceCategories) Any() bool {
	return c.Mesh || c.RPC || c.Shard
}

// Enabled reports whether evt belongs to an enabled category. Events whose
// category cannot be determined (nil event, unset oneof) are never fanned out.
func (c TraceCategories) Enabled(evt *tracepb.TraceEvent) bool {
	if evt == nil {
		return false
	}
	switch evt.GetEvent().(type) {
	case *tracepb.TraceEvent_AddPeer,
		*tracepb.TraceEvent_RemovePeer,
		*tracepb.TraceEvent_Join,
		*tracepb.TraceEvent_Leave,
		*tracepb.TraceEvent_Graft,
		*tracepb.TraceEvent_Prune:
		return c.Mesh

	case *tracepb.TraceEvent_RecvRpc,
		*tracepb.TraceEvent_SendRpc,
		*tracepb.TraceEvent_DropRpc:
		return c.RPC

	case *tracepb.TraceEvent_PublishMessage,
		*tracepb.TraceEvent_DeliverMessage,
		*tracepb.TraceEvent_RejectMessage,
		*tracepb.TraceEvent_DuplicateMessage,
		*tracepb.TraceEvent_HelpfulSymbol,
		*tracepb.TraceEvent_RedundantSymbol,
		*tracepb.TraceEvent_InconsistentSymbol,
		*tracepb.TraceEvent_UnnecessarySymbol,
		*tracepb.TraceEvent_EncodeError,
		*tracepb.TraceEvent_Recode,
		*tracepb.TraceEvent_ChunkDecoded:
		return c.Shard

	default:
		return false
	}
}
