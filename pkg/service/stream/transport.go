package stream

import (
	"sync"
	"time"

	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// Re-auth modes. off skips re-verification entirely; observe counts a failure
// and keeps the stream; enforce closes it. Shipping in observe lets a fleet
// learn how many consumers would be cut before any of them are.
const (
	ReauthOff     = "off"
	ReauthObserve = "observe"
	ReauthEnforce = "enforce"
)

const (
	defaultHeartbeatInterval = 20 * time.Second
	defaultReauthInterval    = 60 * time.Second
	defaultKeepaliveMinTime  = 20 * time.Second
)

const (
	defaultMaxConns       = 256
	defaultMaxConnsPerSub = 8
)

// withDefaults fills unset (<=0) caps so both transports share the same limits.
// Takes a pointer only to stay under gocritic's hugeParam threshold; it works on
// a copy, so the caller's Config is never mutated.
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
	if cfg.Limiter == nil {
		cfg.Limiter = NewConnLimiter(cfg.MaxConns, cfg.MaxConnsPerSub)
	}
	// Negative means unset; zero is meaningful and disables the heartbeat.
	if cfg.HeartbeatInterval < 0 {
		cfg.HeartbeatInterval = defaultHeartbeatInterval
	}
	if cfg.ReauthInterval <= 0 {
		cfg.ReauthInterval = defaultReauthInterval
	}
	if cfg.KeepaliveMinTime <= 0 {
		cfg.KeepaliveMinTime = defaultKeepaliveMinTime
	}
	if cfg.ReauthMode == "" {
		cfg.ReauthMode = ReauthObserve
	}
	return cfg
}

// ReauthEnabled reports whether re-verification should run at all.
func (c *Config) ReauthEnabled() bool { return c.ReauthMode != ReauthOff }

// offerLatest replaces any queued value so the newest token wins. It never
// blocks: the read side that produced the token must stay responsive, and a
// dropped intermediate token costs nothing because the client refreshes again.
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

// reauthTicker returns a ticker for re-verification, or nil when re-auth is
// off. A nil channel blocks forever in a select, which is how a send loop opts
// out without branching.
func reauthTicker(cfg *Config) (ticker *time.Ticker, tick <-chan time.Time) {
	if !cfg.ReauthEnabled() {
		return nil, nil
	}
	t := time.NewTicker(cfg.ReauthInterval)
	return t, t.C
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

// acquire admits a connection for subject when both caps allow it, returning
// the id that release must be given.
func (l *ConnLimiter) acquire(subject string) (uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns >= l.maxConns || l.perSub[subject] >= l.maxConnsPerSub {
		return 0, false
	}
	l.conns++
	l.perSub[subject]++
	id := l.nextID
	l.nextID++
	l.starts[id] = time.Now()
	l.publishOldest()
	telemetry.IncStreamConnections()
	return id, true
}

func (l *ConnLimiter) release(subject string, id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conns--
	l.perSub[subject]--
	if l.perSub[subject] <= 0 {
		delete(l.perSub, subject)
	}
	delete(l.starts, id)
	l.publishOldest()
	telemetry.DecStreamConnections()
}

// publishOldest republishes the oldest start time. Called under l.mu on every
// membership change, which is the only time the answer can change, so the
// gauge never needs a ticker. O(conns) against a cap of a few hundred.
func (l *ConnLimiter) publishOldest() {
	var oldest time.Time
	for _, t := range l.starts {
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	if oldest.IsZero() {
		telemetry.SetStreamOldestConnectionStart(0)
		return
	}
	telemetry.SetStreamOldestConnectionStart(float64(oldest.Unix()))
}
