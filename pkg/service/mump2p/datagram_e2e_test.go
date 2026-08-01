package mump2p_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// The tests in this file are the only place the gateway drives the real RLNC
// coder. Everywhere else PassthroughCoder stands in, carrying a whole payload in
// one symbol, because the real coder cannot be linked in: it and
// mump2p-protocol's vendored rlncpb register the same protobuf descriptors, so a
// binary holding both panics at init. Envelopes, the router, the wire protocol
// and delivery are real in those tests; the Galois-field arithmetic is not
// exercised anywhere but here.

const (
	e2eTopic = "beacon_attestation_17"

	// e2ePayloadSize is chosen so one publish is a real generation the coder has
	// to shard and solve, and still small enough that the whole generation fits
	// the per-peer pubsub outbound queue. At the gateway's defaults it is 5
	// chunks of 5 symbols. Well above that, the stream path drops the tail of the
	// burst with no counter moving and the message never decodes, which is a
	// property of the path rather than something this suite should assert around.
	e2ePayloadSize = 1 << 10

	// e2eSettle bounds the waits for handshake, session, path validation and
	// delivery. Loopback is fast; this is generous enough for a loaded CI box.
	e2eSettle     = 30 * time.Second
	e2eSettleTick = 100 * time.Millisecond

	// meshSettle covers the gap between joining a topic and being a send target.
	meshSettle = 3 * time.Second

	// deliverAttempts and deliverAttemptTimeout bound one payload's delivery.
	// Nothing in the router retries a publish that found no peer to send to, so
	// a publish is repeated rather than waited on once.
	deliverAttempts       = 4
	deliverAttemptTimeout = 10 * time.Second
)

// TestRealCoderShardsAndDecodesOnTheGatewayPath is the coverage hole this suite
// exists for: a gateway node built on the out-of-process coder, publishing a
// payload that has to shard, and a peer that has to solve for it.
//
// The decisive assertions are on the shape of the coding, not on delivery.
// Delivery alone proves nothing, since PassthroughCoder delivers too. Many
// chunks and more rank-increasing symbols than chunks are what only a real
// generation matrix produces: the passthrough carries every payload as chunk 0,
// symbol 1 of 1.
func TestRealCoderShardsAndDecodesOnTheGatewayPath(t *testing.T) {
	sidecar := test_utils.RequireRLNCSidecar(t)
	cnt := test_utils.GetClean(t)
	const clusterID = "optimum_e2e_real_coder"

	publisher := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID, false)
	})
	// Only the receiver traces shards: the counts below have to come from the
	// node that ran decode, not from the one that ran encode.
	subscriber := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID, false, withShardTracing)
	})

	joinMesh(cnt.Ctx, t, publisher.node, subscriber.node)

	payloads := randomPayloads(1)
	got := deliverPayloads(cnt.Ctx, t, publisher.node, subscriber, payloads)

	chunks, helpful := subscriber.coding()
	t.Logf("real coder decoded %d chunks from %d rank-increasing symbols", chunks, helpful)
	require.Equal(t, 1, got, "the real coder must decode the payload")

	// One chunk of one symbol is exactly what PassthroughCoder emits, so this is
	// the assertion that makes it unreachable from here.
	require.Greater(t, chunks, 1, "the payload must shard into many chunks")
	require.Greater(t, helpful, chunks,
		"decoding must consume more than one symbol per chunk, or no linear combination was solved")
}

