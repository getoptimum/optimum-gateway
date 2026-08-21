package telemetry

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

// Block-latency export outcomes (issue #90). Kept as a small, fixed label set so
// success rate, retry pressure, and drop reasons are visible without parsing logs.
const (
	ExportResultSuccess   = "success"         // delivered (2xx), possibly after retries
	ExportResultTransient = "transient_retry" // attempt failed with a retryable error; rescheduled
	ExportResultTerminal  = "terminal_drop"   // non-retryable response (e.g. 4xx); dropped
	ExportResultExpired   = "expired"         // slot left the retention window before delivery
	ExportResultOverflow  = "overflow"        // evicted because the pending set was full
)

var (
	blockLatencyExportTotal          *prometheus.CounterVec
	blockLatencyExportTransientTotal *prometheus.CounterVec
)

func initBootstrapMetrics() {
	blockLatencyExportTotal = commonmetrics.NewCounterVec(
		"block_latency_export_total",
		"bootstrap",
		"Block-latency telemetry export outcomes to the bootstrap service",
		[]string{"result"},
	)
	blockLatencyExportTransientTotal = commonmetrics.NewCounterVec(
		"block_latency_export_transient_total",
		"bootstrap",
		"Retryable block-latency export failures by HTTP status (code 0 = transport failure)",
		[]string{"code"},
	)
}

// RecordBlockLatencyExport records one export outcome by result class.
func RecordBlockLatencyExport(result string) {
	if !enabledMetrics || blockLatencyExportTotal == nil {
		return
	}
	blockLatencyExportTotal.WithLabelValues(result).Inc()
}

// RecordBlockLatencyExportTransientCode records the HTTP status of a retryable
// failure so timeouts (code 0), 521, and 5xx can be told apart.
func RecordBlockLatencyExportTransientCode(code int) {
	if !enabledMetrics || blockLatencyExportTransientTotal == nil {
		return
	}
	blockLatencyExportTransientTotal.WithLabelValues(strconv.Itoa(code)).Inc()
}
