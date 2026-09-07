package gossipsub_gateway

import (
	"time"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// handleMessagesFromCL fetches messages from CL client and passes to mump2p nodes
func (s *Service) handleMessagesFromCL() {
	for msg := range s.clMessages {
		if !s.srvForkMgr.TopicSupported(msg.Topic) {
			continue
		}
		start := time.Now()
		telemetry.RecordCLMessageAt()
		l := s.log.With(
			logger.WithString("source", entities.SourceLibP2P.String()),
			logger.WithInt("size", len(msg.Message)),
			logger.WithString("message_id", msg.MessageID),
			logger.WithTopic(msg.Topic),
		)
		meta := topics.TopicMetaFor(msg.Topic)
		switch {
		case meta.IsBeaconBlock():
			s.processCLBeaconBlock(l, msg)
		case meta.IsAttestation():
			s.processCLAttestation(l, msg, meta)
		}
		telemetry.RecordProcessingTime("cl_processing", time.Since(start))
	}
}

func (s *Service) processCLBeaconBlock(l logger.AppLogger, msg *entities.CLMessage) {
	slot, forward := s.processBeaconBlockArrival(l, msg.Topic, msg.Message, time.Now().UnixMilli(), entities.SourceLibP2P, "", msg.ReceivedFrom)
	if !forward {
		return
	}
	// record arrival before dedup so latency is captured even if mump2p delivered it first
	if s.isDuplicateMessage(msg.Message) {
		return
	}
	if !s.srvMsgRouter.ShouldAccelerateBlock(slot) {
		return
	}
	if s.nodeMumP2P == nil {
		return
	}
	publishedAt := time.Now().UnixMilli()
	if err := s.nodeMumP2P.PublishMessage(s.ctx, msg.Topic, msg.Message); err != nil {
		telemetry.IncreaseBadMessagesToMum()
		l.Error("failed to publish message to mump2p node", err)
		return
	}
	// Count only messages actually published.
	telemetry.MumP2PTotalMessagesInc(msg.Topic)
	s.statSendMum.Upsert(msg.Topic, func(curVal int) int {
		return curVal + 1
	}, 1)
	if slot > 0 {
		s.srvBootstrapper.RecordMumPublishedAt(slot, publishedAt)
	}
}

func (s *Service) processCLAttestation(l logger.AppLogger, msg *entities.CLMessage, meta *topics.TopicMeta) {
	if s.isDuplicateMessage(msg.Message) {
		return
	}

	if !s.srvMsgRouter.ShouldForwardMessageToMumP2P(l, meta.Kind, msg.Topic, msg.Message) {
		return
	}

	s.aggregatorMessages.Enqueue(msg.Topic, meta, msg.Message)
}

// handleMessagesFromMumP2PNode fetches messages from mump2p nodes and passes to CL client
func (s *Service) handleMessagesFromMumP2PNode() {
	for msg := range s.mumP2PMessages {
		if !s.srvForkMgr.TopicSupported(msg.Topic) {
			continue
		}
		start := time.Now()
		telemetry.RecordMumMessageAt()
		l := s.log.With(
			logger.WithTopic(msg.Topic),
			logger.WithInt("size", len(msg.Message)),
			logger.WithString("message_id", msg.MessageID),
			logger.WithString("source_node_id", msg.SourceNodeID),
			logger.WithString("source", entities.SourceMumP2P.String()),
		)
		s.processMumP2PMessage(l, msg)
		telemetry.RecordProcessingTime("mump2p_processing", time.Since(start))
	}
}

func (s *Service) processMumP2PMessage(l logger.AppLogger, msg *commonentities.P2PMessage) {
	messageFromSelf := msg.SourceNodeID == s.nodeMumP2PStr
	if messageFromSelf && s.cfg.GetSkipMessagesFromSelf() {
		return // skip messages from ourselves
	}

	meta := topics.TopicMetaFor(msg.Topic)
	// block measure goes before rejecting self messages: otherwise we send a message,
	// nobody sends it back (we already have it) and we lose the propagation latency sample.
	var slot uint64
	if meta.IsBeaconBlock() {
		var forward bool
		slot, forward = s.processBeaconBlockArrival(l, msg.Topic, msg.Message, time.Now().UnixMilli(), entities.SourceMumP2P, msg.SourceNodeID, msg.UpstreamPeerID)
		if !forward {
			return
		}
	}

	if messageFromSelf {
		return
	}
	if s.isDuplicateMessage(msg.Message) {
		return
	}

	switch {
	case meta.IsBeaconBlock():
		s.processMumP2PBeaconBlock(l, msg, slot)
	case msg.Topic == mumP2PAggregatedMessagesTopic:
		s.handleAggregatedMessages(l, msg)
	}
}

func (s *Service) processMumP2PBeaconBlock(l logger.AppLogger, msg *commonentities.P2PMessage, slot uint64) {
	propagationEnabled := s.cfg.PropagationEnabled()
	telemetry.SetPropagationState(propagationEnabled)
	if !propagationEnabled {
		l.Info("skip processing message since propagation is disabled")
		return
	}
	if !s.srvMsgRouter.ShouldForwardMessageToCLP2P(topics.TopicBeaconBlock, msg.Message) {
		return
	}
	if !s.srvMsgRouter.ShouldAccelerateBlock(slot) {
		return
	}
	if err := s.publishToCLTopic(msg.Message, msg.Topic); err != nil {
		telemetry.IncParseSSZError(msg.Topic, entities.SourceMumP2P)
		telemetry.IncreaseBadMessagesToCL()
		l.Error("failed to publish message to CL", err)
	}
}

func (s *Service) publishToCLTopic(data []byte, topic string) error {
	tp, ok := s.libP2PTopics.Load(topic)
	if !ok {
		return nil
	}
	if err := tp.Publish(s.ctx, data); err != nil {
		return err
	}
	telemetry.RecordCLMessageAt()
	telemetry.CLTotalMessagesInc(topic)
	s.statSendLib.Upsert(topic, func(curVal int) int {
		return curVal + 1
	}, 1)
	return nil
}

// isDuplicateMessage checks if a message has already been processed.
// Returns true if duplicate, false if new. Automatically caches new messages.
// we use XXHash to generate a unique hash for the message
// we do not need strong cryptographic hash here, just a fast and unique identifier
// hash is used in time ~1-2minute, so no collisions are expected
func (s *Service) isDuplicateMessage(message []byte) bool {
	hash := commonhash.XXHash(message)
	if _, exists := s.messagesMap.Get(hash); exists {
		return true
	}
	s.messagesMap.Put(hash, struct{}{})
	return false
}
