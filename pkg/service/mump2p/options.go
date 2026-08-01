package mump2p

import (
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// HandshakeResult is what a verified handshake reports beyond success.
type HandshakeResult struct {
	// TokenExpiry is when the credential that authorized this peer expires, or
	// the zero time when the handler verified no credential. A datagram session
	// for this peer is never allowed to outlive it.
	TokenExpiry time.Time
}

// HandshakeHandler parses and validates a peer's handshake message. A nil error
// admits the peer.
type HandshakeHandler func(peerID peer.ID, decoder *json.Decoder) (HandshakeResult, error)

// NodeOption configures a Node (e.g. WithCustomHandshakeBuilder).
type NodeOption func(*nodeOptions)

// nodeOptions holds the settings a caller may override. They are resolved before
// the node is built because the coder has to exist before the pubsub does.
type nodeOptions struct {
	handshakeBuilder func() any
	handshakeHandler HandshakeHandler
	coder            Coder
}

func WithCustomHandshakeBuilder(handshakeBuilder func() any) NodeOption {
	return func(o *nodeOptions) {
		if handshakeBuilder != nil {
			o.handshakeBuilder = handshakeBuilder
		}
	}
}

func WithCustomHandshakeHandler(handshakeHandler HandshakeHandler) NodeOption {
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
