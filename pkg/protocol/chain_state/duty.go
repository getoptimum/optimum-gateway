package chain_state

import (
	"time"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
)

func genesisTime() time.Time {
	if state := GetGenesisState(); state != nil {
		return state.GenesisTime
	}
	if ts, ok := chain.GenesisTime(chain.ChainHoodi.String()); ok {
		return time.Unix(int64(ts), 0).UTC() //nolint:gosec // G115: known chain genesis timestamps fit int64
	}
	return time.Time{}
}

// CurrentSlot returns the slot number at the given wall-clock time.
// If t is zero, it uses the current system time.
func CurrentSlot(t time.Time) uint64 {
	if t.IsZero() {
		t = time.Now()
	}
	return uint64(consensus.SlotAt(genesisTime(), t))
}

// SlotStartTime returns the start timestamp of a slot (UTC).
func SlotStartTime(slot uint64) time.Time {
	return consensus.SlotStartTime(genesisTime(), consensus.Slot(slot))
}
