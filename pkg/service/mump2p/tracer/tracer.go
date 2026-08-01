// Package tracer fans mump2p trace events out to the node's listeners. Two
// tracers are needed because the RLNC router and the underlying gossipsub report
// on different interfaces and cover disjoint event sets.
package tracer

import (
	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

// MumP2P broadcasts the RLNC router's trace events: publish, delivery, per-symbol
// validity, recoding and chunk completion, plus peer add/remove.
type MumP2P struct {
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse]
	categories  entities.TraceCategories
}

func NewTracerMumP2P(
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse],
	categories entities.TraceCategories,
) *MumP2P {
	return &MumP2P{
		broadcaster: broadcaster,
		categories:  categories,
	}
}

func (p *MumP2P) Trace(evt *tracepb.TraceEvent) {
	p.broadcast(evt)
}

// broadcast fans an event out when its category is enabled. Shared by both tracers.
func (p *MumP2P) broadcast(evt *tracepb.TraceEvent) {
	if !p.categories.Enabled(evt) {
		return
	}
	p.broadcaster.Broadcast(&entities.MumP2PResponse{
		TraceEvent: evt,
		Command:    entities.MumP2PCommandTraceMumP2P,
	})
}
