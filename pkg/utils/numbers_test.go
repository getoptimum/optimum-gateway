package utils_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestDiffUint64(t *testing.T) {
	tests := map[uint64][2]uint64{
		0:          {0, 0},                       // 0 - 0
		7:          {10, 3},                      // 10 - 3
		5:          {5, 0},                       // 5 - 0
		^uint64(0): {^uint64(0), 0},              // max - 0
		1:          {^uint64(0), ^uint64(0) - 1}, // max - (max-1)
	}
	for expected, input := range tests {
		require.Equal(t, expected, utils.DiffUint64(input[0], input[1]))
		require.Equal(t, expected, utils.DiffUint64(input[1], input[0]))
	}
}
