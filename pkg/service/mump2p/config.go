package mump2p

import (
	"fmt"
	"math"

	"github.com/libp2p/go-libp2p/core/connmgr"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	rlncps "github.com/getoptimum/mump2p-protocol/pkg/pubsub"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

const (
	// heartbeatMS, historyLength and historyGossip are gossipsub mesh timings the
	// gateway pins rather than exposing; they are not part of the dynamic config.
	heartbeatMS   = 700
	historyLength = 6
	historyGossip = 3

	// meshDegreeHiOffset widens Dhi above the target degree when the dynamic config
	// serves a target but no explicit maximum.
	meshDegreeHiOffset = 6
)

type Config struct {
	ClusterID string `yaml:"cluster_id"`
	// GatewayID identifies this gateway within the cluster. It becomes the
	// protocol node ID and so the mump2p.node_id span attribute, which analysis
	// keys per-node results on, so it has to be unique across the fleet.
	GatewayID      string `yaml:"gateway_id"`
	ListenPort     int    `yaml:"listen_port"`
	MaxMessageSize int64  `yaml:"max_message_size_bytes"`

	// RLNC and message settings
	RandomMessageSize        int64   `yaml:"random_message_size_bytes"`
	ShardFactor              int     `yaml:"rlnc_shard_factor"`
	PublisherShardMultiplier float32 `yaml:"publisher_shard_multiplier"`
	ForwardShardThreshold    float32 `yaml:"forward_shard_threshold"`

	// Mesh topology settings
	MeshDegreeTarget int `yaml:"mesh_degree_target"`
	MeshDegreeMin    int `yaml:"mesh_degree_min"`
	MeshDegreeMax    int `yaml:"mesh_degree_max"`

	// Shared-memory settings for the out-of-process RLNC coder.
	SHMName  string `yaml:"shm_name"`
	SHMLanes int    `yaml:"shm_lanes"`

	BootstrapPeers []string `yaml:"bootstrap_peers"`

	// Datagram data plane. Off by default: enabling it binds a UDP socket and
	// moves mesh traffic onto keys negotiated per peer, so it is opt in.
	DatagramEnable     bool   `yaml:"datagram_enable"`
	DatagramListenAddr string `yaml:"datagram_listen_addr"`
	DatagramMaxPayload int    `yaml:"datagram_max_payload"`

	// OpenTelemetry span export for RLNC trace events. Off by default.
	OTelEnable      bool    `yaml:"otel_enable"`
	OTelEndpoint    string  `yaml:"otel_endpoint"`
	OTelInsecure    bool    `yaml:"otel_insecure"`
	OTelSampleRatio float64 `yaml:"otel_sample_ratio"`

	// Trace event categories to broadcast to RegisterListener consumers. Shard metrics
	// always run regardless of these; they only gate which raw trace events are fanned
	// out over the broadcaster. All default false (no raw trace fan-out).
	TraceMesh  bool `yaml:"trace_mesh"`  // mesh-topology events (peer/topic membership, graft/prune). Moderate freq.
	TraceRPC   bool `yaml:"trace_rpc"`   // RPC traffic events (recv/send/drop). HIGH FREQUENCY firehose.
	TraceShard bool `yaml:"trace_shard"` // shard/RLNC + message-lifecycle events. Message-dependent.

	// CustomConnectionGater allows custom control over peer connections. If nil, no custom gating is applied.
	CustomConnectionGater connmgr.ConnectionGater

	// Rotator is used to manage dynamic configuration updates
	Rotator *commonconfig.Rotator `yaml:"-"`
}

func (cfg *Config) Validate() error {
	if cfg.ListenPort <= 0 {
		return fmt.Errorf("listen port must be positive: %d", cfg.ListenPort)
	}
	if cfg.MaxMessageSize <= 0 || cfg.MaxMessageSize > math.MaxInt {
		return fmt.Errorf("random message size must be positive and less than max int: %d", cfg.MaxMessageSize)
	}
	return nil
}

func (cfg *Config) Get() *commonentities.OptimumConfig {
	return cfg.Rotator.Get()
}

// sharedMemory returns the coder shared-memory settings, falling back to the
// protocol defaults for anything the gateway config leaves unset.
func (cfg *Config) sharedMemory() mp2pconfig.SharedMemoryConfig {
	shm := mp2pconfig.DefaultSharedMemoryConfig()
	if cfg.SHMName != "" {
		shm.SHMName = cfg.SHMName
	}
	if cfg.SHMLanes > 0 {
		shm.SHMLanes = cfg.SHMLanes
	}
	return shm
}

// toNodeConfig maps the gateway config onto the mump2p node config. Mesh and RLNC
// values come from the dynamic config rotator so operator changes reach the node.
// A served combination the protocol rejects falls back to protocol defaults and is
// reported, since those values arrive at runtime and must not brick a restart.
//
// The datagram path's coding parameters are folded in last, after the served
// values, so the node reports and codes at the same numbers: the coder is built
// from the returned config before the pubsub is, and a coder sized for the
// stream path shards every message for a transport the node is not sending on.
func toNodeConfig(cfg *Config) (*mp2pconfig.Config, error) {
	res := baseNodeConfig(cfg)
	applyServedConfig(res, cfg.Get())
	if err := res.Validate(); err != nil {
		fallback := baseNodeConfig(cfg)
		rlncps.ResolveRLNCConfig(fallback)
		return fallback, fmt.Errorf("dynamic mump2p config rejected, using defaults: %w", err)
	}
	rlncps.ResolveRLNCConfig(res)
	return res, nil
}

// baseNodeConfig is the gateway's fixed part of the mump2p node config: everything
// the dynamic config does not serve.
func baseNodeConfig(cfg *Config) *mp2pconfig.Config {
	res := mp2pconfig.DefaultGossipSubConfig()
	// res.ID is the mump2p.node_id span attribute, so it must identify this
	// gateway alone. ClusterID is fleet-wide and only a last resort.
	switch {
	case cfg.GatewayID != "":
		res.ID = cfg.GatewayID
	case cfg.ClusterID != "":
		res.ID = cfg.ClusterID
	}
	res.ClusterID = cfg.ClusterID
	res.Port = cfg.ListenPort
	res.Transport = mp2pconfig.TransportQUIC
	res.HeartbeatMS = heartbeatMS
	res.HistoryLength = historyLength
	res.HistoryGossip = historyGossip
	res.SharedMemoryConfig = cfg.sharedMemory()
	res.Datagram = cfg.datagram()
	res.OTelConfig = cfg.otel()
	return res
}

// otel maps the gateway's tracing settings onto the protocol's.
func (cfg *Config) otel() mp2pconfig.OTelConfig {
	ot := mp2pconfig.DefaultOTelConfig()
	ot.Enable = cfg.OTelEnable
	ot.Endpoint = cfg.OTelEndpoint
	ot.Insecure = cfg.OTelInsecure
	if cfg.OTelSampleRatio > 0 {
		ot.SampleRatio = cfg.OTelSampleRatio
	}
	return ot
}

// datagram maps the gateway's datagram settings onto the protocol's. An unset
// section leaves the data plane disabled, which is the protocol default too.
func (cfg *Config) datagram() mp2pconfig.DatagramConfig {
	dg := mp2pconfig.DefaultDatagramConfig()
	dg.Enable = cfg.DatagramEnable
	if cfg.DatagramListenAddr != "" {
		dg.ListenAddr = cfg.DatagramListenAddr
	}
	if cfg.DatagramMaxPayload > 0 {
		dg.MaxPayload = cfg.DatagramMaxPayload
	}
	return dg
}

// applyServedConfig overlays the dynamic config. A zero means "not served" and
// leaves the protocol default in place.
func applyServedConfig(res *mp2pconfig.Config, served *commonentities.OptimumConfig) {
	if served.MeshDegreeTarget != 0 {
		res.MeshD = int(served.MeshDegreeTarget)
		res.MeshDlo = int(served.MeshDegreeTarget - 1)
		res.MeshDhi = int(served.MeshDegreeTarget + meshDegreeHiOffset)
	}
	if served.MeshDegreeMin != 0 {
		res.MeshDlo = int(served.MeshDegreeMin)
	}
	if served.MeshDegreeMax != 0 {
		res.MeshDhi = int(served.MeshDegreeMax)
	}

	// ShardFactor is the generation size (k) and the publisher multiplier is the
	// redundancy the publisher adds on top of it; both keep their v1 meaning.
	if served.ShardFactor > 1 {
		res.K = uint32(served.ShardFactor) //nolint:gosec // guarded above, and the served value is a small mesh knob
	}
	if served.PublisherShardMultiplier >= 1 {
		res.RedundancyFraction = float64(served.PublisherShardMultiplier)
	}
	if served.ForwardShardThreshold > 0 {
		res.ForwardingThresholdFraction = float64(served.ForwardShardThreshold)
	}
}
