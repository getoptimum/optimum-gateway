package udpsession

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

// admitBoth marks each node admitted at the other, with the given credential
// expiry, which is what the verified cluster handshake does in production.
func admitBoth(left, right *testNode, tokenExpiry time.Time) {
	left.auth.admit(right.id(), tokenExpiry)
	right.auth.admit(left.id(), tokenExpiry)
}

// TestEstablishInstallsCrosswiseKeys proves the receiver-allocated key id model
// end to end: each side stamps the id its peer allocated, so the two sessions
// name each other's receive tables and nothing had to be negotiated.
func TestEstablishInstallsCrosswiseKeys(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	admitBoth(left, right, time.Time{})
	connect(ctx, t, left, right)
	establish(ctx, t, left, right)

	// Both sides are awaited: which of the two dialed depends on the peer ids the
	// hosts drew, and only the dialer has installed by the time Establish returns.
	leftSess := awaitSession(t, left, right.id())
	rightSess := awaitSession(t, right, left.id())

	// The id one side seals with is the id the other side opens with.
	_, found := right.transport.Sessions().LookupRxKey(leftSess.TxKey().KeyID())
	require.True(t, found, "the peer must hold a receive key for what we stamp")

	_, found = left.transport.Sessions().LookupRxKey(rightSess.TxKey().KeyID())
	require.True(t, found)

	require.NotEqual(t, leftSess.TxKey().KeyID(), rightSess.TxKey().KeyID())
}

// TestEstablishRefusesUnadmittedPeer proves key agreement is gated on admission:
// a peer the predicate rejects gets no keys, in either direction, whether it is
// the one dialing or the one answering.
func TestEstablishRefusesUnadmittedPeer(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	connect(ctx, t, left, right)

	t.Run("DialerRefusesLocally", func(t *testing.T) {
		// Nobody is admitted yet, so the dial must not even be attempted.
		err := dialer.mgr.Establish(ctx, listener.id())
		require.ErrorIs(t, err, ErrNotAdmitted)

		_, ok := dialer.transport.Sessions().Lookup(listener.id())
		require.False(t, ok)
	})

	t.Run("ListenerRefusesDialer", func(t *testing.T) {
		// The dialer admits the listener, but not the other way round: the
		// exchange must fail at the listener and leave no session anywhere.
		dialer.auth.admit(listener.id(), time.Time{})

		require.Error(t, dialer.mgr.Establish(ctx, listener.id()))

		_, ok := listener.transport.Sessions().Lookup(dialer.id())
		require.False(t, ok, "a peer the listener denies must get no receive key")

		require.Never(t, func() bool {
			_, ok := listener.transport.Sessions().Lookup(dialer.id())
			return ok
		}, 200*time.Millisecond, 20*time.Millisecond)
	})
}

// TestSessionLifetimeCappedByToken proves the credential is the ceiling, not the
// default: a token expiring inside the default window shortens the session, and
// one expiring outside it leaves the default in place.
func TestSessionLifetimeCappedByToken(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	connect(ctx, t, left, right)

	t.Run("TokenExpiresFirst", func(t *testing.T) {
		tokenExpiry := clock.Now().Add(5 * time.Minute)
		admitBoth(left, right, tokenExpiry)

		require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))

		at, ok := dialer.mgr.ExpiresAt(listener.id())
		require.True(t, ok)
		require.Equal(t, tokenExpiry, at, "the session must not outlive the token that authorized it")
		require.True(t, at.Before(clock.Now().Add(DefaultMaxLifetime)))

		// The listener derived the same cap from its own copy of the credential.
		require.Eventually(t, func() bool {
			at, ok := listener.mgr.ExpiresAt(dialer.id())
			return ok && at.Equal(tokenExpiry)
		}, 10*time.Second, 20*time.Millisecond)
	})

	t.Run("DefaultLifetimeIsTheCeiling", func(t *testing.T) {
		admitBoth(left, right, clock.Now().Add(24*time.Hour))

		require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))

		at, ok := dialer.mgr.ExpiresAt(listener.id())
		require.True(t, ok)
		require.Equal(t, clock.Now().Add(DefaultMaxLifetime), at)
	})

	t.Run("AlreadyExpiredTokenIsRefused", func(t *testing.T) {
		admitBoth(left, right, clock.Now().Add(-time.Second))

		require.ErrorIs(t, dialer.mgr.Establish(ctx, listener.id()), ErrExpiredToken)
	})
}

