package consensus_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestRPCWireTypesSSZRoundTrip(t *testing.T) {
	attnets := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	syncnets := [1]byte{0x0f}

	t.Run("Status", func(t *testing.T) {
		want := consensus.Status{
			ForkDigest: [4]byte{1, 2, 3, 4}, FinalizedRoot: test_utils.Arr32(0xaa),
			FinalizedEpoch: 5, HeadRoot: test_utils.Arr32(0xbb), HeadSlot: 99,
		}
		requireSSZRoundTrip(t, &want, 84)
	})

	t.Run("StatusV2", func(t *testing.T) {
		want := consensus.StatusV2{
			ForkDigest: [4]byte{1, 2, 3, 4}, FinalizedRoot: test_utils.Arr32(0xaa),
			FinalizedEpoch: 5, HeadRoot: test_utils.Arr32(0xbb), HeadSlot: 99,
			EarliestAvailableSlot: 50,
		}
		requireSSZRoundTrip(t, &want, 92)
	})

	t.Run("MetaDataV0", func(t *testing.T) {
		want := consensus.MetaDataV0{SeqNumber: 7, Attnets: attnets}
		requireSSZRoundTrip(t, &want, 16)
	})

	t.Run("MetaDataV1", func(t *testing.T) {
		want := consensus.MetaDataV1{SeqNumber: 7, Attnets: attnets, Syncnets: syncnets}
		requireSSZRoundTrip(t, &want, 17)
	})

	t.Run("MetaDataV2", func(t *testing.T) {
		want := consensus.MetaDataV2{
			SeqNumber: 7, Attnets: attnets, Syncnets: syncnets, CustodyGroupCount: 8,
		}
		requireSSZRoundTrip(t, &want, 25)
	})
}

type sszRoundTripper interface {
	MarshalSSZ() ([]byte, error)
	UnmarshalSSZ([]byte) error
}

func requireSSZRoundTrip(t *testing.T, want sszRoundTripper, size int) {
	t.Helper()

	raw, err := want.MarshalSSZ()
	require.NoError(t, err)
	require.Len(t, raw, size)

	got := reflect.New(reflect.TypeOf(want).Elem()).Interface().(sszRoundTripper)
	require.NoError(t, got.UnmarshalSSZ(raw))
	require.Equal(t, want, got)
}
