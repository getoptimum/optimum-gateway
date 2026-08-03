package config

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getoptimum/optimum-common/pkg/chain"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/version"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

const (
	DefaultMaxMessageSize            int64   = 1024 * 1024 // 1MB
	DefaultRandomMessageSize         int64   = 512
	DefaultShardFactor               int64   = 4
	DefaultPublisherShardMultiplier  float32 = 1.2
	DefaultForwardShardThreshold     float32 = 0.75
	DefaultMeshDegreeTarget          int64   = 6
	DefaultMeshDegreeMin             int64   = 4
	DefaultMeshDegreeMax             int64   = 12
	DefaultAggregationIntervalMs     int64   = 25
	maxAggregationIntervalMs         int64   = 600000 // 10 minutes
	DefaultAttestationSyncChunkSize  int     = 64
	DefaultAttestationPublishAfterMs int64   = 4000
	DefaultAttestationPublishCapMs   int64   = 8000
	DefaultAttestationMaxSlotAge     uint64  = 0
)

// AppConfig holds all the configuration for the gateway service
type AppConfig struct {
	Version     string `yaml:"version"`
	CommitHash  string `yaml:"commit_hash"`
	EnablePProf bool   `yaml:"enable_pprof" env:"OPT_ENABLE_PPROF" default:"false"`
	PProfAddr   string `yaml:"pprof_addr" env:"OPT_PPROF_ADDR" default:"127.0.0.1:6060"`
	// mump2p trace-event categories consumed in-process for analysis (see handleMumP2PTrace).
	// All default false. TraceRPC is a high-frequency firehose — enable only for deep debugging.
	TraceMesh          bool     `yaml:"trace_mesh" env:"OPT_TRACE_MESH" default:"false"`
	TraceRPC           bool     `yaml:"trace_rpc" env:"OPT_TRACE_RPC" default:"false"`
	TraceShard         bool     `yaml:"trace_shard" env:"OPT_TRACE_SHARD" default:"false"`
	LogLevel           string   `yaml:"log_level" env:"OPT_LOG_LEVEL" default:"debug"`
	IdentityLibP2PDir  string   `yaml:"identity_libp2p_dir" env:"OPT_IDENTITY_LIBP2P_DIR" default:"/tmp/libp2p"`
	IdentityMumP2PDir  string   `yaml:"identity_mump2p_dir" env:"OPT_IDENTITY_MUMP2P_DIR" default:"/tmp/mump2p"`
	AgentLibP2PPort    int      `yaml:"agent_lib_p2p_port" env:"OPT_AGENT_LIB_P2P_PORT" default:"33212"`
	AgentMumP2PPort    int      `yaml:"agent_mump2p_port" env:"OPT_AGENT_MUMP2P_PORT" default:"33213"`
	DirectCLPeers      []string `yaml:"direct_cl_peers" env:"OPT_DIRECT_CL_PEERS"`
	TelemetryEnable    bool     `yaml:"telemetry_enable" env:"OPT_ENABLE_TELEMETRY" default:"false"`
	TelemetryPort      int      `yaml:"telemetry_port" env:"OPT_TELEMETRY_PORT" default:"48123"`
	TelemetryNamespace string   `yaml:"telemetry_namespace" env:"OPT_TELEMETRY_NAMESPACE" default:"mump2p"`
	TelemetrySubsystem string   `yaml:"telemetry_subsystem" env:"OPT_TELEMETRY_SUBSYSTEM" default:"gateway"`
	GatewayClusterID   string   `yaml:"gateway_cluster_id" env:"OPT_GATEWAY_CLUSTER_ID"`
	// Handle of the out-of-process RLNC coder (the rlnc-server sidecar). Both must
	// match the sidecar's own --name and --lanes. Empty/zero keeps the mump2p
	// protocol defaults, which are what the sidecar defaults to as well.
	SHMName               string `yaml:"shm_name" env:"OPT_SHM_NAME"`
	SHMLanes              int    `yaml:"shm_lanes" env:"OPT_SHM_LANES"`
	AggregationIntervalMs int64  `yaml:"aggregation_interval_ms" env:"OPT_AGGREGATION_INTERVAL_MS" default:"25"`
	PropagationEnabledRaw bool   `yaml:"propagation_enabled" env:"OPT_PROPAGATION_ENABLED" default:"true"`
	RemoteBootstrapURL    string `yaml:"remote_bootstrap_url" env:"OPT_REMOTE_BOOTSTRAP_URL" default:"https://bootstrap.getoptimum.io"`
	// Datagram data plane. Off by default: enabling it binds a second UDP socket
	// and moves mesh traffic onto keys negotiated per peer over the handshake
	// connection. An empty listen address takes the protocol's own default.
	DatagramEnable     bool   `yaml:"datagram_enable"      env:"OPT_DATAGRAM_ENABLE"      default:"false"`
	DatagramListenAddr string `yaml:"datagram_listen_addr" env:"OPT_DATAGRAM_LISTEN_ADDR" default:""`
	DatagramMaxPayload int    `yaml:"datagram_max_payload" env:"OPT_DATAGRAM_MAX_PAYLOAD" default:"0"`
	// OpenTelemetry span export for RLNC trace events. Off by default. The
	// endpoint is an OTLP/HTTP host:port with no scheme; insecure selects http.
	OTelEnable      bool    `yaml:"otel_enable"       env:"OPT_OTEL_ENABLE"       default:"false"`
	OTelEndpoint    string  `yaml:"otel_endpoint"     env:"OPT_OTEL_ENDPOINT"     default:""`
	OTelInsecure    bool    `yaml:"otel_insecure"     env:"OPT_OTEL_INSECURE"     default:"false"`
	OTelSampleRatio float64 `yaml:"otel_sample_ratio" env:"OPT_OTEL_SAMPLE_RATIO" default:"1.0"`
	//
	// AUTH Related Configs and dynamic values.
	//
	// Auth service that mints gateway JWTs (POST {url}/api/v1/auth/token) and
	// hosts the JWKS used to verify peer JWTs (GET {issuer}/.well-known/jwks.json).
	RemoteAuthURL          string `yaml:"remote_auth_url"    env:"OPT_REMOTE_AUTH_URL"    default:"https://auth.getoptimum.io"`
	APIKey                 string `yaml:"api_key"            env:"OPT_API_KEY"            default:""`
	JWKSCachePath          string `yaml:"jwks_cache_path"            env:"OPT_JWKS_CACHE_PATH"            default:"/gateway/cache/jwks.json"`
	JWKSRefreshIntervalSec int    `yaml:"jwks_refresh_interval_sec"  env:"OPT_JWKS_REFRESH_INTERVAL_SEC"  default:"3600"`
	// GatewayID is JWT-sourced in production — InitRuntime overwrites this
	// field with the `sub` claim once the auth manager has minted. Yaml /
	// env values are only used in dev mode (OPT_ENABLE_AUTH=false); a yaml
	// or OPT_GATEWAY_ID value in a prod-auth setup is silently replaced by
	// the JWT subject at boot.
	GatewayID string `yaml:"gateway_id" env:"OPT_GATEWAY_ID" default:"dev-gateway"`
	// GatewayType is JWT-sourced — InitRuntime sets it from the `type` claim
	// (hermes|partner|relay) once the auth manager has minted. Empty in dev
	// mode (OPT_ENABLE_AUTH=false), where there is no minted claim. Used as
	// the `paired_with` label on app_build_info metrics.
	GatewayType string `yaml:"-"`
	// OrgID is JWT-sourced — InitRuntime sets it from the `org_id` claim
	// once the auth manager has minted. Empty in dev mode (OPT_ENABLE_AUTH=false).
	OrgID string `yaml:"-"`
	// MetaLabels are services-token gateway metadata (#74) added as metric labels.
	MetaLabels map[string]string `yaml:"-"`
	// EnableAuth gates the gateway-side JWT mint + verify + Bearer-on-outbound
	// pipeline. Default true (prod). Set OPT_ENABLE_AUTH=false ALONGSIDE
	// bootstrap's OPT_ENABLE_AUTH=false for local stacks running without a
	// real billing/auth service. Must not be disabled in production — the
	// gateway logs a loud startup warning when off.
	EnableAuth bool `yaml:"enable_auth" env:"OPT_ENABLE_AUTH" default:"true"`

	RemotePushEnable   bool   `yaml:"remote_push_enable" env:"OPT_REMOTE_PUSH_ENABLE" default:"false"`
	RemotePushMimirURL string `yaml:"remote_push_mimir_url" env:"OPT_REMOTE_PUSH_MIMIR_URL" default:"https://v2-mimir.getoptimum.io"`
	RemotePushLokiURL  string `yaml:"remote_push_loki_url" env:"OPT_REMOTE_PUSH_LOKI_URL" default:"https://v2-loki.getoptimum.io"`
	// RemotePushWALDir is the base dir for prometheus agent + remote-write WAL.
	RemotePushWALDir string `yaml:"remote_push_wal_dir" env:"OPT_REMOTE_PUSH_WAL_DIR" default:"/gateway/storage/wal"`

	rotator               *commonconfig.Rotator
	propagationEnabled    atomic.Bool
	skipMessageFromSelf   atomic.Bool
	aggregationIntervalMs atomic.Int64
	logger                logger.AppLogger
	// effectiveRLNC is the running node's own parameters, installed once the node
	// exists and read by the periodic logging goroutine, hence the atomic.
	effectiveRLNC atomic.Pointer[func() (entities.RLNCParams, bool)]
}

