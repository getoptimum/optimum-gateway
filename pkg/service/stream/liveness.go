package stream

import (
	"time"

	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
)

// livenessState tracks what a heartbeat needs to say. One per connection,
// owned by the send loop that writes both the blocks and the heartbeats, so it
// needs no synchronization.
type livenessState struct {
	lastSlot  uint64
	lastBlock int64 // unix milli of the last block emitted, 0 if none
}

// observe records that a block went out.
func (l *livenessState) observe(slot uint64) {
	l.lastSlot = slot
	l.lastBlock = time.Now().UnixMilli()
}

// snapshot returns last slot, the slot the chain should be on now, and how
// long since a block was emitted. silence is 0 until the first block, because
// "silent since the connection opened" would otherwise read as a stalled feed
// on every fresh subscriber.
func (l *livenessState) snapshot(now time.Time) (lastSlot, expectedSlot, silenceMs uint64) {
	lastSlot = l.lastSlot
	expectedSlot = chainstate.CurrentSlot(now)
	if last := l.lastBlock; last > 0 {
		if ms := now.UnixMilli() - last; ms > 0 {
			silenceMs = uint64(ms)
		}
	}
	return lastSlot, expectedSlot, silenceMs
}

// heartbeatTicker returns a ticker for the configured interval, or nil when
// heartbeats are disabled. A nil channel blocks forever in a select, which is
// how the send loops opt out without branching.
func heartbeatTicker(interval time.Duration) (ticker *time.Ticker, tick <-chan time.Time) {
	if interval <= 0 {
		return nil, nil
	}
	t := time.NewTicker(interval)
	return t, t.C
}
