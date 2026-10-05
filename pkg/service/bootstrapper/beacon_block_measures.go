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
	publishedAt     int64
}

func (s *Service) bgHandleSendSlots() {
	for slot := range s.sendSlotsChan {
		s.sendTrackedSlots(slot)
	}
}

func (s *Service) bgHandleBlockEvents() {
	for ev := range s.blockEvents {
		if ev.publishedAt > 0 {
			s.composePublishTelemetry(ev)
			continue
		}
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

// RecordMumPublishedAt shares the blockEvents queue with HandleBeaconBlock so the
// libp2p arrival that triggered the publish is always composed first.
func (s *Service) RecordMumPublishedAt(slot uint64, publishedAt int64) {
	if !s.cfg.TelemetryEnable || slot == 0 {
		return
	}
	s.enqueueBlockEvent(&blockArrival{slot: slot, publishedAt: publishedAt})
}

// composePublishTelemetry marks the publish as this gateway's mump2p first-seen,
// so a later copy from another publisher cannot set t_mum_seen.
func (s *Service) composePublishTelemetry(ev *blockArrival) {
	s.trackedSlots.Upsert(ev.slot, func(value *entities.LatencyComparator) *entities.LatencyComparator {
		value.ChainID = s.srvForkMgr.AppChainID()
		value.MumPublishedAtMs = ev.publishedAt
		if value.MumSeenAtMs == 0 {
			value.MumSeenAtMs = ev.publishedAt
		}
		return value
	}, &entities.LatencyComparator{
		GatewayID:        s.cfg.GatewayID,
		ChainID:          s.srvForkMgr.AppChainID(),
		BlockSlot:        ev.slot,
		SlotTime:         chainstate.SlotStartTime(ev.slot).UnixMilli(),
		MumPublishedAtMs: ev.publishedAt,
		MumSeenAtMs:      ev.publishedAt,
	})
	s.enqueueSlotForTracking(ev.slot)
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
	s.enqueueBlockEvent(&blockArrival{
		source:          source,
		slot:            slot,
		proposerIndex:   proposerIndex,
		blockSize:       blockSize,
		recvAt:          recvAt,
		originGatewayID: originGatewayID,
		upstreamPeerID:  upstreamPeerID,
	})
}

func (s *Service) enqueueBlockEvent(ev *blockArrival) {
	select {
	case s.blockEvents <- ev:
	default:
		s.log.Debug("blockEvents is full, skipping block latency telemetry", logger.WithUint64("slot", ev.slot))
	}
}

func (s *Service) composeBlockTelemetry(ev *blockArrival) {
	created := &entities.LatencyComparator{
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
		created.MumSeenAtMs = ev.recvAt // mark time when we received block from mump2p
		created.OriginGatewayID = ev.originGatewayID
		created.UpstreamPeerID = ev.upstreamPeerID
		// Record routing information for hop-by-hop latency analysis
		telemetry.RecordBlockPathArrival(true, ev.recvAt, created.SlotTime, ev.originGatewayID, ev.upstreamPeerID)
	case entities.SourceLibP2P:
		created.EthSeenAtMs = ev.recvAt // mark time when we received block from libp2p
		created.EthUpstreamPeerID = ev.upstreamPeerID
		// Record routing information for hop-by-hop latency analysis
		telemetry.RecordBlockPathArrival(false, ev.recvAt, created.SlotTime, "", ev.upstreamPeerID)
	}
	// firstForSlot will be true if this is the first time we're seeing this slot, which we use to increment the appropriate telemetry counter.
	firstForSlot := true
	s.trackedSlots.Upsert(ev.slot, func(value *entities.LatencyComparator) *entities.LatencyComparator {
		firstForSlot = false
		value.ChainID = s.srvForkMgr.AppChainID()
		switch ev.source {
		case entities.SourceLibP2P:
			if value.EthSeenAtMs == 0 {
				value.EthSeenAtMs = ev.recvAt
				value.EthUpstreamPeerID = ev.upstreamPeerID
			}
		case entities.SourceMumP2P:
			// Publish sets t_mum_seen with no peer, so the first copy still fills the peer.
			if value.UpstreamPeerID == "" {
				if value.MumSeenAtMs == 0 {
					value.MumSeenAtMs = ev.recvAt
				}
				value.OriginGatewayID = ev.originGatewayID
				value.UpstreamPeerID = ev.upstreamPeerID
			}
		}
		return value
	}, created)
	if firstForSlot {
		telemetry.IncBlocksFirstSeen(ev.source)
	}
	s.enqueueSlotForTracking(ev.slot)
}
