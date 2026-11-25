package tracer_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commontelemetry "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
)

const topicKey = "topic"

func TestPeerTopicTracerHandleMumP2PTrace(t *testing.T) {
	reg := prometheus.NewRegistry()
	l := logger.NewAppSLogger(logger.Debug)
	commontelemetry.SetLabeledRegistry(reg, "testns")
	telemetry.InitMetricsWithRegistry(l, "test")

	beaconTopic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	attTopic := "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy"

	require.NoError(t, tracer.HandleMumP2PTrace(nil))
	require.NoError(t, tracer.HandleMumP2PTrace(&tracepb.TraceEvent{}))
	require.NoError(t, tracer.HandleMumP2PTrace(&tracepb.TraceEvent{
		Event: &tracepb.TraceEvent_EncodeError{EncodeError: &tracepb.EncodeError{}},
	}))
	require.Zero(t, metricValue(t, reg, "testns_mump2p_trace_messages_total", map[string]string{
		"event":  "publish",
		topicKey: "beacon_block",
	}))

	cases := []struct {
		name   string
		raw    *tracepb.TraceEvent
		checks []metricExpectation
	}{
		{
			name: "publish message",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_PublishMessage{PublishMessage: &tracepb.PublishMessage{Topic: beaconTopic}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_messages_total", labels: map[string]string{
					"event":  "publish",
					topicKey: "beacon_block",
				}, value: 1},
			},
		},
		{
			name: "reject message with empty reason",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_RejectMessage{RejectMessage: &tracepb.RejectMessage{Topic: attTopic}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_messages_total", labels: map[string]string{
					"event":  "reject",
					topicKey: "beacon_attestation",
				}, value: 1},
				{name: "testns_mump2p_trace_message_rejects_total", labels: map[string]string{
					topicKey: "beacon_attestation",
					"reason": "unknown",
				}, value: 1},
			},
		},
		{
			name: "helpful symbol",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_HelpfulSymbol{HelpfulSymbol: &tracepb.SymbolContainer{Coefficients: []byte{1, 2, 3, 4}}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_shards_total", labels: map[string]string{"kind": "new"}, value: 1},
				{name: "testns_mump2p_trace_shard_bytes_total", labels: map[string]string{"kind": "new"}, value: 4},
			},
		},
		{
			name: "recv rpc",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_RecvRpc{RecvRpc: &tracepb.RecvRPC{Length: 17}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_rpc_total", labels: map[string]string{"direction": "recv"}, value: 1},
				{name: "testns_mump2p_trace_rpc_bytes_total", labels: map[string]string{"direction": "recv"}, value: 17},
			},
		},
		{
			name: "drop rpc",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_DropRpc{DropRpc: &tracepb.DropRPC{}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_rpc_total", labels: map[string]string{"direction": "drop"}, value: 1},
			},
		},
		{
			name: "join mesh topic",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_Join{Join: &tracepb.Join{Topic: attTopic}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_mesh_total", labels: map[string]string{
					"event":  "join",
					topicKey: "beacon_attestation",
				}, value: 1},
			},
		},
		{
			name: "add peer mesh event",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_AddPeer{AddPeer: &tracepb.AddPeer{}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_mesh_total", labels: map[string]string{
					"event":  "add_peer",
					topicKey: "none",
				}, value: 1},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tracer.HandleMumP2PTrace(tc.raw))
			for _, check := range tc.checks {
				require.Equal(t, check.value, metricValue(t, reg, check.name, check.labels))
			}
		})
	}
}

type metricExpectation struct {
	name   string
	labels map[string]string
	value  float64
}

func metricValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if hasLabels(metric.GetLabel(), labels) {
				switch {
				case metric.Counter != nil:
					return metric.GetCounter().GetValue()
				case metric.Gauge != nil:
					return metric.GetGauge().GetValue()
				}
			}
		}
	}
	return 0
}

func hasLabels(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	for _, pair := range pairs {
		if got, ok := want[pair.GetName()]; !ok || got != pair.GetValue() {
			return false
		}
	}
	return true
}
