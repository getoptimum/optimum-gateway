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

	executionPayloadSkipped   *prometheus.CounterVec
	executionPayloadReceived  *prometheus.CounterVec
	executionPayloadSizeBytes prometheus.Histogram
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
	executionPayloadSkipped = commonmetrics.NewCounterVec(
		"execution_payload_skipped_total",
		subsystem,
		"Execution payloads skipped before publish because they exceed the destination budget",
		[]string{labelDirection},
	)
	executionPayloadReceived = commonmetrics.NewCounterVec(
		"execution_payload_received_total",
		subsystem,
		"Execution payloads received, by the side they arrived on",
		[]string{labelDirection},
	)
	executionPayloadSizeBytes = commonmetrics.NewSimpleHistogram(
		"execution_payload_size_bytes",
		subsystem,
		"Size of received execution payloads in bytes",
		prometheus.ExponentialBuckets(64, 2, 16),
	)
}

func ObserveBeaconBlockSize(sizeBytes int) {
	if enabledMetrics {
		beaconBlockSizeBytes.Observe(float64(sizeBytes))
	}
}

// IncExecutionPayloadSkipped counts an execution payload that was not published.
// direction is "mump2p" or "cl", the side the publish would have gone to.
func IncExecutionPayloadSkipped(direction string) {
	if enabledMetrics && executionPayloadSkipped != nil {
		executionPayloadSkipped.WithLabelValues(direction).Inc()
	}
}

// ObserveExecutionPayload records one arrival. direction is "mump2p" or "cl".
func ObserveExecutionPayload(direction string, size int) {
	if !enabledMetrics {
		return
	}
	if executionPayloadReceived != nil {
		executionPayloadReceived.WithLabelValues(direction).Inc()
	}
	if executionPayloadSizeBytes != nil {
		executionPayloadSizeBytes.Observe(float64(size))
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
