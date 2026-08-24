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

// Failed block-latency posts are queued and retried as bulk uploads so the
// forwarding path never waits on bootstrap. Tunables are vars so tests can
// shorten them.
var (
	exportMaxPending = 256
	exportChunkSize  = 25
	exportReqTimeout = 5 * time.Second
	exportRetryWait  = time.Minute
)

func (s *Service) enqueueBlockLatencyExport(slot uint64) {
	var snap *entities.LatencyComparator
	ok := s.trackedSlots.DoAndApply(slot, func(v *entities.LatencyComparator) *entities.LatencyComparator {
		cp := *v
		snap = &cp
		return v
	})
	if !ok || snap == nil {
		return
	}
	s.resendList.Add(snap)
	select {
	case s.exportWake <- struct{}{}:
	default:
	}
}

func (s *Service) runBlockLatencyExporter() {
	for {
		if s.resendList.Len() == 0 {
			select {
			case <-s.ctx.Done():
				return
			case <-s.exportWake:
			}
			continue
		}
		retry := s.exportSend()
		if s.ctx.Err() != nil {
			return
		}
		if !retry {
			continue
		}
		timer := time.NewTimer(exportRetryWait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) exportSend() (retry bool) {
	items := s.takeResendBatch()
	if len(items) == 0 {
		return false
	}
	chunks := commonslices.ChunkSlice(items, exportChunkSize)
	url := utils.BootstrapHandleBlockLatencyBulkURL(s.cfg.RemoteBootstrapURL)
	for i, chunk := range chunks {
		ctx, cancel := context.WithTimeout(s.ctx, exportReqTimeout)
		_, code, err := commonnet.PostCurl[any](ctx, url, derefComparators(chunk), s.bearerAuthHeader(ctx))
		cancel()
		switch {
		case utils.IsPostSuccess(code):
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultSuccess)
		case isRetryableExportCode(code):
			s.requeue(flattenChunks(chunks[i:]))
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultTransient)
			telemetry.RecordBlockLatencyExportTransientCode(code)
			s.log.Debug("block latency bulk export failed, will retry", logger.WithInt("n", len(chunk)), logger.WithInt("code", code), logger.WithAny("err", err))
			return true
		default:
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultTerminal)
			s.log.Debug("block latency bulk export dropped (non-retryable response)", logger.WithInt("n", len(chunk)), logger.WithInt("code", code), logger.WithAny("err", err))
		}
	}
	return false
}

func (s *Service) takeResendBatch() []*entities.LatencyComparator {
	items := s.resendList.LoadAndErase()
	if len(items) > exportMaxPending {
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultOverflow)
		return commonslices.KeepLast(items, exportMaxPending)
	}
	return items
}

// requeue puts failed items in front of anything that arrived during the POST
// so later snapshots for the same slot still win at bootstrap.
func (s *Service) requeue(failed []*entities.LatencyComparator) {
	newer := s.resendList.LoadAndErase()
	s.resendList.AddBulk(append(failed, newer...))
}

func flattenChunks(chunks [][]*entities.LatencyComparator) []*entities.LatencyComparator {
	var out []*entities.LatencyComparator
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}

func derefComparators(in []*entities.LatencyComparator) []entities.LatencyComparator {
	out := make([]entities.LatencyComparator, 0, len(in))
	for _, p := range in {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out
}

func isRetryableExportCode(code int) bool {
	return code == 0 || code == 408 || code == 429 || code == 521 || (code >= 500 && code <= 599)
}
