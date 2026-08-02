package mump2p_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/stretchr/testify/require"
	coltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	mp2ppb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	oteltracer "github.com/getoptimum/mump2p-protocol/pkg/telemetry/otel_tracer"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commontest "github.com/getoptimum/optimum-common/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// nodeIDAttr is the resource attribute the netsim span parser keys every
// per-node result on. Two gateways sharing one value silently collapse into a
// single node in every downstream delivery and latency number.
const nodeIDAttr = "mump2p.node_id"

// TestOTelSpansCarryAUniqueNodeIDPerGateway drives a real publish between two
// gateway nodes and asserts on the bytes an OTLP collector receives.
//
// The decisive assertion is that the two gateways, identical but for their
// gateway id, export DIFFERENT mump2p.node_id values. The parser's
// service.instance.id fallback does not rescue a shared node id, because the
// attribute is present, just not distinguishing.
func TestOTelSpansCarryAUniqueNodeIDPerGateway(t *testing.T) {
	cnt := test_utils.GetClean(t)
	sink := newOTelSink(t)
	const clusterID = "bench-cluster"

	publisher, stopPublisher := otelNode(t, cnt, sink, clusterID, "bench-gw-0")
	subscriberWatcher := watchNode(t, func() mump2p.Engine {
		node, _ := otelNode(t, cnt, sink, clusterID, "bench-gw-1")
		return node
	})

	joinMesh(cnt.Ctx, t, publisher, subscriberWatcher.node)
	require.Equal(t, 1, deliverPayloads(cnt.Ctx, t, publisher, subscriberWatcher, randomPayloads(1)))

	// Both sides flush: the publish spans come from one node and the decode spans
	// from the other, and an unflushed exporter loses the tail of every run.
	stopPublisher()
	subscriberWatcher.node.Stop()

	batches := sink.received()
	require.NotEmpty(t, batches, "the collector must have received spans")

	ids := nodeIDs(batches)
	require.Equal(t, []string{"bench-gw-0", "bench-gw-1"}, ids,
		"each gateway must export its own node id")
	require.NotContains(t, ids, clusterID, "the cluster id is fleet-wide and must never be the node id")

	names := spanNames(batches)
	require.Contains(t, names, "rlnc.publish")
	require.Contains(t, names, "rlnc.decode")
	require.Contains(t, names, "rlnc.symbol.recv.helpful")

	requireIntChunkID(t, batches)
}

// TestOTelExportedSpanShape covers the span names and attribute types the netsim
// parser matches on, including rlnc.symbol.recode, which two nodes on the
// passthrough coder cannot produce (recoding needs rank >= 2).
func TestOTelExportedSpanShape(t *testing.T) {
	sink := newOTelSink(t)

	cfg := mp2pconfig.DefaultGossipSubConfig()
	cfg.ID = "bench-gw-7"
	cfg.ClusterID = "bench-cluster"
	cfg.OTelConfig = mp2pconfig.OTelConfig{
		Enable:      true,
		Endpoint:    sink.endpoint(),
		Insecure:    true,
		SampleRatio: 1,
	}

	tracer, shutdown, err := oteltracer.NewProvider(t.Context(), cfg, "peer-7")
	require.NoError(t, err)
	require.NotNil(t, tracer)

	const msgID = "generation-1"
	symbolID := []byte("symbol-a")
	tracer.Trace(publishTraceEvent(msgID, symbolID))
	tracer.Trace(helpfulTraceEvent(msgID, 3, symbolID))
	tracer.Trace(recodeTraceEvent(msgID, 3, []byte("symbol-b"), symbolID))
	tracer.Trace(chunkDecodedTraceEvent(msgID, 3))

	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shCtx))

	batches := sink.received()
	names := spanNames(batches)
	for _, want := range []string{"rlnc.publish", "rlnc.symbol.recv.helpful", "rlnc.symbol.recode", "rlnc.decode"} {
		require.Contains(t, names, want)
	}
	require.Equal(t, []string{"bench-gw-7"}, nodeIDs(batches))
	requireIntChunkID(t, batches)
}

