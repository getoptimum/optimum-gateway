package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var streamEventsDropped prometheus.Counter

func initStreamMetrics() {
	streamEventsDropped = commonmetrics.NewCounter(
		"events_dropped_total",
		"stream",
		"Consumer block-stream events dropped due to ring-buffer overflow",
	)
}

// RecordStreamEventDropped counts one dropped consumer event.
func RecordStreamEventDropped() {
	if enabledMetrics {
		streamEventsDropped.Inc()
	}
}
