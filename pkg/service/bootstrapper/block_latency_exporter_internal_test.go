package bootstrapper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

func init() {
	exportRetryWait = 50 * time.Millisecond
}

func newExportTestService() *Service {
	return &Service{
		log:        logger.NewAppSLogger(logger.Error, logger.WithService("bootstrapper-test")),
		resendList: syncx.NewRWSlice[entities.LatencyComparator](),
	}
}

func TestIsRetryableExportCode(t *testing.T) {
	for _, code := range []int{0, 408, 429, 500, 502, 503, 504, 521} {
		require.Truef(t, isRetryableExportCode(code), "code %d should be retryable", code)
	}
	for _, code := range []int{400, 401, 403, 404, 409, 422} {
		require.Falsef(t, isRetryableExportCode(code), "code %d should be terminal", code)
	}
}

func TestTakeResendBatchKeepsNewest(t *testing.T) {
	old := exportMaxPending
	exportMaxPending = 4
	defer func() { exportMaxPending = old }()

	s := newExportTestService()
	for slot := uint64(1); slot <= 10; slot++ {
		s.resendList.Add(entities.LatencyComparator{BlockSlot: slot})
	}

	got := s.takeResendBatch()
	require.Len(t, got, 4)
	require.Equal(t, uint64(7), got[0].BlockSlot)
	require.Equal(t, uint64(10), got[3].BlockSlot)
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
