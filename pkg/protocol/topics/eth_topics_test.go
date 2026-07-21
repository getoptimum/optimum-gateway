package topics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
)

func TestGetForkDigestFromTopic(t *testing.T) {
	t.Run("valid topic", func(t *testing.T) {
		table := map[string]string{
			"/eth2/c6ecb76c/beacon_block/ssz_snappy": "c6ecb76c",
			"/eth2/12345678/beacon_block/ssz_snappy": "12345678",
			"/eth2/abcdef12/beacon_block/ssz_snappy": "abcdef12",
			"/eth2/8c9f62fe/beacon_block/ssz_snappy": "8c9f62fe",
		}

		for topic, expected := range table {
			require.Equal(t, expected, topics.GetForkDigestFromTopic(topic))
		}
	})
	t.Run("invalid topics", func(t *testing.T) {
		table := []string{
			"",
			"/",
			"/eth2/short",
			"/eth2/c6ecb76/beacon_block/ssz_snappy",
			"eth2/short",
			"/eth2//beacon_block/ssz_snappy",
			"eth2/8c9f62fe/beacon_block/ssz_snappy",
		}

		for _, topic := range table {
			require.Equal(t, "", topics.GetForkDigestFromTopic(topic))
		}
	})
}

// pkg: github.com/getoptimum/optimum-gateway/pkg/entities
// cpu: AMD Ryzen 5 7600X 6-Core Processor
// BenchmarkCheckForkSupportedFromTopicSplit
// BenchmarkCheckForkSupportedFromTopicSplit-12    	125421852	        9.649 ns/op
func BenchmarkCheckForkSupportedFromTopicSplit(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = topics.GetForkDigestFromTopic("/eth2/c6ecb76c/beacon_block/ssz_snappy")
	}
}

func TestIsFullEth2Topic(t *testing.T) {
	tests := map[string]struct {
		topic string
		want  bool
	}{
		"valid full topic": {
			topic: "/eth2/c6ecb76c/beacon_block/ssz_snappy",
			want:  true,
		},
		"valid indexed full topic": {
			topic: "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy",
			want:  true,
		},
		"valid full topic with spaces": {
			topic: "  /eth2/C6ECB76C/beacon_block/ssz_snappy  ",
			want:  true,
		},
		"bare descriptor": {
			topic: "beacon_block",
			want:  false,
		},
		"wrong suffix": {
			topic: "/eth2/c6ecb76c/beacon_block/ssz",
			want:  false,
		},
		"invalid digest length": {
			topic: "/eth2/c6ecb76/beacon_block/ssz_snappy",
			want:  false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, topics.IsFullEth2Topic(tc.topic))
		})
	}
}

func TestBuildFullTopic(t *testing.T) {
	tests := map[string]struct {
		forkDigest string
		descriptor string
		want       string
	}{
		"lowercases digest and trims descriptor": {
			forkDigest: "C6ECB76C",
			descriptor: " beacon_attestation_31 ",
			want:       "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy",
		},
		"preserves descriptor content": {
			forkDigest: "deadbeef",
			descriptor: "beacon_block",
			want:       "/eth2/deadbeef/beacon_block/ssz_snappy",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, topics.BuildFullTopic(tc.forkDigest, tc.descriptor))
		})
	}
}
