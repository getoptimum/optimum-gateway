package udpsession

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

// fakeClock is the injected clock. Every deadline in this package is measured
// through it, so expiry is exercised by moving it rather than by sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// authTable stands in for the node's admission state: who is admitted, and when
// the credential that admitted them expires.
type authTable struct {
	mu      sync.Mutex
	entries map[peer.ID]time.Time
}

func newAuthTable() *authTable {
	return &authTable{entries: make(map[peer.ID]time.Time)}
}

func (a *authTable) admit(p peer.ID, tokenExpiry time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.entries[p] = tokenExpiry
}

func (a *authTable) deny(p peer.ID) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.entries, p)
}

func (a *authTable) authorize(p peer.ID) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	expiry, ok := a.entries[p]

	return expiry, ok
}

// delivery is one payload the datagram transport accepted.
type delivery struct {
	from peer.ID
	body []byte
}

// testNode is one side of the exchange: a libp2p host, a live datagram
// transport, and the session manager that keys it.
type testNode struct {
	host      host.Host
	transport *datagram.Transport
	mgr       *Manager
	auth      *authTable
	clock     *fakeClock

	mu        sync.Mutex
	delivered []delivery
}

func newLoopbackHost(t *testing.T) (host.Host, error) {
	t.Helper()

	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err == nil {
		t.Cleanup(func() { _ = h.Close() })
	}

	return h, err
}

func newTestNode(t *testing.T, clock *fakeClock) *testNode {
	t.Helper()

	h, err := newLoopbackHost(t)
	require.NoError(t, err)

	metrics, err := datagram.NewMetrics(prometheus.NewRegistry())
	require.NoError(t, err)

	node := &testNode{auth: newAuthTable(), clock: clock, host: h}

	node.transport, err = datagram.New(&datagram.Config{
		ListenAddr: "127.0.0.1:0",
		Now:        clock.Now,
		Metrics:    metrics,
		Deliver:    node.record,
	})
	require.NoError(t, err)
	node.transport.Start()

	node.mgr, err = New(&Config{
		Host:      h,
		Transport: node.transport,
		Authorize: node.auth.authorize,
		Candidates: func() []netip.AddrPort {
			return LocalCandidates(node.transport.LocalAddr(), h.Addrs())
		},
		Now: clock.Now,
	})
	require.NoError(t, err)
	node.mgr.Start()

	t.Cleanup(func() {
		node.mgr.Close()
		_ = node.transport.Close()
		_ = h.Close()
	})

	return node
}

// record copies the body: it aliases a pooled buffer that is reused as soon as
// the callback returns.
func (n *testNode) record(from peer.ID, body []byte) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.delivered = append(n.delivered, delivery{from: from, body: append([]byte(nil), body...)})
}

func (n *testNode) deliveries() []delivery {
	n.mu.Lock()
	defer n.mu.Unlock()

	return append([]delivery(nil), n.delivered...)
}

func (n *testNode) id() peer.ID { return n.host.ID() }

// connect opens the libp2p connection the session protocol runs over. The real
// node only reaches establishment after a verified handshake; here the auth
// table stands in for that verdict.
func connect(ctx context.Context, t *testing.T, left, right *testNode) {
	t.Helper()

	require.NoError(t, left.host.Connect(ctx, peer.AddrInfo{ID: right.id(), Addrs: right.host.Addrs()}))
}

// initiatorOf returns the pair ordered so the first element is the one that
// dials, which is the only side whose Establish does anything.
func initiatorOf(left, right *testNode) (dialer, listener *testNode) {
	if Initiator(left.id(), right.id()) {
		return left, right
	}

	return right, left
}

// establish runs one full exchange between two admitted nodes.
func establish(ctx context.Context, t *testing.T, left, right *testNode) {
	t.Helper()

	dialer, _ := initiatorOf(left, right)
	require.NoError(t, dialer.mgr.Establish(ctx, otherOf(dialer, left, right).id()))
}

func otherOf(this, left, right *testNode) *testNode {
	if this == left {
		return right
	}

	return left
}

// awaitSession waits for the session the peer installs on its own side, which
// lands a moment after the dialer's because the exchange finishes there first.
func awaitSession(t *testing.T, node *testNode, remote peer.ID) *datagram.Session {
	t.Helper()

	var sess *datagram.Session

	require.Eventually(t, func() bool {
		var ok bool
		sess, ok = node.transport.Sessions().Lookup(remote)

		return ok
	}, 10*time.Second, 20*time.Millisecond, "no session was installed")

	return sess
}

// awaitConfirmedPath waits until path validation has proven an endpoint, which
// is what the send side refuses to work without.
func awaitConfirmedPath(t *testing.T, node *testNode, remote peer.ID) {
	t.Helper()

	require.Eventually(t, func() bool {
		sess, ok := node.transport.Sessions().Lookup(remote)
		if !ok {
			return false
		}

		_, confirmed := sess.ConfirmedEndpoint()

		return confirmed
	}, 10*time.Second, 20*time.Millisecond, "path was never confirmed")
}
