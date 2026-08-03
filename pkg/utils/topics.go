package utils

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	commonslices "github.com/getoptimum/optimum-common/pkg/slices"
)

const (
	BeaconBlockBase       = "beacon_block"
	BeaconAttestationBase = "beacon_attestation"
	// singleAttestationSSZSize is the fixed SSZ size of an Electra SingleAttestation:
	// committee_index(8) + attester_index(8) + AttestationData(128) + signature(96).
	singleAttestationSSZSize = 240
)

func IsBeaconBlockTopic(topic string) bool {
	return strings.Contains(topic, BeaconBlockBase)
}

func IsAttestationTopic(topic string) bool {
	return strings.Contains(topic, BeaconAttestationBase+"_")
}

// UnpackTopics try to unpack topics
func UnpackTopics(topicsList []string) []string {
	result := make([]string, 0, len(topicsList))
	for _, topic := range topicsList {
		if IsBeaconBlockTopic(topic) || IsAttestationTopic(topic) {
			result = append(result, topic)
			continue
		}
		if strings.Contains(topic, BeaconAttestationBase) {
			for i := range 64 {
				result = append(result, fmt.Sprintf("%s_%d", BeaconAttestationBase, i))
			}
			continue
		}
		result = append(result, topic)
	}
	return commonslices.UniqueComparable(result)
}

func SimplifyTopic(topic string) string {
	if IsBeaconBlockTopic(topic) {
		return BeaconBlockBase
	}
	if IsAttestationTopic(topic) {
		return BeaconAttestationBase
	}
	return topic
}

// we just extract attester and slot from message. it much faster that decode full message
// structure is known and fixed, so we can just read bytes from specific offsets
// decompressed SSZ bytes:
//   - committee_id: bytes [0:8]
//   - attester_index: bytes [8:16]
//   - attestation_data.slot: bytes [16:24]
func ParseAttestationSSZTopic(buf []byte) (attesterIndex, slot uint64, err error) {
	msg, err := DecodeSnappy(buf, 300) // 300 bytes is more than enough for a SingleAttestation (240 SSZ)
	if err != nil {
		return 0, 0, fmt.Errorf("ssz decompressing failed: %w", err)
	}
	// check that we have a valid SingleAttestation SSZ size
	if len(msg) != singleAttestationSSZSize {
		return 0, 0, fmt.Errorf("unexpected attestation ssz size: got %d, want %d", len(msg), singleAttestationSSZSize)
	}
	attesterIndex = binary.LittleEndian.Uint64(msg[8:16])
	slot = binary.LittleEndian.Uint64(msg[16:24])
	return attesterIndex, slot, nil
}

// ExtractAttestationTopicIndex parse attestation topics for eth.
// expect strict format `/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy`
// expect that topic can be from 0 to 63
func ExtractAttestationTopicIndex(s string) (uint32, error) {
	end := strings.LastIndexByte(s, '/')
	if end == -1 {
		return 0, fmt.Errorf("invalid topic id format")
	}
	start := strings.LastIndexByte(s[:end], '_')
	if start == -1 {
		return 0, fmt.Errorf("invalid topic id format")
	}

	n, err := strconv.ParseUint(s[start+1:end], 10, 64)
	if err != nil {
		return 0, err
	}
	if n > 63 {
		return 0, fmt.Errorf("attestation topic index out of range: %d", n)
	}
	return uint32(n), nil
}
