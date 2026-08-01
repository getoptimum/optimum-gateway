package mump2p

import (
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

func (n *Node) RegisterListener(key string) chan *entities.MumP2PResponse {
	return n.broadcaster.RegisterListener(key)
}

func (n *Node) UnregisterListener(key string) {
	n.broadcaster.UnregisterListener(key)
}
