package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	clPeers     *prometheus.GaugeVec
	mumP2PPeers *prometheus.GaugeVec

	clPeersPerTopic     *prometheus.GaugeVec
	mumP2PPeersPerTopic *prometheus.GaugeVec

	clMeshPeer     *prometheus.GaugeVec
	mumP2PMeshPeer *prometheus.GaugeVec

	clPeerConnected    *prometheus.CounterVec
	clPeerDisconnected *prometheus.CounterVec

	mumP2PPeerConnected    *prometheus.CounterVec
	mumP2PPeerDisconnected *prometheus.CounterVec
)

func initPeersMetrics() {
	clPeersPerTopic = commonmetrics.NewGaugeVec(
		"cl_peers_per_topic",
		subsystem,
		"Total CL peers connected to gateway per topic",
		[]string{labelTopic},
	)
	mumP2PPeersPerTopic = commonmetrics.NewGaugeVec(
		"mump2p_peers_per_topic",
		subsystem,
		"Total mump2p peers connected to gateway per topic",
		[]string{labelTopic},
	)
	clPeers = commonmetrics.NewGaugeVec(
		"cl_peers",
		subsystem,
		"Total CL peers connected to gateway",
		nil,
	)
	mumP2PPeers = commonmetrics.NewGaugeVec(
		"mump2p_peers",
		subsystem,
		"Total mump2p peers connected to gateway",
		nil,
	)

	clMeshPeer = commonmetrics.NewGaugeVec(
		"cl_mesh_peer",
		subsystem,
		"CL peers in GossipSub mesh per topic (value 1 per peer)",
		[]string{labelTopic, "peer_id"},
	)
	mumP2PMeshPeer = commonmetrics.NewGaugeVec(
		"mump2p_mesh_peer",
		subsystem,
		"mump2p peers in mesh per topic (value 1 per peer)",
		[]string{labelTopic, "peer_id"},
	)

	clPeerConnected = commonmetrics.NewCounterVec(
		"cl_peer_connected_total",
		subsystem,
		"CL peer connections established",
		nil,
	)
	clPeerDisconnected = commonmetrics.NewCounterVec(
		"cl_peer_disconnected_total",
		subsystem,
		"CL peer disconnections",
		nil,
	)

	mumP2PPeerConnected = commonmetrics.NewCounterVec(
		"mump2p_peer_connected_total",
		subsystem,
		"mump2p peer connections established",
		nil,
	)
	mumP2PPeerDisconnected = commonmetrics.NewCounterVec(
		"mump2p_peer_disconnected_total",
		subsystem,
		"mump2p peer disconnections",
		nil,
	)
}

func CLPeerConnected() {
	if enabledMetrics {
		clPeers.WithLabelValues().Inc()
		clPeerConnected.WithLabelValues().Inc()
	}
}

func CLPeerDisconnected() {
	if enabledMetrics {
		clPeers.WithLabelValues().Dec()
		clPeerDisconnected.WithLabelValues().Inc()
	}
}

func MumP2PPeerConnected() {
	if enabledMetrics {
		mumP2PPeers.WithLabelValues().Inc()
		mumP2PPeerConnected.WithLabelValues().Inc()
	}
}

func MumP2PPeerDisconnected() {
	if enabledMetrics {
		mumP2PPeers.WithLabelValues().Dec()
		mumP2PPeerDisconnected.WithLabelValues().Inc()
	}
}

func SetCLPeers(count int) {
	if enabledMetrics {
		clPeers.WithLabelValues().Set(float64(count))
	}
}

func SetMumP2PPeers(count int) {
	if enabledMetrics {
		mumP2PPeers.WithLabelValues().Set(float64(count))
	}
}

func ResetCLPeersPerTopic() {
	if enabledMetrics {
		clPeersPerTopic.Reset()
	}
}

func SetCLPeersPerTopic(topic string, count int) {
	if enabledMetrics {
		clPeersPerTopic.WithLabelValues(topic).Set(float64(count))
	}
}

func ResetMumP2PPeersPerTopic() {
	if enabledMetrics {
		mumP2PPeersPerTopic.Reset()
	}
}

func SetMumP2PPeersPerTopic(topic string, count int) {
	if enabledMetrics {
		mumP2PPeersPerTopic.WithLabelValues(topic).Set(float64(count))
	}
}

func SetCLMeshPeers(topicPeerIDs map[string][]string) {
	if !enabledMetrics {
		return
	}
	clMeshPeer.Reset()
	for topic, peerIDs := range topicPeerIDs {
		for _, pid := range peerIDs {
			clMeshPeer.WithLabelValues(topic, pid).Set(1)
		}
	}
}

func SetMumP2PMeshPeers(topicPeerIDs map[string][]string) {
	if !enabledMetrics {
		return
	}
	mumP2PMeshPeer.Reset()
	for topic, peerIDs := range topicPeerIDs {
		for _, pid := range peerIDs {
			mumP2PMeshPeer.WithLabelValues(topic, pid).Set(1)
		}
	}
}
