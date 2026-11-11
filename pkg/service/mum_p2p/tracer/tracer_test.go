package tracer_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	commontelemetry "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p/tracer"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

func TestTracerBroadcastsEnabledEventsOnly(t *testing.T) {
	broadcaster := syncx.NewBroadcaster[*entities.MumP2PResponse]()
	listener := broadcaster.RegisterListener("trace-listener")
	t.Cleanup(func() {
		broadcaster.UnregisterListener("trace-listener")
	})

	tracerMumP2P := tracer.NewTracerMumP2P(
		broadcaster,
		map[entities.MumP2PTraceEventKind]struct{}{
			entities.MumP2PTraceEventHelpfulSymbol: {},
		},
	)

	helpfulSymbol := &tracepb.TraceEvent{
		Event: &tracepb.TraceEvent_HelpfulSymbol{
			HelpfulSymbol: &tracepb.SymbolContainer{
				MessageId: []byte("message-1"),
			},
		},
	}
	go tracerMumP2P.Trace(helpfulSymbol)

	select {
	case evt := <-listener:
		require.Equal(t, entities.MumP2PCommandTraceMumP2P, evt.Command)
		require.Equal(t, helpfulSymbol, evt.TraceEvent)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for enabled trace fan-out")
	}

	tracerMumP2P.Trace(&tracepb.TraceEvent{
		Event: &tracepb.TraceEvent_RecvRpc{
			RecvRpc: &tracepb.RecvRPC{},
		},
	})

	select {
	case <-listener:
		t.Fatal("disabled trace event should not be broadcast")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTracerIgnoresNilEvents(t *testing.T) {
	broadcaster := syncx.NewBroadcaster[*entities.MumP2PResponse]()
	listener := broadcaster.RegisterListener("trace-listener")
	t.Cleanup(func() {
		broadcaster.UnregisterListener("trace-listener")
	})

	tracerMumP2P := tracer.NewTracerMumP2P(
		broadcaster,
		map[entities.MumP2PTraceEventKind]struct{}{
			entities.MumP2PTraceEventHelpfulSymbol: {},
		},
	)

	tracerMumP2P.Trace(nil)
	tracerMumP2P.Trace(&tracepb.TraceEvent{})

	select {
	case <-listener:
		t.Fatal("nil trace events should not be broadcast")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTracerIncrementsShardMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	commontelemetry.SetLabeledRegistry(reg, "testns")
	telemetry.InitMetricsWithRegistry(logger.NewAppSLogger(logger.Debug), "test")

	tracerMumP2P := tracer.NewTracerMumP2P(
		syncx.NewBroadcaster[*entities.MumP2PResponse](),
		map[entities.MumP2PTraceEventKind]struct{}{},
	)

	shardEvents := []struct {
		name           string
		evt            *tracepb.TraceEvent
		specificMetric string
	}{
		{
			name: "helpful",
			evt: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_HelpfulSymbol{},
			},
		},
		{
			name: "redundant",
			evt: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_RedundantSymbol{},
			},
			specificMetric: "testns_mump2p_shards_duplicate_total",
		},
		{
			name: "unnecessary",
			evt: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_UnnecessarySymbol{},
			},
			specificMetric: "testns_mump2p_shards_unnecessary_total",
		},
		{
			name: "inconsistent",
			evt: &tracepb.TraceEvent{
				Event: &tracepb.TraceEvent_InconsistentSymbol{},
			},
			specificMetric: "testns_mump2p_shards_unhelpful_total",
		},
	}

	for _, tc := range shardEvents {
		t.Run(tc.name, func(t *testing.T) {
			tracerMumP2P.Trace(tc.evt)
			if tc.specificMetric != "" {
				require.Equal(t, float64(1), counterValue(t, reg, tc.specificMetric),
					"expected %s to increment", tc.specificMetric)
			}
		})
	}

	require.Equal(t, float64(len(shardEvents)), counterValue(t, reg, "testns_mump2p_shards_total"))
}

func counterValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if metric.Counter != nil {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}
