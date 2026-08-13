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

// connLimiter enforces the global and per-subject connection caps shared by the
// WS and gRPC transports (ADR-0011).
type connLimiter struct {
	maxConns       int
	maxConnsPerSub int

	mu     sync.Mutex
	conns  int
	perSub map[string]int
}

func newConnLimiter(maxConns, maxConnsPerSub int) *connLimiter {
	return &connLimiter{
		maxConns:       maxConns,
		maxConnsPerSub: maxConnsPerSub,
		perSub:         make(map[string]int),
	}
}

// acquire admits a connection for subject when both caps allow it.
func (l *connLimiter) acquire(subject string) bool {
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

func (l *connLimiter) release(subject string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conns--
	l.perSub[subject]--
	if l.perSub[subject] <= 0 {
		delete(l.perSub, subject)
	}
	telemetry.DecStreamConnections()
}
