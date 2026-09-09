package stream

import (
	"sync"
	"time"

	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

const (
	defaultMaxConns       = 256
	defaultMaxConnsPerSub = 8
)

// defaultHeartbeatInterval paces the in-band liveness frame.
const defaultHeartbeatInterval = 20 * time.Second

// defaultKeepaliveMinTime is the shortest client ping interval the gRPC server
// accepts. The library default is 5m, which GOAWAYs any consumer that enables
// keepalive at a rate useful to a long-lived stream.
const defaultKeepaliveMinTime = 20 * time.Second

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
	// Negative means unset; zero is meaningful and disables the heartbeat.
	if cfg.HeartbeatInterval < 0 {
		cfg.HeartbeatInterval = defaultHeartbeatInterval
	}
	if cfg.KeepaliveMinTime <= 0 {
		cfg.KeepaliveMinTime = defaultKeepaliveMinTime
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

// oldestStart returns the earliest live connection's start, or the zero time
// when none are open. Caller holds l.mu. O(conns) against a cap of a few
// hundred.
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

// publishOldest republishes the oldest start time. Called under l.mu on every
// membership change, which is the only time the answer can change, so the
// gauge never needs a ticker.
func (l *ConnLimiter) publishOldest() {
	oldest := l.oldestStart()
	if oldest.IsZero() {
		publishOldestStart(0)
		return
	}
	publishOldestStart(float64(oldest.Unix()))
}
