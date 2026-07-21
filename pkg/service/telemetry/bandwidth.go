package telemetry

import (
	"time"

	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

var (
	totalBytes          *prometheus.CounterVec
	messagesPerProtocol *prometheus.CounterVec
	trafficPerProtocol  *prometheus.CounterVec
)

func initBandwidthMetrics() {
	totalBytes = commonmetrics.NewCounterVec(
		"total_bytes",
		"bandwidth",
		"Total traffic",
		[]string{"direction"})
	messagesPerProtocol = commonmetrics.NewCounterVec(
		"messages_total",
		"bandwidth",
		"Messages per protocol",
		[]string{"protocol", "direction"})
	trafficPerProtocol = commonmetrics.NewCounterVec(
		"traffic_bytes_total",
		"bandwidth",
		"Traffic per protocol",
		[]string{"protocol", "direction"})
}

type BandwidthCollector struct{}

func NewBandwidthCollector() *BandwidthCollector { return &BandwidthCollector{} }

// LogSentMessageStream logs the size of a sent message stream along with its protocol and peer ID.
func (b *BandwidthCollector) LogSentMessageStream(size int64, proto protocol.ID, _ peer.ID) {
	totalBytes.WithLabelValues("outgoing").Add(float64(size))
	trafficPerProtocol.WithLabelValues(string(proto), "outgoing").Add(float64(size))
	messagesPerProtocol.WithLabelValues(string(proto), "outgoing").Inc()
}

// LogRecvMessageStream logs the size of a received message stream along with its protocol and peer ID.
func (b *BandwidthCollector) LogRecvMessageStream(size int64, proto protocol.ID, _ peer.ID) {
	totalBytes.WithLabelValues("incoming").Add(float64(size))
	trafficPerProtocol.WithLabelValues(string(proto), "incoming").Add(float64(size))
	messagesPerProtocol.WithLabelValues(string(proto), "incoming").Inc()
}

func (b *BandwidthCollector) LogSentMessage(int64)                      {}
func (b *BandwidthCollector) LogRecvMessage(int64)                      {}
func (b *BandwidthCollector) GetBandwidthForPeer(peer.ID) metrics.Stats { return metrics.Stats{} }
func (b *BandwidthCollector) GetBandwidthForProtocol(protocol.ID) metrics.Stats {
	return metrics.Stats{}
}
func (b *BandwidthCollector) GetBandwidthTotals() metrics.Stats                     { return metrics.Stats{} }
func (b *BandwidthCollector) GetBandwidthByPeer() map[peer.ID]metrics.Stats         { return nil }
func (b *BandwidthCollector) GetBandwidthByProtocol() map[protocol.ID]metrics.Stats { return nil }
func (b *BandwidthCollector) Reset()                                                {}
func (b *BandwidthCollector) TrimIdle(time.Time)                                    {}