// TestDatagramPathCarriesTheMeshBetweenTwoNodes proves the data plane moves
// real traffic: two nodes handshake, key a session, validate a path, and then
// the symbols of a real publish leave over UDP rather than over the stream.
//
// The assertions are counters rather than "the message arrived". Delivery is
// true on the stream path too, so it cannot tell the datagram path from its own
// fallback, and a run in which every symbol quietly fell back would look
// identical. The pair of counters is what distinguishes them: sends on the
// datagram path above zero, and zero fallbacks while every peer being sent to
// holds a session.
func TestDatagramPathCarriesTheMeshBetweenTwoNodes(t *testing.T) {
	sidecar := test_utils.RequireRLNCSidecar(t)
	cnt := test_utils.GetClean(t)
	const clusterID = "optimum_e2e_datagram_pair"

	publisher := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID, true)
	})
	subscriber := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID, true)
	})

	joinMesh(cnt.Ctx, t, publisher.node, subscriber.node)
	awaitDatagramPath(t, publisher.node, subscriber.node)

	hookBefore := datagramSends(t, "hook")
	fallbackBefore := datagramSends(t, "fallback")

	payloads := randomPayloads(3)
	overDatagram := deliverPayloads(cnt.Ctx, t, publisher.node, subscriber, payloads)

	hookSends := datagramSends(t, "hook") - hookBefore
	fallbackSends := datagramSends(t, "fallback") - fallbackBefore
	t.Logf("datagram path carried %.0f sends, stream fallback carried %.0f", hookSends, fallbackSends)

	require.Positive(t, hookSends, "the datagram path must have carried the symbols")
	require.Zero(t, fallbackSends,
		"every peer sent to holds a validated session, so nothing may fall back to the stream path")

	// The control run is the same publish with the data plane off. It is what
	// makes the counters above a statement about which path carried the message
	// rather than about how much of it survived: completeness has to match.
	controlPublisher := realCoderNode(t, cnt, sidecar, clusterID+"_control", false)
	control := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID+"_control", false)
	})
	joinMesh(cnt.Ctx, t, controlPublisher, control.node)

	overStream := deliverPayloads(cnt.Ctx, t, controlPublisher, control, randomPayloads(len(payloads)))

	require.Equal(t, len(payloads), overStream, "the stream-only control run must be complete")
	require.Equal(t, overStream, overDatagram,
		"the datagram run must deliver exactly what the stream-only control run delivers")
}

// TestDatagramIngressRefusesAPeerThatNeverKeyedASession is the negative case,
// and the one that carries the security property: identity on this data plane
// is the key that opens a datagram, never the address it arrived from.
//
// The third node is admitted by nobody, so it never keys a session, so there is
// no key id it can stamp on a datagram. Its traffic is rejected at the cheapest
// step of the ingress, before any cipher runs, and it is never staged into a
// mesh, so no symbol is ever sent to it either.
func TestDatagramIngressRefusesAPeerThatNeverKeyedASession(t *testing.T) {
	sidecar := test_utils.RequireRLNCSidecar(t)
	cnt := test_utils.GetClean(t)
	const clusterID = "optimum_e2e_datagram_admission"

	publisher := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID, true)
	})
	subscriber := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID, true)
	})
	// Same wire protocol, same feature flag, different cluster: the handshake is
	// what denies it, so it is an unadmitted peer rather than a broken one.
	stranger := watchNode(t, func() mump2p.Engine {
		return realCoderNode(t, cnt, sidecar, clusterID+"_stranger", true)
	})

	joinMesh(cnt.Ctx, t, publisher.node, subscriber.node)
	awaitDatagramPath(t, publisher.node, subscriber.node)

	require.NoError(t, stranger.node.SubscribeTopic(e2eTopic))
	_ = stranger.node.GetHost().Connect(cnt.Ctx, publisher.node.GetHostInfo())

	strangerID := stranger.node.GetHostInfo().ID
	require.Eventually(t, func() bool {
		return len(stranger.node.GetPeers()) == 0
	}, e2eSettle, e2eSettleTick, "a peer from another cluster must not stay connected")

	require.False(t, asNode(t, publisher.node).HandshakeVerified(strangerID))
	_, keyed := asNode(t, publisher.node).DatagramSessionExpiry(strangerID)
	require.False(t, keyed, "an unadmitted peer must never hold datagram keys")

	// Forge at the ingress directly. With no session there is only one shape the
	// stranger's datagrams can take: a well-formed cleartext header naming a key
	// id nobody installed.
	dropsBefore := ingressDrops(t, "unknown_key_id")
	deliveredBefore := datagramDelivered(t)

	target, bound := asNode(t, publisher.node).DatagramLocalAddr()
	require.True(t, bound, "the datagram data plane must be bound")
	sendForgedDatagrams(t, target, 32)

	require.Eventually(t, func() bool {
		return ingressDrops(t, "unknown_key_id") > dropsBefore
	}, e2eSettle, e2eSettleTick, "forged datagrams must be dropped on an unresolvable key id")

	require.Equal(t, deliveredBefore, datagramDelivered(t),
		"nothing a stranger sent may reach the delivery callback")

	// The other half of the property: not admitted means never staged, so the
	// stranger is not a send target for the symbols of a real publish either.
	require.Equal(t, 1, deliverPayloads(cnt.Ctx, t, publisher.node, subscriber, randomPayloads(1)),
		"an admitted peer must still be served")

	require.NotContains(t, publisher.node.GetMeshPeers(e2eTopic), strangerID)
	require.Zero(t, stranger.deliveredCount(), "an unadmitted peer must receive no symbols")
}

