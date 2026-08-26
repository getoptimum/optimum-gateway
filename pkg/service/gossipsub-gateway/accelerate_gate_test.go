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

// blockSlotOffset is where BeaconBlock.slot starts in a gossip SignedBeaconBlock:
// the 4-byte SSZ message offset plus the 96-byte BLS signature. Must stay in step
// with consensus.DecodeBeaconBlockHeader, which reads the slot from the same place.
const blockSlotOffset = 4 + 96

// blockAtSlot rewrites the slot of a captured gossip block. Slot sits at a fixed
// offset in the SSZ payload, so no re-signing or re-hashing is involved.
func blockAtSlot(t *testing.T, hexBlock string, slot uint64) []byte {
	t.Helper()
	raw, err := hex.DecodeString(hexBlock)
	require.NoError(t, err)
	ssz, err := utils.DecodeSnappy(raw, utils.MaxGossipPayloadSize)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(ssz), blockSlotOffset+8, "fixture is too short to hold a slot")
	binary.LittleEndian.PutUint64(ssz[blockSlotOffset:blockSlotOffset+8], slot)
	encoded := snappy.Encode(nil, ssz)
	// Read the slot back through the production decoder, so a drifted offset fails
	// here rather than silently handing the gate a stale slot.
	hdr, err := consensus.DecodeBeaconBlockHeader(encoded)
	require.NoError(t, err)
	require.Equal(t, slot, hdr.Header.Slot, "slot rewrite landed at the wrong offset")
	return encoded
}

// joinCLTopic registers a real libp2p topic and subscribes to it, so what the
// gateway hands the CL can be read back rather than inferred from a counter.
func joinCLTopic(t *testing.T, svc *Service, topic string) *libp2ppubsub.Subscription {
	t.Helper()
	h, err := libp2p.New(libp2p.NoListenAddrs) // publishing needs no peers, so bind nothing
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

// An unselected slot is withheld from the CL, but is still measured and streamed:
// ADR-0012 puts the gate after arrival handling, not before it.
func TestMumP2PBeaconBlockAccelerateGate(t *testing.T) {
	svc, bootstrap := newGateway(t)
	topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
	clSub := joinCLTopic(t, svc, topic)
	hub := streamhub.New()
	svc.streamHub = hub
	sub := hub.Subscribe(4)
	t.Cleanup(sub.Close)
	t.Cleanup(svc.messagesMap.Close)

	cur := chainstate.CurrentSlot(time.Now())
	bootstrap.SetAccelerateResponse(map[string]any{
		"to_slot":         cur + 10,
		"slots":           []int64{int64(cur)},
		"generated_at_ms": 1,
	})
	svc.srvMsgRouter.RefreshAccelerateSlots(t.Context())

	// Unselected slot first: if the gate let it through it would be the message
	// waiting on the subscription below, so ordering does the negative assertion
	// without a timeout to wait out.
	unselected := blockAtSlot(t, test_utils.HoodiBeaconBlockMessage2, cur+1)
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: "peer-1", Topic: topic, Message: unselected,
	})

	onList := blockAtSlot(t, test_utils.HoodiBeaconBlockMessage1, cur)
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: "peer-1", Topic: topic, Message: onList,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	got, err := clSub.Next(ctx)
	require.NoError(t, err, "on-list slot must reach the CL")
	// Compare the slot, not the raw block: a failure here prints one number
	// instead of two multi-kilobyte payloads.
	delivered, err := consensus.DecodeBeaconBlockHeader(got.Data)
	require.NoError(t, err)
	require.Equal(t, cur, delivered.Header.Slot, "examined but unselected slot must be withheld from the CL")

	// Nothing else may follow, which holds the negative assertion whichever order
	// the two publishes would have been delivered in. Local delivery is immediate,
	// so the wait is generous rather than load-bearing.
	idle, cancelIdle := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancelIdle()
	_, err = clSub.Next(idle)
	require.ErrorIs(t, err, context.DeadlineExceeded, "only the on-list slot may reach the CL")

	require.Len(t, sub.Events(), 2, "both blocks are streamed regardless of the verdict")
}
