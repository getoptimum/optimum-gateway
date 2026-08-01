package mump2p

import (
	"encoding/json"

	"github.com/libp2p/go-libp2p/core/peer"
)

// NodeOption configures a Node (e.g. WithCustomHandshakeBuilder).
type NodeOption func(*nodeOptions)

// nodeOptions holds the settings a caller may override. They are resolved before
// the node is built because the coder has to exist before the pubsub does.
type nodeOptions struct {
	handshakeBuilder func() any
	handshakeHandler func(peerID peer.ID, decoder *json.Decoder) error
	coder            Coder
}

func WithCustomHandshakeBuilder(handshakeBuilder func() any) NodeOption {
	return func(o *nodeOptions) {
		if handshakeBuilder != nil {
			o.handshakeBuilder = handshakeBuilder
		}
	}
}

func WithCustomHandshakeHandler(handshakeHandler func(peerID peer.ID, decoder *json.Decoder) error) NodeOption {
	return func(o *nodeOptions) {
		if handshakeHandler != nil {
			o.handshakeHandler = handshakeHandler
		}
	}
}

// WithCoder supplies the RLNC coder instead of attaching to the out-of-process
// one over shared memory.
func WithCoder(coder Coder) NodeOption {
	return func(o *nodeOptions) {
		if coder != nil {
			o.coder = coder
		}
	}
}