// SetEffectiveRLNCSource installs the running mump2p node as the authority for
// the RLNC and mesh values LogConfigState reports.
//
// Until it is installed there is no node, and the log falls back to the dynamic
// config's current view. That view is not what any node is running: the rotator
// is seeded with the built-in defaults and fetches in the background, so a log
// line emitted at startup reports the defaults whatever the operator served.
func (c *AppConfig) SetEffectiveRLNCSource(fn func() (entities.RLNCParams, bool)) {
	if fn == nil {
		return
	}
	c.effectiveRLNC.Store(&fn)
}

func LoadConfig(confFile string) (*AppConfig, error) {
	// explicit flag (--config)
	options := make([]commonconfig.Option, 0, 1)
	if confFile != "" {
		options = append(options, commonconfig.WithYAML(confFile))
	}

	var cfg AppConfig
	if err := commonconfig.Load(&cfg, options...); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	cfg.Version = version.GetVersion()
	cfg.CommitHash = version.GetCommitHash()
	cfg.propagationEnabled.Store(cfg.PropagationEnabledRaw)
	cfg.skipMessageFromSelf.Store(true)
	var aggMs int64
	if cfg.AggregationIntervalMs == 0 {
		aggMs = DefaultAggregationIntervalMs
	} else {
		aggMs = cfg.AggregationIntervalMs
	}
	cfg.aggregationIntervalMs.Store(aggMs)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}
	return &cfg, nil
}

