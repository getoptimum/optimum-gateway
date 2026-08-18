package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	streamEventsDropped prometheus.Counter
	streamEventsSent    prometheus.Counter
	streamAuthFailures  prometheus.Counter
	streamConnections   prometheus.Gauge
)

func initStreamMetrics() {
	streamEventsDropped = commonmetrics.NewCounter(
		"events_dropped_total",
		"stream",
		"Consumer block-stream events dropped due to a full subscriber buffer",
	)
	streamEventsSent = commonmetrics.NewCounter(
		"events_sent_total",
		"stream",
		"Consumer block-stream events written to a subscriber connection",
	)
	streamAuthFailures = commonmetrics.NewCounter(
		"auth_failures_total",
		"stream",
		"Consumer stream connections rejected because authentication failed",
	)
	streamConnections = commonmetrics.NewGauge(
		"connections",
		"stream",
		"Currently open consumer stream connections",
	)
}

// RecordStreamEventDropped counts one dropped consumer event.
func RecordStreamEventDropped() {
	if enabledMetrics {
		streamEventsDropped.Inc()
	}
}

// RecordStreamEventSent counts one consumer event written to a connection.
func RecordStreamEventSent() {
	if enabledMetrics {
		streamEventsSent.Inc()
	}
}

// RecordStreamAuthFailure counts one rejected consumer connection.
func RecordStreamAuthFailure() {
	if enabledMetrics {
		streamAuthFailures.Inc()
	}
}

// IncStreamConnections marks a consumer connection as opened.
func IncStreamConnections() {
	if enabledMetrics {
		streamConnections.Inc()
	}
}

// DecStreamConnections marks a consumer connection as closed.
func DecStreamConnections() {
	if enabledMetrics {
		streamConnections.Dec()
	}
}
