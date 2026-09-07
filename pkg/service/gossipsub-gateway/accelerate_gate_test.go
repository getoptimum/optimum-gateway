package gossipsub_gateway

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/libp2p/go-libp2p"
	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// blockAtSlot rewrites the gossip block's slot; DecodeBeaconBlockHeader must read it back.
func blockAtSlot(t *testing.T, hexBlock string, slot uint64) []byte {
	t.Helper()
	raw, err := hex.DecodeString(hexBlock)
	require.NoError(t, err)
	ssz, err := utils.DecodeSnappy(raw, utils.MaxGossipPayloadSize)
	require.NoError(t, err)
	off := 4 + 96 // SSZ prefix + BLS signature
	require.GreaterOrEqual(t, len(ssz), off+8, "fixture is too short to hold a slot")
	binary.LittleEndian.PutUint64(ssz[off:off+8], slot)
	encoded := snappy.Encode(nil, ssz)
	hdr, err := consensus.DecodeBeaconBlockHeader(encoded)
	require.NoError(t, err)
	require.Equal(t, slot, hdr.Header.Slot, "slot rewrite landed at the wrong offset")
	return encoded
}

// joinCLTopic subscribes to a real gossipsub topic so CL publishes can be read back.
func joinCLTopic(t *testing.T, svc *Service, topic string) *libp2ppubsub.Subscription {
	t.Helper()
	h, err := libp2p.New(libp2p.NoListenAddrs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	ps, err := libp2ppubsub.NewGossipSub(t.Context(), h)
	require.NoError(t, err)
	tp, err := ps.Join(topic)
	require.NoError(t, err)
	sub, err := tp.Subscribe()
	require.NoError(t, err)
	t.Cleanup(sub.Cancel)
	svc.libP2PTopics.Store(topic, tp)
	return sub
}

// Unselected slot is withheld from the CL; both blocks are still streamed (gate is after arrival).
func TestMumP2PBeaconBlockAccelerateGate(t *testing.T) {
	cur := chainstate.CurrentSlot(time.Now())
	// Seed before the router exists: bgSync primes at startup and may land after the refresh.
	svc, _ := newGateway(t, func(b *test_utils.LocalBootstrapServer) {
		b.SetAccelerateResponse(map[string]any{
			"to_slot":         cur + 10,
			"slots":           []int64{int64(cur)},
			"generated_at_ms": 1,
		})
	})
	topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
	clSub := joinCLTopic(t, svc, topic)
	hub := streamhub.New()
	svc.streamHub = hub
	sub := hub.Subscribe(4)
	t.Cleanup(sub.Close)
	t.Cleanup(svc.messagesMap.Close)

	svc.srvMsgRouter.RefreshAccelerateSlots(t.Context())

	fixture := test_utils.HoodiBeaconBlockMessage1
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: "peer-1", Topic: topic, Message: blockAtSlot(t, fixture, cur+1),
	})
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: "peer-1", Topic: topic, Message: blockAtSlot(t, fixture, cur),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	got, err := clSub.Next(ctx)
	require.NoError(t, err, "on-list slot must reach the CL")
	delivered, err := consensus.DecodeBeaconBlockHeader(got.Data)
	require.NoError(t, err)
	require.Equal(t, cur, delivered.Header.Slot, "examined but unselected slot must be withheld from the CL")

	// Covers gossipsub delivering the two publishes in either order.
	idle, cancelIdle := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancelIdle()
	_, err = clSub.Next(idle)
	require.ErrorIs(t, err, context.DeadlineExceeded, "only the on-list slot may reach the CL")

	require.Len(t, sub.Events(), 2, "both blocks are streamed regardless of the verdict")
}
