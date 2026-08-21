package bootstrapper_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
)

func TestRecordMumPublishedAtChanOverflow(t *testing.T) {
	srv, _, _ := getTestSrv(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		n := time.Now()
		for i := range uint64(1000) {
			srv.RecordMumPublishedAt(i, n.Unix())
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestRecordMumPublishedAt(t *testing.T) {
	srv, bootstrap, cfg := getTestSrv(t)

	cfg.TelemetryEnable = false
	srv.RecordMumPublishedAt(10, 111)
	bootstrap.AssertNoBlockLatencyRequest(t, 200*time.Millisecond)

	cfg.TelemetryEnable = true
	srv.RecordMumPublishedAt(0, 222)
	bootstrap.AssertNoBlockLatencyRequest(t, 200*time.Millisecond)

	srv.RecordMumPublishedAt(42, 333)
	req := bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Contains(t, req.Authorization, "Bearer ")
	require.Equal(t, cfg.GatewayID, req.Payload.GatewayID)
	require.Equal(t, uint64(42), req.Payload.BlockSlot)
	require.Equal(t, chainstate.SlotStartTime(42).UnixMilli(), req.Payload.SlotTime)
	require.Equal(t, int64(333), req.Payload.MumPublishedAtMs)

	srv.RecordMumPublishedAt(42, 444)
	req = bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, uint64(42), req.Payload.BlockSlot)
	require.Equal(t, int64(444), req.Payload.MumPublishedAtMs)
}

func TestSendTrackedSlotsEmitsLatestSameSlotValue(t *testing.T) {
	srv, bootstrap, cfg := getTestSrv(t)
	cfg.TelemetryEnable = true

	const slot = uint64(128)
	srv.RecordMumPublishedAt(slot, 100)
	srv.RecordMumPublishedAt(slot, 200)
	srv.RecordMumPublishedAt(slot, 300)

	var last int64
	seen := 0
	for {
		req, ok := bootstrap.TryBlockLatencyRequest(500 * time.Millisecond)
		if !ok {
			break
		}
		require.Equal(t, slot, req.Payload.BlockSlot)
		require.GreaterOrEqual(t, req.Payload.MumPublishedAtMs, int64(100))
		require.LessOrEqual(t, req.Payload.MumPublishedAtMs, int64(300))
		last = req.Payload.MumPublishedAtMs
		seen++
	}
	require.GreaterOrEqual(t, seen, 1, "expected at least one emission")
	require.Equal(t, int64(300), last, "settled emission must carry the latest same-slot value")
}

func TestBlockLatencyExportRetriesThenRecovers(t *testing.T) {
	srv, bootstrap, cfg := getTestSrv(t)
	cfg.TelemetryEnable = true

	// Bootstrap is "down": Cloudflare 521 is a transient error and must be retried.
	bootstrap.SetBlockLatencyStatus(521)

	const slot = uint64(700)
	srv.RecordMumPublishedAt(slot, 100)

	// The same slot is attempted more than once while the endpoint keeps failing.
	a1 := bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, slot, a1.Payload.BlockSlot)
	a2 := bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, slot, a2.Payload.BlockSlot)

	// Bootstrap recovers: once a send succeeds, retries for the slot stop.
	bootstrap.SetBlockLatencyStatus(0)

	deadline := time.After(20 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("exporter never quiesced after bootstrap recovery")
		default:
		}
		if _, ok := bootstrap.TryBlockLatencyRequest(2500 * time.Millisecond); !ok {
			return // no further attempts: the successful send cleared the slot
		}
	}
}

func TestBlockLatencyExportTerminalResponseIsNotRetried(t *testing.T) {
	srv, bootstrap, cfg := getTestSrv(t)
	cfg.TelemetryEnable = true

	// A terminal 4xx must be dropped, not retried.
	bootstrap.SetBlockLatencyStatus(http.StatusBadRequest)

	const slot = uint64(800)
	srv.RecordMumPublishedAt(slot, 100)

	req := bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, slot, req.Payload.BlockSlot)

	bootstrap.AssertNoBlockLatencyRequest(t, 2500*time.Millisecond)
}

func TestHandleBeaconBlock(t *testing.T) {
	srv, bootstrap, cfg := getTestSrv(t)
	srv.SetGatewayPeerIDStr("self-peer")

	const slot = uint64(64)

	srv.HandleBeaconBlock(entities.SourceMumP2P, slot, 77, 2048, 1_000, "origin-a", "upstream-a")
	req := bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Contains(t, req.Authorization, "Bearer ")
	require.Equal(t, cfg.GatewayID, req.Payload.GatewayID)
	require.Equal(t, "self-peer", req.Payload.GatewayPeerID)
	require.Equal(t, slot, req.Payload.BlockSlot)
	require.Equal(t, uint64(77), req.Payload.ValidatorIndex)
	require.Equal(t, uint64(2048), req.Payload.BlockSize)
	require.Equal(t, chainstate.SlotStartTime(slot).UnixMilli(), req.Payload.SlotTime)
	require.Equal(t, int64(1_000), req.Payload.MumSeenAtMs)
	require.Equal(t, "origin-a", req.Payload.OriginGatewayID)
	require.Equal(t, "upstream-a", req.Payload.UpstreamPeerID)

	srv.HandleBeaconBlock(entities.SourceLibP2P, slot, 77, 2048, 2_000, "", "upstream-lib")
	req = bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, slot, req.Payload.BlockSlot)
	require.Equal(t, int64(1_000), req.Payload.MumSeenAtMs)
	require.Equal(t, int64(2_000), req.Payload.EthSeenAtMs)
	require.Equal(t, "upstream-lib", req.Payload.EthUpstreamPeerID)

	srv.HandleBeaconBlock(entities.SourceMumP2P, slot, 77, 2048, 3_000, "origin-b", "upstream-b")
	req = bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, slot, req.Payload.BlockSlot)
	require.Equal(t, int64(3_000), req.Payload.MumSeenAtMs)
	require.Equal(t, "origin-b", req.Payload.OriginGatewayID)
	require.Equal(t, "upstream-b", req.Payload.UpstreamPeerID)

	srv.HandleBeaconBlock(entities.SourceLibP2P, slot+1, 88, 4096, 4_000, "", "upstream-lib-first")
	req = bootstrap.WaitBlockLatencyRequest(t, 5*time.Second)
	require.Equal(t, slot+1, req.Payload.BlockSlot)
	require.Equal(t, int64(4_000), req.Payload.EthSeenAtMs)
	require.Equal(t, "upstream-lib-first", req.Payload.EthUpstreamPeerID)
	require.Zero(t, req.Payload.MumSeenAtMs)
}
