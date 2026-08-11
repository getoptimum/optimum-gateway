package streamhub

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// DefaultBufferSize is the per-subscriber buffer depth used when Subscribe is
// given a non-positive size.
const DefaultBufferSize = 64

// Service broadcasts each BlockEvent to all subscribers.
type Service struct {
	bc   *syncx.Broadcaster[*BlockEvent]
	mu   sync.Mutex
	subs map[string]*Subscription
	seq  uint64
}

func New() *Service {
	return &Service{
		bc:   syncx.NewBroadcaster[*BlockEvent](),
		subs: make(map[string]*Subscription),
	}
}

// Subscribe registers a consumer with a bounded buffer of bufSize events. The
// caller drains Events() and must Close() when done.
func (s *Service) Subscribe(bufSize int) *Subscription {
	if bufSize <= 0 {
		bufSize = DefaultBufferSize
	}
	s.mu.Lock()
	s.seq++
	key := strconv.FormatUint(s.seq, 10)
	ch := s.bc.RegisterBufferedListener(key, bufSize)
	sub := &Subscription{svc: s, key: key, events: ch}
	s.subs[key] = sub
	s.mu.Unlock()
	return sub
}

// Emit broadcasts ev without blocking; the event is shared read-only, so
// callers must not mutate it afterwards.
func (s *Service) Emit(ev *BlockEvent) {
	s.bc.BroadcastTry(ev, func(key string, num uint64) {
		s.mu.Lock()
		sub, ok := s.subs[key]
		s.mu.Unlock()
		if !ok {
			return
		}
		for range num {
			sub.recordDrop()
		}
	})
}

// Subscription is one consumer's bounded view of the stream.
type Subscription struct {
	svc     *Service
	key     string
	events  chan *BlockEvent
	dropped atomic.Uint64
}

// Events is the read side of the buffer; Close() closes it.
func (s *Subscription) Events() <-chan *BlockEvent { return s.events }

// Dropped is the cumulative count of events dropped on overflow.
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Close unregisters the subscriber and closes its channel.
func (s *Subscription) Close() {
	s.svc.bc.UnregisterListener(s.key)
	s.svc.mu.Lock()
	delete(s.svc.subs, s.key)
	s.svc.mu.Unlock()
}

func (s *Subscription) recordDrop() {
	s.dropped.Add(1)
	telemetry.RecordStreamEventDropped()
}
