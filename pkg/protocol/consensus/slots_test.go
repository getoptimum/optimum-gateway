package consensus_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
)

func TestSlotToEpoch(t *testing.T) {
	tests := []struct {
		name string
		slot consensus.Slot
		want consensus.Epoch
	}{
		{"genesis slot", 0, 0},
		{"last slot of epoch 0", consensus.SlotsPerEpoch - 1, 0},
		{"first slot of epoch 1", consensus.SlotsPerEpoch, 1},
		{"epoch 2", consensus.SlotsPerEpoch * 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := consensus.Epoch(uint64(tt.slot) / consensus.SlotsPerEpoch)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSlotAtAndStartTime(t *testing.T) {
	genesis := time.Unix(1_700_000_000, 0).UTC()

	// Given a time N slots after genesis, SlotAt returns N and SlotStartTime is its inverse.
	for _, slot := range []consensus.Slot{0, 1, 31, 32, 1000} {
		at := genesis.Add(time.Duration(uint64(slot)*consensus.SecondsPerSlot) * time.Second)
		require.Equal(t, slot, consensus.SlotAt(genesis, at))
		require.Equal(t, at.UTC(), consensus.SlotStartTime(genesis, slot))
	}

	// Times within a slot round down; times at or before genesis are slot 0.
	require.Equal(t, consensus.Slot(1), consensus.SlotAt(genesis, genesis.Add(20*time.Second)))
	require.Equal(t, consensus.Slot(0), consensus.SlotAt(genesis, genesis))
	require.Equal(t, consensus.Slot(0), consensus.SlotAt(genesis, genesis.Add(-time.Hour)))
}