// InitRuntime finishes the boot of AppConfig once the gateway knows its
// JWT-derived identity: chainStr is the `chain_id` claim (or OPT_DEV_CHAIN
// in dev mode); gatewayID is the `sub` claim (or the yaml/OPT_GATEWAY_ID
// fallback in dev mode); gatewayType is the `type` claim (empty in dev mode).
// Empty values are ignored so the yaml/env defaults stay in place.
func (c *AppConfig) InitRuntime(ctx context.Context, log logger.AppLogger, chainStr, gatewayID, gatewayType, orgID string) error {
	ch, err := chain.ChainFromString(chainStr)
	if err != nil {
		return fmt.Errorf("error parse chain from string: %w", err)
	}

	// Empty values are ignored so the yaml/env defaults (dev mode / pre-mint) stay in place.
	if gatewayID != "" {
		c.GatewayID = gatewayID
	}
	if gatewayType != "" {
		c.GatewayType = gatewayType
	}
	if orgID != "" {
		c.OrgID = orgID
	}
	// "gateway-" prefix differentiates this service in dynamic-config / monitoring (e.g. gateway-v0.0.1-rc11).
	bootstrapURL := strings.TrimSpace(c.RemoteBootstrapURL)
	if bootstrapURL != "" && !strings.HasPrefix(bootstrapURL, "http://") && !strings.HasPrefix(bootstrapURL, "https://") {
		bootstrapURL = "https://" + bootstrapURL
	}
	c.rotator = commonconfig.NewConfigRotator(ctx,
		log,
		&commonentities.OptimumConfig{
			MaxMessageSize:           DefaultMaxMessageSize,
			RandomMessageSize:        DefaultRandomMessageSize,
			ShardFactor:              DefaultShardFactor,
			PublisherShardMultiplier: DefaultPublisherShardMultiplier,
			ForwardShardThreshold:    DefaultForwardShardThreshold,
			MeshDegreeTarget:         DefaultMeshDegreeTarget,
			MeshDegreeMin:            DefaultMeshDegreeMin,
			MeshDegreeMax:            DefaultMeshDegreeMax,
		},
		ch.String(),
		c.GatewayClusterID,
		func(dc *commonentities.DynamicConfig) {
			c.propagationEnabled.Store(dc.PropagationEnabled)
			c.skipMessageFromSelf.Store(dc.ExcludeSelfMessages)
		},
		commonconfig.WithServiceVersion("gateway-"+c.Version),
		commonconfig.WithBootstrapBaseURL(bootstrapURL),
	)
	// Start periodic config logging goroutine (every 10 minutes).
	c.logger = log
	go c.logConfigPeriodically(ctx)
	return nil
}

