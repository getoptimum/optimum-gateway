package message_router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
)

func TestShouldAccelerateBlockFromPolledList(t *testing.T) {
	srv := newTestService(t, commonentities.GatewayTypePartner)
	require.True(t, srv.ShouldAccelerateBlock(1), "no list fail-opens")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/hoodi/accelerate_slots", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chain_id":        "hoodi",
			"to_slot":         120,
			"slots":           []int64{100, 101},
			"generated_at_ms": 1,
		})
	}))
	t.Cleanup(ts.Close)

	message_router.PollAccelerateSlotsForTest(t, srv, ts.URL)
	require.True(t, srv.ShouldAccelerateBlock(100))
	require.True(t, srv.ShouldAccelerateBlock(101))
	require.False(t, srv.ShouldAccelerateBlock(110), "examined, not selected")
	require.True(t, srv.ShouldAccelerateBlock(121), "past to_slot fail-opens")
}

func TestAcceleratePollKeepsPreviousOnFailure(t *testing.T) {
	srv := newTestService(t, commonentities.GatewayTypePartner)
	ok := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"to_slot": 120,
			"slots":   []int64{100},
		})
	}))
	t.Cleanup(ts.Close)

	message_router.PollAccelerateSlotsForTest(t, srv, ts.URL)
	require.False(t, srv.ShouldAccelerateBlock(110))

	ok = false
	message_router.PollAccelerateSlotsForTest(t, srv, ts.URL)
	require.False(t, srv.ShouldAccelerateBlock(110), "failed poll must not clear the list")
	require.True(t, srv.ShouldAccelerateBlock(100))
}
