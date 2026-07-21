package aggregator

import (
	"bytes"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// ValidatorChecker returns true if the validator index is in the known set.
type ValidatorChecker func(attesterIndex uint64) bool

// attestationsPerDataIDHint is the initial capacity hint for per-data-ID
// attestation slices and per-topic decode buckets.
const attestationsPerDataIDHint = 10_000

// AttestationPacker handle all attestation messages and pack them into final bytes that will be sent
type AttestationPacker struct {
	sszEncoder          *consensus.SSZSnappyCodec
	encodedData         map[uint64][]byte
	encodedAttestations map[uint64][]*AttestationItem
	isKnownValidator    ValidatorChecker
}

func NewAttestationPacker(checker ValidatorChecker) *AttestationPacker {
	return &AttestationPacker{
		sszEncoder:          &consensus.SSZSnappyCodec{},
		encodedData:         make(map[uint64][]byte),
		encodedAttestations: make(map[uint64][]*AttestationItem, attestationsPerDataIDHint),
		isKnownValidator:    checker,
	}
}

// Add decode attestation and group payload by attestationData
func (a *AttestationPacker) Add(meta *topics.TopicMeta, message []byte) error {
	if meta == nil || !meta.IsAttestation() {
		return fmt.Errorf("not an attestation topic")
	}
	telemetry.IncAttestationSubnet(meta.AttestationSubnet)
	var attestation consensus.SingleAttestation
	if err := a.sszEncoder.DecodeGossip(message, &attestation); err != nil {
		return fmt.Errorf("decoding message: %w", err)
	}
	var encodedData bytes.Buffer
	if _, err := a.sszEncoder.EncodeGossip(&encodedData, &attestation.Data); err != nil {
		return fmt.Errorf("encoding message: %w", err)
	}
	dataID := commonhash.XXHash(encodedData.Bytes())
	if _, ok := a.encodedData[dataID]; !ok {
		a.encodedData[dataID] = encodedData.Bytes()
	}
	if _, ok := a.encodedAttestations[dataID]; !ok {
		a.encodedAttestations[dataID] = make([]*AttestationItem, 0, attestationsPerDataIDHint)
	}

	a.encodedAttestations[dataID] = append(a.encodedAttestations[dataID], &AttestationItem{
		CommitteeId:   uint64(attestation.CommitteeIndex),
		AttesterIndex: uint64(attestation.AttesterIndex),
		TopicIndex:    meta.AttestationSubnet,
		Signature:     attestation.Signature[:],
	})
	return nil
}

func (a *AttestationPacker) EncodeCurrent() ([][]byte, error) {
	result := make([][]byte, 0, len(a.encodedData))
	nowMs := time.Now().UnixMilli()
	for dataID := range a.encodedData {
		data, err := proto.Marshal(&AttestationPack{
			AttestationSszData: a.encodedData[dataID],
			Items:              a.encodedAttestations[dataID],
			SendTsMs:           nowMs,
		})
		if err != nil {
			return nil, fmt.Errorf("encoding message: %w", err)
		}
		result = append(result, prefixed(PrefixPacker, data))
	}
	return result, nil
}

func (a *AttestationPacker) Decode(currentForkDigest string, payload []byte) (map[string][][]byte, error) {
	var decodedPackedAggregator AttestationPack
	if err := proto.Unmarshal(payload, &decodedPackedAggregator); err != nil {
		return nil, fmt.Errorf("decoding message: %w", err)
	}
	if decodedPackedAggregator.SendTsMs > 0 {
		telemetry.ObserveAttestationPropagationLatency(float64(time.Now().UnixMilli() - decodedPackedAggregator.SendTsMs))
	}
	var attestationData consensus.AttestationData
	if err := a.sszEncoder.DecodeGossip(decodedPackedAggregator.AttestationSszData, &attestationData); err != nil {
		return nil, fmt.Errorf("decoding message: %w", err)
	}
	result := make(map[string][][]byte, 64)
	for i := range decodedPackedAggregator.Items {
		if len(decodedPackedAggregator.Items[i].Signature) != 96 {
			return nil, fmt.Errorf("decoding message: invalid signature length %d", len(decodedPackedAggregator.Items[i].Signature))
		}
		var sig [96]byte
		copy(sig[:], decodedPackedAggregator.Items[i].Signature)
		attestation := &consensus.SingleAttestation{
			CommitteeIndex: consensus.CommitteeIndex(decodedPackedAggregator.Items[i].CommitteeId),
			AttesterIndex:  consensus.ValidatorIndex(decodedPackedAggregator.Items[i].AttesterIndex),
			Data:           attestationData,
			Signature:      sig,
		}
		var res bytes.Buffer
		if _, err := a.sszEncoder.EncodeGossip(&res, attestation); err != nil {
			return nil, fmt.Errorf("encoding message: %w", err)
		}
		topic := topics.BuildFullTopic(currentForkDigest, fmt.Sprintf("beacon_attestation_%d", decodedPackedAggregator.Items[i].TopicIndex))
		if _, ok := result[topic]; !ok {
			result[topic] = make([][]byte, 0, attestationsPerDataIDHint)
		}
		result[topic] = append(result[topic], res.Bytes())
	}
	return result, nil
}

func (a *AttestationPacker) TotalItems() int {
	n := 0
	for _, items := range a.encodedAttestations {
		n += len(items)
	}
	return n
}

func (a *AttestationPacker) UniqueDataKeys() int {
	return len(a.encodedData)
}

func (a *AttestationPacker) Clean() {
	clear(a.encodedData)
	clear(a.encodedAttestations)
}
