package utils_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestShrinkPeerID(t *testing.T) {
	table := map[string]string{
		"":          "",
		"abc":       "abc",
		"12345678":  "...12345678",
		"123456789": "...23456789",
		"QmYyQSo1c1Ym7orWxLYvCrM794i86YACRPBJFa9h7rVdQ3": "...9h7rVdQ3",
	}
	for input, output := range table {
		require.Equal(t, output, utils.ShrinkPeerID(input))
	}
}

func TestParseCommaSeparatedUint64s(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		got, err := utils.ParseCommaSeparatedUint64s("")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("whitespace-only input returns nil", func(t *testing.T) {
		got, err := utils.ParseCommaSeparatedUint64s("   \t  ")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("happy path", func(t *testing.T) {
		got, err := utils.ParseCommaSeparatedUint64s("42,1337,9001")
		require.NoError(t, err)
		require.Equal(t, []uint64{42, 1337, 9001}, got)
	})
	t.Run("tolerates whitespace and empty segments", func(t *testing.T) {
		got, err := utils.ParseCommaSeparatedUint64s(" 42 , , 1337 ,")
		require.NoError(t, err)
		require.Equal(t, []uint64{42, 1337}, got)
	})
	t.Run("rejects non-numeric segments", func(t *testing.T) {
		_, err := utils.ParseCommaSeparatedUint64s("42,nope,1337")
		require.Error(t, err)
	})
	t.Run("rejects negative numbers", func(t *testing.T) {
		_, err := utils.ParseCommaSeparatedUint64s("42,-1")
		require.Error(t, err)
	})
}
