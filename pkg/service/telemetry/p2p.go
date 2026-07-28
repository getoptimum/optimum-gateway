package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var p2pActiveTopics prometheus.Gauge

func initP2PMetrics() {
	p2pActiveTopics = commonmetrics.NewGauge(
		"active_topics",
		"p2p",
		"Number of active topics subscribed to",
	)
}

func SetP2PActiveTopics(count int) {
	if MetricsEnabled() {
		p2pActiveTopics.Set(float64(count))
	}
}