// logConfigPeriodically logs the configuration state every 10 minutes
func (c *AppConfig) logConfigPeriodically(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	// Log immediately on startup
	c.LogConfigState()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.LogConfigState()
		}
	}
}

func (c *AppConfig) GetDCRotator() *commonconfig.Rotator {
	return c.rotator
}

// Validate ensures the AppConfig has valid and complete values
func (c *AppConfig) Validate() error {
	if c.IdentityLibP2PDir == "" {
		return fmt.Errorf("OPT_IDENTITY_LIBP2P_DIR is required")
	}
	if err := os.MkdirAll(c.IdentityLibP2PDir, 0o750); err != nil {
		return fmt.Errorf("failed to create identity directory %s: %w", c.IdentityLibP2PDir, err)
	}
	if c.IdentityMumP2PDir == "" {
		return fmt.Errorf("OPT_IDENTITY_MUMP2P_DIR is required")
	}
	if err := os.MkdirAll(c.IdentityMumP2PDir, 0o750); err != nil {
		return fmt.Errorf("failed to create identity directory %s: %w", c.IdentityMumP2PDir, err)
	}
	if c.AgentLibP2PPort <= 0 || c.AgentLibP2PPort > 65535 {
		return fmt.Errorf("OPT_AGENT_LIB_P2P_PORT must be between 1 and 65535")
	}
	if c.AgentMumP2PPort <= 0 || c.AgentMumP2PPort > 65535 {
		return fmt.Errorf("OPT_AGENT_MUMP2P_PORT must be between 1 and 65535")
	}
	if c.TelemetryPort <= 0 || c.TelemetryPort > 65535 {
		return fmt.Errorf("OPT_TELEMETRY_PORT must be between 1 and 65535")
	}
	if c.EnablePProf {
		addr := strings.TrimSpace(c.PProfAddr)
		if addr == "" {
			return fmt.Errorf("pprof_addr is required when enable_pprof is true")
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("invalid pprof_addr %q: %w", addr, err)
		}
		_ = ln.Close()
	}
	if c.GatewayClusterID == "" {
		return fmt.Errorf("OPT_GATEWAY_CLUSTER_ID is required")
	}
	if c.OTelEnable && strings.TrimSpace(c.OTelEndpoint) == "" {
		return fmt.Errorf("OPT_OTEL_ENDPOINT is required when OPT_OTEL_ENABLE is true")
	}
	// Mirrors the protocol's own gte=0,lte=1 bound, so a bad value fails here
	// rather than being silently clamped by the sampler.
	if c.OTelSampleRatio < 0 || c.OTelSampleRatio > 1 {
		return fmt.Errorf("OPT_OTEL_SAMPLE_RATIO must be between 0 and 1, got %v", c.OTelSampleRatio)
	}

	if c.AggregationIntervalMs < 0 {
		return fmt.Errorf("aggregation_interval_ms must be non-negative")
	}
	var effectiveAggMs int64
	if c.AggregationIntervalMs == 0 {
		effectiveAggMs = DefaultAggregationIntervalMs
	} else {
		effectiveAggMs = c.AggregationIntervalMs
	}
	if effectiveAggMs > maxAggregationIntervalMs {
		return fmt.Errorf("aggregation_interval_ms must be <= %d", maxAggregationIntervalMs)
	}

	return nil
}

func (c *AppConfig) PropagationEnabled() bool {
	return c.propagationEnabled.Load()
}

func (c *AppConfig) GetSkipMessagesFromSelf() bool {
	return c.skipMessageFromSelf.Load()
}