// TestOTelDisabledExportsNothing pins the opt-in: an unconfigured gateway must
// not reach out to a collector at all.
func TestOTelDisabledExportsNothing(t *testing.T) {
	cnt := test_utils.GetClean(t)
	sink := newOTelSink(t)

	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, "otel_off", commontest.GetFreePortT(t), nil)
	cfg.GatewayID = "bench-gw-0"
	cfg.OTelEndpoint = sink.endpoint()

	node := test_utils.NewTestNodeWithCfg(cnt.Ctx, t, cnt.Log, t.TempDir(), cfg)
	require.NoError(t, node.PublishMessage(cnt.Ctx, e2eTopic, []byte("payload")))
	node.Stop()

	require.Empty(t, sink.received())
}

// TestOTelEnabledWithoutEndpointFailsStartup pins the loud-failure contract:
// tracing that is asked for and cannot run must stop the node, never come up
// quietly exporting nothing.
func TestOTelEnabledWithoutEndpointFailsStartup(t *testing.T) {
	cnt := test_utils.GetClean(t)

	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, "otel_no_endpoint", commontest.GetFreePortT(t), nil)
	cfg.OTelEnable = true

	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	_, err = mump2p.NewNodeWithHost(cnt.Ctx, cnt.Log, cfg, h, t.TempDir(), test_utils.TestNodeOptions()...)
	require.ErrorContains(t, err, "otel.endpoint is empty")
}

// otelNode builds a gateway node exporting to sink. It registers no Stop cleanup
// of its own beyond a guard, so the test decides when the final flush happens.
func otelNode(
	t *testing.T,
	cnt *test_utils.Container,
	sink *otelSink,
	clusterID, gatewayID string,
) (node *mump2p.Node, stop func()) {
	t.Helper()

	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, commontest.GetFreePortT(t), nil)
	cfg.GatewayID = gatewayID
	cfg.OTelEnable = true
	cfg.OTelEndpoint = sink.endpoint()
	cfg.OTelInsecure = true
	cfg.OTelSampleRatio = 1

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", commontest.GetFreePortT(t))),
	)
	require.NoError(t, err)

	node, err = mump2p.NewNodeWithHost(
		cnt.Ctx,
		cnt.Log.With(logger.WithService("mump2p_otel")),
		cfg,
		h,
		t.TempDir(),
		test_utils.TestNodeOptions()...,
	)
	require.NoError(t, err)

	var once sync.Once
	stop = func() { once.Do(node.Stop) }
	t.Cleanup(stop)

	return node, stop
}

// otelSink is a minimal OTLP/HTTP trace collector. Asserting on what it received
// is asserting on the bytes a real collector, and the span parser behind it, see.
type otelSink struct {
	server *httptest.Server

	mu      sync.Mutex
	batches []*tracev1.ResourceSpans
}

func newOTelSink(t *testing.T) *otelSink {
	t.Helper()

	sink := new(otelSink)
	sink.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req coltracev1.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		sink.mu.Lock()
		sink.batches = append(sink.batches, req.GetResourceSpans()...)
		sink.mu.Unlock()

		out, err := proto.Marshal(&coltracev1.ExportTraceServiceResponse{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(out)
	}))
	t.Cleanup(sink.server.Close)

	return sink
}

// endpoint is host:port with no scheme, the form the collector is configured with.
func (s *otelSink) endpoint() string {
	return strings.TrimPrefix(s.server.URL, "http://")
}

func (s *otelSink) received() []*tracev1.ResourceSpans {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.batches)
}

