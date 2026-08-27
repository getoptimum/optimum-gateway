package message_router

import (
	"context"
	"fmt"
	"net/http"
	"time"

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

// ShouldAccelerateBlock is ADR-0012: accelerate unless the slot was examined and
// not selected. Header slot, not the clock. No list / past to_slot fail-opens.
func (s *Service) ShouldAccelerateBlock(slot uint64) bool {
	decision := decideAccelerate(s.accelerate.Load(), slot)
	telemetry.IncAccelerateDecision(decision)
	return decision != "not_on_list"
}

func decideAccelerate(w *accelerateWindow, slot uint64) string {
	if w == nil || w.toSlot == 0 || slot > w.toSlot {
		return "fail_open"
	}
	if _, ok := w.slots[slot]; ok {
		return "on_list"
	}
	return "not_on_list"
}

// RefreshAccelerateSlots runs one poll and swaps the whole window. A failed poll keeps the previous one.
func (s *Service) RefreshAccelerateSlots(ctx context.Context) {
	chainID := s.authMgr.Chain()
	if chainID == "" || s.cfg.RemoteBootstrapURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var headers map[string]string
	if tok, err := s.authMgr.ServicesToken(ctx); err == nil && tok != "" {
		headers = map[string]string{"Authorization": "Bearer " + tok}
	}
	res, code, err := commonnet.GetCurl[accelerateSlotsResponse](ctx, utils.BootstrapAccelerateSlotsURL(s.cfg.RemoteBootstrapURL, chainID), headers)
	if err != nil {
		s.log.Error("accelerate_slots poll failed, keeping previous list", err)
		return
	}
	if code != http.StatusOK || res == nil {
		s.log.Error("accelerate_slots poll failed, keeping previous list", fmt.Errorf("status code: %d", code))
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