// TestExpiredSessionIsDestroyed proves the cap is enforced and not merely
// recorded: past the deadline the keys are gone and the peer has to handshake
// again. Driven entirely by the injected clock.
func TestExpiredSessionIsDestroyed(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	tokenExpiry := clock.Now().Add(5 * time.Minute)
	admitBoth(left, right, tokenExpiry)

	connect(ctx, t, left, right)
	require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))

	// The id the listener stamps on what it sends is this node's receive key id,
	// which is what has to disappear when the session expires.
	rxKeyID := awaitSession(t, listener, dialer.id()).TxKey().KeyID()
	_, ok := dialer.transport.Sessions().LookupRxKey(rxKeyID)
	require.True(t, ok)

	// One tick short of the deadline changes nothing.
	require.Zero(t, dialer.mgr.Tick(tokenExpiry.Add(-time.Nanosecond)))
	_, ok = dialer.transport.Sessions().Lookup(listener.id())
	require.True(t, ok)

	require.Equal(t, 1, dialer.mgr.Tick(tokenExpiry))

	_, ok = dialer.transport.Sessions().Lookup(listener.id())
	require.False(t, ok, "an expired session must not survive its deadline")

	_, ok = dialer.mgr.ExpiresAt(listener.id())
	require.False(t, ok)

	_, ok = dialer.transport.Sessions().LookupRxKey(rxKeyID)
	require.False(t, ok, "the expired session's key id must be freed")
}

