package tracer

import (
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	pb "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

type MumP2P struct {
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse]
	traceEvents map[pb.TraceEvent_Type]struct{} // event types to fan out over broadcaster
}

func NewTracerMumP2P(
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse],
	traceEvents map[pb.TraceEvent_Type]struct{},
) *MumP2P {
	return &MumP2P{
		broadcaster: broadcaster,
		traceEvents: traceEvents,
	}
}

func (p *MumP2P) Trace(evt *pb.TraceEvent) {
	if evt == nil || evt.Type == nil {
		return
	}
	p.notifyMetrics(evt.Type)

	if _, ok := p.traceEvents[*evt.Type]; ok {
		p.broadcaster.Broadcast(&entities.MumP2PResponse{
			TraceEvent: evt,
			Command:    entities.MumP2PCommandTraceMumP2P,
		})
	}
}

func (p *MumP2P) notifyMetrics(msgType *pb.TraceEvent_Type) {
	switch *msgType {
	case pb.TraceEvent_NEW_SHARD:
		telemetry.AddTotalShardCount()
	case pb.TraceEvent_DUPLICATE_SHARD:
		telemetry.AddDuplicateShardCount()
	case pb.TraceEvent_UNNECESSARY_SHARD:
		telemetry.AddUnnecessaryShardCount()
	case pb.TraceEvent_UNHELPFUL_SHARD:
		telemetry.AddUnhelpfulShardCount()
	}
}
