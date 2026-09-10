package mum_p2p

import (
	"encoding/json"

	"github.com/libp2p/go-libp2p/core/peer"

	pubsub "github.com/getoptimum/optimum-p2p/optimum-pubsub"
)

// NodeOption configures a Node (e.g. WithCustomHandshakeBuilder).
type NodeOption func(*Node)

func WithCustomHandshakeBuilder(handshakeBuilder func() any) NodeOption {
	return func(n *Node) {
		if handshakeBuilder != nil {
			n.handshakeBuilder = handshakeBuilder
		}
	}
}

func WithCustomHandshakeHandler(
	handshakeHandler func(peerID peer.ID, decoder *json.Decoder) (pubsub.PeerCapability, error),
) NodeOption {
	return func(n *Node) {
		if handshakeHandler != nil {
			n.handshakeHandler = handshakeHandler
		}
	}
}
