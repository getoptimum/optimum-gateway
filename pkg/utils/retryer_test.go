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

func TestRetryPostRequestWithStopCodes_StopsOnStopCode(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "stop"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{http.StatusBadGateway, http.StatusBadRequest}, want)

	got, code, err := utils.RetryPostRequestWithStopCodes[retryResponse](
		context.Background(), srv.URL, map[string]string{"hello": "world"}, nil, []int{http.StatusBadRequest},
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 2, calls.Load())
}

func TestRetryPostRequestWithStopCodes_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	want := retryResponse{Value: "created"}
	srv, calls := newRetryServer(t, http.MethodPost, []int{
		http.StatusBadGateway,
		http.StatusInternalServerError,
		http.StatusCreated,
	}, want)

	got, code, err := utils.RetryPostRequestWithStopCodes[retryResponse](
		context.Background(), srv.URL, map[string]string{"hello": "world"}, nil, []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
		},
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, code)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
	require.EqualValues(t, 3, calls.Load())
}
