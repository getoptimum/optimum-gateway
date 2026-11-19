package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeerConnectionGaugesAreLabeledAndSetCurrentState(t *testing.T) {
	reg := initTestMetricsRegistry(t, initPeersMetrics)

	SetCLPeerConnection("cl-peer-a", true)
	SetCLPeerConnection("cl-peer-a", false)
	SetCLPeerConnection("cl-peer-b", true)
	SetMumP2PPeerConnection("mesh-peer-a", true)
	SetMumP2PPeerConnection("mesh-peer-a", false)

	name := testMetricsNamespace + "_" + testMetricsSubsystem + "_"
	require.Equal(t, float64(0), metricByLabels(t, reg, name+"cl_peer_connection", map[string]string{"peer_id": "cl-peer-a"}).GetGauge().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg, name+"cl_peer_connection", map[string]string{"peer_id": "cl-peer-b"}).GetGauge().GetValue())
	require.Equal(t, float64(0), metricByLabels(t, reg, name+"mump2p_peer_connection", map[string]string{"peer_id": "mesh-peer-a"}).GetGauge().GetValue())
}
