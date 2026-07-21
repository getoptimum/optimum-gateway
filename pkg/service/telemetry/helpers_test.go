package telemetry

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

const (
	testMetricsNamespace = "testns"
	testMetricsSubsystem = "telemetrytest"
	testGatewayID        = "test-gw"
	testGatewayClusterID = "test-cluster"
)

func initTestMetricsRegistry(t *testing.T, initFns ...func()) *prometheus.Registry {
	t.Helper()

	prevRegistry := CustomRegistry
	prevLabeledRegistry := labeledRegistry
	prevNamespace := namespace
	prevSubsystem := subsystem
	prevEnabled := enabledMetrics
	prevBadMsgToMum := atomic.LoadUint64(&badMsgToMum)
	prevBadMsgToCL := atomic.LoadUint64(&badMsgToCL)
	prevLastCL := lastCLMessageAt.Load()
	prevLastMum := lastMumMessageAt.Load()
	prevCLWorking := clWorking.Load()
	prevMumWorking := mumWorking.Load()

	reg := prometheus.NewRegistry()
	CustomRegistry = reg
	labeledRegistry = reg
	subsystem = testMetricsSubsystem
	namespace = testMetricsNamespace
	commonmetrics.SetLabeledRegistry(reg, testMetricsNamespace)
	enabledMetrics = false

	atomic.StoreUint64(&badMsgToMum, 0)
	atomic.StoreUint64(&badMsgToCL, 0)
	lastCLMessageAt.Store(0)
	lastMumMessageAt.Store(0)
	clWorking.Store(0)
	mumWorking.Store(0)

	for _, initFn := range initFns {
		initFn()
	}
	enabledMetrics = true

	t.Cleanup(func() {
		CustomRegistry = prevRegistry
		labeledRegistry = prevLabeledRegistry
		namespace = prevNamespace
		subsystem = prevSubsystem
		enabledMetrics = prevEnabled
		atomic.StoreUint64(&badMsgToMum, prevBadMsgToMum)
		atomic.StoreUint64(&badMsgToCL, prevBadMsgToCL)
		lastCLMessageAt.Store(prevLastCL)
		lastMumMessageAt.Store(prevLastMum)
		clWorking.Store(prevCLWorking)
		mumWorking.Store(prevMumWorking)
		if prevLabeledRegistry != nil {
			commonmetrics.SetLabeledRegistry(prevLabeledRegistry, prevNamespace)
		}
	})

	return reg
}

func metricFamilyByName(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	t.Fatalf("metric family %q not found", name)
	return nil
}

func metricByLabels(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) *dto.Metric {
	t.Helper()

	family := metricFamilyByName(t, reg, name)
	for _, metric := range family.Metric {
		matches := true
		for k, v := range labels {
			found := false
			for _, label := range metric.Label {
				if label.GetName() == k && label.GetValue() == v {
					found = true
					break
				}
			}
			if !found {
				matches = false
				break
			}
		}
		if matches {
			return metric
		}
	}

	t.Fatalf("metric %q with labels %v not found", name, labels)
	return nil
}

func TestMessageAndAuthMetricsHelpers(t *testing.T) {
	reg := initTestMetricsRegistry(t, initMessageSizeMetrics, initAuthMetrics)

	IncreaseBadMessagesToMum()
	IncreaseBadMessagesToCL()
	IncAuthMintResult(AuthMintResultSuccess)
	SetAuthTokenExpiresAt(12345)

	require.Equal(t, uint64(1), GetBadMessagesToMum())
	require.Equal(t, uint64(1), GetBadMessagesToCL())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_bad_messages_to_cl_total",
		map[string]string{labelDirection: "mum"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_bad_messages_to_cl_total",
		map[string]string{labelDirection: "cl"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_auth_token_mint_total",
		map[string]string{"result": AuthMintResultSuccess},
	).GetCounter().GetValue())
	require.Equal(t, float64(12345), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_auth_token_expires_at_seconds",
		nil,
	).GetGauge().GetValue())
}

