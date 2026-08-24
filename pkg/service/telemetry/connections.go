package telemetry

import (
	"strings"

	"github.com/libp2p/go-libp2p/core/network"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var connections *prometheus.GaugeVec

func initConnMetrics() {
	connections = commonmetrics.NewGaugeVec(
		"total_connections",
		"conn",
		"Active libp2p connections",
		[]string{"direction"})
}

// ConnectionsMeeter implements network.Notifiee for connection counts.
// Stream metrics come from libp2p.PrometheusRegisterer (libp2p_rcmgr_streams).
type ConnectionsMeeter struct{}

var _ network.Notifiee = (*ConnectionsMeeter)(nil)

// NewConnectionsMeeter creates a new ConnectionsMeeter instance.
func NewConnectionsMeeter() *ConnectionsMeeter { return &ConnectionsMeeter{} }

func (c *ConnectionsMeeter) Listen(network.Network, ma.Multiaddr)      {}
func (c *ConnectionsMeeter) ListenClose(network.Network, ma.Multiaddr) {}

func (c *ConnectionsMeeter) Connected(_ network.Network, conn network.Conn) {
	connections.WithLabelValues(strings.ToLower(conn.Stat().Direction.String())).Inc()
}

func (c *ConnectionsMeeter) Disconnected(_ network.Network, conn network.Conn) {
	connections.WithLabelValues(strings.ToLower(conn.Stat().Direction.String())).Dec()
}
