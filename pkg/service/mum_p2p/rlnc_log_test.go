package mum_p2p

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/mump2p-protocol/pkg/config"
)

func TestIsBeaconBlockTopic(t *testing.T) {
	t.Parallel()

	require.True(t, isBeaconBlockTopic("/eth2/c6ecb76c/beacon_block/ssz_snappy"))
	require.True(t, isBeaconBlockTopic("beacon_block"))
	require.False(t, isBeaconBlockTopic("/eth2/c6ecb76c/beacon_attestation_0/ssz_snappy"))
	require.False(t, isBeaconBlockTopic("auto-topic"))
}

func TestDescribeRLNCGeom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		payloadLen int
		cfg        config.RLNCConfig
		want       rlncGeom
	}{
		{
			// ~14KB hoodi block, old k=8 / 3300 / redun=8: m=5 so policy K clamps to 5.
			name:       "14kib k8 shard3300 redun8",
			payloadLen: 14_000,
			cfg:        config.RLNCConfig{K: 8, MaxShardSize: 3300, RedundancyFraction: 8},
			want: rlncGeom{
				policyK: 5, sourceShards: 5, chunks: 1, codedPerChunk: 40, totalCoded: 40,
			},
		},
		{
			// 26KB straddles 1↔2 chunks at 3300/k=8.
			name:       "26kib k8 shard3300 redun8",
			payloadLen: 26_000,
			cfg:        config.RLNCConfig{K: 8, MaxShardSize: 3300, RedundancyFraction: 8},
			want: rlncGeom{
				policyK: 8, sourceShards: 8, chunks: 1, codedPerChunk: 64, totalCoded: 64,
			},
		},
		{
			// e29bea2-style k=64 / 1600 / redun=2: 14KB → m=9, policy K=9, n=18.
			name:       "14kib k64 shard1600 redun2",
			payloadLen: 14_000,
			cfg:        config.RLNCConfig{K: 64, MaxShardSize: 1600, RedundancyFraction: 2},
			want: rlncGeom{
				policyK: 9, sourceShards: 9, chunks: 1, codedPerChunk: 18, totalCoded: 18,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := describeRLNCGeom(tt.payloadLen, tt.cfg)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
