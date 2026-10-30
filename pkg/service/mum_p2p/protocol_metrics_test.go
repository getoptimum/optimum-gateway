package mum_p2p

import (
	"log/slog"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	protocoltelemetry "github.com/getoptimum/mump2p-protocol/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

func TestEnableProtocolMetricsRecordsPartialControl(t *testing.T) {
	psCfg := toMumP2PConfig()
	metrics, err := newProtocolMetrics(psCfg, "optimum_test", "peer-1", slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.Empty(t, psCfg.ClusterID, "metric identity must not rewrite the live config")

	protocoltelemetry.NewTraceCollector(metrics).Trace(&tracepb.TraceEvent{Event: &tracepb.TraceEvent_PartialControl{
		PartialControl: &tracepb.PartialControl{
			Topic:         "/mump2p/a",
			Kind:          "basis",
			Outcome:       "coalesced",
			QueueDepth:    17,
			QueueCapacity: 512,
		},
	}})

	families, err := telemetry.MetricsGatherer().Gather()
	require.NoError(t, err)

	var control float64
	var depth float64
	var sawControl bool
	var sawDepth bool
	for _, family := range families {
		switch family.GetName() {
		case "mump2p_rlnc_partial_control_total":
			for _, metric := range family.GetMetric() {
				labels := labelMap(metric.GetLabel())
				if labels["topic"] == "/mump2p/a" && labels["kind"] == "basis" && labels["outcome"] == "coalesced" {
					control = metric.GetCounter().GetValue()
					sawControl = true
				}
			}
		case "mump2p_rlnc_partial_control_queue_depth":
			require.NotEmpty(t, family.GetMetric())
			depth = family.GetMetric()[0].GetGauge().GetValue()
			sawDepth = true
		}
	}
	require.True(t, sawControl)
	require.GreaterOrEqual(t, control, float64(1))
	require.True(t, sawDepth)
	require.Equal(t, float64(17), depth)
}

func labelMap(labels []*dto.LabelPair) map[string]string {
	out := make(map[string]string, len(labels))
	for _, lp := range labels {
		out[lp.GetName()] = lp.GetValue()
	}
	return out
}
