package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	accelerateDecisionTotal *prometheus.CounterVec
	accelerateToSlot        prometheus.Gauge
	accelerateSlotsLen      prometheus.Gauge
	accelerateGeneratedAtMs prometheus.Gauge
)

func initAccelerateMetrics() {
	accelerateDecisionTotal = commonmetrics.NewCounterVec(
		"accelerate_decision_total",
		subsystem,
		"Beacon-block acceleration verdicts (ADR-0012): on_list, not_on_list, fail_open",
		[]string{"result"},
	)
	accelerateToSlot = commonmetrics.NewGauge(
		"accelerate_to_slot",
		subsystem,
		"Last successfully polled accelerate_slots horizon (0 = not covered)",
	)
	accelerateSlotsLen = commonmetrics.NewGauge(
		"accelerate_slots_len",
		subsystem,
		"Number of slots in the last successfully polled accelerate_slots list",
	)
	accelerateGeneratedAtMs = commonmetrics.NewGauge(
		"accelerate_generated_at_ms",
		subsystem,
		"generated_at_ms from the last successfully polled accelerate_slots body",
	)
}

func IncAccelerateDecision(result string) {
	if enabledMetrics {
		accelerateDecisionTotal.WithLabelValues(result).Inc()
	}
}

func SetAccelerateWindow(toSlot uint64, slotsLen int, generatedAtMs int64) {
	if enabledMetrics {
		accelerateToSlot.Set(float64(toSlot))
		accelerateSlotsLen.Set(float64(slotsLen))
		accelerateGeneratedAtMs.Set(float64(generatedAtMs))
	}
}
