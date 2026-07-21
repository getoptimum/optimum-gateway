package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectionAlive(t *testing.T) {
	require.Equal(t, float64(1), connectionAlive(100, 100))
	require.Equal(t, float64(1), connectionAlive(100, 99))
	require.Equal(t, float64(1), connectionAlive(100, 71))
	require.Equal(t, float64(1), connectionAlive(100, 70))
	require.Equal(t, float64(0), connectionAlive(100, 69))
}
