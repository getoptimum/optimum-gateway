package telemetry

import (
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	connections        *prometheus.GaugeVec
	streamsPerProtocol *prometheus.GaugeVec
	durationHistogram  *prometheus.HistogramVec
)

func initConnMetrics() {
	connections = commonmetrics.NewGaugeVec(
		"total_connections",
		"conn",
		"Active libp2p connections",
		[]string{"direction"})
	streamsPerProtocol = commonmetrics.NewGaugeVec(
		"streams_current",
		"conn",
		"Active streams per protocol",
		[]string{labelProtocol})
	durationHistogram = commonmetrics.NewHistogram(
		"stream_duration_seconds",
		"conn",
		"Duration of streams",
		[]string{labelProtocol})
}

type ConnectionsMeeter struct{}

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

func (c *ConnectionsMeeter) OpenedStream(_ network.Network, str network.Stream) {
	streamsPerProtocol.WithLabelValues(string(str.Protocol())).Inc()
}

func (c *ConnectionsMeeter) ClosedStream(_ network.Network, str network.Stream) {
	protocolID := string(str.Protocol())
	streamsPerProtocol.WithLabelValues(protocolID).Dec()
	duration := time.Since(str.Stat().Opened)
	durationHistogram.WithLabelValues(protocolID).Observe(duration.Seconds())
}