// realCoderNode builds a gateway node on the sidecar's shared memory. It passes
// no coder option at all, so the node attaches to the out-of-process coder the
// way the shipped binary does and PassthroughCoder is not reachable from here.
func realCoderNode(
	t *testing.T,
	cnt *test_utils.Container,
	sidecar *test_utils.RLNCSidecar,
	clusterID string,
	enableDatagram bool,
	opts ...func(*mump2p.Config),
) mump2p.Engine {
	t.Helper()

	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, test_utils.GetFreePortT(t), nil)
	cfg.SHMName = sidecar.SHMName
	cfg.SHMLanes = sidecar.SHMLanes
	if enableDatagram {
		cfg.DatagramEnable = true
		cfg.DatagramListenAddr = "127.0.0.1:0"
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return test_utils.NewRealCoderNode(cnt.Ctx, t, cnt.Log, t.TempDir(), cfg)
}

func withShardTracing(cfg *mump2p.Config) { cfg.TraceShard = true }

func randomPayloads(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = test_utils.RandBytes(e2ePayloadSize)
	}
	return out
}

// deliverPayloads publishes each payload until it arrives and reports how many
// of them did. Completeness is the return value rather than an assertion, so a
// datagram run and a stream-only control run can be compared against each other.
func deliverPayloads(
	ctx context.Context,
	t *testing.T,
	publisher mump2p.Engine,
	subscriber *nodeWatcher,
	payloads [][]byte,
) int {
	t.Helper()

	var delivered int
	for _, payload := range payloads {
		for range deliverAttempts {
			if err := publisher.PublishMessage(ctx, e2eTopic, payload); err != nil {
				t.Logf("publish failed, retrying: %v", err)
				continue
			}
			if subscriber.awaitPayload(payload, deliverAttemptTimeout) {
				delivered++
				break
			}
		}
	}
	return delivered
}

// joinMesh connects two nodes and waits for the handshake that admits them and
// for the topic mesh to form on both sides.
func joinMesh(ctx context.Context, t *testing.T, left, right mump2p.Engine) {
	t.Helper()

	require.NoError(t, left.SubscribeTopic(e2eTopic))
	require.NoError(t, right.SubscribeTopic(e2eTopic))
	require.NoError(t, left.GetHost().Connect(ctx, right.GetHostInfo()))

	require.Eventually(t, func() bool {
		return asNode(t, left).HandshakeVerified(right.GetHostInfo().ID) &&
			asNode(t, right).HandshakeVerified(left.GetHostInfo().ID)
	}, e2eSettle, e2eSettleTick, "both sides must verify the handshake that admits them")

	require.Eventually(t, func() bool {
		return len(left.GetMeshPeers(e2eTopic)) == 1 && len(right.GetMeshPeers(e2eTopic)) == 1
	}, e2eSettle, e2eSettleTick, "the topic mesh must form on both sides")

	// Being in the topic peer set is not yet being a send target: the router
	// picks mesh peers first, and that cache fills when the outbound pubsub
	// stream opens, a gossipsub heartbeat or two later.
	time.Sleep(meshSettle)
}

// awaitDatagramPath waits until both nodes hold a session and have confirmed an
// endpoint for it. Publishing before the path is validated is what produces a
// run that silently takes the stream fallback for every symbol.
func awaitDatagramPath(t *testing.T, left, right mump2p.Engine) {
	t.Helper()

	leftID, rightID := left.GetHostInfo().ID, right.GetHostInfo().ID

	require.Eventually(t, func() bool {
		_, okLeft := asNode(t, left).DatagramSessionExpiry(rightID)
		_, okRight := asNode(t, right).DatagramSessionExpiry(leftID)

		return okLeft && okRight
	}, e2eSettle, e2eSettleTick, "both sides must key a datagram session off the handshake")

	require.Eventually(t, func() bool {
		return asNode(t, left).DatagramPathConfirmed(rightID) &&
			asNode(t, right).DatagramPathConfirmed(leftID)
	}, e2eSettle, e2eSettleTick, "both sides must confirm an endpoint before the data plane carries anything")
}

