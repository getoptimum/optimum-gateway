package bootstrapper

import (
	"context"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	commonslices "github.com/getoptimum/optimum-common/pkg/slices"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// Snapshots sit on resendList and are flushed once per exportRetryWait so the
// forwarding path never waits on bootstrap. Tunables are vars so tests can
// shorten them.
var (
	exportMaxPending = 256
	exportChunkSize  = 25
	exportReqTimeout = 5 * time.Second
	exportRetryWait  = time.Minute
)

func (s *Service) enqueueBlockLatencyExport(slot uint64) {
	v, ok := s.trackedSlots.Get(slot)
	if !ok || v == nil {
		return
	}
	s.resendList.Add(*v)
}

func (s *Service) runBlockLatencyExporter() {
	t := time.NewTicker(exportRetryWait)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.exportSend()
		}
	}
}

func (s *Service) exportSend() {
	items := s.takeResendBatch()
	if len(items) == 0 {
		return
	}
	url := utils.BootstrapHandleBlockLatencyBulkURL(s.cfg.RemoteBootstrapURL)
	chunks := commonslices.ChunkSlice(items, exportChunkSize)
	for i, chunk := range chunks {
		ctx, cancel := context.WithTimeout(s.ctx, exportReqTimeout)
		_, code, err := commonnet.PostCurl[any](ctx, url, chunk, s.bearerAuthHeader(ctx))
		cancel()
		switch {
		case utils.IsPostSuccess(code):
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultSuccess)
		case isRetryableExportCode(code):
			s.requeue(items[i*exportChunkSize:])
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultTransient)
			telemetry.RecordBlockLatencyExportTransientCode(code)
			s.log.Debug("block latency bulk export failed, will retry", logger.WithInt("n", len(chunk)), logger.WithInt("code", code), logger.WithAny("err", err))
			return
		default:
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultTerminal)
			s.log.Debug("block latency bulk export dropped (non-retryable response)", logger.WithInt("n", len(chunk)), logger.WithInt("code", code), logger.WithAny("err", err))
		}
	}
}

func (s *Service) takeResendBatch() []entities.LatencyComparator {
	items := s.resendList.LoadAndErase()
	if len(items) > exportMaxPending {
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultOverflow)
		return commonslices.KeepLast(items, exportMaxPending)
	}
	return items
}

// requeue puts failed items in front of anything that arrived during the POST
// so later snapshots for the same slot still win at bootstrap.
func (s *Service) requeue(failed []entities.LatencyComparator) {
	newer := s.resendList.LoadAndErase()
	s.resendList.AddBulk(append(append([]entities.LatencyComparator{}, failed...), newer...))
}

func isRetryableExportCode(code int) bool {
	return code == 0 || code == 408 || code == 429 || code == 521 || (code >= 500 && code <= 599)
}
