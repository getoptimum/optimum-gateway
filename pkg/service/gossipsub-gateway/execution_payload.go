package gossipsub_gateway

import (
	"strconv"
	"time"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
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

// executionPayloadHash is the same on the libp2p and mump2p lines and on every
// gateway, since both paths carry the same compressed bytes.
func executionPayloadHash(msg []byte) string {
	return strconv.FormatUint(commonhash.XXHash(msg), 16)
}

// logExecutionPayloadArrival records one observation of an envelope. Every
// copy is logged, including the one that loses the race, so a same-hub
// libp2p versus mump2p delta exists for every slot.
func logExecutionPayloadArrival(l logger.AppLogger, receivedAt time.Time, direction string, msg []byte, fields ...logger.Field) {
	l.Info("execution_payload arrived", append([]logger.Field{
		logger.WithInt64("received_at_ms", receivedAt.UnixMilli()),
		logger.WithString("direction", direction),
		logger.WithString("payload_hash", executionPayloadHash(msg)),
		logger.WithInt("size", len(msg)),
	}, fields...)...)
}

// logMumP2PExecutionPayloadArrival stamps a mesh copy as it leaves the mump2p
// node, before the mumP2PMessages queue it shares with blocks and aggregates.
func (s *Service) logMumP2PExecutionPayloadArrival(msg *commonentities.P2PMessage) {
	if msg == nil || msg.SourceNodeID == s.nodeMumP2PStr || !utils.IsExecutionPayloadTopic(msg.Topic) {
		return
	}
	if len(msg.Message) > consensus.MaxCompressedGossipSize() {
		return
	}
	logExecutionPayloadArrival(s.log, time.Now(), "mump2p", msg.Message,
		logger.WithString("source", entities.SourceMumP2P.String()),
		logger.WithTopic(msg.Topic),
		logger.WithString("message_id", msg.MessageID),
		logger.WithString("source_node_id", msg.SourceNodeID),
	)
}

// processCLExecutionPayload forwards a consensus-client envelope into the mesh.
// Every gateway type does this, including relay. The message router is not
// consulted, matching beacon blocks.
func (s *Service) processCLExecutionPayload(l logger.AppLogger, msg *entities.CLMessage) {
	if len(msg.Message) > consensus.MaxCompressedGossipSize() {
		noteExecutionPayloadSkipped(l, msg.Message, "mump2p")
		return
	}
	// The arrival is logged by executionPayloadTracer. Only the first copy is
	// counted and published.
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
	// The arrival was logged at receive, before the queue. This counts the first copy.
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
