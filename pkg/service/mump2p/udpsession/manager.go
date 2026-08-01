package udpsession

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

const (
	// DefaultMaxLifetime is the ceiling on a session's life. The token that
	// authorized the peer is the other ceiling, and the lower of the two wins.
	DefaultMaxLifetime = 60 * time.Minute

	// DefaultDialTimeout bounds one establishment: a dial, one write and one
	// bounded read on an already open connection.
	DefaultDialTimeout = 15 * time.Second

	// DefaultSweepInterval is how often expired sessions are reaped. A session
	// past its deadline is unusable from the moment it passes it, so this only
	// decides how promptly its keys are wiped.
	DefaultSweepInterval = 30 * time.Second

	// streamDeadline bounds each side of the exchange, so a peer that opens the
	// stream and then stalls cannot hold a goroutine.
	streamDeadline = 10 * time.Second

	// keyIDAttempts is how many draws are made before allocation gives up. The
	// space is 2^32 and the occupancy is one id per peer, so a second draw is
	// already vanishingly unlikely; this only stops an unbounded loop.
	keyIDAttempts = 16

	// pathRetryInterval is how often path validation is re-attempted while no
	// endpoint has answered, and pathRetryAttempts bounds the retries. They all
	// have to land inside the validator's probe timeout, because the first probe
	// to expire unconfirmed is what pins the session to the stream fallback.
	pathRetryInterval = 400 * time.Millisecond
	pathRetryAttempts = 4
)

// Authorizer reports whether p may hold a datagram session and when the
// credential that authorized it expires.
//
// A false result denies establishment outright. A true result with a zero
// expiry means the handshake verified no credential, so there is nothing to cap
// the session against beyond the default lifetime.
type Authorizer func(p peer.ID) (tokenExpiry time.Time, ok bool)

// Config configures a Manager. Every zero valued field takes a documented
// default, except Host, Transport and Authorize, which are required.
type Config struct {
	Host      host.Host
	Transport *datagram.Transport
	Authorize Authorizer

	// Candidates reports the UDP endpoints this node advertises to peers.
	Candidates func() []netip.AddrPort

	// Now is the injected clock. Session lifetime is measured entirely through
	// it, so expiry is testable without sleeping.
	Now func() time.Time

	Logger        *slog.Logger
	MaxLifetime   time.Duration
	DialTimeout   time.Duration
	SweepInterval time.Duration
}

// Manager runs the session protocol: it establishes a peer's datagram keys over
// the authenticated stream, installs them into the transport, and destroys them
// when the peer's authorization ends.
//
// A nil *Manager is the disabled data plane and every method is safe on it, so
// callers do not branch on the feature flag.
type Manager struct {
	host       host.Host
	transport  *datagram.Transport
	authorize  Authorizer
	candidates func() []netip.AddrPort
	now        func() time.Time
	log        *slog.Logger

	maxLifetime   time.Duration
	dialTimeout   time.Duration
	sweepInterval time.Duration

	mu sync.Mutex
	// reserved holds key ids drawn for an establishment that has not installed
	// yet, so two concurrent establishments cannot pick the same one.
	reserved map[uint32]struct{}
	// expiry is when each live session must be destroyed.
	expiry map[peer.ID]time.Time
	// closed stops new background work being started while Close waits for the
	// work already running.
	closed bool

	done      chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// spawn runs fn in the background unless the manager is closing, in which case
// it is dropped: adding to the wait group after Close began waiting would race.
func (m *Manager) spawn(fn func()) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()

		return
	}

	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()

		fn()
	}()
}

