package mump2p

import (
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

func (n *Node) DumpState(clusterID string) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
			n.log.Info("dumping state",
				logger.WithString("cluster_id", clusterID),
				logger.WithString("host_id", n.host.ID().String()),
				logger.WithInt("peers", len(n.host.Network().Peers())),
			)
			for _, topic := range n.GetTopics() {
				n.log.Info("topic peers", logger.WithTopic(topic), logger.WithInt("peers", len(n.GetMeshPeers(topic))))
			}
		}
	}
}
