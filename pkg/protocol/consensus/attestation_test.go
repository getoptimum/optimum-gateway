package consensus_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestValidBeaconAttestation31Decodes(t *testing.T) {
	raw, err := hex.DecodeString(test_utils.ValidBeaconAttestation31)
	require.NoError(t, err)

	var att consensus.SingleAttestation
	require.NoError(t, (&consensus.SSZSnappyCodec{}).DecodeGossip(raw, &att))
	require.Equal(t, uint64(997023), uint64(att.AttesterIndex))
	require.Equal(t, uint64(2778822), uint64(att.Data.Slot))
}

func TestAttestationDataSSZRoundTrip(t *testing.T) {
	data := consensus.AttestationData{
		Slot:            111,
		CommitteeIndex:  3,
		BeaconBlockRoot: test_utils.Arr32(0xaa),
		Source:          consensus.Checkpoint{Epoch: 5, Root: test_utils.Arr32(0xbb)},
		Target:          consensus.Checkpoint{Epoch: 6, Root: test_utils.Arr32(0xcc)},
	}
	raw, err := data.MarshalSSZ()
	require.NoError(t, err)
	require.Len(t, raw, 128)

	var got consensus.AttestationData
	require.NoError(t, got.UnmarshalSSZ(raw))
	require.Equal(t, data, got)
}

func TestSingleAttestationSSZRoundTrip(t *testing.T) {
	want := consensus.SingleAttestation{
		CommitteeIndex: 7,
		AttesterIndex:  42,
		Data: consensus.AttestationData{
			Slot:            111,
			CommitteeIndex:  3,
			BeaconBlockRoot: test_utils.Arr32(0xaa),
			Source:          consensus.Checkpoint{Epoch: 5, Root: test_utils.Arr32(0xbb)},
			Target:          consensus.Checkpoint{Epoch: 6, Root: test_utils.Arr32(0xcc)},
		},
		Signature: test_utils.Arr96(0xdd),
	}
	raw, err := want.MarshalSSZ()
	require.NoError(t, err)
	require.Len(t, raw, 240)

	var got consensus.SingleAttestation
	require.NoError(t, got.UnmarshalSSZ(raw))
	require.Equal(t, want, got)
}
