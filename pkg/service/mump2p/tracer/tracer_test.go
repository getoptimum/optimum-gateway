package tracer_test

import (
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p/tracer"
)

const listenerKey = "trace-listener"

func newListener(t *testing.T) (
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse],
	listener chan *entities.MumP2PResponse,
) {
	t.Helper()

	broadcaster = syncx.NewBroadcaster[*entities.MumP2PResponse]()
	listener = broadcaster.RegisterListener(listenerKey)
	t.Cleanup(func() {
		broadcaster.UnregisterListener(listenerKey)
	})
	return broadcaster, listener
}

// awaitEvent returns the next broadcast trace event, failing if none arrives.
func awaitEvent(t *testing.T, listener chan *entities.MumP2PResponse) *tracepb.TraceEvent {
	t.Helper()

	select {
	case resp := <-listener:
		require.Equal(t, entities.MumP2PCommandTraceMumP2P, resp.Command)
		return resp.TraceEvent
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for trace fan-out")
		return nil
	}
}

func requireNoEvent(t *testing.T, listener chan *entities.MumP2PResponse) {
	t.Helper()

	select {
	case resp := <-listener:
		t.Fatalf("unexpected trace fan-out: %v", resp.TraceEvent)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTracerBroadcastsEnabledCategoriesOnly(t *testing.T) {
	broadcaster, listener := newListener(t)
	tr := tracer.NewTracerMumP2P(broadcaster, entities.NewTraceCategories(false, false, true))

	helpful := &tracepb.TraceEvent{
		MsgId: []byte("message-1"),
		Event: &tracepb.TraceEvent_HelpfulSymbol{
			HelpfulSymbol: &tracepb.SymbolContainer{MessageId: []byte("message-1")},
		},
	}
	go tr.Trace(helpful)
	require.Equal(t, helpful, awaitEvent(t, listener))

	// Mesh is disabled, so an add-peer event must not reach the listener.
	tr.Trace(&tracepb.TraceEvent{Event: &tracepb.TraceEvent_AddPeer{AddPeer: &tracepb.AddPeer{}}})
	requireNoEvent(t, listener)
}

func TestTracerIgnoresUnclassifiableEvents(t *testing.T) {
	broadcaster, listener := newListener(t)
	tr := tracer.NewTracerMumP2P(broadcaster, entities.NewTraceCategories(true, true, true))

	tr.Trace(nil)
	tr.Trace(&tracepb.TraceEvent{})
	requireNoEvent(t, listener)
}

func TestRawTracerBroadcastsMeshEvents(t *testing.T) {
	broadcaster, listener := newListener(t)
	raw := tracer.NewRawTracerMumP2P(broadcaster, entities.NewTraceCategories(true, false, false))
	pid := peer.ID("peer-a")

	go raw.Join("topic-a")
	require.Equal(t, "topic-a", awaitEvent(t, listener).GetJoin().GetTopic())

	go raw.Leave("topic-a")
	require.Equal(t, "topic-a", awaitEvent(t, listener).GetLeave().GetTopic())

	go raw.Graft(pid, "topic-a")
	graft := awaitEvent(t, listener)
	require.Equal(t, "topic-a", graft.GetGraft().GetTopic())
	require.Equal(t, []byte(pid), graft.GetGraft().GetPeerId())

	go raw.Prune(pid, "topic-a")
	require.Equal(t, []byte(pid), awaitEvent(t, listener).GetPrune().GetPeerId())

	// RPC and shard categories are off.
	raw.RecvRPC(&pubsub.RPC{})
	raw.DuplicateMessage(&pubsub.Message{Message: &pubsubpb.Message{}})
	requireNoEvent(t, listener)
}

func TestRawTracerBroadcastsRPCEvents(t *testing.T) {
	broadcaster, listener := newListener(t)
	raw := tracer.NewRawTracerMumP2P(broadcaster, entities.NewTraceCategories(false, true, false))
	pid := peer.ID("peer-b")

	rpc := &pubsub.RPC{}
	rpc.Publish = []*pubsubpb.Message{{Data: []byte("some-payload")}}

	go raw.RecvRPC(rpc)
	require.Positive(t, awaitEvent(t, listener).GetRecvRpc().GetLength())

	go raw.SendRPC(rpc, pid)
	sent := awaitEvent(t, listener)
	require.Positive(t, sent.GetSendRpc().GetLength())
	require.Equal(t, []byte(pid), sent.GetSendRpc().GetSendTo())

	// A dropped RPC carries no length, only the intended recipient.
	go raw.DropRPC(rpc, pid)
	require.Equal(t, []byte(pid), awaitEvent(t, listener).GetDropRpc().GetSendTo())

	go raw.RecvRPC(nil)
	require.Zero(t, awaitEvent(t, listener).GetRecvRpc().GetLength())

	raw.Join("topic-a")
	requireNoEvent(t, listener)
}

func TestRawTracerBroadcastsMessageEvents(t *testing.T) {
	broadcaster, listener := newListener(t)
	raw := tracer.NewRawTracerMumP2P(broadcaster, entities.NewTraceCategories(false, false, true))

	topic := "topic-a"
	msg := &pubsub.Message{
		Message:      &pubsubpb.Message{Topic: &topic},
		ID:           "msg-1",
		ReceivedFrom: peer.ID("peer-c"),
	}

	go raw.RejectMessage(msg, pubsub.RejectValidationFailed)
	rejected := awaitEvent(t, listener)
	require.Equal(t, topic, rejected.GetRejectMessage().GetTopic())
	require.Equal(t, pubsub.RejectValidationFailed, rejected.GetRejectMessage().GetReason())
	require.Equal(t, []byte("msg-1"), rejected.GetMsgId())

	go raw.DuplicateMessage(msg)
	duplicate := awaitEvent(t, listener)
	require.Equal(t, topic, duplicate.GetDuplicateMessage().GetTopic())
	require.Equal(t, []byte(msg.ReceivedFrom), duplicate.GetDuplicateMessage().GetReceivedFrom())

	raw.RejectMessage(nil, "ignored")
	raw.DuplicateMessage(nil)
	requireNoEvent(t, listener)
}

// The router already traces these, so the raw tracer must stay silent on them or
// every count would be doubled.
func TestRawTracerStaysSilentOnRouterTracedEvents(t *testing.T) {
	broadcaster, listener := newListener(t)
	raw := tracer.NewRawTracerMumP2P(broadcaster, entities.NewTraceCategories(true, true, true))
	pid := peer.ID("peer-d")
	msg := &pubsub.Message{Message: &pubsubpb.Message{}}

	require.NotPanics(t, func() {
		raw.OnNewOutboundStream(pid, "/mump2p/1.0.0/data")
		raw.OnClosedOutboundStream(pid)
		raw.ValidateMessage(msg)
		raw.DeliverMessage(msg)
		raw.ThrottlePeer(pid)
		raw.UndeliverableMessage(msg)
	})
	requireNoEvent(t, listener)
}

func TestRawTracerDropsEverythingWhenAllCategoriesDisabled(t *testing.T) {
	broadcaster, listener := newListener(t)
	raw := tracer.NewRawTracerMumP2P(broadcaster, entities.NewTraceCategories(false, false, false))

	raw.Join("topic-a")
	raw.Leave("topic-a")
	raw.Graft(peer.ID("p"), "topic-a")
	raw.Prune(peer.ID("p"), "topic-a")
	raw.RecvRPC(&pubsub.RPC{})
	raw.SendRPC(&pubsub.RPC{}, peer.ID("p"))
	raw.DropRPC(&pubsub.RPC{}, peer.ID("p"))
	raw.RejectMessage(&pubsub.Message{Message: &pubsubpb.Message{}}, "reason")
	raw.DuplicateMessage(&pubsub.Message{Message: &pubsubpb.Message{}})
	requireNoEvent(t, listener)
}
