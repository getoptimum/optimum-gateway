package mump2p

import (
	"context"

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
}

var _ Engine = (*Node)(nil)
