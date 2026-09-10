package stream

import (
	"sync"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

const (
	defaultMaxConns       = 256
	defaultMaxConnsPerSub = 8
)

// Re-auth modes: off skips re-verification, observe counts a failure and keeps
// the stream, enforce closes it. Observe ships first so a fleet is measured.
const (
	ReauthOff     = "off"
	ReauthObserve = "observe"
	ReauthEnforce = "enforce"
)

// defaultReauthInterval paces re-verification of the presented token.
const defaultReauthInterval = 60 * time.Second

// defaultKeepaliveMinTime is the shortest client ping interval accepted. The
// library default of 5m GOAWAYs any consumer that pings at a useful rate.
const defaultKeepaliveMinTime = 20 * time.Second

// withDefaults fills unset (<=0) caps so both transports share the same limits.
// Pointer only for gocritic's hugeParam; it copies, so the caller is unaffected.
func withDefaults(in *Config) Config {
	cfg := *in
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = defaultMaxConns
	}
	if cfg.MaxConnsPerSub <= 0 {
		cfg.MaxConnsPerSub = defaultMaxConnsPerSub
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = streamhub.DefaultBufferSize
	}
	// Zero disables the heartbeat, so unset cannot mean "use the default" as it
	// does for the caps; a negative clamps to disabled, never to the default.
	if cfg.HeartbeatInterval < 0 {
		cfg.HeartbeatInterval = 0
	}
	if cfg.KeepaliveMinTime <= 0 {
		cfg.KeepaliveMinTime = defaultKeepaliveMinTime
	}
	if cfg.ReauthInterval <= 0 {
		cfg.ReauthInterval = defaultReauthInterval
	}
	if cfg.ReauthMode == "" {
		cfg.ReauthMode = ReauthObserve
	}
	if cfg.Limiter == nil {
		cfg.Limiter = NewConnLimiter(cfg.MaxConns, cfg.MaxConnsPerSub)
	}
	return cfg
}

// optionalTicker returns a ticker, or nil for a non-positive interval: a nil
// channel blocks forever in a select, so a send loop opts out without branching.
func optionalTicker(d time.Duration) (ticker *time.Ticker, tick <-chan time.Time) {
	if d <= 0 {
		return nil, nil
	}
	t := time.NewTicker(d)
	return t, t.C
}

// stopTicker is nil-safe, so a disabled ticker needs no guard at the call site.
func stopTicker(t *time.Ticker) {
	if t != nil {
		t.Stop()
	}
}

// reauthInterval is 0 when re-auth is off, which optionalTicker reads as
// disabled, so the mode needs no second branch at the call sites.
func (c *Config) reauthInterval() time.Duration {
	if c.ReauthMode == ReauthOff {
		return 0
	}
	return c.ReauthInterval
}

// connAuth is one connection's auth policy, shared by both transports so only
// the way a stream ends stays transport-specific.
type connAuth struct {
	cfg     *Config
	auth    ConsumerAuthenticator
	log     logger.AppLogger
	subject string
	failing bool
}

// accept reports whether a refresh may replace the current token. It may only
// re-prove the admitted subject, which owns the cap slot and the log lines.
func (c *connAuth) accept(candidate string) bool {
	got, err := c.auth.Authenticate(candidate)
	if err != nil || got != c.subject {
		telemetry.RecordStreamAuthFailure()
		return false
	}
	return true
}

// reverify re-checks the presented token and reports whether to cut the stream.
// The verifier checks exp, so expiry needs no tracking here.
func (c *connAuth) reverify(token string) (cut bool) {
	_, err := c.auth.Authenticate(token)
	if err == nil {
		c.failing = false
		return false
	}
	telemetry.RecordStreamReauthFailure()
	if c.cfg.ReauthMode == ReauthEnforce {
		c.log.Error("closing consumer stream, token no longer verifies (enforce mode)", err,
			logger.WithString("subject", c.subject))
		return true
	}
	// Transition only: observe mode exists to learn which consumers have not
	// adopted refresh, and the metric cannot name them.
	if !c.failing {
		c.failing = true
		c.log.Error("consumer stream token no longer verifies, keeping stream (observe mode)", err,
			logger.WithString("subject", c.subject))
	}
	return false
}

// offerLatest replaces any queued value so the newest token wins, never
// blocking: a dropped intermediate token costs nothing, the client refreshes.
func offerLatest(ch chan string, v string) {
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- v:
	default:
	}
}

// normalizeMode defaults empty to metadata and reports whether the value is allowed.
func normalizeMode(mode string) (string, bool) {
	if mode == "" {
		mode = modeMetadata
	}
	return mode, mode == modeMetadata || mode == modeRaw
}

// topicsOK is true when every topic is empty or the v1-only beacon_block topic.
func topicsOK(topics ...string) bool {
	for _, t := range topics {
		if t != "" && t != defaultTopic {
			return false
		}
	}
	return true
}

// ConnLimiter enforces the global and per-subject connection caps. One instance
// is shared by both transports so the caps stay global, not per-transport (ADR-0011).
type ConnLimiter struct {
	maxConns       int
	maxConnsPerSub int

	mu     sync.Mutex
	conns  int
	perSub map[string]int
	// starts is keyed by the id acquire returns, so releasing a newer
	// connection cannot be mistaken for releasing the oldest.
	nextID uint64
	starts map[uint64]time.Time
}

// NewConnLimiter returns a limiter for the given caps.
func NewConnLimiter(maxConns, maxConnsPerSub int) *ConnLimiter {
	return &ConnLimiter{
		maxConns:       maxConns,
		maxConnsPerSub: maxConnsPerSub,
		perSub:         make(map[string]int),
		starts:         make(map[uint64]time.Time),
	}
}

// acquire admits a connection when both caps allow it, returning the closer
// that frees it so the bookkeeping stays here rather than in every caller.
func (l *ConnLimiter) acquire(subject string) (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns >= l.maxConns || l.perSub[subject] >= l.maxConnsPerSub {
		return nil, false
	}
	l.conns++
	l.perSub[subject]++
	id := l.nextID
	l.nextID++
	l.starts[id] = time.Now()
	l.publishOldest()
	telemetry.IncStreamConnections()
	return func() { l.release(subject, id) }, true
}

func (l *ConnLimiter) release(subject string, id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Idempotent: the id records that this connection is still counted, so a
	// repeated release cannot decrement the caps twice.
	if _, live := l.starts[id]; !live {
		return
	}
	l.conns--
	l.perSub[subject]--
	if l.perSub[subject] <= 0 {
		delete(l.perSub, subject)
	}
	delete(l.starts, id)
	l.publishOldest()
	telemetry.DecStreamConnections()
}

// oldestStart returns the earliest live connection's start, or the zero time
// when none are open. Caller holds l.mu.
func (l *ConnLimiter) oldestStart() time.Time {
	var oldest time.Time
	for _, t := range l.starts {
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	return oldest
}

// publishOldestStart is the telemetry seam, swapped in tests so the publish
// itself is asserted rather than assumed.
var publishOldestStart = telemetry.SetStreamOldestConnectionStart

// publishOldest republishes the oldest start. Called on every membership
// change, the only time the answer can change, so no ticker is needed.
func (l *ConnLimiter) publishOldest() {
	oldest := l.oldestStart()
	if oldest.IsZero() {
		publishOldestStart(0)
		return
	}
	publishOldestStart(float64(oldest.Unix()))
}
