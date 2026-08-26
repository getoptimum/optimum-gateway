package message_router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

func TestShouldAccelerateBlock(t *testing.T) {
	var fail atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/hoodi/accelerate_slots", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("Authorization"))
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_slot":         120,
			"slots":           []int64{100, 101},
			"generated_at_ms": 1,
		})
	}))
	t.Cleanup(ts.Close)

	srv := newTestServiceAt(t, commonentities.GatewayTypePartner, ts.URL)
	require.True(t, srv.ShouldAccelerateBlock(1), "no list fail-opens")

	srv.RefreshAccelerateSlots(t.Context())
	require.True(t, srv.ShouldAccelerateBlock(100))
	require.True(t, srv.ShouldAccelerateBlock(101))
	require.False(t, srv.ShouldAccelerateBlock(110), "examined, not selected")
	require.True(t, srv.ShouldAccelerateBlock(121), "past to_slot fail-opens")

	fail.Store(true)
	srv.RefreshAccelerateSlots(t.Context())
	require.False(t, srv.ShouldAccelerateBlock(110), "failed poll must not clear the list")
	require.True(t, srv.ShouldAccelerateBlock(100))
}
