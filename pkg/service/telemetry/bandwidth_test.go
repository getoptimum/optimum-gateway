package telemetry

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"
)

func TestBandwidthCollector(t *testing.T) {
	reg := initTestMetricsRegistry(t, initBandwidthMetrics)
	collector := NewBandwidthCollector()
	require.NotNil(t, collector)

	const protoID = protocol.ID("/optimum/test/1.0.0")

	collector.LogSentMessageStream(42, protoID, peer.ID("peer-a"))
	collector.LogRecvMessageStream(84, protoID, peer.ID("peer-b"))

	require.Equal(t, float64(42), metricByLabels(t, reg,
		testMetricsNamespace+"_bandwidth_total_bytes",
		map[string]string{labelDirection: "outgoing"},
	).GetCounter().GetValue())
	require.Equal(t, float64(84), metricByLabels(t, reg,
		testMetricsNamespace+"_bandwidth_total_bytes",
		map[string]string{labelDirection: "incoming"},
	).GetCounter().GetValue())

	require.Equal(t, float64(42), metricByLabels(t, reg,
		testMetricsNamespace+"_bandwidth_traffic_bytes_total",
		map[string]string{labelProtocol: string(protoID), labelDirection: "outgoing"},
	).GetCounter().GetValue())
	require.Equal(t, float64(84), metricByLabels(t, reg,
		testMetricsNamespace+"_bandwidth_traffic_bytes_total",
		map[string]string{labelProtocol: string(protoID), labelDirection: "incoming"},
	).GetCounter().GetValue())

	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_bandwidth_messages_total",
		map[string]string{labelProtocol: string(protoID), labelDirection: "outgoing"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_bandwidth_messages_total",
		map[string]string{labelProtocol: string(protoID), labelDirection: "incoming"},
	).GetCounter().GetValue())

	collector.LogSentMessage(1)
	collector.LogRecvMessage(1)
	collector.Reset()
	collector.TrimIdle(time.Now())

	require.Equal(t, metrics.Stats{}, collector.GetBandwidthForPeer(peer.ID("peer-a")))
	require.Equal(t, metrics.Stats{}, collector.GetBandwidthForProtocol(protoID))
	require.Equal(t, metrics.Stats{}, collector.GetBandwidthTotals())
	require.Nil(t, collector.GetBandwidthByPeer())
	require.Nil(t, collector.GetBandwidthByProtocol())
}
