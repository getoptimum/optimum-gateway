package consensus_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
)

func TestSSZUint64RoundTrip(t *testing.T) {
	for _, v := range []consensus.SSZUint64{0, 1, 12345, ^consensus.SSZUint64(0)} {
		b, err := v.MarshalSSZ()
		require.NoError(t, err)
		require.Len(t, b, 8)

		var got consensus.SSZUint64
		require.NoError(t, got.UnmarshalSSZ(b))
		require.Equal(t, v, got)
	}

	var s consensus.SSZUint64
	require.Error(t, s.UnmarshalSSZ([]byte{1, 2, 3}))
}

func TestSSZUint64Encoding(t *testing.T) {
	b, err := consensus.SSZUint64(1).MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, []byte{1, 0, 0, 0, 0, 0, 0, 0}, b)
}
