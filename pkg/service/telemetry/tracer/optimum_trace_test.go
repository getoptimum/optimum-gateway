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

func TestPeerTopicTracerHandleMumP2PTrace(t *testing.T) {
	reg := prometheus.NewRegistry()
	l := logger.NewAppSLogger(logger.Debug)
	commontelemetry.SetLabeledRegistry(reg, "testns")
	telemetry.InitMetricsWithRegistry(l, "test")

	beaconTopic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	attTopic := "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy"

	const (
		labelEvent    = "event"
		labelTopic    = "topic"
		labelKind     = "kind"
		classBlock    = "beacon_block"
		classAtt      = "beacon_attestation"
		metricMessage = "testns_mump2p_trace_messages_total"
		metricShards  = "testns_mump2p_trace_shards_total"
		metricMesh    = "testns_mump2p_trace_mesh_total"
	)

	require.NoError(t, tracer.HandleMumP2PTrace(nil))
	require.NoError(t, tracer.HandleMumP2PTrace(&tracepb.TraceEvent{}))

	cases := []struct {
		name   string
		raw    *tracepb.TraceEvent
		checks []metricExpectation
	}{
		{
			name: "publish message",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_PublishMessage{
					PublishMessage: &tracepb.PublishMessage{Topic: beaconTopic},
				},
			},
			checks: []metricExpectation{
				{name: metricMessage, labels: map[string]string{labelEvent: "publish", labelTopic: classBlock}, value: 1},
			},
		},
		{
			name: "deliver message",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_DeliverMessage{
					DeliverMessage: &tracepb.DeliverMessage{Topic: beaconTopic},
				},
			},
			checks: []metricExpectation{
				{name: metricMessage, labels: map[string]string{labelEvent: "deliver", labelTopic: classBlock}, value: 1},
			},
		},
		{
			name: "duplicate message",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_DuplicateMessage{
					DuplicateMessage: &tracepb.DuplicateMessage{Topic: beaconTopic},
				},
			},
			checks: []metricExpectation{
				{name: metricMessage, labels: map[string]string{labelEvent: "duplicate", labelTopic: classBlock}, value: 1},
			},
		},
		{
			name: "reject message with empty reason",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_RejectMessage{
					RejectMessage: &tracepb.RejectMessage{Topic: attTopic},
				},
			},
			checks: []metricExpectation{
				{name: metricMessage, labels: map[string]string{labelEvent: "reject", labelTopic: classAtt}, value: 1},
				{name: "testns_mump2p_trace_message_rejects_total", labels: map[string]string{labelTopic: classAtt, "reason": "unknown"}, value: 1},
			},
		},
		{
			name: "helpful symbol",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_HelpfulSymbol{
					HelpfulSymbol: &tracepb.SymbolContainer{Coefficients: []byte{1, 2, 3, 4}},
				},
			},
			checks: []metricExpectation{
				{name: metricShards, labels: map[string]string{labelKind: "new"}, value: 1},
				{name: "testns_mump2p_trace_shard_bytes_total", labels: map[string]string{labelKind: "new"}, value: 4},
			},
		},
		{
			name: "redundant symbol",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_RedundantSymbol{
					RedundantSymbol: &tracepb.SymbolContainer{Coefficients: []byte{1, 2}},
				},
			},
			checks: []metricExpectation{
				{name: metricShards, labels: map[string]string{labelKind: "duplicate"}, value: 1},
				{name: "testns_mump2p_trace_shard_bytes_total", labels: map[string]string{labelKind: "duplicate"}, value: 2},
			},
		},
		{
			name: "inconsistent symbol",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_InconsistentSymbol{
					InconsistentSymbol: &tracepb.SymbolContainer{Coefficients: []byte{9}},
				},
			},
			checks: []metricExpectation{
				{name: metricShards, labels: map[string]string{labelKind: "unhelpful"}, value: 1},
			},
		},
		{
			name: "unnecessary symbol",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_UnnecessarySymbol{
					UnnecessarySymbol: &tracepb.SymbolContainer{Coefficients: []byte{9}},
				},
			},
			checks: []metricExpectation{
				{name: metricShards, labels: map[string]string{labelKind: "unnecessary"}, value: 1},
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
			name: "send rpc",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_SendRpc{SendRpc: &tracepb.SendRPC{Length: 3}},
			},
			checks: []metricExpectation{
				{name: "testns_mump2p_trace_rpc_total", labels: map[string]string{"direction": "send"}, value: 1},
				{name: "testns_mump2p_trace_rpc_bytes_total", labels: map[string]string{"direction": "send"}, value: 3},
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
				{name: metricMesh, labels: map[string]string{labelEvent: "join", labelTopic: classAtt}, value: 1},
			},
		},
		{
			name: "leave mesh topic",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_Leave{Leave: &tracepb.Leave{Topic: attTopic}},
			},
			checks: []metricExpectation{
				{name: metricMesh, labels: map[string]string{labelEvent: "leave", labelTopic: classAtt}, value: 1},
			},
		},
		{
			name: "graft mesh topic",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_Graft{Graft: &tracepb.Graft{Topic: attTopic}},
			},
			checks: []metricExpectation{
				{name: metricMesh, labels: map[string]string{labelEvent: "graft", labelTopic: classAtt}, value: 1},
			},
		},
		{
			name: "prune mesh topic",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_Prune{Prune: &tracepb.Prune{Topic: attTopic}},
			},
			checks: []metricExpectation{
				{name: metricMesh, labels: map[string]string{labelEvent: "prune", labelTopic: classAtt}, value: 1},
			},
		},
		{
			name: "add peer mesh event",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_AddPeer{AddPeer: &tracepb.AddPeer{}},
			},
			checks: []metricExpectation{
				{name: metricMesh, labels: map[string]string{labelEvent: "add_peer", labelTopic: "none"}, value: 1},
			},
		},
		{
			name: "remove peer mesh event",
			raw: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_RemovePeer{RemovePeer: &tracepb.RemovePeer{}},
			},
			checks: []metricExpectation{
				{name: metricMesh, labels: map[string]string{labelEvent: "remove_peer", labelTopic: "none"}, value: 1},
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
