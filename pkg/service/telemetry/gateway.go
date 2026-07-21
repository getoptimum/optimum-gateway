package telemetry

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

const (
	maxMessageAgeSeconds = 30
)

var (
	clHealthy  prometheus.Gauge
	mumHealthy prometheus.Gauge

	lastCLMessageAt  atomic.Int64
	lastMumMessageAt atomic.Int64

	clWorking  atomic.Int64
	mumWorking atomic.Int64

	propagationState prometheus.Gauge
	parseCLErrors    = syncx.NewRWMap[string, uint64]()
	parseMumErrors   = syncx.NewRWMap[string, uint64]()
)

func initGatewayHealthMetrics() {
	propagationState = commonmetrics.NewGaugeVec(
		"propagation_state",
		subsystem,
		"Gateway's propagation state",
		nil,
	).WithLabelValues()
	clHealthy = commonmetrics.NewGaugeVec(
		"cl_health_status",
		subsystem,
		"Gateway CL health: 1 = healthy, 0 = degraded",
		nil,
	).WithLabelValues()
	mumHealthy = commonmetrics.NewGaugeVec(
		"mump2p_health_status",
		subsystem,
		"Gateway MumP2P health: 1 = healthy, 0 = degraded",
		nil,
	).WithLabelValues()
	go bgUpdateHealthStatus()
}

func bgUpdateHealthStatus() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		n := time.Now().Unix()
		clState := connectionAlive(n, lastCLMessageAt.Load())
		clHealthy.Set(clState)
		clWorking.Store(int64(clState))

		mumState := connectionAlive(n, lastMumMessageAt.Load())
		mumWorking.Store(int64(mumState))
		mumHealthy.Set(mumState)
	}
}

func HealthCL() int64 {
	return clWorking.Load()
}

func HealthMUM() int64 {
	return mumWorking.Load()
}

func RecordCLMessageAt() {
	if enabledMetrics {
		lastCLMessageAt.Store(time.Now().Unix())
	}
}

func RecordMumMessageAt() {
	if enabledMetrics {
		lastMumMessageAt.Store(time.Now().Unix())
	}
}

func SetPropagationState(enabled bool) {
	if enabledMetrics {
		val := float64(0)
		if enabled {
			val = float64(1)
		}

		propagationState.Set(val)
	}
}

func connectionAlive(currentUnix, lastSeenUnix int64) float64 {
	// if we haven't seen any message, consider the connection degraded (0)
	// we expect that in 30 sec at least on message will come
	diff := currentUnix - lastSeenUnix
	if diff > maxMessageAgeSeconds {
		return 0
	}
	return 1
}

func IncParseSSZError(topic string, source entities.Source) {
	if source == entities.SourceLibP2P {
		parseCLErrors.Upsert(topic, func(value uint64) uint64 {
			return value + 1
		}, 1)
	} else {
		parseMumErrors.Upsert(topic, func(value uint64) uint64 {
			return value + 1
		}, 1)
	}
}

func GetCLSSZErrors() map[string]uint64 {
	return parseCLErrors.LoadAllAndErase()
}

func GetMumSSZErrors() map[string]uint64 {
	return parseMumErrors.LoadAllAndErase()
}
