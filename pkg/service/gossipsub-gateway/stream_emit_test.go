package gossipsub_gateway

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// A decoded beacon block is emitted to the hub with its metadata and raw bytes.
func TestProcessBeaconBlockArrivalEmitsToStreamHub(t *testing.T) {
	svc, _ := newGateway(t)
	hub := streamhub.New()
	svc.streamHub = hub
	sub := hub.Subscribe(4)

	raw, err := hex.DecodeString(test_utils.HoodiBeaconBlockMessage1)
	require.NoError(t, err)
	topic := "/eth2/deadbeef/beacon_block/ssz_snappy"

	// The gauge moves when the block arrives. Nothing here calls /health.
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)
	recvAt := time.Now().UnixMilli()
	slot, _ := svc.processBeaconBlockArrival(svc.log, topic, raw, recvAt, entities.SourceLibP2P, "", "peer-x")
	require.Equal(t, uint64(3435697), slot)
	require.InDelta(t, float64(recvAt)/1000, lastBlockReceivedGauge(t), 0.001)

	select {
	case ev := <-sub.Events():
		require.Equal(t, uint64(3435697), ev.Slot)
		require.Equal(t, uint64(526417), ev.ProposerIndex)
		require.Equal(t, entities.SourceLibP2P, ev.Source)
		require.Equal(t, topic, ev.Topic)
		require.Equal(t, "deadbeef", ev.ForkDigest)
		require.Equal(t, raw, ev.Raw)
		require.True(t, ev.Stale, "an old-slot fixture is flagged stale but still streamed")
	default:
		t.Fatal("expected a block event on the hub")
	}
}

func lastBlockReceivedGauge(t *testing.T) float64 {
	t.Helper()
	families, err := telemetry.CustomRegistry.Gather()
	require.NoError(t, err)
	var got *dto.Metric
	for _, family := range families {
		if strings.HasSuffix(family.GetName(), "last_block_received_timestamp") {
			require.NotEmpty(t, family.Metric)
			got = family.Metric[0]
			break
		}
	}
	require.NotNil(t, got)
	return got.GetGauge().GetValue()
}