// sendForgedDatagrams sprays datagrams with a valid cleartext header and an
// unallocated key id at target, which is the best an unkeyed peer can do.
func sendForgedDatagrams(t *testing.T, target netip.AddrPort, count int) {
	t.Helper()

	conn, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:0")))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	for i := range count {
		// Header layout is version(1) || keyID(4) || seq(8). Everything after it
		// is ciphertext and a Poly1305 tag, which a forger cannot produce.
		dgram := test_utils.RandBytes(64)
		dgram[0] = datagram.Version
		binary.BigEndian.PutUint32(dgram[1:5], uint32(i)+1)
		binary.BigEndian.PutUint64(dgram[5:13], uint64(i))

		_, writeErr := conn.WriteToUDPAddrPort(dgram, target)
		require.NoError(t, writeErr)
	}
}

func datagramSends(t *testing.T, path string) float64 {
	t.Helper()

	return counterValue(t, "mump2p_datagram_sends_total", map[string]string{"path": path})
}

func ingressDrops(t *testing.T, reason string) float64 {
	t.Helper()

	return counterValue(t, "mump2p_datagram_ingress_drops_total", map[string]string{"reason": reason})
}

func datagramDelivered(t *testing.T) float64 {
	t.Helper()

	return counterValue(t, "mump2p_datagram_delivered_total", nil)
}

// counterValue reads one counter out of the default Prometheus registry, which
// is where the datagram transport registers.
//
// The series are process-wide rather than per node, so every assertion on them
// is a delta measured across a window in which the test drives the only traffic
// that can move them.
func counterValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric.GetLabel(), labels) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelsMatch(pairs []*dto.LabelPair, want map[string]string) bool {
	got := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		got[pair.GetName()] = pair.GetValue()
	}
	for name, value := range want {
		if got[name] != value {
			return false
		}
	}
	return true
}

// nodeWatcher drains a node's listener channel for the life of the test.
//
// The broadcaster is unbuffered and its send blocks, so an undrained listener
// stalls whichever goroutine traced or delivered into it. Draining
// unconditionally, and unregistering only after the node has stopped, is what
// keeps that from stalling the router or closing the channel underneath it.
type nodeWatcher struct {
	node mump2p.Engine

	mu        sync.Mutex
	delivered []*commonentities.P2PMessage
	chunks    map[uint32]struct{}
	helpful   int
}

const watcherListenerKey = "datagram-e2e"

func watchNode(t *testing.T, build func() mump2p.Engine) *nodeWatcher {
	t.Helper()

	// Registered before the node exists so it runs after the node's own Stop:
	// cleanups are LIFO, and closing the channel first would be a send on a
	// closed channel for anything still in flight.
	var unregister func()
	t.Cleanup(func() {
		if unregister != nil {
			unregister()
		}
	})

	w := &nodeWatcher{node: build(), chunks: make(map[uint32]struct{})}
	events := w.node.RegisterListener(watcherListenerKey)
	unregister = func() { w.node.UnregisterListener(watcherListenerKey) }

	go w.drain(events)
	return w
}

func (w *nodeWatcher) drain(events <-chan *entities.MumP2PResponse) {
	for event := range events {
		w.mu.Lock()
		switch {
		case event.Command == entities.MumP2PCommandMessage && event.Message != nil:
			w.delivered = append(w.delivered, event.Message)
		case event.TraceEvent != nil:
			w.noteTraceLocked(event.TraceEvent)
		}
		w.mu.Unlock()
	}
}

func (w *nodeWatcher) noteTraceLocked(evt *tracepb.TraceEvent) {
	switch e := evt.GetEvent().(type) {
	case *tracepb.TraceEvent_HelpfulSymbol:
		w.helpful++
	case *tracepb.TraceEvent_ChunkDecoded:
		w.chunks[e.ChunkDecoded.GetChunkId()] = struct{}{}
	}
}

// awaitPayload reports whether the exact payload was delivered within timeout.
func (w *nodeWatcher) awaitPayload(payload []byte, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if w.hasPayload(payload) {
			return true
		}
		time.Sleep(e2eSettleTick)
	}
	return w.hasPayload(payload)
}

func (w *nodeWatcher) hasPayload(payload []byte) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, msg := range w.delivered {
		if msg.Topic == e2eTopic && bytes.Equal(msg.Message, payload) {
			return true
		}
	}
	return false
}

func (w *nodeWatcher) deliveredCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.delivered)
}

// coding reports how many distinct chunks the node decoded and how many
// rank-increasing symbols it consumed doing so.
func (w *nodeWatcher) coding() (chunks, helpful int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.chunks), w.helpful
}
