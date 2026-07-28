package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestP2PMetricsHelpers(t *testing.T) {
	reg := initTestMetricsRegistry(t, initP2PMetrics)

	SetP2PActiveTopics(3)

	require.Equal(t, float64(3), metricByLabels(t, reg,
		testMetricsNamespace+"_p2p_active_topics",
		nil,
	).GetGauge().GetValue())
}
