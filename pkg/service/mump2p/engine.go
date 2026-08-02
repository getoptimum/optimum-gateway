package mump2p

import (
	"context"
	"net/netip"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

// Engine is the p2p pub/sub surface the gateway drives. Consumers depend on this
// instead of a concrete node so an alternative implementation can be dropped in
// without retyping them.
type Engine interface {
	Start() error
	Stop()

	PublishMessage(ctx context.Context, topic string, msg []byte) error
	SubscribeTopic(topic string) error
	UnsubscribeTopic(topic string) error

	RegisterListener(key string) chan *entities.MumP2PResponse
	UnregisterListener(key string)

	GetPeers() []peer.ID
	GetTopics() []string
	GetMeshPeers(topic string) []peer.ID
	CountConnectedPeers() (totalPeers int, perTopicPeers map[string]int)

	GetHost() host.Host
	GetHostInfo() peer.AddrInfo

	// Datagram data plane state. Reported because the send path falls back to
	// streams silently: without a confirmed path nothing else says so.
	DatagramSessionExpiry(peerID peer.ID) (time.Time, bool)
	DatagramLocalAddr() (netip.AddrPort, bool)
	DatagramPathConfirmed(peerID peer.ID) bool
}

var _ Engine = (*Node)(nil)
