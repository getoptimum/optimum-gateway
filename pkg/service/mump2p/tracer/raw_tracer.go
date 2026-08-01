package tracer

import (
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"google.golang.org/protobuf/proto"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

var _ pubsub.RawTracer = (*RawMumP2P)(nil)

// RawMumP2P broadcasts the gossipsub-level events the RLNC router does not report:
// topic join/leave, mesh graft/prune, RPC traffic, and message reject/duplicate.
// Peer add/remove and delivery are deliberately left out, since the router already
// traces those and reporting them twice would double every count.
type RawMumP2P struct {
	MumP2P
}

func NewRawTracerMumP2P(
	broadcaster *syncx.Broadcaster[*entities.MumP2PResponse],
	categories entities.TraceCategories,
) *RawMumP2P {
	return &RawMumP2P{MumP2P: *NewTracerMumP2P(broadcaster, categories)}
}

func (p *RawMumP2P) Join(topic string) {
	if !p.categories.Mesh {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		Event:     &tracepb.TraceEvent_Join{Join: &tracepb.Join{Topic: topic}},
	})
}

func (p *RawMumP2P) Leave(topic string) {
	if !p.categories.Mesh {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		Event:     &tracepb.TraceEvent_Leave{Leave: &tracepb.Leave{Topic: topic}},
	})
}

func (p *RawMumP2P) Graft(pid peer.ID, topic string) {
	if !p.categories.Mesh {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		PeerId:    []byte(pid),
		Event:     &tracepb.TraceEvent_Graft{Graft: &tracepb.Graft{PeerId: []byte(pid), Topic: topic}},
	})
}

func (p *RawMumP2P) Prune(pid peer.ID, topic string) {
	if !p.categories.Mesh {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		PeerId:    []byte(pid),
		Event:     &tracepb.TraceEvent_Prune{Prune: &tracepb.Prune{PeerId: []byte(pid), Topic: topic}},
	})
}

func (p *RawMumP2P) RejectMessage(msg *pubsub.Message, reason string) {
	if !p.categories.Shard || msg == nil {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		MsgId:     []byte(msg.ID),
		Event: &tracepb.TraceEvent_RejectMessage{RejectMessage: &tracepb.RejectMessage{
			MessageId:    []byte(msg.ID),
			ReceivedFrom: []byte(msg.ReceivedFrom),
			Reason:       reason,
			Topic:        msg.GetTopic(),
		}},
	})
}

func (p *RawMumP2P) DuplicateMessage(msg *pubsub.Message) {
	if !p.categories.Shard || msg == nil {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		MsgId:     []byte(msg.ID),
		Event: &tracepb.TraceEvent_DuplicateMessage{DuplicateMessage: &tracepb.DuplicateMessage{
			MessageId:    []byte(msg.ID),
			ReceivedFrom: []byte(msg.ReceivedFrom),
			Topic:        msg.GetTopic(),
		}},
	})
}

func (p *RawMumP2P) RecvRPC(rpc *pubsub.RPC) {
	if !p.categories.RPC {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		Event:     &tracepb.TraceEvent_RecvRpc{RecvRpc: &tracepb.RecvRPC{Length: rpcSize(rpc)}},
	})
}

func (p *RawMumP2P) SendRPC(rpc *pubsub.RPC, pid peer.ID) {
	if !p.categories.RPC {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		Event: &tracepb.TraceEvent_SendRpc{
			SendRpc: &tracepb.SendRPC{SendTo: []byte(pid), Length: rpcSize(rpc)},
		},
	})
}

func (p *RawMumP2P) DropRPC(_ *pubsub.RPC, pid peer.ID) {
	if !p.categories.RPC {
		return
	}
	p.broadcast(&tracepb.TraceEvent{
		Timestamp: time.Now().UnixNano(),
		Event:     &tracepb.TraceEvent_DropRpc{DropRpc: &tracepb.DropRPC{SendTo: []byte(pid)}},
	})
}

// Traced by the RLNC router instead, or carrying nothing the gateway consumes.
func (p *RawMumP2P) OnNewOutboundStream(peer.ID, protocol.ID) {}
func (p *RawMumP2P) OnClosedOutboundStream(peer.ID)           {}
func (p *RawMumP2P) ValidateMessage(*pubsub.Message)          {}
func (p *RawMumP2P) DeliverMessage(*pubsub.Message)           {}
func (p *RawMumP2P) ThrottlePeer(peer.ID)                     {}
func (p *RawMumP2P) UndeliverableMessage(*pubsub.Message)     {}

func rpcSize(rpc *pubsub.RPC) uint64 {
	if rpc == nil {
		return 0
	}
	return uint64(proto.Size(&rpc.RPC)) //nolint:gosec // a serialized message size is never negative
}
