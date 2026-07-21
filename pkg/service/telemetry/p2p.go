package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	p2pMessagesPublished *prometheus.CounterVec
	p2pMessageSize       *prometheus.HistogramVec
	p2pActiveTopics      prometheus.Gauge
	p2pPublishErrors     *prometheus.CounterVec
)

func initP2PMetrics() {
	p2pMessagesPublished = commonmetrics.NewCounterVec(
		"messages_published_total",
		"p2p",
		"Total number of messages published by the P2P node",
		[]string{labelTopic},
	)
	p2pMessageSize = commonmetrics.NewHistogram(
		"message_size_bytes",
		"p2p",
		"Histogram of published message sizes",
		[]string{labelTopic},
	)
	p2pActiveTopics = commonmetrics.NewGauge(
		"active_topics",
		"p2p",
		"Number of active topics subscribed to",
	)
	p2pPublishErrors = commonmetrics.NewCounterVec(
		"publish_errors_total",
		"p2p",
		"Total number of publish errors",
		[]string{"topic", "error_type"},
	)
}

func IncP2PMessagesPublished(topic string) {
	if MetricsEnabled() {
		p2pMessagesPublished.WithLabelValues(topic).Inc()
	}
}

func ObserveP2PMessageSize(topic string, size int) {
	if MetricsEnabled() {
		p2pMessageSize.WithLabelValues(topic).Observe(float64(size))
	}
}

func SetP2PActiveTopics(count int) {
	if MetricsEnabled() {
		p2pActiveTopics.Set(float64(count))
	}
}

func IncP2PPublishError(topic, errType string) {
	if MetricsEnabled() {
		p2pPublishErrors.WithLabelValues(topic, errType).Inc()
	}
}
