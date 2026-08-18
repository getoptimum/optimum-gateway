package gossipsub_gateway

import (
	"fmt"
	"time"

	"github.com/montanaflynn/stats"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

var aggregated = syncx.NewRWSlice[float64]()

func (s *Service) dumpAggregateStats() {
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			a := aggregated.LoadAndErase()
			if len(a) == 0 {
				s.log.Debug("Aggregated message stats: no messages received in this interval")
				continue
			}
			maxVal, _ := stats.Max(a)
			minVal, _ := stats.Min(a)
			meanVal, _ := stats.Mean(a)
			medianVal, _ := stats.Median(a)

			s.log.Info(fmt.Sprintf("Aggregated message stats: max=%.2f, min=%.2f, mean=%.2f, median=%.2f", maxVal, minVal, meanVal, medianVal))
		}
	}
}

func (s *Service) EmitAggregatedMessage(payload []byte) {
	if s.nodeMumP2P == nil {
		s.log.Error("mump2p node is not initialized, cannot emit aggregated message", fmt.Errorf("nodeMumP2P is nil"))
		return
	}

	if s.isDuplicateMessage(payload) {
		return
	}

	telemetry.MumP2PTotalMessagesInc(mumP2PAggregatedMessagesTopic)

	s.statSendMum.Upsert(mumP2PAggregatedMessagesTopic, func(curVal int) int {
		return curVal + 1
	}, 1)
	aggregated.Add(float64(len(payload)))
	telemetry.IncAggregationMessage("out", len(payload))

	if err := s.nodeMumP2P.PublishMessage(s.ctx, mumP2PAggregatedMessagesTopic, payload); err != nil {
		msgHash := commonhash.XXHash(payload)
		telemetry.IncreaseBadMessagesToMum()
		s.log.Error("failed to publish message to mump2p node", err,
			logger.WithTopic(mumP2PAggregatedMessagesTopic),
			logger.WithUint64("hash", msgHash),
		)
	}
}

func (s *Service) handleAggregatedMessages(l logger.AppLogger, message *commonentities.P2PMessage) {
	aggregated.Add(float64(len(message.Message)))
	telemetry.IncAggregationMessage("in", len(message.Message))
	decoded, err := s.aggregatorMessages.DecodeAggregatedMessage(s.srvForkMgr.ActiveDigest(), message.Message)
	if err != nil {
		telemetry.IncParseSSZError(mumP2PAggregatedMessagesTopic, entities.SourceMumP2P)
		telemetry.IncreaseBadMessagesToCL()
		l.Error("failed to unmarshal aggregated message", err)
		return
	}

	for topic, topicPayload := range decoded {
		if _, ok := s.libP2PTopics.Load(topic); !ok {
			continue // skip messages for topics we don't subscribe to
		}
		meta := topics.TopicMetaFor(topic)
		if !meta.IsAttestation() {
			continue // aggregation only carries attestations
		}
		for i := range topicPayload {
			if s.isDuplicateMessage(topicPayload[i]) {
				continue
			}
			telemetry.IncAggregationMessage("expand", len(topicPayload[i]))
			if !s.srvMsgRouter.ShouldForwardMessageToCLP2P(meta.Kind, topicPayload[i]) {
				continue
			}
			var att consensus.SingleAttestation
			if err := s.sszEncoder.DecodeGossip(topicPayload[i], &att); err != nil {
				telemetry.IncParseSSZError(topic, entities.SourceMumP2P)
				telemetry.IncreaseBadMessagesToCL()
				l.Error("failed to validate attestation payload", err)
				continue
			}
			if err := s.publishToCLTopic(topicPayload[i], topic); err != nil {
				telemetry.IncParseSSZError(topic, entities.SourceMumP2P)
				telemetry.IncreaseBadMessagesToCL()
				l.Error("failed to publish aggregated message to CL", err)
			}
			telemetry.IncAttestationForwarded(entities.SourceLibP2P)
		}
	}
}
