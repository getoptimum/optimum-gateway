package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

// processingTime records how fast our proxy function process message from CL or MUMP2P
var processingTime *prometheus.HistogramVec

func initProcessingSpeedMetrics() {
	processingTime = commonmetrics.NewHistogramWithBuckets(
		"processing_time_nanoseconds",
		subsystem,
		"Processing duration of messages in nanoseconds",
		[]string{"process"},
		prometheus.ExponentialBuckets(1e5, 2, 10), // 100µs to ~51ms in nanoseconds
	)
}

func RecordProcessingTime(process string, duration time.Duration) {
	if enabledMetrics {
		processingTime.WithLabelValues(process).Observe(float64(duration.Nanoseconds()))
	}
}
