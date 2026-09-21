package stream

import (
	"time"

	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
)

// livenessState tracks what a heartbeat needs to say. Owned by the one send
// loop that writes both blocks and heartbeats, so it needs no synchronization.
type livenessState struct {
	lastSlot  uint64
	lastBlock int64 // unix milli of the last block emitted, 0 if none
}

// observe records that a block went out.
func (l *livenessState) observe(slot uint64) {
	l.lastSlot = slot
	l.lastBlock = time.Now().UnixMilli()
}

// snapshot returns last slot, expected slot, and silence since the last block.
// Silence stays 0 until the first block, or fresh subscribers read as stalled.
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
