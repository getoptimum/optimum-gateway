package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestP2PMetricsHelpers(t *testing.T) {
	reg := initTestMetricsRegistry(t, initP2PMetrics)
	topic := "beacon_block"

	IncP2PMessagesPublished(topic)
	ObserveP2PMessageSize(topic, 256)
	SetP2PActiveTopics(3)
	IncP2PPublishError(topic, "timeout")

	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_p2p_messages_published_total",
		map[string]string{labelTopic: topic},
	).GetCounter().GetValue())
	require.Equal(t, uint64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_p2p_message_size_bytes",
		map[string]string{labelTopic: topic},
	).GetHistogram().GetSampleCount())
	require.Equal(t, float64(256), metricByLabels(t, reg,
		testMetricsNamespace+"_p2p_message_size_bytes",
		map[string]string{labelTopic: topic},
	).GetHistogram().GetSampleSum())
	require.Equal(t, float64(3), metricByLabels(t, reg,
		testMetricsNamespace+"_p2p_active_topics",
		nil,
	).GetGauge().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_p2p_publish_errors_total",
		map[string]string{labelTopic: topic, "error_type": "timeout"},
	).GetCounter().GetValue())
}
