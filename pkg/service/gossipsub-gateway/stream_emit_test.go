package gossipsub_gateway

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
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

	slot, _ := svc.processBeaconBlockArrival(svc.log, topic, raw, time.Now().UnixMilli(), entities.SourceLibP2P, "", "peer-x")
	require.Equal(t, uint64(3435697), slot)

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