// TestDeniedPeerLosesSessionAndDatagrams proves revocation: a peer denied while
// its session is live has that session destroyed, and the datagrams it keeps
// sending under the old key are dropped rather than delivered.
func TestDeniedPeerLosesSessionAndDatagrams(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	admitBoth(left, right, time.Time{})

	connect(ctx, t, left, right)
	require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))

	// Both sides must have proven an endpoint before anything can be sent.
	awaitConfirmedPath(t, dialer, listener.id())
	awaitConfirmedPath(t, listener, dialer.id())

	payload := []byte("carried before revocation")
	require.True(t, dialer.transport.Forwarder().Forward(listener.id(), datagram.TypeSymbol, payload))

	require.Eventually(t, func() bool {
		for _, d := range listener.deliveries() {
			if d.from == dialer.id() && bytes.Equal(d.body, payload) {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond, "the session must carry traffic before it is revoked")

	before := len(listener.deliveries())

	// The peer is denied at the listener and its session torn down, which is what
	// the node does from disconnectPeer when a handshake is rejected.
	listener.auth.deny(dialer.id())
	require.True(t, listener.mgr.Destroy(dialer.id()))

	// Revocation is effective on the very next datagram: the key id is out of the
	// receive table, so nothing is decrypted and nothing is injected.
	sess, ok := dialer.transport.Sessions().Lookup(listener.id())
	require.True(t, ok, "the sender still holds its half and keeps sealing")
	_, found := listener.transport.Sessions().LookupRxKey(sess.TxKey().KeyID())
	require.False(t, found)

	for range 8 {
		dialer.transport.Forwarder().Forward(listener.id(), datagram.TypeSymbol, []byte("after revocation"))
	}

	require.Never(t, func() bool {
		return len(listener.deliveries()) > before
	}, 500*time.Millisecond, 20*time.Millisecond, "a revoked peer's datagrams must be dropped")
}

// TestGlareOnlyLowerPeerIDDials proves simultaneous open is eliminated rather
// than resolved: the higher peer ID never dials even when asked to, and both
// sides converge on the one session the lower peer ID opened.
func TestGlareOnlyLowerPeerIDDials(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	admitBoth(left, right, time.Time{})
	connect(ctx, t, left, right)

	// The listener has no session protocol handler to reach: if it dialed at all,
	// NewStream would fail and Establish would report it.
	listener.mgr.host.RemoveStreamHandler(ProtocolID)
	require.NoError(t, listener.mgr.Establish(ctx, dialer.id()),
		"the higher peer ID must not dial")
	require.Zero(t, listener.transport.Sessions().Len())
	require.Zero(t, dialer.transport.Sessions().Len())

	listener.mgr.host.SetStreamHandler(ProtocolID, listener.mgr.handleStream)

	// Both sides now ask at once. Only one exchange can happen, so both end up
	// with exactly one session and the key ids match crosswise.
	done := make(chan error, 2)
	go func() { done <- dialer.mgr.Establish(ctx, listener.id()) }()
	go func() { done <- listener.mgr.Establish(ctx, dialer.id()) }()

	require.NoError(t, <-done)
	require.NoError(t, <-done)

	require.Eventually(t, func() bool {
		return dialer.transport.Sessions().Len() == 1 && listener.transport.Sessions().Len() == 1
	}, 10*time.Second, 20*time.Millisecond)

	dialerSess, ok := dialer.transport.Sessions().Lookup(listener.id())
	require.True(t, ok)
	listenerSess, ok := listener.transport.Sessions().Lookup(dialer.id())
	require.True(t, ok)

	_, found := listener.transport.Sessions().LookupRxKey(dialerSess.TxKey().KeyID())
	require.True(t, found, "both sides must have converged on the same session")
	_, found = dialer.transport.Sessions().LookupRxKey(listenerSess.TxKey().KeyID())
	require.True(t, found)
}

// TestGlareViolatorIsRejected proves the rule is enforced and not merely
// followed: a peer that dials when it should have waited is refused, so it
// cannot open a second session behind the first.
func TestGlareViolatorIsRejected(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	admitBoth(left, right, time.Time{})
	connect(ctx, t, left, right)

	// Speak the protocol from the wrong side: the higher peer ID opens the stream.
	stream, err := listener.host.NewStream(ctx, dialer.id(), ProtocolID)
	require.NoError(t, err)
	defer stream.Close()

	msg, eph, err := listener.mgr.buildMessage()
	require.NoError(t, err)
	defer eph.zeroize()

	// The write may or may not land before the reset; the outcome that matters is
	// that no session exists on the side that should have dialed.
	_ = writeMessage(stream, msg)

	_, err = readMessage(stream)
	require.Error(t, err)

	require.Never(t, func() bool {
		return dialer.transport.Sessions().Len() > 0
	}, 300*time.Millisecond, 20*time.Millisecond)
}

// TestOversizedSessionPayloadRejectedOnTheWire proves the bound holds where it
// matters: a peer streaming a huge payload into the session stream is cut off
// and gets no session.
func TestOversizedSessionPayloadRejectedOnTheWire(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	admitBoth(left, right, time.Time{})
	connect(ctx, t, left, right)

	stream, err := dialer.host.NewStream(ctx, listener.id(), ProtocolID)
	require.NoError(t, err)
	defer stream.Close()

	// A well formed message with a megabyte of padding: only its size can be what
	// rejects it. The write may fail part way, because being cut off is exactly
	// what the bound does.
	_, _ = stream.Write([]byte(`{"version":1,"padding":"` + strings.Repeat("A", 1<<20) + `"}` + "\n"))

	require.Never(t, func() bool {
		return listener.transport.Sessions().Len() > 0
	}, 500*time.Millisecond, 20*time.Millisecond, "an oversized payload must not produce a session")
}

// TestDestroyIsIdempotentAndFreesTheKeyID proves a session is never resumed: the
// keys and the key id are gone, and a second destroy is a no-op rather than a
// double free.
func TestDestroyIsIdempotentAndFreesTheKeyID(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	admitBoth(left, right, time.Time{})
	connect(ctx, t, left, right)
	require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))

	revoked := awaitSession(t, listener, dialer.id())
	rxKeyID := revoked.TxKey().KeyID()

	require.True(t, dialer.mgr.Destroy(listener.id()))
	require.False(t, dialer.mgr.Destroy(listener.id()))

	_, ok := dialer.transport.Sessions().LookupRxKey(rxKeyID)
	require.False(t, ok, "the destroyed session's receive key id must be freed")
	require.Zero(t, dialer.transport.Sessions().Len())

	// Re-establishing after revocation is a fresh exchange, never a resume: new
	// key ids are drawn, so a datagram sealed under the old epoch stays unusable.
	require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))
	require.Equal(t, 1, dialer.transport.Sessions().Len())

	freshRxKeyID := awaitReplacedSession(t, listener, dialer.id(), revoked).TxKey().KeyID()
	require.NotEqual(t, rxKeyID, freshRxKeyID)

	_, ok = dialer.transport.Sessions().LookupRxKey(rxKeyID)
	require.False(t, ok, "the old key id must not come back")
}