// nodeIDs returns the sorted distinct mump2p.node_id values across the export.
func nodeIDs(batches []*tracev1.ResourceSpans) []string {
	var ids []string
	for _, rs := range batches {
		for _, kv := range rs.GetResource().GetAttributes() {
			if kv.GetKey() == nodeIDAttr && !slices.Contains(ids, kv.GetValue().GetStringValue()) {
				ids = append(ids, kv.GetValue().GetStringValue())
			}
		}
	}
	slices.Sort(ids)

	return ids
}

func spanNames(batches []*tracev1.ResourceSpans) map[string]int {
	names := make(map[string]int)
	for _, span := range allSpans(batches) {
		names[span.GetName()]++
	}

	return names
}

// requireIntChunkID asserts rlnc.chunk_id is exported as an int, which is what
// the parser's `Int(\d+)` match depends on.
func requireIntChunkID(t *testing.T, batches []*tracev1.ResourceSpans) {
	t.Helper()

	var seen int
	for _, span := range allSpans(batches) {
		for _, kv := range span.GetAttributes() {
			if kv.GetKey() != "rlnc.chunk_id" {
				continue
			}
			seen++
			require.IsType(t, (*commonv1.AnyValue_IntValue)(nil), kv.GetValue().GetValue(),
				"rlnc.chunk_id on %s must be an int attribute", span.GetName())
		}
	}
	require.Positive(t, seen, "no span carried rlnc.chunk_id")
}

func allSpans(batches []*tracev1.ResourceSpans) []*tracev1.Span {
	var spans []*tracev1.Span
	for _, rs := range batches {
		for _, ss := range rs.GetScopeSpans() {
			spans = append(spans, ss.GetSpans()...)
		}
	}

	return spans
}

func publishTraceEvent(msgID string, symbolIDs ...[]byte) *mp2ppb.TraceEvent {
	return &mp2ppb.TraceEvent{
		MsgId:     []byte(msgID),
		PeerId:    []byte("pub"),
		Timestamp: time.Now().UnixNano(),
		Event: &mp2ppb.TraceEvent_PublishMessage{
			PublishMessage: &mp2ppb.PublishMessage{
				MessageId: []byte(msgID),
				Topic:     e2eTopic,
				SymbolIds: symbolIDs,
			},
		},
	}
}

func helpfulTraceEvent(msgID string, chunkID uint32, symbolID []byte) *mp2ppb.TraceEvent {
	return &mp2ppb.TraceEvent{
		MsgId:     []byte(msgID),
		PeerId:    []byte("relay"),
		Timestamp: time.Now().UnixNano(),
		Event: &mp2ppb.TraceEvent_HelpfulSymbol{
			HelpfulSymbol: &mp2ppb.SymbolContainer{
				MessageId:    []byte(msgID),
				ChunkId:      chunkID,
				SymbolId:     symbolID,
				Coefficients: []byte{1, 0},
				ReceivedFrom: []byte("pub"),
			},
		},
	}
}

func recodeTraceEvent(msgID string, chunkID uint32, out []byte, inputs ...[]byte) *mp2ppb.TraceEvent {
	return &mp2ppb.TraceEvent{
		MsgId:     []byte(msgID),
		PeerId:    []byte("relay"),
		Timestamp: time.Now().UnixNano(),
		Event: &mp2ppb.TraceEvent_Recode{
			Recode: &mp2ppb.Recode{
				MessageId:      []byte(msgID),
				ChunkId:        chunkID,
				SymbolId:       out,
				Coefficients:   []byte{1, 1},
				InputSymbolIds: inputs,
			},
		},
	}
}

func chunkDecodedTraceEvent(msgID string, chunkID uint32) *mp2ppb.TraceEvent {
	return &mp2ppb.TraceEvent{
		MsgId:     []byte(msgID),
		PeerId:    []byte("relay"),
		Timestamp: time.Now().UnixNano(),
		Event: &mp2ppb.TraceEvent_ChunkDecoded{
			ChunkDecoded: &mp2ppb.ChunkDecoded{MessageId: []byte(msgID), ChunkId: chunkID},
		},
	}
}