func (c *AppConfig) GetAggregationInterval() time.Duration {
	return time.Duration(c.aggregationIntervalMs.Load()) * time.Millisecond
}

// GetAttestationPublishGate is the fallback attestation release from slot start (default 4s).
// If a block is seen earlier, the aggregator releases 2s after it; 0 disables the gate.
func (c *AppConfig) GetAttestationPublishGate() time.Duration {
	return time.Duration(DefaultAttestationPublishAfterMs) * time.Millisecond
}

// GetAttestationPublishCap returns the slot-aware cap duration that closes the
// publish window. 0 means no cap (publish until slot end). See ADR-010.
func (c *AppConfig) GetAttestationPublishCap() time.Duration {
	return time.Duration(DefaultAttestationPublishCapMs) * time.Millisecond
}

// GetAttestationMaxSlotAge returns the maximum slot age (in slots) of
// attestations the router will forward. Currently a strict gate at 0 — only
// attestations matching the current slot pass.
func (c *AppConfig) GetAttestationMaxSlotAge() uint64 {
	return DefaultAttestationMaxSlotAge
}

// LogConfigState logs the current configuration state for debugging purposes
func (c *AppConfig) LogConfigState() {
	c.logger.Info("configuration state",
		logger.WithString("version", c.Version),
		logger.WithString("commit", c.CommitHash),
		logger.WithClusterID(c.GatewayClusterID),
		logger.WithInt("libp2p_port", c.AgentLibP2PPort),
		logger.WithInt("mump2p_port", c.AgentMumP2PPort),
		logger.WithInt("telemetry_port", c.TelemetryPort),
		logger.WithBool("telemetry_enable", c.TelemetryEnable),
		logger.WithBool("enable_pprof", c.EnablePProf),
		logger.WithString("pprof_addr", c.PProfAddr),
		logger.WithBool("propagation_enabled", c.PropagationEnabled()),
		logger.WithBool("skip_messages_from_self", c.GetSkipMessagesFromSelf()),
	)
	c.logMumP2PMeshConfig()
}

// logMumP2PMeshConfig reports the mesh and RLNC parameters, and in `source`
// where they came from, matching what /api/v1/self_info reports so the two
// cannot tell an operator different stories.
//
// The running node is the authority. It resolves these once, at construction,
// so the dynamic config's current view is only ever what was asked for. The
// distinction is not academic at startup: the rotator is seeded with the
// built-in defaults and fetches in the background, so this line ran before the
// first fetch landed and reported the defaults as though they were the node's.
func (c *AppConfig) logMumP2PMeshConfig() {
	if fn := c.effectiveRLNC.Load(); fn != nil {
		if params, ok := (*fn)(); ok {
			c.logger.Info("mump2p mesh config",
				logger.WithString("source", entities.RLNCParamsSourceNode),
				logger.WithInt("mesh_degree_target", params.MeshDegreeTarget),
				logger.WithInt("mesh_degree_min", params.MeshDegreeMin),
				logger.WithInt("mesh_degree_max", params.MeshDegreeMax),
				logger.WithInt("shard_factor", int(params.ShardFactor)),
				logger.WithInt("max_shard_size", int(params.MaxShardSize)),
				logger.WithFloat64("redundancy_fraction", params.RedundancyFraction),
				logger.WithFloat64("forward_shard_threshold", params.ForwardThreshold),
				logger.WithInt("forward_rank_threshold", params.ForwardRankThreshold),
				logger.WithInt64("aggregation_interval_ms", c.aggregationIntervalMs.Load()),
			)
			return
		}
	}

	if c.rotator == nil || c.rotator.Get() == nil {
		return
	}
	optCfg := c.rotator.Get()
	c.logger.Info("mump2p mesh config",
		logger.WithString("source", entities.RLNCParamsSourceDynamicConfig),
		logger.WithInt64("mesh_degree_target", optCfg.MeshDegreeTarget),
		logger.WithInt64("mesh_degree_min", optCfg.MeshDegreeMin),
		logger.WithInt64("mesh_degree_max", optCfg.MeshDegreeMax),
		logger.WithInt64("shard_factor", optCfg.ShardFactor),
		logger.WithInt64("aggregation_interval_ms", c.aggregationIntervalMs.Load()),
	)
}
