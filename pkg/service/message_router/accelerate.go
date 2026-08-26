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

const (
	accelerateFailOpen  = "fail_open"
	accelerateOnList    = "on_list"
	accelerateNotOnList = "not_on_list"

	acceleratePollTimeout = 5 * time.Second
)

// accelerateWindow is one atomic snapshot of bootstrap's accelerate_slots body.
type accelerateWindow struct {
	toSlot        uint64
	slots         map[uint64]struct{}
	generatedAtMs int64
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

func (s *Service) pollAccelerateSlots(ctx context.Context) {
	chainID := s.authMgr.Chain()
	if chainID == "" || s.cfg.RemoteBootstrapURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, acceleratePollTimeout)
	defer cancel()

	var headers map[string]string
	if tok, err := s.authMgr.ServicesToken(ctx); err == nil && tok != "" {
		headers = map[string]string{"Authorization": "Bearer " + tok}
	}
	res, code, err := commonnet.GetCurl[accelerateSlotsResponse](ctx, utils.BootstrapAccelerateSlotsURL(s.cfg.RemoteBootstrapURL, chainID), headers)
	if err != nil || code != http.StatusOK || res == nil {
		s.log.Error("accelerate_slots poll failed, keeping previous list", fmt.Errorf("status code: %d, error: %w", code, err))
		return
	}
	s.storeAccelerateWindow(res)
}

func (s *Service) storeAccelerateWindow(res *accelerateSlotsResponse) {
	toSlot := uint64(0)
	if res.ToSlot > 0 {
		toSlot = uint64(res.ToSlot)
	}
	slots := make(map[uint64]struct{}, len(res.Slots))
	for _, slot := range res.Slots {
		if slot >= 0 {
			slots[uint64(slot)] = struct{}{}
		}
	}
	s.accelerate.Store(&accelerateWindow{
		toSlot:        toSlot,
		slots:         slots,
		generatedAtMs: res.GeneratedAtMs,
	})
	telemetry.SetAccelerateWindow(toSlot, len(slots), res.GeneratedAtMs)
}
