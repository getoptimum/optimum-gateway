package gossipsub_gateway

import (
	"time"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// executionPayloadLimit is the largest envelope this gateway will forward.
// Both directions use the consensus-client budget. The 1 MiB GossipSub cap
// bounds one RPC after the mesh library shards the payload.
func executionPayloadLimit() int {
	return consensus.MaxCompressedGossipSize()
}

func (s *Service) skipOversizedExecutionPayload(l logger.AppLogger, msg []byte, toMesh bool) bool {
	limit := executionPayloadLimit()
	if len(msg) <= limit {
		return false
	}
	direction := "cl"
	if toMesh {
		direction = "mump2p"
	}
	telemetry.IncExecutionPayloadSkipped(direction)
	l.Info("skipping execution_payload, larger than the publish budget",
		logger.WithInt("size", len(msg)),
		logger.WithInt("limit", limit),
		logger.WithString("direction", direction),
	)
	return true
}

func (s *Service) noteExecutionPayloadArrival(l logger.AppLogger, direction string, msg []byte) {
	telemetry.ObserveExecutionPayload(direction, len(msg))
	l.Info("execution_payload arrived",
		logger.WithInt64("received_at_ms", time.Now().UnixMilli()),
		logger.WithString("direction", direction),
	)
}

func (s *Service) processCLExecutionPayload(l logger.AppLogger, msg *entities.CLMessage) {
	if s.skipOversizedExecutionPayload(l, msg.Message, true) {
		return
	}
	s.noteExecutionPayloadArrival(l, "cl", msg.Message)
	if s.isDuplicateMessage(msg.Message) {
		return
	}
	if s.nodeMumP2P == nil {
		return
	}
	if err := s.nodeMumP2P.PublishMessage(s.ctx, msg.Topic, msg.Message); err != nil {
		telemetry.IncreaseBadMessagesToMum()
		l.Error("failed to publish execution payload to mump2p node", err)
		return
	}
	telemetry.MumP2PTotalMessagesInc(msg.Topic)
	s.statSendMum.Upsert(msg.Topic, func(curVal int) int {
		return curVal + 1
	}, 1)
}

func (s *Service) processMumP2PExecutionPayload(l logger.AppLogger, msg *commonentities.P2PMessage) {
	if s.skipOversizedExecutionPayload(l, msg.Message, false) {
		return
	}
	s.noteExecutionPayloadArrival(l, "mump2p", msg.Message)
	if s.propagationDisabled(l) {
		return
	}
	if !s.srvMsgRouter.ShouldForwardMessageToCLP2P(topics.TopicExecutionPayload, 0, msg.Message) {
		return
	}
	if err := s.publishToCLTopic(msg.Message, msg.Topic); err != nil {
		telemetry.IncParseSSZError(msg.Topic, entities.SourceMumP2P)
		telemetry.IncreaseBadMessagesToCL()
		l.Error("failed to publish execution payload to CL", err)
	}
}