// TestNilManagerIsSafe proves the disabled data plane needs no branching at the
// call sites: every entry point is safe on the nil the constructor returns.
func TestNilManagerIsSafe(t *testing.T) {
	mgr, err := New(&Config{})
	require.NoError(t, err)
	require.Nil(t, mgr)

	require.NotPanics(t, func() {
		mgr.Start()
		require.NoError(t, mgr.Establish(t.Context(), "peer"))
		require.False(t, mgr.Destroy("peer"))
		require.Zero(t, mgr.Tick(time.Now()))
		_, ok := mgr.ExpiresAt("peer")
		require.False(t, ok)
		mgr.Close()
	})
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	transport, err := datagram.New(&datagram.Config{ListenAddr: "127.0.0.1:0"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = transport.Close() })

	_, err = New(&Config{Transport: transport})
	require.ErrorContains(t, err, "host must not be nil")

	h, err := newLoopbackHost(t)
	require.NoError(t, err)

	_, err = New(&Config{Transport: transport, Host: h})
	require.ErrorContains(t, err, "authorizer must not be nil")
}

// TestCloseDestroysEveryLiveSession proves shutdown leaves no keyed peer behind.
func TestCloseDestroysEveryLiveSession(t *testing.T) {
	ctx := t.Context()
	clock := newFakeClock()

	left := newTestNode(t, clock)
	right := newTestNode(t, clock)

	dialer, listener := initiatorOf(left, right)
	admitBoth(left, right, time.Time{})
	connect(ctx, t, left, right)
	require.NoError(t, dialer.mgr.Establish(ctx, listener.id()))

	require.Equal(t, 1, dialer.transport.Sessions().Len())

	dialer.mgr.Close()
	require.Zero(t, dialer.transport.Sessions().Len())

	// Close is called again by the harness cleanup.
	require.NotPanics(t, dialer.mgr.Close)
}

// TestKeyIDAllocationIsUniqueAndNonZero proves receiver allocation cannot hand
// two peers the same receive id, and never hands out the zero id the datagram
// layer reads as "no key".
func TestKeyIDAllocationIsUniqueAndNonZero(t *testing.T) {
	clock := newFakeClock()
	node := newTestNode(t, clock)

	const draws = 512

	seen := make(map[uint32]struct{}, draws)
	for range draws {
		id, err := node.mgr.allocateKeyID()
		require.NoError(t, err)
		require.NotZero(t, id)
		require.NotContains(t, seen, id, "a reserved id must not be drawn twice")
		seen[id] = struct{}{}
	}

	// Releasing puts an id back in circulation; nothing else does.
	for id := range seen {
		node.mgr.releaseKeyID(id)
	}

	node.mgr.mu.Lock()
	defer node.mgr.mu.Unlock()
	require.Empty(t, node.mgr.reserved)
}
