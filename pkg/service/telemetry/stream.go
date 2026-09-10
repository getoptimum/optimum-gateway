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
	streamOldestStart   prometheus.Gauge
	streamHeartbeats    prometheus.Counter
	streamReauthFailure prometheus.Counter
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
		"Consumer stream tokens rejected, at connect or on an in-band refresh",
	)
	streamConnections = commonmetrics.NewGauge(
		"connections",
		"stream",
		"Currently open consumer stream connections",
	)
	streamHeartbeats = commonmetrics.NewCounter(
		"heartbeats_sent_total",
		"stream",
		"Consumer block-stream liveness frames written to a subscriber connection",
	)
	streamReauthFailure = commonmetrics.NewCounter(
		"reauth_failures_total",
		"stream",
		"Consumer stream re-verifications that failed, meaning the presented token stopped verifying",
	)
	streamOldestStart = commonmetrics.NewGauge(
		"oldest_connection_started_seconds",
		"stream",
		"Unix start time of the longest-running consumer stream connection, 0 when none are open",
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

// SetStreamOldestConnectionStart publishes when the longest-running consumer
// connection opened. A timestamp rather than an age so the value stays correct
// between scrapes; query it as time() - <this>. Zero means nothing is
// connected. It bounds how long a connection has been trusted on the strength
// of a single subscribe-time authentication.
func SetStreamOldestConnectionStart(unixSeconds float64) {
	if enabledMetrics {
		streamOldestStart.Set(unixSeconds)
	}
}

// RecordStreamHeartbeatSent counts one liveness frame.
func RecordStreamHeartbeatSent() {
	if enabledMetrics {
		streamHeartbeats.Inc()
	}
}

// RecordStreamReauthFailure counts one failed re-verification. In observe mode
// this is the only signal that a consumer would have been cut.
func RecordStreamReauthFailure() {
	if enabledMetrics {
		streamReauthFailure.Inc()
	}
}
