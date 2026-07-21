package topics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
)

func TestParseTopicMeta_AttestationSubnet(t *testing.T) {
	meta := topics.ParseTopicMeta("/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy")
	require.Equal(t, topics.TopicAttestation, meta.Kind)
	require.True(t, meta.IsAttestation())
	require.False(t, meta.IsBeaconBlock())
	require.Equal(t, uint32(31), meta.AttestationSubnet)

	meta = topics.ParseTopicMeta("/eth2/c6ecb76c/beacon_attestation_notanint/ssz_snappy")
	require.False(t, meta.IsAttestation())
	require.False(t, meta.IsBeaconBlock())
	require.Equal(t, topics.TopicUnknown, meta.Kind)

	meta = topics.ParseTopicMeta("/eth2/c6ecb76c/beacon_block/ssz_snappy")
	require.False(t, meta.IsAttestation())
	require.True(t, meta.IsBeaconBlock())
	require.Equal(t, topics.TopicBeaconBlock, meta.Kind)

	meta = topics.ParseTopicMeta("completely-unknown-topic")
	require.False(t, meta.IsAttestation())
	require.False(t, meta.IsBeaconBlock())
	require.Equal(t, topics.TopicUnknown, meta.Kind)
}

func TestTopicMetaFor_CachesParsedMeta(t *testing.T) {
	topic := "/eth2/c6ecb76c/beacon_attestation_17/ssz_snappy"

	first := topics.TopicMetaFor(topic)
	second := topics.TopicMetaFor(topic)

	require.Same(t, first, second)
	require.Equal(t, topics.TopicAttestation, first.Kind)
	require.Equal(t, uint32(17), first.AttestationSubnet)
}
