package entities

import (
	commonmaps "github.com/getoptimum/optimum-common/pkg/maps"
	pboptimum "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

var (
	// TraceEventsMeshTopology are mesh-topology events from mump2p: peer and topic
	// membership plus mesh grafting. Moderate frequency. Opt-in via Config.TraceMesh.
	TraceEventsMeshTopology = map[pboptimum.TraceEvent_Type]struct{}{
		pboptimum.TraceEvent_ADD_PEER:    {},
		pboptimum.TraceEvent_REMOVE_PEER: {},
		pboptimum.TraceEvent_JOIN:        {},
		pboptimum.TraceEvent_LEAVE:       {},
		pboptimum.TraceEvent_GRAFT:       {},
		pboptimum.TraceEvent_PRUNE:       {},
	}

	// TraceEventsRPC are the RPC traffic events from optimum p2p. These are the FIREHOSE
	// (RECV_RPC/SEND_RPC fire on every RPC), kept as their own category so topology can be
	// observed without the RPC volume. Opt-in via Config.TraceRPC.
	TraceEventsRPC = map[pboptimum.TraceEvent_Type]struct{}{
		pboptimum.TraceEvent_RECV_RPC: {},
		pboptimum.TraceEvent_SEND_RPC: {},
		pboptimum.TraceEvent_DROP_RPC: {},
	}

	// TraceEventsShard are shard/RLNC-behavior events from optimum p2p. Message-dependent:
	// the message lifecycle (publish/deliver/reject/duplicate) plus the RLNC shard
	// coding/decoding efficiency events. Opt-in via Config.TraceShard.
	TraceEventsShard = map[pboptimum.TraceEvent_Type]struct{}{
		pboptimum.TraceEvent_PUBLISH_MESSAGE:   {},
		pboptimum.TraceEvent_DELIVER_MESSAGE:   {},
		pboptimum.TraceEvent_REJECT_MESSAGE:    {},
		pboptimum.TraceEvent_DUPLICATE_MESSAGE: {},
		pboptimum.TraceEvent_NEW_SHARD:         {},
		pboptimum.TraceEvent_DUPLICATE_SHARD:   {},
		pboptimum.TraceEvent_UNHELPFUL_SHARD:   {},
		pboptimum.TraceEvent_UNNECESSARY_SHARD: {},
	}
)

// OptimumTraceEventSet returns the set of optimum p2p trace events to broadcast to
// listeners given the enabled categories. Shard metrics always run regardless of this
// set; it only controls which raw events are fanned out over the broadcaster. With all
// categories disabled the set is empty and no raw trace events are broadcast.
func OptimumTraceEventSet(mesh, rpc, shard bool) map[pboptimum.TraceEvent_Type]struct{} {
	set := make(map[pboptimum.TraceEvent_Type]struct{})
	add := func(events map[pboptimum.TraceEvent_Type]struct{}) {
		for _, t := range commonmaps.MapKeys(events) {
			set[t] = struct{}{}
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
