package message_router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

func TestShouldAccelerateBlock(t *testing.T) {
	var fail atomic.Bool
	// bgSync primes on startup, so serve failures until the fail-open assertion
	// below is done. A failed poll leaves the window nil, which is what it needs.
	fail.Store(true)
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

	fail.Store(false)
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

// Without a prime the window stays empty until the first 30s tick, so a restarted
// gateway accelerates every block for half a minute: fail-open for want of an
// answer rather than because one was unavailable.
func TestAccelerateSlotsPrimedAtStartup(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_slot":         120,
			"slots":           []int64{100},
			"generated_at_ms": 1,
		})
	}))
	t.Cleanup(ts.Close)

	srv := newTestServiceAt(t, commonentities.GatewayTypePartner, ts.URL)

	// Slot 110 is inside the horizon and unselected, so it only stops accelerating
	// once the window has been fetched. The prime runs on bgSync's goroutine.
	require.Eventually(t, func() bool {
		return !srv.ShouldAccelerateBlock(110)
	}, 5*time.Second, 5*time.Millisecond, "startup must fetch the window without waiting for a tick")
	require.True(t, srv.ShouldAccelerateBlock(100), "selected slot still accelerates")
}
