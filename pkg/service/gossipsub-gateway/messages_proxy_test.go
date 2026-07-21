package gossipsub_gateway

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

func TestIsDuplicateMessage(t *testing.T) {
	svc := &Service{
		messagesMap: syncx.NewTTLMap[uint64, struct{}](1*time.Minute, 1*time.Minute),
	}

	msg1 := []byte("test message 1")
	msg2 := []byte("test message 2")

	// Tests are sequential - each builds on previous state
	tests := []struct {
		name string
		msg  []byte
		want bool
	}{
		{"first message is not duplicate", msg1, false},
		{"same message is duplicate", msg1, true},
		{"different message is not duplicate", msg2, false},
		{"same different message is now duplicate", msg2, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := svc.isDuplicateMessage(tc.msg)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestHandleMessagesFromCLCachesHash(t *testing.T) {
	svc, _ := newGateway(t)
	topic := "/eth2/deadbeef/beacon_attestation_0/ssz_snappy" // non-beacon block topic avoids publish path

	var wg sync.WaitGroup
	wg.Go(func() {
		svc.handleMessagesFromCL()
	})

	svc.clMessages <- &entities.CLMessage{Topic: topic, Message: []byte("payload")}
	close(svc.clMessages)

	wg.Wait()

	require.True(t, svc.isDuplicateMessage([]byte("payload")), "handler should record message hash")
	svc.messagesMap.Close()
}

func TestHandleMessagesFromMumP2PNodeCachesHash(t *testing.T) {
	svc, _ := newGateway(t)

	var wg sync.WaitGroup
	wg.Go(func() {
		svc.handleMessagesFromMumP2PNode()
	})

	svc.mumP2PMessages <- &commonentities.P2PMessage{
		SourceNodeID: "peer-1",
		Topic:        mumP2PAggregatedMessagesTopic,
		Message:      []byte("payload"),
	}
	close(svc.mumP2PMessages)

	wg.Wait()

	require.True(t, svc.isDuplicateMessage([]byte("payload")), "handler should record message hash")
	svc.messagesMap.Close()
}

func TestHandleMessagesFromMumP2PNode_NotSubscribedTopicSkipsWithoutBadMessage(t *testing.T) {
	startBad := telemetry.GetBadMessagesToCL()

	// Beacon block uses a dedicated topic on mump2p; if the handler proceeds past the
	// subscription check it would publish without SSZ decode, so bad-to-CL must stay flat.
	svc, _ := newGateway(t)
	topic := "/eth2/deadbeef/beacon_block/ssz_snappy"
	var wg sync.WaitGroup
	wg.Go(func() {
		svc.handleMessagesFromMumP2PNode()
	})

	svc.mumP2PMessages <- &commonentities.P2PMessage{
		SourceNodeID: "peer-1",
		Topic:        topic,
		Message:      []byte("definitely-not-ssz"),
		MessageID:    "msg-1",
	}
	close(svc.mumP2PMessages)
	wg.Wait()

	require.Equal(t, startBad, telemetry.GetBadMessagesToCL(), "not subscribed topic should skip before mapping/decode")
	_, ok := svc.statSendLib.Load(topic)
	require.False(t, ok, "should not publish/record stats when not subscribed")
	svc.messagesMap.Close()
}
