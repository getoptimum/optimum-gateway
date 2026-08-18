package mum_p2p

import (
	"fmt"
	"math"

	"github.com/libp2p/go-libp2p/core/connmgr"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
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

var cfgMap = make(map[int]mump2pcfg.RLNCConfig, 4)

func (n *Node) initConfigMap() {
	dc := n.cfg.Get()

	cfgMap[512] = mump2pcfg.RLNCConfig{
		K:                           2,
		MaxShardSize:                shardSize(512, 2),
		RedundancyFraction:          1.5,
		ForwardingThresholdFraction: 0.5,
		MeshDegreeMax:               int(dc.MeshDegreeMax),
	}
	cfgMap[1024] = mump2pcfg.RLNCConfig{
		K:                           4,
		MaxShardSize:                shardSize(1024, 4),
		RedundancyFraction:          1.5,
		ForwardingThresholdFraction: 0.5,
		MeshDegreeMax:               int(dc.MeshDegreeMax),
	}
	cfgMap[2048] = mump2pcfg.RLNCConfig{
		K:                           8,
		MaxShardSize:                shardSize(2048, 8),
		RedundancyFraction:          1.5,
		ForwardingThresholdFraction: 0.5,
		MeshDegreeMax:               int(dc.MeshDegreeMax),
	}
	cfgMap[4096] = mump2pcfg.RLNCConfig{
		K:                           16,
		MaxShardSize:                shardSize(4096, 16),
		RedundancyFraction:          1.5,
		ForwardingThresholdFraction: 0.5,
		MeshDegreeMax:               int(dc.MeshDegreeMax),
	}
}

func (n *Node) GetConfig(msgSize int, _ string) mump2pcfg.RLNCConfig {
	switch {
	case msgSize <= 512:
		return cfgMap[512]
	case msgSize <= 1024:
		return cfgMap[1024]
	case msgSize <= 2048:
		return cfgMap[2048]
	default:
		return cfgMap[4096]
	}
	dc := n.cfg.Get()

	cfg := mump2pcfg.RLNCConfig{
		RedundancyFraction:          1.5,
		ForwardingThresholdFraction: 0.5,
		MeshDegreeMax:               int(dc.MeshDegreeMax),
	}
	switch {
	case msgSize <= 512:
		cfg.K = 2
	case msgSize <= 1024:
		cfg.K = 4
	case msgSize <= 2048:
		cfg.K = 8
	default:
		return mump2pcfg.RLNCConfig{
			K:                           dc.ShardFactor,
			MaxShardSize:                dc.RandomMessageSize,
			RedundancyFraction:          dc.PublisherShardMultiplier,
			ForwardingThresholdFraction: dc.ForwardShardThreshold,
			MeshDegreeMax:               int(dc.MeshDegreeMax),
		}
	}
	cfg.MaxShardSize = shardSize(msgSize, cfg.K)
	return cfg
}

const (
	rlncFramingOverhead = 16
	rlncMinShardSize    = 12
)

func shardSize(msgSize int, k uint32) uint32 {
	size := (msgSize + rlncFramingOverhead + int(k) - 1) / int(k)
	return uint32(max(size, rlncMinShardSize))
}

func (n *Node) logConfigs() {
	n.logRLNCConfig(512)
	n.logRLNCConfig(1024)
	n.logRLNCConfig(2048)
	n.logRLNCConfig(2049)
}

func (n *Node) logRLNCConfig(sizeThreshold int) {
	cfgLog := n.GetConfig(sizeThreshold, "")
	n.log.Info("RLNC config",
		logger.WithFlow("RLNC"),
		logger.WithInt("MessageSizeThreshold", sizeThreshold),
		logger.WithUint64("RLNC_K", uint64(cfgLog.K)),
		logger.WithUint64("MaxShardSize", uint64(cfgLog.MaxShardSize)),
		logger.WithFloat64("RedundancyFraction", cfgLog.RedundancyFraction),
		logger.WithFloat64("ForwardingThresholdFraction", cfgLog.ForwardingThresholdFraction),
		logger.WithInt("MeshDegreeMax", cfgLog.MeshDegreeMax),
	)
}
