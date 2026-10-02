package entities

import (
	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
)

// MumP2PTraceEventKind identifies a trace event without depending on a legacy
// protobuf enum. The mump2p protocol represents events as a oneof.
type MumP2PTraceEventKind uint8

const (
	MumP2PTraceEventUnknown MumP2PTraceEventKind = iota
	MumP2PTraceEventAddPeer
	MumP2PTraceEventRemovePeer
	MumP2PTraceEventJoin
	MumP2PTraceEventLeave
	MumP2PTraceEventGraft
	MumP2PTraceEventPrune
	MumP2PTraceEventRecvRPC
	MumP2PTraceEventSendRPC
	MumP2PTraceEventDropRPC
	MumP2PTraceEventPublishMessage
	MumP2PTraceEventDeliverMessage
	MumP2PTraceEventRejectMessage
	MumP2PTraceEventDuplicateMessage
	MumP2PTraceEventHelpfulSymbol
	MumP2PTraceEventRedundantSymbol
	MumP2PTraceEventInconsistentSymbol
	MumP2PTraceEventUnnecessarySymbol
)

var (
	// TraceEventsMeshTopology are mesh-topology events from mump2p: peer and topic
	// membership plus mesh grafting. Moderate frequency. Opt-in via Config.TraceMesh.
	TraceEventsMeshTopology = map[MumP2PTraceEventKind]struct{}{
		MumP2PTraceEventAddPeer:    {},
		MumP2PTraceEventRemovePeer: {},
		MumP2PTraceEventJoin:       {},
		MumP2PTraceEventLeave:      {},
		MumP2PTraceEventGraft:      {},
		MumP2PTraceEventPrune:      {},
	}

	// TraceEventsRPC are the RPC traffic events from mump2p. These are the firehose
	// (RecvRpc/SendRpc fire on every RPC), kept as their own category so topology can be
	// observed without the RPC volume. Opt-in via Config.TraceRPC.
	TraceEventsRPC = map[MumP2PTraceEventKind]struct{}{
		MumP2PTraceEventRecvRPC: {},
		MumP2PTraceEventSendRPC: {},
		MumP2PTraceEventDropRPC: {},
	}

	// TraceEventsShard are shard/RLNC-behavior events from mump2p. Message-dependent:
	// the message lifecycle (publish/deliver/reject/duplicate) plus the RLNC shard
	// coding/decoding efficiency events. Opt-in via Config.TraceShard.
	TraceEventsShard = map[MumP2PTraceEventKind]struct{}{
		MumP2PTraceEventPublishMessage:     {},
		MumP2PTraceEventDeliverMessage:     {},
		MumP2PTraceEventRejectMessage:      {},
		MumP2PTraceEventDuplicateMessage:   {},
		MumP2PTraceEventHelpfulSymbol:      {},
		MumP2PTraceEventRedundantSymbol:    {},
		MumP2PTraceEventInconsistentSymbol: {},
		MumP2PTraceEventUnnecessarySymbol:  {},
	}
)

// OptimumTraceEventSet returns the set of mump2p trace events to broadcast to
// listeners given the enabled categories. Shard metrics always run regardless of this
// set; it only controls which raw events are fanned out over the broadcaster. With all
// categories disabled the set is empty and no raw trace events are broadcast.
func OptimumTraceEventSet(mesh, rpc, shard bool) map[MumP2PTraceEventKind]struct{} {
	set := make(map[MumP2PTraceEventKind]struct{})
	add := func(events map[MumP2PTraceEventKind]struct{}) {
		for kind := range events {
			set[kind] = struct{}{}
		}
	}
	if mesh {
		add(TraceEventsMeshTopology)
	}
	if rpc {
		add(TraceEventsRPC)
	}
	if shard {
		add(TraceEventsShard)
	}
	return set
}

// MumP2PTraceEventKindOf classifies a protocol trace event by its oneof wrapper.
func MumP2PTraceEventKindOf(evt *tracepb.TraceEvent) MumP2PTraceEventKind {
	if evt == nil {
		return MumP2PTraceEventUnknown
	}

	switch evt.GetEvent().(type) {
	case *tracepb.TraceEvent_AddPeer:
		return MumP2PTraceEventAddPeer
	case *tracepb.TraceEvent_RemovePeer:
		return MumP2PTraceEventRemovePeer
	case *tracepb.TraceEvent_Join:
		return MumP2PTraceEventJoin
	case *tracepb.TraceEvent_Leave:
		return MumP2PTraceEventLeave
	case *tracepb.TraceEvent_Graft:
		return MumP2PTraceEventGraft
	case *tracepb.TraceEvent_Prune:
		return MumP2PTraceEventPrune
	case *tracepb.TraceEvent_RecvRpc:
		return MumP2PTraceEventRecvRPC
	case *tracepb.TraceEvent_SendRpc:
		return MumP2PTraceEventSendRPC
	case *tracepb.TraceEvent_DropRpc:
		return MumP2PTraceEventDropRPC
	case *tracepb.TraceEvent_PublishMessage:
		return MumP2PTraceEventPublishMessage
	case *tracepb.TraceEvent_DeliverMessage:
		return MumP2PTraceEventDeliverMessage
	case *tracepb.TraceEvent_RejectMessage:
		return MumP2PTraceEventRejectMessage
	case *tracepb.TraceEvent_DuplicateMessage:
		return MumP2PTraceEventDuplicateMessage
	case *tracepb.TraceEvent_HelpfulSymbol:
		return MumP2PTraceEventHelpfulSymbol
	case *tracepb.TraceEvent_RedundantSymbol:
		return MumP2PTraceEventRedundantSymbol
	case *tracepb.TraceEvent_InconsistentSymbol:
		return MumP2PTraceEventInconsistentSymbol
	case *tracepb.TraceEvent_UnnecessarySymbol:
		return MumP2PTraceEventUnnecessarySymbol
	default:
		return MumP2PTraceEventUnknown
	}
}
