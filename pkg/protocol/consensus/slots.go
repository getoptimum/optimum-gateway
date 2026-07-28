package consensus

import "time"

const (
	SecondsPerSlot = 12
	SlotsPerEpoch  = 32
)

// SlotAt returns the slot number at wall-clock time t for a chain with the given genesis time.
func SlotAt(genesis, t time.Time) Slot {
	if !t.After(genesis) {
		return 0
	}
	//nolint:gosec // G115: guarded by !t.After(genesis) above, so the value is non-negative.
	return Slot(uint64(t.Sub(genesis)/time.Second) / SecondsPerSlot)
}

// SlotStartTime returns the UTC wall-clock start time of a slot.
func SlotStartTime(genesis time.Time, slot Slot) time.Time {
	//nolint:gosec // G115: slot*SecondsPerSlot fits int64 far beyond any real slot (see note above).
	return genesis.Add(time.Duration(uint64(slot)*SecondsPerSlot) * time.Second).UTC()
}
