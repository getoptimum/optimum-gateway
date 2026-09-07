package message_router

import (
	"context"
	"net/http"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	// 3 epochs: published list is at most 2, plus one epoch so a late block for a
	// previously-selected slot is still on_list after the window rolls.
	accelerateSlotTTL     = 3 * 32 * 12 * time.Second
	accelerateSlotCleanup = time.Minute
)

type accelerateSlotsResponse struct {
	ToSlot        int64   `json:"to_slot"`
	Slots         []int64 `json:"slots"`
	GeneratedAtMs int64   `json:"generated_at_ms"`
}

// Verdicts double as the `result` label on accelerate_decision_total.
const (
	accelerateOnList    = "on_list"
	accelerateNotOnList = "not_on_list"
	accelerateFailOpen  = "fail_open"
)

// ShouldAccelerateBlock is ADR-0012: accelerate unless the slot was examined and
// not selected. Header slot, not the clock. No list / past to_slot fail-opens.
func (s *Service) ShouldAccelerateBlock(slot uint64) bool {
	_, onList := s.accelerateSlots.Get(slot)
	decision := decideAccelerate(s.accelerateToSlot.Load(), onList, slot)
	telemetry.IncAccelerateDecision(decision)
	return decision != accelerateNotOnList
}

func decideAccelerate(toSlot uint64, onList bool, slot uint64) string {
	if onList {
		return accelerateOnList
	}
	if toSlot == 0 || slot > toSlot {
		return accelerateFailOpen
	}
	return accelerateNotOnList
}

// RefreshAccelerateSlots runs one poll and upserts selected slots into the TTL map.
// A failed poll keeps the previous set. Previously-selected slots stay until TTL.
func (s *Service) RefreshAccelerateSlots(ctx context.Context) {
	chainID := s.authMgr.Chain()
	if chainID == "" || s.cfg.RemoteBootstrapURL == "" {
		return
	}
	// Mint gets its own deadline: it runs on http.DefaultClient, so a hung auth stalls bgSync.
	var headers map[string]string
	tokCtx, cancelTok := context.WithTimeout(ctx, 5*time.Second)
	tok, tokErr := s.authMgr.ServicesToken(tokCtx)
	cancelTok()
	if tokErr != nil {
		s.log.Error("accelerate_slots poll has no services token, polling unauthenticated", tokErr)
	}
	if tok != "" {
		headers = map[string]string{"Authorization": "Bearer " + tok}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, code, err := commonnet.GetCurl[accelerateSlotsResponse](ctx, utils.BootstrapAccelerateSlotsURL(s.cfg.RemoteBootstrapURL, chainID), headers)
	// GetCurl reports a non-JSON body as an unmarshal error alongside the code.
	if err != nil || code != http.StatusOK || res == nil {
		s.log.Error("accelerate_slots poll failed, keeping previous list", err, logger.WithInt("status_code", code))
		return
	}
	// Put slots before advancing to_slot so a slot on the new list that is
	// still past the old horizon fail-opens rather than reading as not_on_list.
	for _, slot := range res.Slots {
		if slot >= 0 {
			s.accelerateSlots.Put(uint64(slot), struct{}{})
		}
	}
	if res.ToSlot > 0 {
		s.accelerateToSlot.Store(uint64(res.ToSlot))
	}
	telemetry.SetAccelerateWindow(s.accelerateToSlot.Load(), res.GeneratedAtMs)
}
