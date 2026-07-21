package topics

import (
	commonsyncx "github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

type TopicKind uint8

const (
	TopicUnknown TopicKind = iota
	TopicBeaconBlock
	TopicAttestation
)

func (s *TopicKind) String() string {
	switch *s {
	case TopicBeaconBlock:
		return "BeaconBlock"
	case TopicAttestation:
		return "Attestation"
	default:
		return "Unknown"
	}
}

var topicMetaCache = commonsyncx.NewRWMap[string, *TopicMeta]()

type TopicMeta struct {
	Kind              TopicKind
	AttestationSubnet uint32
}

func (m *TopicMeta) IsBeaconBlock() bool { return m != nil && m.Kind == TopicBeaconBlock }

func (m *TopicMeta) IsAttestation() bool { return m != nil && m.Kind == TopicAttestation }

func ParseTopicMeta(fullTopic string) *TopicMeta {
	meta := &TopicMeta{Kind: TopicUnknown}
	if utils.IsBeaconBlockTopic(fullTopic) {
		meta.Kind = TopicBeaconBlock
		return meta
	}
	if utils.IsAttestationTopic(fullTopic) {
		idx, err := utils.ExtractAttestationTopicIndex(fullTopic)
		if err != nil {
			return meta
		}
		meta.Kind = TopicAttestation
		meta.AttestationSubnet = idx
	}
	return meta
}

func TopicMetaFor(fullTopic string) *TopicMeta {
	if meta, ok := topicMetaCache.Load(fullTopic); ok {
		return meta
	}
	parsed := ParseTopicMeta(fullTopic)
	topicMetaCache.Store(fullTopic, parsed)
	return parsed
}
