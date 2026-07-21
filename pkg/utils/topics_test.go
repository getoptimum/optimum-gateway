package utils_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"

	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestIsBeaconBlockTopic(t *testing.T) {
	table := map[string]bool{
		"beacon_block":                           true,
		"/eth2/c6ecb76c/beacon_block/ssz_snappy": true,
		"beacon_attestation_5":                   false,
		"beacon_aggregate_and_proof":             false,
		"":                                       false,
	}
	for topic, expect := range table {
		require.Equal(t, expect, utils.IsBeaconBlockTopic(topic))
	}
}

func TestIsAttestationTopic(t *testing.T) {
	table := map[string]bool{
		"beacon_attestation_0":                            true,
		"beacon_attestation_63":                           true,
		"/eth2/abcdfgef/beacon_attestation_51/ssz_snappy": true,
		utils.BeaconAttestationBase:                       false,
		"beacon_block":                                    false,
		"":                                                false,
	}
	for topic, expect := range table {
		require.Equal(t, expect, utils.IsAttestationTopic(topic))
	}
}

func TestSimplifyTopic(t *testing.T) {
	table := map[string]string{
		"/eth2/deadbeef/beacon_block/ssz_snappy":         utils.BeaconBlockBase,
		"/eth2/12345678/beacon_attestation_5/ssz_snappy": utils.BeaconAttestationBase,
		"beacon_block":               utils.BeaconBlockBase,
		"beacon_attestation_31":      utils.BeaconAttestationBase,
		"beacon_aggregate_and_proof": "beacon_aggregate_and_proof",
		"beacon_attestation":         "beacon_attestation",
	}
	for topic, result := range table {
		require.Equal(t, result, utils.SimplifyTopic(topic))
	}
}

func TestIsSameTopic(t *testing.T) {
	tests := map[string]struct {
		src  string
		cfg  string
		want bool
	}{
		"bare equal": {
			src:  "beacon_attestation_29",
			cfg:  "beacon_attestation_29",
			want: true,
		},
		"bare different": {
			src:  "beacon_attestation_11",
			cfg:  "beacon_aggregate_and_proof",
			want: false,
		},
		"full and bare equal": {
			src:  "/eth2/c6ecb76c/beacon_attestation_22/ssz_snappy",
			cfg:  "beacon_attestation_22",
			want: true,
		},
		"full and bare different": {
			src:  "/eth2/c6ecb76c/beacon_attestation_39/ssz_snappy",
			cfg:  "beacon_aggregate_and_proof",
			want: false,
		},
		"full same topic different fork digest": {
			src:  "/eth2/c6ecb76c/beacon_attestation_36/ssz_snappy",
			cfg:  "/eth2/11111111/beacon_attestation_36/ssz_snappy",
			want: true,
		},
		"full different topic names": {
			src:  "/eth2/c6ecb76c/beacon_attestation_34/ssz_snappy",
			cfg:  "/eth2/c6ecb76c/beacon_aggregate_and_proof/ssz_snappy",
			want: false,
		},
		"full same topic different encodings": {
			src:  "/eth2/c6ecb76c/beacon_attestation_32/ssz_snappy",
			cfg:  "/eth2/11111111/beacon_attestation_32/ssz",
			want: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, utils.IsSameTopic(tc.src, tc.cfg))
		})
	}
}

func TestParseAttestationSSZTopic(t *testing.T) {
	tests := map[string]struct {
		src                         string
		expectValidator, expectSlot uint64
	}{
		"valid payload1": {
			src:             "f001041a000901089f360f050908c6662a05081101889fb61534b6ad0d499d825e964f4b882ce12c82bafcb43353f21e6b890eea96f9355301052b80db0b2252d3591fd8189bd3ff18d8629508d598db4a764b019d6a9c5140ff38a4360d28f07fe50ea4ced0f03591599cd1db526a7dcece828c0bc37561ece8587edaed2c247fad9c466a4b1181d7948cadcb716c0b282c3adb06622ee185a8007548c3c62fd64acbf62aadcdf9257bf5e7a07307abb3156c2c9953d0ad1ae2b97a89283f8872ceed815427ad264f4ea8ea05b901559327cdc760bf9d3e4a83f2cdc6826d9c4e",
			expectValidator: 997023,
			expectSlot:      2778822,
		},
		"valid payload2": {
			src:             "f001043900090108142c1305090828672a0508110188a50e909e4f2f3180e8a78ba7ea60af387a87ebc52d07158192d7ba6a17220868385301052b807b2afe8ba3d6883be57de5b23d5ac8482e15d87f1d74c24ef4b5a8180b69e555390d28f07fb153c49dd8db8256cb4bdb40753c00e18417035907ab21eda28fa158bae2ef7da737251c3ae8061d92151afb9f15ac5aba4044c96b8afe844ceb6cbc564daf17a51c81249610bf49209dfb26e6ea74f5045dc1a7f5c2fc15d2767985c206132e22f09b1f993c8d35d12cb520a77982e9115d67ff5ba8f40110763a18c0c10c61",
			expectValidator: 1256468,
			expectSlot:      2778920,
		},
		"valid payload3": {
			src:             test_utils.ValidBeaconAttestation31,
			expectValidator: 997023,
			expectSlot:      2778822,
		},
	}
	sszEncoder := &consensus.SSZSnappyCodec{}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.src)
			require.NoError(t, err)
			validator, slot, err := utils.ParseAttestationSSZTopic(data)
			require.NoError(t, err)
			require.Equal(t, tc.expectValidator, validator)
			require.Equal(t, tc.expectSlot, slot)

			var a consensus.SingleAttestation
			require.NoError(t, sszEncoder.DecodeGossip(data, &a))
			require.Equal(t, tc.expectValidator, uint64(a.AttesterIndex))
			require.Equal(t, tc.expectSlot, uint64(a.Data.Slot))

			var res bytes.Buffer
			_, err = sszEncoder.EncodeGossip(&res, &a)
			require.NoError(t, err)
			require.Equal(t, tc.src, hex.EncodeToString(res.Bytes()))
		})
	}

	validator, slot, err := utils.ParseAttestationSSZTopic([]byte{0x1, 0x2, 0x3})
	require.Error(t, err)
	require.Equal(t, uint64(0), validator)
	require.Equal(t, uint64(0), slot)

	compressed := snappy.Encode(nil, []byte("foo"))
	validator, slot, err = utils.ParseAttestationSSZTopic(compressed)
	require.Error(t, err)
	require.Equal(t, uint64(0), validator)
	require.Equal(t, uint64(0), slot)
}

