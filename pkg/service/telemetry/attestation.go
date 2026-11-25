package telemetry

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

var (
	attestationForwardedTotalMumP2P prometheus.Counter
	attestationForwardedTotalCL     prometheus.Counter

	// TODO: verify do we need this
	attestationPackTotal          prometheus.Counter
	attestationPackErrorTotal     prometheus.Counter
	attestationPackSizeBytes      prometheus.Histogram
	attestationPackItems          prometheus.Histogram
	attestationPackUniqueDataKeys prometheus.Histogram

	attestationSubnetMessages *prometheus.CounterVec

	attestationEvaluatedTotal       prometheus.Counter
	attestationDroppedTotal         *prometheus.CounterVec
	attestationInclusionDelay       prometheus.Histogram
	attestationPackLatencyMs        prometheus.Histogram
	attestationPropagationLatencyMs prometheus.Histogram
)

func initAttestationMetrics() {
	attestationPackTotal = commonmetrics.NewCounter(
		"attestation_pack_total",
		subsystem,
		"Total attestation messages successfully packed",
	)
	attestationPackErrorTotal = commonmetrics.NewCounter(
		"attestation_pack_errors_total",
		subsystem,
		"Attestation messages that failed packing",
	)
	attestationPackSizeBytes = commonmetrics.NewSimpleHistogram(
		"attestation_pack_size_bytes",
		subsystem,
		"Size of emitted packed attestation blobs in bytes",
		prometheus.ExponentialBuckets(256, 2, 16),
	)
	attestationPackItems = commonmetrics.NewSimpleHistogram(
		"attestation_pack_items",
		subsystem,
		"Number of attestation items per packed blob",
		prometheus.ExponentialBuckets(1, 2, 16),
	)

	attestationPackUniqueDataKeys = commonmetrics.NewSimpleHistogram(
		"attestation_pack_unique_data_keys",
		subsystem,
		"Number of unique AttestationData hashes per pack emit cycle (measures dedup effectiveness)",
		prometheus.ExponentialBuckets(1, 2, 12),
	)

	attestationSubnetMessages = commonmetrics.NewCounterVec(
		"attestation_subnet_messages_total",
		subsystem,
		"Attestation messages received from CL per subnet index",
		[]string{"subnet"},
	)

	attestationEvaluatedTotal = commonmetrics.NewCounter(
		"attestation_evaluated_total",
		subsystem,
		"Total attestations evaluated by the router (forwarded + dropped, for inclusion rate)",
	)
	attestationForwardedTotalMumP2P = commonmetrics.NewCounter(
		"attestation_forwarded_mump2p_total",
		subsystem,
		"Attestations forwarded to mump2p after router filter",
	)
	attestationForwardedTotalCL = commonmetrics.NewCounter(
		"attestation_forwarded_cl_total",
		subsystem,
		"Attestations forwarded to CL after router filter",
	)
	attestationDroppedTotal = commonmetrics.NewCounterVec(
		"attestation_dropped_total",
		subsystem,
		"Attestations dropped by the message router",
		[]string{"reason"},
	)
	attestationInclusionDelay = commonmetrics.NewSimpleHistogram(
		"attestation_inclusion_delay_slots",
		subsystem,
		"Slot distance between attestation data.slot and current wall-clock slot at receive time",
		[]float64{0, 1, 2, 3, 4, 8, 16, 32, 64},
	)
	attestationPackLatencyMs = commonmetrics.NewSimpleHistogram(
		"attestation_pack_latency_ms",
		subsystem,
		"Time in milliseconds to encode and emit one attestation pack cycle",
		[]float64{0.1, 0.5, 1, 2, 5, 10, 25, 50, 100},
	)
	attestationPropagationLatencyMs = commonmetrics.NewSimpleHistogram(
		"attestation_propagation_latency_ms",
		subsystem,
		"End-to-end latency from sender gateway emit to receiver gateway decode (ms)",
		[]float64{5, 10, 25, 50, 100, 250, 500, 650, 800, 1000, 1500, 2000, 5000, 8000},
	)
}

func IncAttestationPack(count int) {
	if enabledMetrics {
		attestationPackTotal.Add(float64(count))
	}
}

func IncAttestationPackError() {
	if enabledMetrics {
		attestationPackErrorTotal.Inc()
	}
}

func ObserveAttestationPackSize(sizeBytes int) {
	if enabledMetrics {
		attestationPackSizeBytes.Observe(float64(sizeBytes))
	}
}

func ObserveAttestationPackItems(items int) {
	if enabledMetrics {
		attestationPackItems.Observe(float64(items))
	}
}

func ObserveAttestationPackUniqueDataKeys(keys int) {
	if enabledMetrics {
		attestationPackUniqueDataKeys.Observe(float64(keys))
	}
}

func IncAttestationSubnet(subnetIndex uint32) {
	if enabledMetrics {
		attestationSubnetMessages.WithLabelValues(strconv.FormatUint(uint64(subnetIndex), 10)).Inc()
	}
}

func IncAttestationEvaluated() {
	if enabledMetrics {
		attestationEvaluatedTotal.Inc()
	}
}

func IncAttestationForwarded(target entities.Source) {
	if !enabledMetrics {
		return
	}
	switch target {
	case entities.SourceLibP2P:
		attestationForwardedTotalCL.Inc()
	case entities.SourceMumP2P:
		attestationForwardedTotalMumP2P.Inc()
	}
}

func IncAttestationDropped(reason string) {
	if enabledMetrics {
		attestationDroppedTotal.WithLabelValues(reason).Inc()
	}
}

func ObserveAttestationInclusionDelay(slotDelta uint64) {
	if enabledMetrics {
		attestationInclusionDelay.Observe(float64(slotDelta))
	}
}

func ObserveAttestationPackLatency(ms float64) {
	if enabledMetrics {
		attestationPackLatencyMs.Observe(ms)
	}
}

func ObserveAttestationPropagationLatency(ms float64) {
	if enabledMetrics && ms > 0 {
		attestationPropagationLatencyMs.Observe(ms)
	}
}
