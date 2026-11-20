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

// noteExecutionPayloadSkipped counts an envelope that was not published.
// direction is the side the publish would have gone to.
func noteExecutionPayloadSkipped(l logger.AppLogger, msg []byte, direction string) {
	telemetry.IncExecutionPayloadSkipped(direction)
	l.Info("skipping execution_payload, larger than the publish budget",
		logger.WithInt("size", len(msg)),
		logger.WithInt("limit", consensus.MaxCompressedGossipSize()),
		logger.WithString("direction", direction),
	)
}

// logExecutionPayloadArrival records one observation of an in-budget envelope.
// Both paths log, including the copy that loses the race. The later copy is
// what makes a same-hub libp2p versus mump2p delta possible.
func logExecutionPayloadArrival(l logger.AppLogger, direction string) {
	l.Info("execution_payload arrived",
		logger.WithInt64("received_at_ms", time.Now().UnixMilli()),
		logger.WithString("direction", direction),
	)
}

func logExecutionPayloadIfInBudget(l logger.AppLogger, direction string, msg []byte) {
	if len(msg) > consensus.MaxCompressedGossipSize() {
		return
	}
	logExecutionPayloadArrival(l, direction)
}

// processCLExecutionPayload forwards a consensus-client envelope into the mesh.
// Every gateway type does this, including relay. The message router is not
// consulted, matching beacon blocks.
func (s *Service) processCLExecutionPayload(l logger.AppLogger, msg *entities.CLMessage) {
	if len(msg.Message) > consensus.MaxCompressedGossipSize() {
		noteExecutionPayloadSkipped(l, msg.Message, "mump2p")
		return
	}
	// Log before dedup. A payload that already arrived on mump2p is still a
	// libp2p sample; only the first copy is counted and published.
	logExecutionPayloadArrival(l, "cl")
	if s.isDuplicateMessage(msg.Message) {
		return
	}
	telemetry.ObserveExecutionPayload("cl", len(msg.Message))
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
	if len(msg.Message) > consensus.MaxCompressedGossipSize() {
		// Propagation off would not have forwarded it, so it is not a skip.
		if !s.propagationDisabled(l) {
			noteExecutionPayloadSkipped(l, msg.Message, "cl")
		}
		return
	}
	// The arrival itself was logged before dedup. This counts the first copy.
	telemetry.ObserveExecutionPayload("mump2p", len(msg.Message))
	if s.propagationDisabled(l) {
		return
	}
	if !s.srvMsgRouter.ShouldForwardMessageToCLP2P(topics.TopicExecutionPayload, 0, msg.Message) {
		return
	}
	if err := s.publishToCLTopic(msg.Message, msg.Topic); err != nil {
		telemetry.IncreaseBadMessagesToCL()
		l.Error("failed to publish execution payload to CL", err)
	}
}
