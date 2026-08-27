package utils_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

type retryResponse struct {
	Value string `json:"value"`
}

func TestIsPostSuccess(t *testing.T) {
	tests := map[int]bool{
		http.StatusOK:                  true,
		http.StatusCreated:             true,
		http.StatusNoContent:           false,
		http.StatusBadRequest:          false,
		http.StatusInternalServerError: false,
		0:                              false,
	}

	for code, want := range tests {
		require.Equal(t, want, utils.IsPostSuccess(code))
	}
}

func TestRetryPostRequest_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "created"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{http.StatusBadGateway, http.StatusCreated}, want)

	got, code, err := utils.RetryPostRequest[retryResponse](context.Background(), srv.URL, map[string]string{"hello": "world"}, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 2, calls.Load())
}

func TestRetryPostRequest_ReturnsLastFailureAfterExhaustion(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "still failing"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{
		http.StatusBadGateway,
		http.StatusBadGateway,
		http.StatusBadGateway,
	}, want)

	got, code, err := utils.RetryPostRequest[retryResponse](context.Background(), srv.URL, map[string]string{"hello": "world"}, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 3, calls.Load())
}

func TestRetryGetRequest_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "ok"}
	srv, calls := newRetryServer(t, http.MethodGet, []int{http.StatusInternalServerError, http.StatusOK}, want)

	got, code, err := utils.RetryGetRequest[retryResponse](context.Background(), srv.URL, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 2, calls.Load())
}

func newRetryServer(t *testing.T, wantMethod string, statuses []int, body retryResponse) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, wantMethod, r.Method)

		callIndex := int(calls.Add(1)) - 1
		status := statuses[callIndex]

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

func TestRetryPostRequest_StopsOnStopCode(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "stop"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{http.StatusBadGateway, http.StatusBadRequest}, want)

	got, code, err := utils.RetryPostRequest[retryResponse](
		context.Background(), srv.URL, map[string]string{"hello": "world"}, nil, http.StatusBadRequest,
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 2, calls.Load())
}

func TestRetryPostRequest_RetriesUntilSuccess_WithUnusedStopCodes(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "created"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{
		http.StatusBadGateway,
		http.StatusInternalServerError,
		http.StatusCreated,
	}, want)

	got, code, err := utils.RetryPostRequest[retryResponse](
		context.Background(), srv.URL, map[string]string{"hello": "world"}, nil, http.StatusUnauthorized, http.StatusForbidden,
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 3, calls.Load())
}

func TestRetryPostRequest_DoNotRetry(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "created"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{
		http.StatusUnauthorized,
	}, want)

	got, code, err := utils.RetryPostRequest[retryResponse](
		context.Background(), srv.URL, map[string]string{"hello": "world"},
		nil,
		http.StatusUnauthorized,
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 1, calls.Load())
}

func TestRetryGetRequestStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cancel()
		// Never respond. Writing a status here would race the cancellation: if the
		// response landed first the call returned it with a nil error, the opposite
		// of what this asserts. Leaving the client's canceled context as the only
		// way out makes the transport error the single possible outcome.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	got, code, err := utils.RetryGetRequest[retryResponse](ctx, srv.URL, nil)

	require.ErrorContains(t, err, "context canceled")
	require.Zero(t, code)
	require.Nil(t, got)
	require.LessOrEqual(t, calls.Load(), int32(1))
}

// The test above pins that a canceled request surfaces as a context error, but
// not that the retry loop itself gives up: once ctx is canceled the real fn
// fails before reaching the server, so the attempt count looks identical either
// way and only the elapsed time differs. Driving the loop with an fn that
// ignores ctx makes "stopped after one attempt" observable as a count.
func TestRetryRequestStopsRetryingWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls atomic.Int32
	_, code, err := utils.RetryRequestForTest(ctx, func() (*retryResponse, int, error) {
		calls.Add(1)
		return &retryResponse{Value: "retry-me"}, http.StatusInternalServerError, nil
	}, func(c int) bool { return c == http.StatusOK })

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, code)
	require.Equal(t, int32(1), calls.Load(), "a canceled context must stop the loop after the first attempt")
}
