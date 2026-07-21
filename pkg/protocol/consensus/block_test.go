package consensus_test

import (
	"encoding/hex"
	"testing"

	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestDecodeBeaconBlock(t *testing.T) {
	tests := map[string]struct {
		src        string
		expectSlot uint64

		expectProposer uint64
		signatureHash  string
	}{
		"valid payload1": {
			src:            test_utils.HoodiBeaconBlockMessage1,
			expectSlot:     3435697,
			expectProposer: 526417,
			signatureHash:  "21a18be8d9a1f6ef6dd96df2eb8938e0051f172e9047a8781e104fff66c1a064",
		},
		"valid payload2": {
			src:            test_utils.HoodiBeaconBlockMessage2,
			expectSlot:     3435699,
			expectProposer: 1059907,
			signatureHash:  "149eca1a61aa9933f40ceda2f7bcdc1958e31d4eaf9cccb972c2bfbd5bd2b332",
		},
		"valid payload3": {
			src:            test_utils.HoodiBeaconBlockMessage3,
			expectSlot:     3435761,
			expectProposer: 1050729,
			signatureHash:  "fe6416133b1c6cd4a7d8a526559d5a36c96ef8108be50428a1901f2874c5d537",
		},
		"shared hoodi fixture": {
			src: test_utils.ValidBeaconBlockMessage,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Decode hex -> snappy -> SSZ bytes
			raw, err := hex.DecodeString(tc.src)
			require.NoError(t, err)

			block, err := consensus.DecodeBeaconBlockHeader(raw)
			require.NoError(t, err)
			if tc.expectSlot == 0 {
				require.NotZero(t, block.Header.Slot)
				require.NotZero(t, block.Header.ProposerIndex)
				return
			}
			require.Equal(t, tc.expectSlot, block.Header.Slot)
			require.Equal(t, tc.expectProposer, block.Header.ProposerIndex)
			require.Equal(t, tc.signatureHash, commonhash.SHA256(block.Signature))
		})
	}
}

func TestDecodeBeaconBlockInvalid(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		_, err := consensus.DecodeBeaconBlockHeader(nil)
		require.Error(t, err)
	})

	t.Run("invalid snappy", func(t *testing.T) {
		_, err := consensus.DecodeBeaconBlockHeader([]byte{0xff, 0xff, 0xff})
		require.Error(t, err)
		require.Contains(t, err.Error(), "snappy")
	})

	t.Run("decompressed payload too short", func(t *testing.T) {
		raw := snappy.Encode(nil, make([]byte, 50))

		_, err := consensus.DecodeBeaconBlockHeader(raw)
		require.Error(t, err)
		require.Contains(t, err.Error(), "too short")
	})

	t.Run("payload shorter than header prefix", func(t *testing.T) {
		// long enough for slot/proposer_index but truncated mid parent/state root
		raw := snappy.Encode(nil, make([]byte, 150))

		_, err := consensus.DecodeBeaconBlockHeader(raw)
		require.Error(t, err)
		require.Contains(t, err.Error(), "too short")
	})

	t.Run("wrong message offset", func(t *testing.T) {
		// zeroed offset prefix instead of the fixed 100 of SignedBeaconBlock
		raw := snappy.Encode(nil, make([]byte, 300))

		_, err := consensus.DecodeBeaconBlockHeader(raw)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected message offset")
	})
}
