package aggregator_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	commonrand "github.com/getoptimum/optimum-common/pkg/rand"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/aggregator"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const (
	items          = 2000
	testForkDigest = "c6ecb76c"
)

func TestPackAttestations(t *testing.T) {
	// given
	packer := aggregator.NewAttestationPacker(nil)
	dataList := []*consensus.AttestationData{
		generateAttestationData(t),
		generateAttestationData(t),
		generateAttestationData(t),
	}

	rawAttestations := make([]*consensus.SingleAttestation, 0, items)
	rawAttestationsMap := make(map[string]struct{}, items)
	for i := range items {
		data := dataList[i%len(dataList)]
		attestation := generateAttestation(t, data)

		topic := generateAttestationTopic(t)
		require.NoError(t, packer.Add(topics.ParseTopicMeta(topic), test_utils.SSZSnappyEncode(t, attestation)))

		rawAttestations = append(rawAttestations, attestation)
		encodedAttestation := test_utils.SSZSnappyEncode(t, attestation)
		rawAttestationsMap[commonhash.SHA256(encodedAttestation)] = struct{}{}
	}

	// when pack in ordinary and specified way
	batch := make([][]byte, 0, items)
	for _, attestation := range rawAttestations {
		batch = append(batch, test_utils.SSZSnappyEncode(t, attestation))
	}
	ordinaryAggregator := &aggregator.Msg{
		Tms: time.Now().UnixMilli(),
		Container: []*aggregator.Container{
			{
				Topic: "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy",
				Data:  batch,
			},
		},
	}
	result, err := proto.Marshal(ordinaryAggregator)
	require.NoError(t, err)
	resultPacked, err := packer.EncodeCurrent()
	require.NoError(t, err)
	require.Len(t, resultPacked, len(dataList), "packed should equal to number of unique slot attestations")

	lenNative := len(result)
	lenPacked := len(resultPacked)
	t.Logf("raw attestations: %d", lenNative)
	t.Logf("packing attestations: %d", lenPacked)
	benefit := (float64(lenNative-lenPacked) / float64(lenNative)) * 100
	t.Logf("benefit: %.2f", benefit)

	// then decode and verify that all match
	t.Run("ordinary packed attestations should be restored", func(t *testing.T) {
		var decodedOrdinaryAggregator aggregator.Msg
		require.NoError(t, proto.Unmarshal(result, &decodedOrdinaryAggregator))

		require.Len(t, decodedOrdinaryAggregator.Container, 1)
		for _, cnt := range decodedOrdinaryAggregator.Container[0].Data {
			_, ok := rawAttestationsMap[commonhash.SHA256(cnt)]
			require.True(t, ok)
		}
	})
	t.Run("packed attestations should be restored", func(t *testing.T) {
		for i := range resultPacked {
			require.True(t, bytes.HasPrefix(resultPacked[i], aggregator.PrefixPacker))
			decodedPacked, errD := packer.Decode(testForkDigest, resultPacked[i][len(aggregator.PrefixPacker):])
			require.NoError(t, errD)

			for topic := range decodedPacked {
				for _, sszMessage := range decodedPacked[topic] {
					_, ok := rawAttestationsMap[commonhash.SHA256(sszMessage)]
					require.True(t, ok)
				}
			}
		}
	})
}

func TestPackAttestations_SendTsMs(t *testing.T) {
	packer := aggregator.NewAttestationPacker(nil)
	data := generateAttestationData(t)

	for range 10 {
		topic := generateAttestationTopic(t)
		att := generateAttestation(t, data)
		require.NoError(t, packer.Add(topics.ParseTopicMeta(topic), test_utils.SSZSnappyEncode(t, att)))
	}

	beforeEncode := time.Now().UnixMilli()
	encoded, err := packer.EncodeCurrent()
	afterEncode := time.Now().UnixMilli()
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	for _, packed := range encoded {
		require.True(t, bytes.HasPrefix(packed, aggregator.PrefixPacker))
		var ap aggregator.AttestationPack
		require.NoError(t, proto.Unmarshal(packed[len(aggregator.PrefixPacker):], &ap))

		require.GreaterOrEqual(t, ap.SendTsMs, beforeEncode, "SendTsMs should be >= timestamp before encode")
		require.LessOrEqual(t, ap.SendTsMs, afterEncode, "SendTsMs should be <= timestamp after encode")
	}
}

func randRoot(t *testing.T) [32]byte {
	t.Helper()

	var r [32]byte
	copy(r[:], test_utils.RandBytes(32))
	return r
}

func generateAttestationData(t *testing.T) *consensus.AttestationData {
	t.Helper()

	return &consensus.AttestationData{
		Slot:            consensus.Slot(test_utils.TestRand(t)),
		CommitteeIndex:  consensus.CommitteeIndex(test_utils.TestRand(t)),
		BeaconBlockRoot: randRoot(t),
		Source: consensus.Checkpoint{
			Epoch: consensus.Epoch(test_utils.TestRand(t)),
			Root:  randRoot(t),
		},
		Target: consensus.Checkpoint{
			Epoch: consensus.Epoch(test_utils.TestRand(t)),
			Root:  randRoot(t),
		},
	}
}

func generateAttestation(t *testing.T, data *consensus.AttestationData) *consensus.SingleAttestation {
	t.Helper()

	var sig [96]byte
	copy(sig[:], test_utils.RandBytes(96))
	return &consensus.SingleAttestation{
		CommitteeIndex: consensus.CommitteeIndex(test_utils.TestRand(t)),
		AttesterIndex:  consensus.ValidatorIndex(test_utils.TestRand(t)),
		Data:           *data,
		Signature:      sig,
	}
}

func generateAttestationTopic(t *testing.T) string {
	t.Helper()

	r, err := commonrand.RandBetween(0, 64)
	require.NoError(t, err)
	return topics.BuildFullTopic(testForkDigest, fmt.Sprintf("beacon_attestation_%d", r))
}
