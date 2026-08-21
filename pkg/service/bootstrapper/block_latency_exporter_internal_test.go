package bootstrapper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

func newExportTestService() *Service {
	return &Service{
		log:        logger.NewAppSLogger(logger.Error, logger.WithService("bootstrapper-test")),
		exportPend: make(map[uint64]*pendingExport),
		exportWake: make(chan struct{}, 1),
	}
}

func TestIsRetryableExportCode(t *testing.T) {
	// Transport failures (code 0), 408/429, Cloudflare 521, and any 5xx are transient.
	for _, code := range []int{0, 408, 429, 500, 502, 503, 504, 521} {
		require.Truef(t, isRetryableExportCode(code), "code %d should be retryable", code)
	}
	// Terminal 4xx must not be retried.
	for _, code := range []int{400, 401, 403, 404, 409, 422} {
		require.Falsef(t, isRetryableExportCode(code), "code %d should be terminal", code)
	}
}

func TestExportBackoffIsCapped(t *testing.T) {
	maxWithJitter := exportMaxBackoff + exportMaxBackoff/2
	for attempt := 1; attempt <= 20; attempt++ {
		d := exportBackoff(attempt)
		require.GreaterOrEqual(t, d, exportBaseBackoff)
		require.LessOrEqual(t, d, maxWithJitter)
	}
}

func TestExportPendingSetIsBounded(t *testing.T) {
	old := exportMaxPending
	exportMaxPending = 4
	defer func() { exportMaxPending = old }()

	s := newExportTestService()
	for slot := uint64(1); slot <= 10; slot++ {
		s.enqueueBlockLatencyExport(slot)
	}

	s.exportMu.Lock()
	defer s.exportMu.Unlock()
	require.LessOrEqual(t, len(s.exportPend), exportMaxPending)

	// Oldest (lowest) slots are evicted; the newest slot is retained.
	_, hasNewest := s.exportPend[10]
	require.True(t, hasNewest, "newest slot must be retained")
	_, hasOldest := s.exportPend[1]
	require.False(t, hasOldest, "oldest slot must be evicted under pressure")
}

func TestExportResolveGenerationGuard(t *testing.T) {
	s := newExportTestService()
	const slot = uint64(5)
	s.exportPend[slot] = &pendingExport{version: 1, nextAt: time.Now()}

	// A newer update lands (version bumped) while a version-1 attempt is in flight.
	s.exportPend[slot].version = 2

	// Success for the stale version 1 must NOT drop the newer update.
	s.exportResolve(slot, 1, exportOutcomeSuccess, 200, nil)
	pe, ok := s.exportPend[slot]
	require.True(t, ok, "stale success must not discard a newer update")
	require.Equal(t, 0, pe.attempt, "newer update should be rescheduled promptly")

	// Success for the current version 2 clears the slot.
	s.exportResolve(slot, 2, exportOutcomeSuccess, 200, nil)
	_, ok = s.exportPend[slot]
	require.False(t, ok, "success for the current version must clear the slot")
}

func TestExportResolveTransientReschedulesWithBackoff(t *testing.T) {
	s := newExportTestService()
	const slot = uint64(9)
	s.exportPend[slot] = &pendingExport{version: 1, nextAt: time.Now()}

	before := time.Now()
	s.exportResolve(slot, 1, exportOutcomeTransient, 521, nil)

	pe, ok := s.exportPend[slot]
	require.True(t, ok, "transient failure must keep the slot for retry")
	require.Equal(t, 1, pe.attempt)
	require.True(t, pe.nextAt.After(before), "retry must be scheduled in the future")
}

func TestExportResolveTerminalDropsSlot(t *testing.T) {
	s := newExportTestService()
	const slot = uint64(11)
	s.exportPend[slot] = &pendingExport{version: 1, nextAt: time.Now()}

	s.exportResolve(slot, 1, exportOutcomeTerminal, 400, nil)
	_, ok := s.exportPend[slot]
	require.False(t, ok, "terminal response must drop the slot")
}

func TestExportResolveExpiredDropsSlot(t *testing.T) {
	s := newExportTestService()
	const slot = uint64(13)
	s.exportPend[slot] = &pendingExport{version: 1, nextAt: time.Now()}

	s.exportResolve(slot, 1, exportOutcomeExpired, 0, nil)
	_, ok := s.exportPend[slot]
	require.False(t, ok, "expiry must drop the slot")
}

func TestRunBlockLatencyExporterStopsPromptlyOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newExportTestService()
	s.ctx = ctx

	done := make(chan struct{})
	go func() {
		s.runBlockLatencyExporter()
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exporter did not stop promptly on context cancellation")
	}
}
