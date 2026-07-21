package bootstrapper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/service/bootstrapper"
)

func TestGenerateRandomDelay(t *testing.T) {
	delay := bootstrapper.GenerateRandomDelay(5*time.Minute, 30*time.Minute)
	require.GreaterOrEqual(t, delay, 5*time.Minute)
	require.Less(t, delay, 30*time.Minute)

	require.Equal(t, 30*time.Minute, bootstrapper.GenerateRandomDelay(30*time.Minute, 5*time.Minute))
}