// New builds a Manager. A nil config or a nil transport is the disabled data
// plane: it returns a nil Manager and no error, so a caller can wire it
// unconditionally.
func New(cfg *Config) (*Manager, error) {
	if cfg == nil || cfg.Transport == nil {
		return nil, nil
	}

	if cfg.Host == nil {
		return nil, errors.New("udpsession: host must not be nil")
	}

	if cfg.Authorize == nil {
		return nil, errors.New("udpsession: authorizer must not be nil")
	}

	m := &Manager{
		host:          cfg.Host,
		transport:     cfg.Transport,
		authorize:     cfg.Authorize,
		candidates:    cfg.Candidates,
		now:           cfg.Now,
		log:           cfg.Logger,
		maxLifetime:   cfg.MaxLifetime,
		dialTimeout:   cfg.DialTimeout,
		sweepInterval: cfg.SweepInterval,
		reserved:      make(map[uint32]struct{}),
		expiry:        make(map[peer.ID]time.Time),
		done:          make(chan struct{}),
	}

	if m.candidates == nil {
		m.candidates = func() []netip.AddrPort { return nil }
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	if m.maxLifetime <= 0 {
		m.maxLifetime = DefaultMaxLifetime
	}
	if m.dialTimeout <= 0 {
		m.dialTimeout = DefaultDialTimeout
	}
	if m.sweepInterval <= 0 {
		m.sweepInterval = DefaultSweepInterval
	}

	return m, nil
}

// Start registers the stream handler and begins expiring sessions. It is safe
// to call more than once.
func (m *Manager) Start() {
	if m == nil {
		return
	}

	m.startOnce.Do(func() {
		m.host.SetStreamHandler(ProtocolID, m.handleStream)
		m.spawn(m.sweep)
	})
}

// Close stops expiring sessions, stops answering the protocol, and destroys
// every session it holds.
func (m *Manager) Close() {
	if m == nil {
		return
	}

	m.closeOnce.Do(func() {
		close(m.done)
		m.host.RemoveStreamHandler(ProtocolID)

		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()

		m.wg.Wait()

		m.mu.Lock()
		live := make([]peer.ID, 0, len(m.expiry))
		for p := range m.expiry {
			live = append(live, p)
		}
		clear(m.expiry)
		clear(m.reserved)
		m.mu.Unlock()

		for _, p := range live {
			m.transport.CloseSession(p)
		}
	})
}

// Establish opens a datagram session with remote.
//
// It returns nil without dialing when this node is not the initiator: the lower
// peer ID by byte comparison dials and the higher waits, so simultaneous open
// cannot happen. Establishment is always gated on a current authorization and
// is never skipped because a session was held before, so a revoked peer cannot
// carry a data plane key across a reconnect.
func (m *Manager) Establish(ctx context.Context, remote peer.ID) error {
	if m == nil {
		return nil
	}

	if !Initiator(m.host.ID(), remote) {
		return nil
	}

	deadline, err := m.authorized(remote)
	if err != nil {
		return err
	}

	dialCtx, cancel := context.WithTimeout(ctx, m.dialTimeout)
	defer cancel()

	stream, err := m.host.NewStream(dialCtx, remote, ProtocolID)
	if err != nil {
		return fmt.Errorf("udpsession: open session stream to %s: %w", remote, err)
	}
	defer stream.Close()

	m.setStreamDeadlines(stream)

	return m.exchange(stream, remote, true, deadline)
}

// Destroy tears down a peer's session and frees its key id.
//
// Sessions are never resumed. A peer that reconnects or is re-admitted
// establishes a new one from a fresh handshake, so there is no caching window in
// which a revoked peer keeps a usable key.
func (m *Manager) Destroy(remote peer.ID) bool {
	if m == nil {
		return false
	}

	m.mu.Lock()
	delete(m.expiry, remote)
	m.mu.Unlock()

	return m.transport.CloseSession(remote)
}

// Tick destroys every session whose deadline has passed and reports how many.
//
// A session is never extended past its deadline: the peer has to complete a
// fresh handshake, which is what stops a data plane key outliving the token that
// authorized it.
func (m *Manager) Tick(now time.Time) int {
	if m == nil {
		return 0
	}

	m.mu.Lock()

	var due []peer.ID

	for p, at := range m.expiry {
		if !now.Before(at) {
			due = append(due, p)
		}
	}

	for _, p := range due {
		delete(m.expiry, p)
	}

	m.mu.Unlock()

	for _, p := range due {
		m.transport.CloseSession(p)
		m.log.Debug("datagram session expired", "peer_id", p.String())
	}

	return len(due)
}

// ExpiresAt reports when a peer's session is due to be destroyed.
func (m *Manager) ExpiresAt(remote peer.ID) (time.Time, bool) {
	if m == nil {
		return time.Time{}, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	at, ok := m.expiry[remote]

	return at, ok
}

// handleStream answers an establishment opened by the peer.
func (m *Manager) handleStream(stream network.Stream) {
	defer stream.Close()

	remote := stream.Conn().RemotePeer()

	// The lower peer ID dials. A peer that dialed when we should have dialed it
	// is not following the rule, and answering would open a second session.
	if Initiator(m.host.ID(), remote) {
		m.log.Debug("refusing session stream", "err", ErrGlare, "peer_id", remote.String())
		_ = stream.Reset()

		return
	}

	deadline, err := m.authorized(remote)
	if err != nil {
		m.log.Warn("refusing session stream", "err", err, "peer_id", remote.String())
		_ = stream.Reset()

		return
	}

	m.setStreamDeadlines(stream)

	if err := m.exchange(stream, remote, false, deadline); err != nil {
		m.log.Warn("session exchange failed", "err", err, "peer_id", remote.String())
		_ = stream.Reset()
	}
}

// exchange runs both halves of the protocol and installs what they agree on.
func (m *Manager) exchange(stream network.Stream, remote peer.ID, isInitiator bool, deadline time.Time) error {
	local, eph, err := m.buildMessage()
	if err != nil {
		return err
	}

	defer eph.zeroize()
	// Freed again after a successful install; until then this is what keeps a
	// concurrent establishment from drawing the same id.
	defer m.releaseKeyID(local.KeyID)

	remoteMsg, err := m.trade(stream, local, isInitiator)
	if err != nil {
		return err
	}

	initiator, responder := m.host.ID(), remote
	initiatorSalt, responderSalt := local.Salt, remoteMsg.Salt

	if !isInitiator {
		initiator, responder = remote, m.host.ID()
		initiatorSalt, responderSalt = remoteMsg.Salt, local.Salt
	}

	keys, err := derive(eph, remoteMsg.EphPubKey, initiatorSalt, responderSalt, initiator, responder)
	if err != nil {
		return err
	}

	return m.install(remote, local, remoteMsg, keys, isInitiator, deadline)
}

// trade writes this side's message and reads the peer's. The initiator writes
// first, so each side has exactly one message in flight and neither blocks
// waiting for the other to speak.
func (m *Manager) trade(stream network.Stream, local *message, isInitiator bool) (*message, error) {
	if isInitiator {
		if err := writeMessage(stream, local); err != nil {
			return nil, err
		}

		return readMessage(stream)
	}

	remoteMsg, err := readMessage(stream)
	if err != nil {
		return nil, err
	}

	if err := writeMessage(stream, local); err != nil {
		return nil, err
	}

	return remoteMsg, nil
}

// install puts the derived keys into the transport and starts path validation.
func (m *Manager) install(
	remote peer.ID,
	local, remoteMsg *message,
	keys *sessionKeys,
	isInitiator bool,
	deadline time.Time,
) error {
	// The key stamped on what we send is the one the peer allocated for its own
	// receive table; the key we open with carries the id we allocated.
	tx, err := datagram.NewTxKey(remoteMsg.KeyID, keys.send(isInitiator))
	if err != nil {
		clear(keys.initiatorToResponder)
		clear(keys.responderToInitiator)

		return fmt.Errorf("udpsession: send key for %s: %w", remote, err)
	}

	rx, err := datagram.NewRxKey(local.KeyID, keys.receive(isInitiator))
	if err != nil {
		tx.Zeroize()
		clear(keys.receive(isInitiator))

		return fmt.Errorf("udpsession: receive key for %s: %w", remote, err)
	}

	sess, err := m.transport.InstallSession(remote, tx, rx)
	if err != nil {
		tx.Zeroize()
		rx.Zeroize()

		return fmt.Errorf("udpsession: install session for %s: %w", remote, err)
	}

	sess.SetPathTokens(local.pathToken(), remoteMsg.pathToken())

	m.mu.Lock()
	m.expiry[remote] = deadline
	delete(m.reserved, local.KeyID)
	m.mu.Unlock()

	m.validatePath(remote, remoteMsg.candidates)

	m.log.Info("datagram session installed",
		"peer_id", remote.String(),
		"initiator", isInitiator,
		"rx_key_id", local.KeyID,
		"tx_key_id", remoteMsg.KeyID,
		"expires_at", deadline,
	)

	return nil
}

// validatePath probes the peer's advertised addresses and keeps at it while
// none has answered.
//
// One probe is not enough on its own. The two sides install a round trip apart,
// so the first probe out can reach a peer that cannot authenticate it yet, and a
// probe lost to ordinary packet loss looks no different. Nothing is ever sent to
// a candidate but the probe, so a retry costs one datagram and cannot be aimed
// at a third party. The pacing is wall clock because it is retransmission, not
// policy: the injected clock decides when a session dies, not how a probe is
// spaced.
func (m *Manager) validatePath(remote peer.ID, candidates []netip.AddrPort) {
	if len(candidates) == 0 {
		return
	}

	m.spawn(func() {
		ticker := time.NewTicker(pathRetryInterval)
		defer ticker.Stop()

		for range pathRetryAttempts {
			if m.pathSettled(remote) {
				return
			}

			// Every failure here is terminal: no session, no candidates, or a path
			// the validator has already given up on. None is retryable.
			if err := m.transport.ValidatePath(remote, candidates); err != nil {
				m.log.Debug("path validation not started", "err", err, "peer_id", remote.String())

				return
			}

			select {
			case <-m.done:
				return
			case <-ticker.C:
			}
		}
	})
}

// pathSettled reports whether there is nothing left for validation to do: the
// session is gone, an endpoint is confirmed, or the path has been given up on.
func (m *Manager) pathSettled(remote peer.ID) bool {
	sess, ok := m.transport.Sessions().Lookup(remote)
	if !ok {
		return true
	}

	if _, confirmed := sess.ConfirmedEndpoint(); confirmed {
		return true
	}

	return sess.DatagramsDisabled()
}

// buildMessage draws this side's ephemeral key, salt, key id and path token.
func (m *Manager) buildMessage() (*message, *ephemeral, error) {
	keyID, err := m.allocateKeyID()
	if err != nil {
		return nil, nil, err
	}

	eph, err := newEphemeral()
	if err != nil {
		m.releaseKeyID(keyID)

		return nil, nil, err
	}

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		eph.zeroize()
		m.releaseKeyID(keyID)

		return nil, nil, fmt.Errorf("udpsession: read salt: %w", err)
	}

	token, err := datagram.NewPathToken()
	if err != nil {
		eph.zeroize()
		m.releaseKeyID(keyID)

		return nil, nil, fmt.Errorf("udpsession: path token: %w", err)
	}

	candidates := m.candidates()
	endpoints := make([]string, 0, len(candidates))

	for _, ep := range candidates {
		if len(endpoints) == maxEndpoints {
			break
		}

		endpoints = append(endpoints, ep.String())
	}

	return &message{
		Version:   Version,
		EphPubKey: eph.publicKey(),
		Salt:      salt,
		KeyID:     keyID,
		PathToken: append([]byte(nil), token[:]...),
		Endpoints: endpoints,
	}, eph, nil
}

// allocateKeyID draws an id that is free in this node's receive table and
// reserves it. Receiver allocation is what makes ids unique by construction:
// each side only ever names an id it owns, so there is no collision to resolve.
func (m *Manager) allocateKeyID() (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for range keyIDAttempts {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("udpsession: read key id: %w", err)
		}

		id := binary.BigEndian.Uint32(raw[:])
		if id == 0 {
			continue
		}

		if _, taken := m.reserved[id]; taken {
			continue
		}

		if _, taken := m.transport.Sessions().LookupRxKey(id); taken {
			continue
		}

		m.reserved[id] = struct{}{}

		return id, nil
	}

	return 0, ErrKeyIDSpace
}

