package streamhub_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
)

// A full ring must not block Emit; it drops the oldest events and keeps the
// newest, counting the drops.
func TestHubDropsOldestWhenFull(t *testing.T) {
	h := streamhub.New()
	sub := h.Subscribe(2)

	for i := 1; i <= 5; i++ {
		h.Emit(&streamhub.BlockEvent{Slot: uint64(i)}) // no consumer draining
	}

	require.Equal(t, uint64(3), sub.Dropped())
	require.Equal(t, []uint64{4, 5}, drainSlots(sub))
}

// After Close, buffered events stay readable, the channel then reports closed,
// and further Emit is a no-op.
func TestHubCloseStopsSubscription(t *testing.T) {
	h := streamhub.New()
	sub := h.Subscribe(2)
	h.Emit(&streamhub.BlockEvent{Slot: 1})

	sub.Close()

	ev, ok := <-sub.Events()
	require.True(t, ok)
	require.Equal(t, uint64(1), ev.Slot)
	_, ok = <-sub.Events()
	require.False(t, ok)

	require.NotPanics(t, func() { h.Emit(&streamhub.BlockEvent{Slot: 2}) })
}

func drainSlots(sub *streamhub.Subscription) []uint64 {
	var out []uint64
	for {
		select {
		case ev := <-sub.Events():
			out = append(out, ev.Slot)
		default:
			return out
		}
	}
}
