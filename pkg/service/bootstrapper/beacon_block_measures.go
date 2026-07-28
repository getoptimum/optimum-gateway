package bootstrapper

import (
	"context"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// blockArrival carries a beacon block observation off the gateway forwarding
// path so the latency comparator is composed by a background worker.
type blockArrival struct {
	source          entities.Source
	slot            uint64
	proposerIndex   uint64
	blockSize       uint64
	recvAt          int64
	originGatewayID string
	upstreamPeerID  string
}

func (s *Service) bgHandleSendSlots() {
	for slot := range s.sendSlotsChan {
		s.sendTrackedSlots(slot)
	}
}

func (s *Service) bgHandleBlockEvents() {
	for ev := range s.blockEvents {
		s.composeBlockTelemetry(ev)
	}
}

// sendTrackedSlots sends the latency comparator data for a given slot to a remote URL for further analysis.
// on any changes we just send updated data to remote server.
// we do not wait for response or care about errors here, as this is best-effort telemetry data.
// as we use TTL map, old slots will be removed automatically.
// also TTL map is sequence change operations, so we may expect that each next call will have latest data.
func (s *Service) sendTrackedSlots(slot uint64) {
	l := s.log.With(logger.WithUint64("slot", slot))
	var data entities.LatencyComparator
	// do and apply is used to get latest data for slot, as we may have multiple updates for same slot.
	ok := s.trackedSlots.DoAndApply(slot, func(v *entities.LatencyComparator) *entities.LatencyComparator {
		data = *v
		return v
	})
	if !ok {
		l.Info("no tracked data for slot")
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	url := utils.BootstrapHandleBlockLatencyURL(s.cfg.RemoteBootstrapURL)
	_, code, err := commonnet.PostCurl[any](ctx, url, data, s.bearerAuthHeader(ctx))
	if utils.IsPostSuccess(code) {
		return
	}
	l.Error("failed to send tracked slot data", err, logger.WithInt("code", code))
}

// enqueueSlotForTracking enqueues slot export without blocking the CL/mump2p path.
func (s *Service) enqueueSlotForTracking(slot uint64) {
	select {
	case s.sendSlotsChan <- slot:
	default:
		s.log.Debug("sendSlotsChan is full, skipping sending slot for latency tracking", logger.WithUint64("slot", slot))
	}
}

func (s *Service) RecordMumPublishedAt(slot uint64, publishedAt int64) {
	if !s.cfg.TelemetryEnable || slot == 0 {
		return
	}
	s.trackedSlots.Upsert(slot, func(value *entities.LatencyComparator) *entities.LatencyComparator {
		value.ChainID = s.srvForkMgr.AppChainID()
		value.MumPublishedAtMs = publishedAt
		return value
	}, &entities.LatencyComparator{
		GatewayID:        s.cfg.GatewayID,
		ChainID:          s.srvForkMgr.AppChainID(),
		BlockSlot:        slot,
		SlotTime:         chainstate.SlotStartTime(slot).UnixMilli(),
		MumPublishedAtMs: publishedAt,
	})
	s.enqueueSlotForTracking(slot)
}

// HandleBeaconBlock records a beacon block observation without blocking the
// caller's forwarding path; the latency comparator is composed asynchronously
// by bgHandleBlockEvents.
func (s *Service) HandleBeaconBlock(
	source entities.Source,
	slot,
	proposerIndex,
	blockSize uint64,
	recvAt int64,
	originGatewayID,
	upstreamPeerID string,
) {
	if !s.cfg.TelemetryEnable {
		return
	}
	ev := &blockArrival{
		source:          source,
		slot:            slot,
		proposerIndex:   proposerIndex,
		blockSize:       blockSize,
		recvAt:          recvAt,
		originGatewayID: originGatewayID,
		upstreamPeerID:  upstreamPeerID,
	}
	select {
	case s.blockEvents <- ev:
	default:
		s.log.Debug("blockEvents is full, skipping block latency telemetry", logger.WithUint64("slot", slot))
	}
}

func (s *Service) composeBlockTelemetry(ev *blockArrival) {
	zeroVal := &entities.LatencyComparator{
		GatewayID:      s.cfg.GatewayID,
		GatewayPeerID:  s.nodeMumP2PStr,
		ChainID:        s.srvForkMgr.AppChainID(),
		BlockSlot:      ev.slot,
		ValidatorIndex: ev.proposerIndex,
		SlotTime:       chainstate.SlotStartTime(ev.slot).UnixMilli(), // expected start time of the slot
		BlockSize:      ev.blockSize,
	}
	switch ev.source {
	case entities.SourceMumP2P:
		zeroVal.MumSeenAtMs = ev.recvAt // mark time when we received block from mump2p
		zeroVal.OriginGatewayID = ev.originGatewayID
		zeroVal.UpstreamPeerID = ev.upstreamPeerID
		// Record routing information for hop-by-hop latency analysis
		telemetry.RecordBlockPathArrival(true, ev.recvAt, zeroVal.SlotTime, ev.originGatewayID, ev.upstreamPeerID)
	case entities.SourceLibP2P:
		zeroVal.EthSeenAtMs = ev.recvAt // mark time when we received block from libp2p
		zeroVal.EthUpstreamPeerID = ev.upstreamPeerID
		// Record routing information for hop-by-hop latency analysis
		telemetry.RecordBlockPathArrival(false, ev.recvAt, zeroVal.SlotTime, "", ev.upstreamPeerID)
	}
	// firstForSlot will be true if this is the first time we're seeing this slot, which we use to increment the appropriate telemetry counter.
	firstForSlot := true
	s.trackedSlots.Upsert(ev.slot, func(value *entities.LatencyComparator) *entities.LatencyComparator {
		firstForSlot = false
		value.ChainID = s.srvForkMgr.AppChainID()
		switch ev.source {
		case entities.SourceLibP2P:
			value.EthSeenAtMs = ev.recvAt
			value.EthUpstreamPeerID = ev.upstreamPeerID
		case entities.SourceMumP2P:
			value.MumSeenAtMs = ev.recvAt
			value.OriginGatewayID = ev.originGatewayID
			value.UpstreamPeerID = ev.upstreamPeerID
		}
		return value
	}, zeroVal)
	if firstForSlot {
		telemetry.IncBlocksFirstSeen(ev.source)
	}
	s.enqueueSlotForTracking(ev.slot)
}
