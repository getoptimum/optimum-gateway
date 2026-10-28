package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var rlncExecutionDuration *prometheus.HistogramVec

func initRLNCMetrics() {
	rlncExecutionDuration = commonmetrics.NewHistogramWithBuckets(
		"rlnc_execution_duration_seconds",
		subsystem,
		"Execution duration of RLNC functions",
		[]string{"function"},
		prometheus.ExponentialBuckets(0.0001, 2, 18),
	)
}

func MeasureRLNC(operation string, duration time.Duration) {
	if !enabledMetrics {
		return
	}
	rlncExecutionDuration.WithLabelValues(operation).Observe(duration.Seconds())
}
