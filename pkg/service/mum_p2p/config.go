package mum_p2p

import (
	"fmt"
	"math"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

type Config struct {
	ClusterID      string `yaml:"cluster_id"`
	ListenPort     int    `yaml:"listen_port"`
	MaxMessageSize int64  `yaml:"max_message_size_bytes"`
	RLNCServer     string `yaml:"rlnc_server"`

	// RLNC and message settings
	RandomMessageSize        uint32  `yaml:"random_message_size_bytes"`
	ShardFactor              uint32  `yaml:"rlnc_shard_factor"`
	PublisherShardMultiplier float64 `yaml:"publisher_shard_multiplier"`
	ForwardShardThreshold    float64 `yaml:"forward_shard_threshold"`

	// Mesh topology settings
	MeshDegreeTarget int `yaml:"mesh_degree_target"`
	MeshDegreeMin    int `yaml:"mesh_degree_min"`
	MeshDegreeMax    int `yaml:"mesh_degree_max"`

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
	res := mump2pcfg.DefaultConfig()
	if cfg.Get().MeshDegreeTarget != 0 {
		res.MeshD = int(cfg.Get().MeshDegreeTarget)
		res.MeshDlo = int(cfg.Get().MeshDegreeTarget - 1)
		res.MeshDhi = int(cfg.Get().MeshDegreeTarget + 6)
	}
	if cfg.Get().MeshDegreeMin != 0 {
		res.MeshDlo = int(cfg.Get().MeshDegreeMin)
	}
	if cfg.Get().MeshDegreeMax != 0 {
		res.MeshDhi = int(cfg.Get().MeshDegreeMax)
	}

	res.HeartbeatMS = int((700 * time.Millisecond).Milliseconds()) // frequency of heartbeat, milliseconds
	res.HistoryLength = 6                                          // number of windows to retain full messages in cache for `IWANT` responses
	res.HistoryGossip = 3                                          // number of windows to gossip about
	return res
}
