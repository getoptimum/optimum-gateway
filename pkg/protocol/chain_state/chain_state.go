// Package chain_state holds beacon genesis time and derives slot timing
// (CurrentSlot, SlotStartTime). It does not track broader chain state.
package chain_state

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-common/pkg/logger"
)

// GenesisState holds the loaded genesis state for the chain.
type GenesisState struct {
	GenesisTime time.Time
}

var genesisState atomic.Pointer[GenesisState]

// ResetGenesisStateForTest clears loaded genesis state so duty helpers exercise pre-load fallback.
func ResetGenesisStateForTest() {
	genesisState.Store(nil)
}

// GetGenesisState returns the loaded genesis state.
func GetGenesisState() *GenesisState {
	return genesisState.Load()
}

// LoadGenesisState loads genesis time from optimum-common chain defaults (hoodi/mainnet).
func LoadGenesisState(log logger.AppLogger, cfgChain chain.Chain) error {
	log = log.With(logger.WithString("chain", cfgChain.String()))

	ts, ok := chain.GenesisTime(cfgChain.String())
	if !ok {
		return fmt.Errorf("unknown chain: no genesis time: %s", cfgChain)
	}
	genesisTime := int64(ts) //nolint:gosec // G115: known chain genesis timestamps are within int64 range
	log.Info("using genesis time for chain", logger.WithInt64("genesis_time", genesisTime))

	genesisState.Store(&GenesisState{
		GenesisTime: time.Unix(genesisTime, 0).UTC(),
	})
	return nil
}
