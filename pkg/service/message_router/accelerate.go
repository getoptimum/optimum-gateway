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

type accelerateWindow struct {
	toSlot uint64
	slots  map[uint64]struct{}
}

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
	decision := decideAccelerate(s.accelerate.Load(), slot)
	telemetry.IncAccelerateDecision(decision)
	return decision != accelerateNotOnList
}

func decideAccelerate(w *accelerateWindow, slot uint64) string {
	if w == nil || w.toSlot == 0 || slot > w.toSlot {
		return accelerateFailOpen
	}
	if _, ok := w.slots[slot]; ok {
		return accelerateOnList
	}
	return accelerateNotOnList
}

// RefreshAccelerateSlots runs one poll and swaps the whole window. A failed poll keeps the previous one.
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
	w := &accelerateWindow{slots: make(map[uint64]struct{}, len(res.Slots))}
	if res.ToSlot > 0 {
		w.toSlot = uint64(res.ToSlot)
	}
	for _, slot := range res.Slots {
		if slot >= 0 {
			w.slots[uint64(slot)] = struct{}{}
		}
	}
	s.accelerate.Store(w)
	telemetry.SetAccelerateWindow(w.toSlot, res.GeneratedAtMs)
}
