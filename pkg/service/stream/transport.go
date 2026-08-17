package stream

import (
	"sync"

	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

const (
	defaultMaxConns       = 256
	defaultMaxConnsPerSub = 8
)

// withDefaults fills unset (<=0) caps so both transports share the same limits.
func withDefaults(cfg Config) Config {
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
	return cfg
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
}

// NewConnLimiter returns a limiter for the given caps.
func NewConnLimiter(maxConns, maxConnsPerSub int) *ConnLimiter {
	return &ConnLimiter{
		maxConns:       maxConns,
		maxConnsPerSub: maxConnsPerSub,
		perSub:         make(map[string]int),
	}
}

// acquire admits a connection for subject when both caps allow it.
func (l *ConnLimiter) acquire(subject string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns >= l.maxConns || l.perSub[subject] >= l.maxConnsPerSub {
		return false
	}
	l.conns++
	l.perSub[subject]++
	telemetry.IncStreamConnections()
	return true
}

func (l *ConnLimiter) release(subject string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conns--
	l.perSub[subject]--
	if l.perSub[subject] <= 0 {
		delete(l.perSub, subject)
	}
	telemetry.DecStreamConnections()
}
