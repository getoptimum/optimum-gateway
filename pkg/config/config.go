package config

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getoptimum/optimum-common/pkg/chain"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/version"
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
	TraceMesh             bool     `yaml:"trace_mesh" env:"OPT_TRACE_MESH" default:"false"`
	TraceRPC              bool     `yaml:"trace_rpc" env:"OPT_TRACE_RPC" default:"false"`
	TraceShard            bool     `yaml:"trace_shard" env:"OPT_TRACE_SHARD" default:"false"`
	LogLevel              string   `yaml:"log_level" env:"OPT_LOG_LEVEL" default:"debug"`
	IdentityLibP2PDir     string   `yaml:"identity_libp2p_dir" env:"OPT_IDENTITY_LIBP2P_DIR" default:"/tmp/libp2p"`
	IdentityMumP2PDir     string   `yaml:"identity_mump2p_dir" env:"OPT_IDENTITY_MUMP2P_DIR" default:"/tmp/mump2p"`
	AgentLibP2PPort       int      `yaml:"agent_lib_p2p_port" env:"OPT_AGENT_LIB_P2P_PORT" default:"33212"`
	AgentMumP2PPort       int      `yaml:"agent_mump2p_port" env:"OPT_AGENT_MUMP2P_PORT" default:"33213"`
	DirectCLPeers         []string `yaml:"direct_cl_peers" env:"OPT_DIRECT_CL_PEERS"`
	TelemetryEnable       bool     `yaml:"telemetry_enable" env:"OPT_ENABLE_TELEMETRY" default:"false"`
	TelemetryPort         int      `yaml:"telemetry_port" env:"OPT_TELEMETRY_PORT" default:"48123"`
	TelemetryNamespace    string   `yaml:"telemetry_namespace" env:"OPT_TELEMETRY_NAMESPACE" default:"mump2p"`
	TelemetrySubsystem    string   `yaml:"telemetry_subsystem" env:"OPT_TELEMETRY_SUBSYSTEM" default:"gateway"`
	GatewayClusterID      string   `yaml:"gateway_cluster_id" env:"OPT_GATEWAY_CLUSTER_ID"`
	AggregationIntervalMs int64    `yaml:"aggregation_interval_ms" env:"OPT_AGGREGATION_INTERVAL_MS" default:"25"`
	PropagationEnabledRaw bool     `yaml:"propagation_enabled" env:"OPT_PROPAGATION_ENABLED" default:"true"`
	RemoteBootstrapURL    string   `yaml:"remote_bootstrap_url" env:"OPT_REMOTE_BOOTSTRAP_URL" default:"https://bootstrap.getoptimum.io"`
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

	// Consumer block-stream (ADR-0011): opt-in read-only fan-out of decoded
	// beacon blocks over WebSocket/gRPC, off by default.
	StreamEnable bool `yaml:"stream_enable" env:"OPT_STREAM_ENABLE" default:"false"`
	// StreamOnly skips the CL host and ingest: no CL connection, never publishes
	// into the mesh. Requires stream_enable.
	StreamOnly bool `yaml:"stream_only" env:"OPT_STREAM_ONLY" default:"false"`
	// Both listeners default to loopback, matching pprof_addr: exposing a
	// consumer feed on every interface should be a deliberate act, not what
	// happens when someone flips stream_enable. Binding beyond loopback puts
	// the consumer JWT and the gateway's timing claims on the wire, so it
	// requires TLS in front.
	StreamAddr           string `yaml:"stream_addr" env:"OPT_STREAM_ADDR" default:"127.0.0.1:9600"`
	StreamGRPCAddr       string `yaml:"stream_grpc_addr" env:"OPT_STREAM_GRPC_ADDR" default:"127.0.0.1:9601"`
	StreamRequireAuth    bool   `yaml:"stream_require_auth" env:"OPT_STREAM_REQUIRE_AUTH" default:"true"`
	StreamMaxConns       int    `yaml:"stream_max_conns" env:"OPT_STREAM_MAX_CONNS" default:"256"`
	StreamMaxConnsPerSub int    `yaml:"stream_max_conns_per_sub" env:"OPT_STREAM_MAX_CONNS_PER_SUB" default:"8"`
	StreamBufferSize     int    `yaml:"stream_buffer_size" env:"OPT_STREAM_BUFFER_SIZE" default:"64"`

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
	cfg.InitDerived()

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

// InitDerived seeds the internal state mirroring the yaml/env fields. LoadConfig
// calls it; anything building an AppConfig directly, such as a test, must too, or
// the getters below read zero values rather than the configured ones.
//
// Not safe on a config already in service: dynamic-config rotations own
// propagationEnabled and skipMessageFromSelf afterwards, and this resets them.
func (c *AppConfig) InitDerived() {
	c.propagationEnabled.Store(c.PropagationEnabledRaw)
	c.skipMessageFromSelf.Store(true)
	aggMs := c.AggregationIntervalMs
	if aggMs == 0 {
		aggMs = DefaultAggregationIntervalMs
	}
	c.aggregationIntervalMs.Store(aggMs)
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

	if c.StreamEnable {
		if err := validateStreamListener("stream_addr", c.StreamAddr, c.StreamRequireAuth); err != nil {
			return err
		}
		if err := validateStreamListener("stream_grpc_addr", c.StreamGRPCAddr, c.StreamRequireAuth); err != nil {
			return err
		}
		if strings.TrimSpace(c.StreamAddr) == strings.TrimSpace(c.StreamGRPCAddr) {
			return fmt.Errorf("stream_addr and stream_grpc_addr must differ, got %q", c.StreamAddr)
		}
		if c.StreamMaxConns <= 0 {
			return fmt.Errorf("stream_max_conns must be > 0")
		}
		if c.StreamMaxConnsPerSub <= 0 {
			return fmt.Errorf("stream_max_conns_per_sub must be > 0")
		}
		if c.StreamBufferSize <= 0 {
			return fmt.Errorf("stream_buffer_size must be > 0")
		}
	}
	if c.StreamOnly && !c.StreamEnable {
		return fmt.Errorf("stream_only requires stream_enable")
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

// validateStreamListener enforces the ADR-0011 exposure rule: auth may be
// disabled only on a loopback bind.
func validateStreamListener(field, addr string, requireAuth bool) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", field, addr, err)
	}
	if p, perr := strconv.Atoi(port); perr != nil || p <= 0 || p > 65535 {
		return fmt.Errorf("invalid %s %q: port must be between 1 and 65535", field, addr)
	}
	if !requireAuth && !isLoopbackHost(host) {
		return fmt.Errorf("%s=%q requires stream_require_auth=true (auth may be disabled only on a loopback bind)", field, addr)
	}
	return nil
}

// isLoopbackHost treats an empty host (binds all interfaces) as non-loopback.
func isLoopbackHost(host string) bool {
	switch host {
	case "":
		return false
	case "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	if c.rotator != nil && c.rotator.Get() != nil {
		optCfg := c.rotator.Get()
		c.logger.Info("mump2p mesh config",
			logger.WithInt64("mesh_degree_target", optCfg.MeshDegreeTarget),
			logger.WithInt64("mesh_degree_min", optCfg.MeshDegreeMin),
			logger.WithInt64("mesh_degree_max", optCfg.MeshDegreeMax),
			logger.WithInt64("shard_factor", optCfg.ShardFactor),
			logger.WithInt64("aggregation_interval_ms", c.aggregationIntervalMs.Load()),
		)
	}
}
