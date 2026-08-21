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

func TestExportResolve(t *testing.T) {
	s := newExportTestService()
	s.exportPend[5] = &pendingExport{version: 2, nextAt: time.Now()}
	s.exportPend[7] = &pendingExport{version: 2, nextAt: time.Now()}
	s.exportPend[9] = &pendingExport{version: 1, nextAt: time.Now()}
	s.exportPend[11] = &pendingExport{version: 1, nextAt: time.Now()}
	s.exportPend[13] = &pendingExport{version: 1, nextAt: time.Now()}

	before := time.Now()
	s.exportResolve(5, 1, exportOutcomeSuccess, 200, nil)  // stale success must keep the newer update
	s.exportResolve(7, 1, exportOutcomeTerminal, 400, nil) // stale terminal must not drop the newer update
	s.exportResolve(9, 1, exportOutcomeTransient, 521, nil)
	s.exportResolve(11, 1, exportOutcomeTerminal, 400, nil)
	s.exportResolve(13, 1, exportOutcomeExpired, 0, nil)

	pe, ok := s.exportPend[5]
	require.True(t, ok)
	require.Equal(t, 0, pe.attempt)
	s.exportResolve(5, 2, exportOutcomeSuccess, 200, nil)
	_, ok = s.exportPend[5]
	require.False(t, ok)

	_, ok = s.exportPend[7]
	require.True(t, ok)

	pe, ok = s.exportPend[9]
	require.True(t, ok)
	require.Equal(t, 1, pe.attempt)
	require.True(t, pe.nextAt.After(before))

	_, ok = s.exportPend[11]
	require.False(t, ok)
	_, ok = s.exportPend[13]
	require.False(t, ok)
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
