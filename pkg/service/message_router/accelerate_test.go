package message_router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

func TestShouldAccelerateBlock(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true) // keep window nil until the fail-open assert; bgSync polls at start
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// assert, not require: FailNow off the test goroutine is unsupported.
		assert.Equal(t, "/api/v2/hoodi/accelerate_slots", r.URL.Path)
		assert.NotEmpty(t, r.Header.Get("Authorization"))
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

func TestAccelerateSlotsMergeKeepsPrevious(t *testing.T) {
	var slots atomic.Value
	slots.Store([]int64{100})
	var toSlot atomic.Int64
	toSlot.Store(120)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_slot":         toSlot.Load(),
			"slots":           slots.Load(),
			"generated_at_ms": 1,
		})
	}))
	t.Cleanup(ts.Close)

	srv := newTestServiceAt(t, commonentities.GatewayTypePartner, ts.URL)
	srv.RefreshAccelerateSlots(t.Context())
	require.True(t, srv.ShouldAccelerateBlock(100))

	slots.Store([]int64{110})
	toSlot.Store(140)
	srv.RefreshAccelerateSlots(t.Context())
	require.True(t, srv.ShouldAccelerateBlock(100), "previous on-list slot must survive a rolled window")
	require.True(t, srv.ShouldAccelerateBlock(110))
	require.False(t, srv.ShouldAccelerateBlock(130), "examined, not selected")
	require.True(t, srv.ShouldAccelerateBlock(141), "past to_slot fail-opens")
}

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

	require.Eventually(t, func() bool {
		return !srv.ShouldAccelerateBlock(110)
	}, 5*time.Second, 5*time.Millisecond, "startup must fetch the window without waiting for a tick")
	require.True(t, srv.ShouldAccelerateBlock(100), "selected slot still accelerates")
}
