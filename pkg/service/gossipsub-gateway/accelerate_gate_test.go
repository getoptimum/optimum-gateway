package gossipsub_gateway

import (
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
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// blockAtSlot rewrites the slot of a captured gossip block. Slot sits at a fixed
// offset in the SSZ payload, so no re-signing or re-hashing is involved.
func blockAtSlot(t *testing.T, hexBlock string, slot uint64) []byte {
	t.Helper()
	raw, err := hex.DecodeString(hexBlock)
	require.NoError(t, err)
	ssz, err := utils.DecodeSnappy(raw, utils.MaxGossipPayloadSize)
	require.NoError(t, err)
	binary.LittleEndian.PutUint64(ssz[100:108], slot)
	return snappy.Encode(nil, ssz)
}

// joinCLTopic registers a real libp2p topic so publishToCLTopic is observable.
func joinCLTopic(t *testing.T, svc *Service, topic string) {
	t.Helper()
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	ps, err := libp2ppubsub.NewGossipSub(t.Context(), h)
	require.NoError(t, err)
	tp, err := ps.Join(topic)
	require.NoError(t, err)
	svc.libP2PTopics.Store(topic, tp)
}

// An unselected slot is withheld from the CL, but is still measured and streamed:
// ADR-0012 puts the gate after arrival handling, not before it.
func TestMumP2PBeaconBlockAccelerateGate(t *testing.T) {
	svc, bootstrap := newGateway(t)
	topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
	joinCLTopic(t, svc, topic)
	svc.cfg.SetPropagationEnabled(true)
	hub := streamhub.New()
	svc.streamHub = hub
	sub := hub.Subscribe(4)

	cur := chainstate.CurrentSlot(time.Now())
	bootstrap.SetAccelerateResponse(map[string]any{
		"to_slot":         cur + 10,
		"slots":           []int64{int64(cur)},
		"generated_at_ms": 1,
	})
	svc.srvMsgRouter.RefreshAccelerateSlots(t.Context())

	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: "peer-1", Topic: topic,
		Message: blockAtSlot(t, test_utils.HoodiBeaconBlockMessage1, cur),
	})
	_, ok := svc.statSendLib.Load(topic)
	require.True(t, ok, "on-list slot must reach the CL")

	svc.statSendLib.DeleteAll()

	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: "peer-1", Topic: topic,
		Message: blockAtSlot(t, test_utils.HoodiBeaconBlockMessage2, cur+1),
	})
	_, ok = svc.statSendLib.Load(topic)
	require.False(t, ok, "examined but unselected slot must be withheld from the CL")

	require.Len(t, sub.Events(), 2, "both blocks are streamed regardless of the verdict")
	svc.messagesMap.Close()
}