func (m *Manager) releaseKeyID(id uint32) {
	m.mu.Lock()
	delete(m.reserved, id)
	m.mu.Unlock()
}

// authorized resolves the peer's admission and the instant its session must not
// outlive.
func (m *Manager) authorized(remote peer.ID) (time.Time, error) {
	expiry, ok := m.authorize(remote)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: %s", ErrNotAdmitted, remote)
	}

	now := m.now()
	deadline := now.Add(m.maxLifetime)

	// No credential means there is nothing to cap against; the default lifetime
	// is then the only ceiling.
	if expiry.IsZero() {
		return deadline, nil
	}

	if !expiry.After(now) {
		return time.Time{}, fmt.Errorf("%w: %s", ErrExpiredToken, remote)
	}

	// A data plane key must never outlive the token that authorized it, so the
	// token wins whenever it expires first.
	if expiry.Before(deadline) {
		deadline = expiry
	}

	return deadline, nil
}

// setStreamDeadlines bounds the exchange in real time. These are the OS's I/O
// timers, so they take the wall clock: the injected clock drives session policy,
// and feeding it here would make a test clock silently expire real sockets.
func (m *Manager) setStreamDeadlines(stream network.Stream) {
	at := time.Now().Add(streamDeadline)

	if err := stream.SetReadDeadline(at); err != nil {
		m.log.Debug("could not set session stream read deadline", "err", err)
	}

	if err := stream.SetWriteDeadline(at); err != nil {
		m.log.Debug("could not set session stream write deadline", "err", err)
	}
}

func (m *Manager) sweep() {
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.Tick(m.now())
		}
	}
}
