package tracer

import (
	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/mump2p-protocol/pkg/telemetry/rlnctrace"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

var _ rlnctrace.RLNCTracer = (*MumP2P)(nil)

type MumP2P struct {
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse]
	traceEvents map[entities.MumP2PTraceEventKind]struct{} // event kinds to fan out over broadcaster
}

func NewTracerMumP2P(
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse],
	traceEvents map[entities.MumP2PTraceEventKind]struct{},
) *MumP2P {
	return &MumP2P{
		broadcaster: broadcaster,
		traceEvents: traceEvents,
	}
}

func (p *MumP2P) Trace(evt *tracepb.TraceEvent) {
	if evt == nil || evt.GetEvent() == nil {
		return
	}
	p.notifyMetrics(evt)

	if _, ok := p.traceEvents[entities.MumP2PTraceEventKindOf(evt)]; ok {
		p.broadcaster.Broadcast(&entities.MumP2PResponse{
			TraceEvent: evt,
			Command:    entities.MumP2PCommandTraceMumP2P,
		})
	}
}

func (p *MumP2P) notifyMetrics(evt *tracepb.TraceEvent) {
	switch evt.GetEvent().(type) {
	case *tracepb.TraceEvent_HelpfulSymbol,
		*tracepb.TraceEvent_RedundantSymbol,
		*tracepb.TraceEvent_UnnecessarySymbol,
		*tracepb.TraceEvent_InconsistentSymbol:
		telemetry.AddTotalShardCount()
	}

	switch evt.GetEvent().(type) {
	case *tracepb.TraceEvent_RedundantSymbol:
		telemetry.AddDuplicateShardCount()
	case *tracepb.TraceEvent_UnnecessarySymbol:
		telemetry.AddUnnecessaryShardCount()
	case *tracepb.TraceEvent_InconsistentSymbol:
		telemetry.AddUnhelpfulShardCount()
	}
}
