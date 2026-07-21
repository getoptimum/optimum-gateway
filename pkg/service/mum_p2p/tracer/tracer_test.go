package tracer_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	commontelemetry "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p/tracer"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	pboptimum "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

func TestTracerBroadcastsEnabledEventsOnly(t *testing.T) {
	broadcaster := syncx.NewBroadcaster[*entities.MumP2PResponse]()
	listener := broadcaster.RegisterListener("trace-listener")
	t.Cleanup(func() {
		broadcaster.UnregisterListener("trace-listener")
	})

	tracerMumP2P := tracer.NewTracerMumP2P(
		broadcaster,
		map[pboptimum.TraceEvent_Type]struct{}{
			pboptimum.TraceEvent_NEW_SHARD: {},
		},
	)

	newShard := &pboptimum.TraceEvent{
		Type: pboptimum.TraceEvent_NEW_SHARD.Enum(),
		NewShard: &pboptimum.TraceEvent_ShardContainer{
			MessageID: []byte("message-1"),
		},
	}
	go tracerMumP2P.Trace(newShard)

	select {
	case evt := <-listener:
		require.Equal(t, entities.MumP2PCommandTraceMumP2P, evt.Command)
		require.Equal(t, newShard, evt.TraceEvent)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for enabled trace fan-out")
	}

	tracerMumP2P.Trace(&pboptimum.TraceEvent{
		Type: pboptimum.TraceEvent_RECV_RPC.Enum(),
		RecvRPC: &pboptimum.TraceEvent_RecvRPC{
			Length: new(uint64),
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
		map[pboptimum.TraceEvent_Type]struct{}{
			pboptimum.TraceEvent_NEW_SHARD: {},
		},
	)

	tracerMumP2P.Trace(nil)
	tracerMumP2P.Trace(&pboptimum.TraceEvent{})

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
		map[pboptimum.TraceEvent_Type]struct{}{},
	)

	shardTypes := map[pboptimum.TraceEvent_Type]string{
		pboptimum.TraceEvent_NEW_SHARD:         "testns_mump2p_shards_total",
		pboptimum.TraceEvent_DUPLICATE_SHARD:   "testns_mump2p_shards_duplicate_total",
		pboptimum.TraceEvent_UNNECESSARY_SHARD: "testns_mump2p_shards_unnecessary_total",
		pboptimum.TraceEvent_UNHELPFUL_SHARD:   "testns_mump2p_shards_unhelpful_total",
	}

	for evtType, metricName := range shardTypes {
		tracerMumP2P.Trace(&pboptimum.TraceEvent{Type: evtType.Enum()})
		require.Equal(t, float64(1), counterValue(t, reg, metricName),
			"expected %s to increment for %s", metricName, evtType)
	}
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
