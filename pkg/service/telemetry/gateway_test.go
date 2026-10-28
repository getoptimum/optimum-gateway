package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthWithoutTelemetry(t *testing.T) {
	prev := enabledMetrics
	enabledMetrics = false
	lastCLMessageAt.Store(0)
	lastMumMessageAt.Store(0)
	t.Cleanup(func() {
		enabledMetrics = prev
		lastCLMessageAt.Store(0)
		lastMumMessageAt.Store(0)
	})

	require.Equal(t, int64(0), HealthCL())
	require.Equal(t, int64(0), HealthMUM())
	RecordCLMessageAt()
	RecordMumMessageAt()
	require.Equal(t, int64(1), HealthCL())
	require.Equal(t, int64(1), HealthMUM())
}

func TestConnectionAlive(t *testing.T) {
	require.Equal(t, float64(1), connectionAlive(100, 100))
	require.Equal(t, float64(1), connectionAlive(100, 99))
	require.Equal(t, float64(1), connectionAlive(100, 71))
	require.Equal(t, float64(1), connectionAlive(100, 70))
	require.Equal(t, float64(0), connectionAlive(100, 69))
}
