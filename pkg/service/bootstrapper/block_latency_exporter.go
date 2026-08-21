package bootstrapper

import (
	"context"
	"net/http"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	commonrand "github.com/getoptimum/optimum-common/pkg/rand"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// Block-latency export retry scheduler: bounded, coalesced per-slot
// retry off the forwarding path. Tunables are vars so tests can shorten them.
var (
	exportMaxPending  = 256
	exportReqTimeout  = 5 * time.Second
	exportBaseBackoff = 1 * time.Second
	exportMaxBackoff  = 30 * time.Second
)

type exportOutcome int

const (
	exportOutcomeSuccess exportOutcome = iota
	exportOutcomeTransient
	exportOutcomeTerminal
	exportOutcomeExpired
)

// pendingExport is the retry state for one slot. version bumps on every enqueue
// so a stale in-flight attempt can never discard a newer update for the slot.
type pendingExport struct {
	version uint64
	attempt int
	nextAt  time.Time
}

// enqueueBlockLatencyExport coalesces a slot into the pending set without doing
// any network work. Safe to call from the forwarding path.
func (s *Service) enqueueBlockLatencyExport(slot uint64) {
	now := time.Now()
	s.exportMu.Lock()
	if pe, ok := s.exportPend[slot]; ok {
		// New data for a slot we're already tracking: bump version and retry now.
		pe.version++
		pe.attempt = 0
		pe.nextAt = now
	} else {
		if len(s.exportPend) >= exportMaxPending {
			s.exportEvictOldestLocked()
		}
		s.exportPend[slot] = &pendingExport{version: 1, nextAt: now}
	}
	s.exportMu.Unlock()
	s.exportSignal()
}

// exportEvictOldestLocked drops the oldest (lowest) slot to keep the pending set
// bounded. Slots are monotonic, so the lowest is the least useful to retry.
func (s *Service) exportEvictOldestLocked() {
	var oldest uint64
	found := false
	for slot := range s.exportPend {
		if !found || slot < oldest {
			oldest, found = slot, true
		}
	}
	if found {
		delete(s.exportPend, oldest)
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultOverflow)
	}
}

func (s *Service) exportSignal() {
	select {
	case s.exportWake <- struct{}{}:
	default:
	}
}

// runBlockLatencyExporter is the single worker. It sends at most one request at
// a time and returns promptly on context cancellation.
func (s *Service) runBlockLatencyExporter() {
	for {
		s.exportMu.Lock()
		slot, ready, wait := s.exportNextLocked()
		s.exportMu.Unlock()

		if ready {
			s.exportSend(slot)
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-s.exportWake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// exportNextLocked returns the earliest-due slot (ready) or, if none is due, how
// long to wait for the next one (a long idle wait when nothing is pending).
func (s *Service) exportNextLocked() (slot uint64, ready bool, wait time.Duration) {
	var best uint64
	var bestAt time.Time
	found := false
	for sl, pe := range s.exportPend {
		if !found || pe.nextAt.Before(bestAt) || (pe.nextAt.Equal(bestAt) && sl < best) {
			best, bestAt, found = sl, pe.nextAt, true
		}
	}
	if !found {
		return 0, false, time.Hour // nothing pending; wait for a wake signal
	}
	if d := time.Until(bestAt); d > 0 {
		return 0, false, d
	}
	return best, true, 0
}

// exportSend snapshots the newest comparator for slot and posts it once.
func (s *Service) exportSend(slot uint64) {
	s.exportMu.Lock()
	pe, ok := s.exportPend[slot]
	if !ok {
		s.exportMu.Unlock()
		return
	}
	startVersion := pe.version
	s.exportMu.Unlock()

	// DoAndApply holds the trackedSlots lock during fn, so we copy the latest
	// merged value without racing composeBlockTelemetry.
	var data entities.LatencyComparator
	present := s.trackedSlots.DoAndApply(slot, func(v *entities.LatencyComparator) *entities.LatencyComparator {
		data = *v
		return v
	})
	if !present {
		s.exportResolve(slot, startVersion, exportOutcomeExpired, 0, nil)
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, exportReqTimeout)
	url := utils.BootstrapHandleBlockLatencyURL(s.cfg.RemoteBootstrapURL)
	_, code, err := commonnet.PostCurl[any](ctx, url, data, s.bearerAuthHeader(ctx))
	cancel()

	switch {
	case utils.IsPostSuccess(code):
		s.exportResolve(slot, startVersion, exportOutcomeSuccess, code, err)
	case isRetryableExportCode(code):
		s.exportResolve(slot, startVersion, exportOutcomeTransient, code, err)
	default:
		s.exportResolve(slot, startVersion, exportOutcomeTerminal, code, err)
	}
}

// exportResolve applies the attempt result to the pending set (with the version
// guard) and records telemetry.
func (s *Service) exportResolve(slot, startVersion uint64, outcome exportOutcome, code int, err error) {
	now := time.Now()

	s.exportMu.Lock()
	pe, ok := s.exportPend[slot]
	if !ok {
		s.exportMu.Unlock()
		return
	}
	newer := pe.version != startVersion
	switch {
	case newer:
		// Newer update arrived mid-flight; retry it now regardless of this outcome.
		pe.attempt = 0
		pe.nextAt = now
	case outcome == exportOutcomeTransient:
		pe.attempt++
		pe.nextAt = now.Add(exportBackoff(pe.attempt))
	default:
		// success, terminal, or expired for the current version: stop tracking.
		delete(s.exportPend, slot)
	}
	s.exportMu.Unlock()

	switch outcome {
	case exportOutcomeSuccess:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultSuccess)
	case exportOutcomeTerminal:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultTerminal)
		s.exportLogDrop("block latency export dropped (non-retryable response)", slot, code, err)
	case exportOutcomeExpired:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultExpired)
	case exportOutcomeTransient:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultTransient)
		telemetry.RecordBlockLatencyExportTransientCode(code)
		s.exportLogDrop("block latency export failed, will retry", slot, code, err)
	}

	if newer {
		s.exportSignal()
	}
}

func (s *Service) exportLogDrop(msg string, slot uint64, code int, err error) {
	fields := []logger.Field{logger.WithUint64("slot", slot), logger.WithInt("code", code)}
	if err != nil {
		fields = append(fields, logger.WithError(err))
	}
	s.log.Debug(msg, fields...)
}

// isRetryableExportCode treats transport failures (code 0), 408/429, Cloudflare
// 521, and any 5xx as transient. Terminal 4xx are not retried.
func isRetryableExportCode(code int) bool {
	switch {
	case code == 0:
		return true
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests:
		return true
	case code == 521: // Cloudflare "Web Server Is Down"
		return true
	case code >= 500 && code <= 599:
		return true
	default:
		return false
	}
}

// exportBackoff is capped exponential backoff with up to 50% jitter.
func exportBackoff(attempt int) time.Duration {
	d := exportMaxBackoff
	if attempt >= 1 && attempt <= 8 {
		if shifted := exportBaseBackoff << (attempt - 1); shifted > 0 && shifted < exportMaxBackoff {
			d = shifted
		}
	}
	if half := int(d / 2); half > 0 {
		if j, err := commonrand.RandBetween(0, half); err == nil {
			d += time.Duration(j)
		}
	}
	return d
}