func TestUnpackTopics(t *testing.T) {
	tests := map[string]struct {
		input []string
		want  []string
	}{
		"expand attestation topic": {
			input: []string{"beacon_attestation", "beacon_topic"},
			want: append(func() []string {
				topics := make([]string, 0, 65)
				for i := range 64 {
					topics = append(topics, "beacon_attestation_"+strconv.Itoa(i))
				}
				return topics
			}(), "beacon_topic"),
		},
		"deduplicate expanded attestation topic": {
			input: []string{"beacon_attestation_0", "beacon_attestation", "beacon_topic"},
			want: append(func() []string {
				topics := []string{"beacon_attestation_0"}
				for i := 1; i < 64; i++ {
					topics = append(topics, "beacon_attestation_"+strconv.Itoa(i))
				}
				return topics
			}(), "beacon_topic"),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, utils.UnpackTopics(tc.input))
		})
	}
}

func BenchmarkParseAttestationSSZTopic(b *testing.B) {
	src := "f001043900090108142c1305090828672a0508110188a50e909e4f2f3180e8a78ba7ea60af387a87ebc52d07158192d7ba6a17220868385301052b807b2afe8ba3d6883be57de5b23d5ac8482e15d87f1d74c24ef4b5a8180b69e555390d28f07fb153c49dd8db8256cb4bdb40753c00e18417035907ab21eda28fa158bae2ef7da737251c3ae8061d92151afb9f15ac5aba4044c96b8afe844ceb6cbc564daf17a51c81249610bf49209dfb26e6ea74f5045dc1a7f5c2fc15d2767985c206132e22f09b1f993c8d35d12cb520a77982e9115d67ff5ba8f40110763a18c0c10c61"
	data, err := hex.DecodeString(src)
	require.NoError(b, err)
	for n := 0; n < b.N; n++ {
		_, _, _ = utils.ParseAttestationSSZTopic(data)
	}
}

func BenchmarkParseAttestationSSZTopicUnmarshal(b *testing.B) {
	src := "f001043900090108142c1305090828672a0508110188a50e909e4f2f3180e8a78ba7ea60af387a87ebc52d07158192d7ba6a17220868385301052b807b2afe8ba3d6883be57de5b23d5ac8482e15d87f1d74c24ef4b5a8180b69e555390d28f07fb153c49dd8db8256cb4bdb40753c00e18417035907ab21eda28fa158bae2ef7da737251c3ae8061d92151afb9f15ac5aba4044c96b8afe844ceb6cbc564daf17a51c81249610bf49209dfb26e6ea74f5045dc1a7f5c2fc15d2767985c206132e22f09b1f993c8d35d12cb520a77982e9115d67ff5ba8f40110763a18c0c10c61"
	data, err := hex.DecodeString(src)
	require.NoError(b, err)
	var a consensus.SingleAttestation
	sszEncoder := &consensus.SSZSnappyCodec{}
	for n := 0; n < b.N; n++ {
		_ = sszEncoder.DecodeGossip(data, &a)
	}
}

func TestExtractTopicID(t *testing.T) {
	for i := range 64 {
		topic := fmt.Sprintf("/eth2/c6ecb76c/beacon_attestation_%d/ssz_snappy", i)
		res, err := utils.ExtractAttestationTopicIndex(topic)
		require.NoError(t, err)
		require.Equal(t, uint32(i), res)
	}

	t.Run("empty topic index", func(t *testing.T) {
		invalid := []string{
			"/eth2/ssz_snappy",
			"ssz_snappy",
			"/eth2/c6ecb76c/beacon_attestation_64/ssz_snappy",
			"/eth2/c6ecb76c/beacon_attestation_abc/ssz_snappy",
		}
		for _, topic := range invalid {
			res, err := utils.ExtractAttestationTopicIndex(topic)
			require.Error(t, err)
			require.Equal(t, uint32(0), res)
		}
	})
}
