package streamhub

import (
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// DefaultBufferSize is the per-subscriber buffer depth used when Subscribe is
// given a non-positive size.
const DefaultBufferSize = 64

// Service broadcasts each BlockEvent to all subscribers.
type Service struct {
	bc      *syncx.Broadcaster[*BlockEvent]
	dropped *syncx.RWMap[string, *atomic.Uint64] // listener key -> subscriber drop counter
}

func New() *Service {
	return &Service{
		bc:      syncx.NewBroadcaster[*BlockEvent](),
		dropped: syncx.NewRWMap[string, *atomic.Uint64](),
	}
}

// Subscribe registers a consumer with a bounded buffer of bufSize events. The
// caller drains Events() and must Close() when done.
func (s *Service) Subscribe(bufSize int) *Subscription {
	if bufSize <= 0 {
		bufSize = DefaultBufferSize
	}
	key := uuid.New().String()
	ch := s.bc.RegisterBufferedListener(key, bufSize)
	sub := &Subscription{svc: s, key: key, events: ch}
	s.dropped.Store(key, &sub.dropped)
	return sub
}

// Emit broadcasts ev without blocking; on a full subscriber buffer the event is
// dropped. ev is shared read-only, so callers must not mutate it afterwards.
func (s *Service) Emit(ev *BlockEvent) {
	s.bc.BroadcastTry(ev, func(key string, num uint64) {
		if d, ok := s.dropped.Load(key); ok {
			d.Add(num)
		}
		for range num {
			telemetry.RecordStreamEventDropped()
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
	s.svc.dropped.Delete(s.key)
}
