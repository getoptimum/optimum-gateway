package mum_p2p

import (
	"strings"

	"github.com/getoptimum/mump2p-protocol/pkg/config"
	"github.com/getoptimum/mump2p-protocol/pkg/engine"
	"github.com/getoptimum/optimum-common/pkg/logger"
)

const (
	beaconBlockTopicKey = "beacon_block"
	// Keep in sync with engine.sharderMetadataSize (header + hash placeholder).
	rlncSharderMetadataSize = 16
)

// rlncGeom is the per-message geometry the engine will use to encode a payload
// under the resolved topic config. Decode logs reuse the same numbers so we can
// compare publisher vs receiver predictions when configs diverge.
type rlncGeom struct {
	policyK       uint32
	sourceShards  uint64
	chunks        uint64
	codedPerChunk int
	totalCoded    uint64
}

func isBeaconBlockTopic(topic string) bool {
	return strings.Contains(topic, beaconBlockTopicKey)
}

func describeRLNCGeom(payloadLen int, cfg config.RLNCConfig) (rlncGeom, error) {
	policy, err := engine.SelectPublishPolicy(payloadLen, cfg)
	if err != nil {
		return rlncGeom{}, err
	}
	base := uint64(payloadLen) + rlncSharderMetadataSize
	m := (base + uint64(cfg.MaxShardSize) - 1) / uint64(cfg.MaxShardSize)
	var chunks uint64
	if policy.K > 0 {
		chunks = (m + uint64(policy.K) - 1) / uint64(policy.K)
	}
	coded := cfg.RedundantSymbolCount(int(policy.K))
	return rlncGeom{
		policyK:       policy.K,
		sourceShards:  m,
		chunks:        chunks,
		codedPerChunk: coded,
		totalCoded:    chunks * uint64(coded),
	}, nil
}

func (n *Node) logRLNCTopicMap() {
	if n.rlncConfigs == nil {
		return
	}
	for key, cfg := range n.rlncConfigs {
		n.log.Info("rlnc topic map",
			logger.WithFlow("RLNC"),
			logger.WithString("config_key", key),
			logger.WithUint64("RLNC_K", uint64(cfg.K)),
			logger.WithUint64("MaxShardSize", uint64(cfg.MaxShardSize)),
			logger.WithFloat64("RedundancyFraction", cfg.RedundancyFraction),
			logger.WithInt("MeshDegreeMin", cfg.MeshDegreeMin),
			logger.WithInt("MeshDegreeTarget", cfg.MeshDegreeTarget),
			logger.WithInt("MeshDegreeMax", cfg.MeshDegreeMax),
		)
	}
}

// logRLNCMessage emits one Info line for beacon_block encode/decode. Other
// topics (attestations) are skipped so this stays grep-able in prod.
func (n *Node) logRLNCMessage(msg, topic, msgID string, payloadLen int, extra ...logger.Field) {
	if n.rlncConfigs == nil || !isBeaconBlockTopic(topic) {
		return
	}
	cfg := config.ResolveRLNCConfig(n.rlncConfigs, topic)
	fields := []logger.Field{
		logger.WithFlow("RLNC"),
		logger.WithTopic(topic),
		logger.WithString("msg_id", msgID),
		logger.WithInt("payload_bytes", payloadLen),
		logger.WithUint64("RLNC_K", uint64(cfg.K)),
		logger.WithUint64("MaxShardSize", uint64(cfg.MaxShardSize)),
		logger.WithFloat64("RedundancyFraction", cfg.RedundancyFraction),
	}
	geom, err := describeRLNCGeom(payloadLen, cfg)
	if err != nil {
		fields = append(fields, logger.WithError(err))
	} else {
		fields = append(fields,
			logger.WithUint64("policy_k", uint64(geom.policyK)),
			logger.WithUint64("source_shards", geom.sourceShards),
			logger.WithUint64("chunks", geom.chunks),
			logger.WithInt("coded_per_chunk", geom.codedPerChunk),
			logger.WithUint64("total_coded", geom.totalCoded),
		)
	}
	fields = append(fields, extra...)
	n.log.Info(msg, fields...)
}
