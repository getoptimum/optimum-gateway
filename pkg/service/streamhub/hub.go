package streamhub

import (
	"sync"
	"sync/atomic"

	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// DefaultBufferSize is the per-subscriber ring depth used when Subscribe is
// given a non-positive size.
const DefaultBufferSize = 64

// Hub broadcasts each BlockEvent to all subscribers.
type Hub struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
}

func New() *Hub {
	return &Hub{subs: make(map[*Subscription]struct{})}
}

// Subscribe registers a consumer with a bounded ring of bufSize events. The
// caller drains Events() and must Close() when done.
func (h *Hub) Subscribe(bufSize int) *Subscription {
	if bufSize <= 0 {
		bufSize = DefaultBufferSize
	}
	sub := &Subscription{hub: h, events: make(chan *BlockEvent, bufSize)}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Emit broadcasts ev without blocking; the event is shared read-only, so
// callers must not mutate it afterwards.
func (h *Hub) Emit(ev *BlockEvent) {
	h.mu.RLock()
	for sub := range h.subs {
		sub.offer(ev)
	}
	h.mu.RUnlock()
}

// Subscription is one consumer's bounded, drop-oldest view of the stream.
type Subscription struct {
	hub     *Hub
	events  chan *BlockEvent
	mu      sync.Mutex // serializes concurrent emits' evict+send
	dropped atomic.Uint64
}

// Events is the read side of the ring; Close() closes it.
func (s *Subscription) Events() <-chan *BlockEvent { return s.events }

// Dropped is the cumulative count of events dropped on overflow.
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Close unregisters the subscriber and closes its channel. Holding the hub
// write lock guarantees no Emit is mid-send when the channel closes.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	if _, ok := s.hub.subs[s]; ok {
		delete(s.hub.subs, s)
		close(s.events)
	}
	s.hub.mu.Unlock()
}

// offer enqueues ev, evicting the oldest event when the ring is full.
func (s *Subscription) offer(ev *BlockEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case s.events <- ev:
		return
	default:
	}
	select {
	case <-s.events:
		s.recordDrop()
	default:
	}
	select {
	case s.events <- ev:
	default:
		s.recordDrop()
	}
}

func (s *Subscription) recordDrop() {
	s.dropped.Add(1)
	telemetry.RecordStreamEventDropped()
}
