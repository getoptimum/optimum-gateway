package telemetry

import (
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	protocoltelemetry "github.com/getoptimum/mump2p-protocol/pkg/telemetry"
)

func TestSetRegistererPutsProtocolMetricsOnGatewayRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	labeled := prometheus.WrapRegistererWith(prometheus.Labels{
		"gateway_id":         "gw-1",
		"gateway_cluster_id": "optimum_hoodi_v0_2",
	}, reg)
	protocoltelemetry.SetRegisterer(labeled)
	t.Cleanup(func() {
		if labeledRegistry != nil {
			protocoltelemetry.SetRegisterer(labeledRegistry)
			return
		}
		protocoltelemetry.SetRegisterer(nil)
	})

	cfg := mump2pcfg.DefaultGossipSubConfig()
	cfg.MetricsAddr = ""
	_, err := protocoltelemetry.NewMetrics("node-1", "peer-1", cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	families, err := reg.Gather()
	require.NoError(t, err)

	var found bool
	for _, family := range families {
		if family.GetName() != "mump2p_node_up_info" {
			continue
		}
		found = true
		require.NotEmpty(t, family.GetMetric())
		labels := map[string]string{}
		for _, lp := range family.GetMetric()[0].GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		require.Equal(t, "gw-1", labels["gateway_id"])
		require.Equal(t, "optimum_hoodi_v0_2", labels["gateway_cluster_id"])
		require.Equal(t, float64(1), family.GetMetric()[0].GetGauge().GetValue())
	}
	require.True(t, found)
}
