package telemetry

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

var (
	badMessage           *prometheus.CounterVec
	beaconBlockSizeBytes prometheus.Histogram

	badMsgToMum = uint64(0)
	badMsgToCL  = uint64(0)
)

func initMessageSizeMetrics() {
	badMessage = commonmetrics.NewCounterVec(
		"bad_messages_to_cl_total",
		subsystem,
		"Total number of bad messages sent to CL nodes",
		[]string{"direction"},
	)
	beaconBlockSizeBytes = commonmetrics.NewSimpleHistogram(
		"beacon_block_size_bytes",
		subsystem,
		"Size of received beacon block messages in bytes",
		prometheus.ExponentialBuckets(64, 2, 16),
	)
}

func ObserveBeaconBlockSize(sizeBytes int) {
	if enabledMetrics {
		beaconBlockSizeBytes.Observe(float64(sizeBytes))
	}
}

func IncreaseBadMessages(source entities.Source) {
	switch source {
	case entities.SourceMumP2P:
		IncreaseBadMessagesToMum()
	case entities.SourceLibP2P:
		IncreaseBadMessagesToCL()
	}
}

func IncreaseBadMessagesToMum() {
	atomic.AddUint64(&badMsgToMum, 1)
	if enabledMetrics {
		badMessage.WithLabelValues("mum").Inc()
	}
}

func GetBadMessagesToMum() uint64 {
	return atomic.LoadUint64(&badMsgToMum)
}

func IncreaseBadMessagesToCL() {
	atomic.AddUint64(&badMsgToCL, 1)
	if enabledMetrics {
		badMessage.WithLabelValues("cl").Inc()
	}
}

func GetBadMessagesToCL() uint64 {
	return atomic.LoadUint64(&badMsgToCL)
}
