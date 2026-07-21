package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	// aggregationMessagesTotal is count of messages by direction for aggregation topic
	aggregationMessagesTotal *prometheus.CounterVec

	// aggregationMessageSizeBytes histogram of message size in bytes (derive avg/quantiles in PromQL)
	aggregationMessageSizeBytes *prometheus.HistogramVec
)

func initAggregationMetrics() {
	aggregationMessagesTotal = commonmetrics.NewCounterVec(
		"aggregation_messages_total",
		subsystem,
		"Total number of messages seen by the aggregation topic",
		[]string{labelDirection}, // add more labels if needed (instance, gateway_id, etc.)
	)
	aggregationMessageSizeBytes = commonmetrics.NewHistogramWithBuckets(
		"aggregation_message_size_bytes",
		subsystem,
		"Distribution of message sizes in bytes across aggregation topic",
		[]string{labelDirection},
		prometheus.ExponentialBuckets(64, 1.5, 18),
	)
}

func IncAggregationMessage(direction string, sizeBytes int) {
	if !enabledMetrics || aggregationMessagesTotal == nil || aggregationMessageSizeBytes == nil {
		return
	}
	aggregationMessagesTotal.WithLabelValues(direction).Inc()
	aggregationMessageSizeBytes.WithLabelValues(direction).Observe(float64(sizeBytes))
}
