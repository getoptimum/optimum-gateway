package bootstrapper

import (
	"context"
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
			oldest := ^uint64(0)
			for sl := range s.exportPend {
				if sl < oldest {
					oldest = sl
				}
			}
			delete(s.exportPend, oldest)
			telemetry.RecordBlockLatencyExport(telemetry.ExportResultOverflow)
		}
		s.exportPend[slot] = &pendingExport{version: 1, nextAt: now}
	}
	s.exportMu.Unlock()
	s.exportSignal()
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
		if !found || pe.nextAt.Before(bestAt) {
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

	outcome := exportOutcomeTerminal
	switch {
	case utils.IsPostSuccess(code):
		outcome = exportOutcomeSuccess
	case isRetryableExportCode(code):
		outcome = exportOutcomeTransient
	}
	s.exportResolve(slot, startVersion, outcome, code, err)
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

	// A stale in-flight result must not look like a drop (or a finish) of the live slot.
	if newer {
		s.exportSignal()
		return
	}

	switch outcome {
	case exportOutcomeSuccess:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultSuccess)
	case exportOutcomeTerminal:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultTerminal)
		s.log.Debug("block latency export dropped (non-retryable response)", logger.WithUint64("slot", slot), logger.WithInt("code", code), logger.WithAny("err", err))
	case exportOutcomeExpired:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultExpired)
	case exportOutcomeTransient:
		telemetry.RecordBlockLatencyExport(telemetry.ExportResultTransient)
		telemetry.RecordBlockLatencyExportTransientCode(code)
		s.log.Debug("block latency export failed, will retry", logger.WithUint64("slot", slot), logger.WithInt("code", code), logger.WithAny("err", err))
	}
}

// isRetryableExportCode treats transport failures (code 0), 408/429, Cloudflare
// 521, and any 5xx as transient. Terminal 4xx are not retried.
func isRetryableExportCode(code int) bool {
	return code == 0 || code == 408 || code == 429 || code == 521 || (code >= 500 && code <= 599)
}

func exportBackoff(attempt int) time.Duration {
	d := min(exportBaseBackoff<<min(max(attempt-1, 0), 8), exportMaxBackoff)
	if j, err := commonrand.RandBetween(0, int(d/2)); err == nil {
		d += time.Duration(j)
	}
	return d
}
