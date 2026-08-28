package telemetry

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
	"github.com/getoptimum/optimum-common/pkg/version"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

// namespace and subsystem are set from AppConfig.TelemetryNamespace / TelemetrySubsystem.
const (
	labelDirection = "direction"
	labelTopic     = "topic"
	labelProtocol  = "protocol"
)

var (
	namespace string
	subsystem string
)

var (
	labeledRegistry prometheus.Registerer
	CustomRegistry  *prometheus.Registry
	oncer           sync.Once
	mimirDone       <-chan struct{}
	enabledMetrics  bool
)

// pushToken holds the current bearer token used by mimir/loki remote pushes.
// auth_token.Manager calls SetPushToken on every successful mint; pushers
// read currentPushToken() before each request and skip the push when it's
// empty (v2-mimir / v2-loki require Bearer auth).
var pushToken atomic.Pointer[string]

const (
	// scrapePushTimeout bounds a single Loki push round-trip.
	scrapePushTimeout = 5 * time.Second
)

// MetricsGatherer returns the gatherer used for /metrics, including remote-write when active.
func MetricsGatherer() prometheus.Gatherer {
	if r := mimirRemoteRegistry(); r != nil {
		return prometheus.Gatherers{CustomRegistry, r}
	}
	return CustomRegistry
}

// HTTPMetricsGatherer re-resolves MetricsGatherer on each scrape so async remote-write metrics are included.
var HTTPMetricsGatherer prometheus.Gatherer = prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) {
	return MetricsGatherer().Gather()
})

func MetricsEnabled() bool {
	return enabledMetrics
}

// newMetricsHTTPClient creates an HTTP client with keep-alive enabled
// to reuse TCP connections and avoid DNS lookup + TCP handshake on each push
func newMetricsHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp4", addr)
			},
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
			MaxIdleConnsPerHost: 2,
		},
		Timeout: 5 * time.Second,
	}
}

// InitMetrics initializes the metrics registry.
// namespace and subsystem are taken directly from cfg — defaults are defined
// in AppConfig (OPT_TELEMETRY_NAMESPACE / OPT_TELEMETRY_SUBSYSTEM).
// It returns a done channel that is closed once the Mimir remote-write goroutine has
// completed its final flush (nil when RemotePushEnable is false).
func InitMetrics(ctx context.Context, log logger.AppLogger, cfg *config.AppConfig) <-chan struct{} {
	oncer.Do(func() {
		namespace = cfg.TelemetryNamespace
		subsystem = cfg.TelemetrySubsystem
		// wrap the default registry with our global labels
		CustomRegistry = prometheus.NewRegistry()
		baseLabels := prometheus.Labels{
			"gateway_id":         cfg.GatewayID,
			"gateway_cluster_id": cfg.GatewayClusterID,
			"org_id":             cfg.OrgID,
		}
		maps.Copy(baseLabels, cfg.MetaLabels)
		labeledRegistry = prometheus.WrapRegistererWith(baseLabels, CustomRegistry)
		labeledRegistry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)

		commonmetrics.SetLabeledRegistry(labeledRegistry, namespace)
		InitMetricsWithRegistry(log, cfg.GatewayType)
		if cfg.RemotePushEnable {
			mimirDone = startMimirRemoteWrite(ctx, log, cfg)
		}
	})
	return mimirDone
}

// SetPushToken installs the bearer token used by subsequent mimir/loki pushes.
func SetPushToken(token string) {
	pushToken.Store(&token)
	refreshMimirRemoteWriteHeaders()
}

// currentPushToken returns the most recently installed token, or "" if none.
func currentPushToken() string {
	p := pushToken.Load()
	if p == nil {
		return ""
	}
	return *p
}

// pushAuthHeaders returns a fresh copy of base with Authorization set to the current bearer token.
func pushAuthHeaders(base map[string]string) (map[string]string, error) {
	token := currentPushToken()
	if token == "" {
		return nil, errors.New("token is unavailable skipping push")
	}
	headers := make(map[string]string, len(base)+1)
	maps.Copy(headers, base)
	headers["Authorization"] = "Bearer " + token
	return headers, nil
}

// InitMetricsWithRegistry initializes and registers all gateway metric collectors and enables
// metrics collection. pairedWith labels the app build-info metric with this gateway's type.
func InitMetricsWithRegistry(log logger.AppLogger, pairedWith string) {
	go commonmetrics.GetCoordinates()

	initGatewayMetrics()
	initMessageSizeMetrics()
	initPeersMetrics()
	initProcessingSpeedMetrics()
	initAggregationMetrics()
	initAttestationMetrics()
	initAuthMetrics()
	initGatewayHealthMetrics()
	initMump2pTraceMetrics()
	initBandwidthMetrics()
	initConnMetrics()
	initMumP2PMetrics()
	initP2PMetrics()
	initStreamMetrics()
	initBootstrapMetrics()
	initRLNCMetrics()
	publicIP, _, err := commonnet.GetExternalIPs()
	if err != nil {
		log.Error("could not get public IP", err)
		publicIP = "unknown"
	}
	appInfo := commonmetrics.NewGaugeVec("app_build_info", subsystem, "Build information.", []string{
		"version",
		"commit",
		"go",
		"public_ip",
		"paired_with",
	})
	appInfo.With(prometheus.Labels{
		"version":     version.GetVersion(),
		"commit":      version.GetCommitHash(),
		"go":          runtime.Version(),
		"public_ip":   publicIP,
		"paired_with": pairedWith,
	}).Set(1)
	enabledMetrics = true
}
