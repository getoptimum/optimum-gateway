package mum_p2p

import (
	"fmt"
	"math"

	"github.com/libp2p/go-libp2p/core/connmgr"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

type Config struct {
	ClusterID      string `yaml:"cluster_id"`
	ListenPort     int    `yaml:"listen_port"`
	MaxMessageSize int64  `yaml:"max_message_size_bytes"`

	BootstrapPeers []string `yaml:"bootstrap_peers"`

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

func toMumP2PConfig(cfg *Config) *mump2pcfg.Config {
	res := mump2pcfg.DefaultGossipSubConfig()
	dc := cfg.Get()
	if dc.MeshDegreeTarget != 0 {
		res.MeshD = int(dc.MeshDegreeTarget)
		res.MeshDlo = int(dc.MeshDegreeTarget - 1)
		res.MeshDhi = int(dc.MeshDegreeTarget + 6)
	}
	if dc.MeshDegreeMin != 0 {
		res.MeshDlo = int(dc.MeshDegreeMin)
	}
	if dc.MeshDegreeMax != 0 {
		res.MeshDhi = int(dc.MeshDegreeMax)
	}
	res.RLNC = mump2pcfg.RLNCConfig{
		K:                           dc.ShardFactor,
		MaxShardSize:                dc.RandomMessageSize,
		RedundancyFraction:          dc.PublisherShardMultiplier,
		ForwardingThresholdFraction: dc.ForwardShardThreshold,
		MeshDegreeMax:               res.MeshDhi,
	}
	return res
}
