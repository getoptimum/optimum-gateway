package chain_state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-common/pkg/logger"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
)

func TestLoadGenesisStateUsesChainDefaults(t *testing.T) {
	log := logger.NewAppSLogger(logger.Debug)

	tests := []struct {
		name     string
		cfgChain chain.Chain
	}{
		{name: "hoodi", cfgChain: chain.ChainHoodi},
		{name: "mainnet", cfgChain: chain.ChainMainnet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genesisTime, ok := chain.GenesisTime(tt.cfgChain.String())
			require.True(t, ok)

			err := chainstate.LoadGenesisState(log, tt.cfgChain)
			require.NoError(t, err)

			state := chainstate.GetGenesisState()
			require.NotNil(t, state)
			require.Equal(t, time.Unix(int64(genesisTime), 0).UTC(), state.GenesisTime)
		})
	}
}

func TestLoadGenesisStateRejectsUnsupportedChain(t *testing.T) {
	err := chainstate.LoadGenesisState(logger.NewAppSLogger(logger.Debug), chain.Chain(""))
	require.ErrorContains(t, err, "unknown chain: no genesis time")
}

func TestCurrentSlotUsesHoodiGenesisBeforeLoadGenesisState(t *testing.T) {
	chainstate.ResetGenesisStateForTest()
	t.Cleanup(chainstate.ResetGenesisStateForTest)

	hoodiGenesis, ok := chain.GenesisTime(chain.ChainHoodi.String())
	require.True(t, ok)
	require.Nil(t, chainstate.GetGenesisState())

	genesis := time.Unix(int64(hoodiGenesis), 0).UTC()
	slotDuration := time.Duration(consensus.SecondsPerSlot) * time.Second
	require.Equal(t, uint64(0), chainstate.CurrentSlot(genesis.Add(-time.Second)))
	require.Equal(t, uint64(0), chainstate.CurrentSlot(genesis))
	require.Equal(t, uint64(1), chainstate.CurrentSlot(genesis.Add(slotDuration)))
	require.Equal(t, genesis.Add(slotDuration), chainstate.SlotStartTime(1))
}

func TestCurrentSlotAndSlotStartTimeUseLoadedGenesisState(t *testing.T) {
	hoodiGenesis, ok := chain.GenesisTime(chain.ChainHoodi.String())
	require.True(t, ok)

	err := chainstate.LoadGenesisState(
		logger.NewAppSLogger(logger.Debug),
		chain.ChainHoodi,
	)
	require.NoError(t, err)

	genesis := time.Unix(int64(hoodiGenesis), 0).UTC()
	slotDuration := time.Duration(consensus.SecondsPerSlot) * time.Second
	require.Equal(t, uint64(0), chainstate.CurrentSlot(genesis.Add(-time.Second)))
	require.Equal(t, uint64(1), chainstate.CurrentSlot(genesis.Add(slotDuration)))
	require.Equal(t, genesis.Add(slotDuration), chainstate.SlotStartTime(1))
}
