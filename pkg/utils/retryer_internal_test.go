package utils

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// fn ignores ctx so the loop's ctx.Done() branch is what stops the retries.
func TestRetryRequestStopsRetryingWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls atomic.Int32
	_, code, err := retryRequest(ctx, func() (*struct{}, int, error) {
		calls.Add(1)
		return &struct{}{}, http.StatusInternalServerError, nil
	}, func(c int) bool { return c == http.StatusOK })

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, code)
	require.Equal(t, int32(1), calls.Load(), "a canceled context must stop the loop after the first attempt")
}
