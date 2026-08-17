package gossipsub_gateway

import (
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const staleSlotThreshold = 3 // max slots behind current before we skip forwarding the block

// processBeaconBlockArrival decodes a beacon block exactly once, hands the
// observation to the bootstrapper for asynchronous latency telemetry and reports
// the slot together with whether the block is fresh enough to forward.
func (s *Service) processBeaconBlockArrival(
	l logger.AppLogger,
	topic string,
	msg []byte,
	recvAt int64,
	source entities.Source,
	originGatewayID,
	upstreamPeerID string,
) (slot uint64, forward bool) {
	s.lastBlockReceivedAt.Store(recvAt)

	blockDecoded, err := consensus.DecodeBeaconBlockHeader(msg)
	if err != nil {
		telemetry.IncreaseBadMessages(source)
		telemetry.IncParseSSZError(topic, source)
		l.Error("failed to decode beacon block slot/proposer index", err)
		return 0, false
	}

	s.srvBootstrapper.HandleBeaconBlock(source,
		blockDecoded.Header.Slot,
		blockDecoded.Header.ProposerIndex,
		uint64(len(msg)),
		recvAt,
		originGatewayID,
		upstreamPeerID,
	)
	l = l.With(logger.WithUint64("slot", blockDecoded.Header.Slot))

	// stale blocks are still measured above but not forwarded, to avoid polluting the mesh
	currentSlot := chainstate.CurrentSlot(time.Now())
	diff := utils.DiffUint64(blockDecoded.Header.Slot, currentSlot)
	stale := diff > staleSlotThreshold

	// Stream every observation, stale flagged rather than dropped (ADR-0011).
	if s.streamHub != nil {
		s.streamHub.Emit(&streamhub.BlockEvent{
			Slot:           blockDecoded.Header.Slot,
			ProposerIndex:  blockDecoded.Header.ProposerIndex,
			ParentRoot:     blockDecoded.Header.ParentRoot,
			StateRoot:      blockDecoded.Header.StateRoot,
			BlockSizeBytes: uint64(len(msg)),
			Topic:          topic,
			Source:         source,
			ReceivedAtMs:   recvAt,
			GatewayID:      s.cfg.GatewayID,
			ForkDigest:     s.srvForkMgr.ActiveDigest(),
			Stale:          stale,
			Raw:            msg,
		})
	}

	if stale {
		l.Info("stale_data, skipping publish of beacon_block",
			logger.WithUint64("current_slot", currentSlot),
			logger.WithUint64("diff", diff),
		)
		return blockDecoded.Header.Slot, false
	}

	l.Info("processing beacon_block", logger.WithString("hmr", time.Now().Format("15:04:05.000000")))
	return blockDecoded.Header.Slot, true
}
