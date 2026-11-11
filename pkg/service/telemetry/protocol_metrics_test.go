package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestProtocolMetricGathererStampsGatewayLabels(t *testing.T) {
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "mump2p_gateway_wire_test_total",
		Help: "test series for the protocol gatherer",
	})
	require.NoError(t, prometheus.DefaultRegisterer.Register(counter))
	t.Cleanup(func() {
		prometheus.DefaultRegisterer.Unregister(counter)
	})
	counter.Inc()

	other := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gateway_not_protocol_total",
		Help: "must stay off the protocol gatherer",
	})
	require.NoError(t, prometheus.DefaultRegisterer.Register(other))
	t.Cleanup(func() {
		prometheus.DefaultRegisterer.Unregister(other)
	})

	prev := protocolIdentityLabels.Load()
	t.Cleanup(func() { protocolIdentityLabels.Store(prev) })
	SetProtocolIdentityLabels(prometheus.Labels{
		"gateway_id":         "gw-1",
		"gateway_cluster_id": "optimum_hoodi_v0_2",
	})

	families, err := (protocolMetricGatherer{}).Gather()
	require.NoError(t, err)

	var found bool
	for _, family := range families {
		require.NotEqual(t, "gateway_not_protocol_total", family.GetName())
		if family.GetName() != "mump2p_gateway_wire_test_total" {
			continue
		}
		found = true
		require.Len(t, family.GetMetric(), 1)
		labels := map[string]string{}
		for _, lp := range family.GetMetric()[0].GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		require.Equal(t, "gw-1", labels["gateway_id"])
		require.Equal(t, "optimum_hoodi_v0_2", labels["gateway_cluster_id"])
		require.Equal(t, float64(1), family.GetMetric()[0].GetCounter().GetValue())
	}
	require.True(t, found)
}