func TestAggregationAndAttestationMetricsHelpers(t *testing.T) {
	reg := initTestMetricsRegistry(t, initAggregationMetrics, initAttestationMetrics)

	IncAggregationMessage("outbound", 512)
	IncAttestationPack(3)
	IncAttestationPackError()
	ObserveAttestationPackSize(1024)
	ObserveAttestationPackItems(2)
	ObserveAttestationPackUniqueDataKeys(5)
	IncAttestationSubnet(7)
	IncAttestationEvaluated()
	IncAttestationForwarded()
	IncAttestationDropped("expired")
	ObserveAttestationInclusionDelay(4)
	ObserveAttestationPackLatency(3.5)
	ObserveAttestationPropagationLatency(0)
	ObserveAttestationPropagationLatency(12.5)

	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_aggregation_messages_total",
		map[string]string{labelDirection: "outbound"},
	).GetCounter().GetValue())
	require.Equal(t, uint64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_aggregation_message_size_bytes",
		map[string]string{labelDirection: "outbound"},
	).GetHistogram().GetSampleCount())
	require.Equal(t, float64(512), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_aggregation_message_size_bytes",
		map[string]string{labelDirection: "outbound"},
	).GetHistogram().GetSampleSum())

	require.Equal(t, float64(3), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_pack_total",
		nil,
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_pack_errors_total",
		nil,
	).GetCounter().GetValue())
	require.Equal(t, uint64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_pack_size_bytes",
		nil,
	).GetHistogram().GetSampleCount())
	require.Equal(t, float64(1024), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_pack_size_bytes",
		nil,
	).GetHistogram().GetSampleSum())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_subnet_messages_total",
		map[string]string{"subnet": "7"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_evaluated_total",
		nil,
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_forwarded_mump2p_total",
		nil,
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_dropped_total",
		map[string]string{"reason": "expired"},
	).GetCounter().GetValue())
	require.Equal(t, uint64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_inclusion_delay_slots",
		nil,
	).GetHistogram().GetSampleCount())
	require.Equal(t, uint64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_pack_latency_ms",
		nil,
	).GetHistogram().GetSampleCount())
	propagation := metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_attestation_propagation_latency_ms",
		nil,
	).GetHistogram()
	require.Equal(t, uint64(1), propagation.GetSampleCount(), "zero or negative latency must not be recorded")
	require.Equal(t, float64(12.5), propagation.GetSampleSum())
}

func TestGatewayStateHelpers(t *testing.T) {
	enabledMetrics = true
	lastCLMessageAt.Store(0)
	lastMumMessageAt.Store(0)

	RecordCLMessageAt()
	RecordMumMessageAt()

	require.WithinDuration(t, time.Now(), time.Unix(lastCLMessageAt.Load(), 0), 2*time.Second)
	require.WithinDuration(t, time.Now(), time.Unix(lastMumMessageAt.Load(), 0), 2*time.Second)
	require.GreaterOrEqual(t, HealthCL(), int64(0))
	require.GreaterOrEqual(t, HealthMUM(), int64(0))
}

func TestTraceHelpers(t *testing.T) {
	reg := initTestMetricsRegistry(t, initMump2pTraceMetrics)
	topic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"

	TraceMessage("publish", topic)
	TraceMessageReject(topic, "")
	TraceShard("new", 4)
	TraceRPC("recv", 17)
	TraceRPC("drop", 0)
	TraceMesh("add_peer", "")

	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_messages_total",
		map[string]string{"event": "publish", labelTopic: "beacon_block"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_message_rejects_total",
		map[string]string{labelTopic: "beacon_block", "reason": "unknown"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_shards_total",
		map[string]string{"kind": "new"},
	).GetCounter().GetValue())
	require.Equal(t, float64(4), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_shard_bytes_total",
		map[string]string{"kind": "new"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_rpc_total",
		map[string]string{labelDirection: "recv"},
	).GetCounter().GetValue())
	require.Equal(t, float64(17), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_rpc_bytes_total",
		map[string]string{labelDirection: "recv"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_rpc_total",
		map[string]string{labelDirection: "drop"},
	).GetCounter().GetValue())
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_"+testMetricsSubsystem+"_mump2p_trace_mesh_total",
		map[string]string{"event": "add_peer", labelTopic: "none"},
	).GetCounter().GetValue())
}
