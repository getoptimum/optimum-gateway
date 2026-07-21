package consensus

import (
	"encoding/binary"
	"fmt"

	generatedspecs "github.com/getoptimum/optimum-gateway/pkg/protocol/fastssz_codegen"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	// sszOffsetLen is the 4-byte SSZ offset prefix used when the first field is variable-length.
	// For SignedBeaconBlock, this offset points to the BeaconBlock message payload.
	gossipPrefixLen = 4
	// blsSignatureLen is the fixed BLS12-381 signature size on SignedBeaconBlock.
	blsSignatureLen = 96
	// signedBlockHeaderOffset is where BeaconBlock.slot starts after the SSZ offset + signature.
	signedBlockHeaderOffset = gossipPrefixLen + blsSignatureLen
	blockHeaderFixedLen     = 112 // BeaconBlockHeader SSZ fixed size
)

// DecodeBeaconBlockHeader reads slot, proposer_index, parent_root, state_root and the
// signature from a gossip SignedBeaconBlock. BodyRoot is left unset: at that position
// the payload holds a body offset, not a body_root, so the returned Header is not a
// valid header for hashing.
func DecodeBeaconBlockHeader(encodedBlock []byte) (*generatedspecs.SignedBeaconBlockHeader, error) {
	sszBytes, err := utils.DecodeSnappy(encodedBlock, utils.MaxGossipPayloadSize)
	if err != nil {
		return nil, fmt.Errorf("decode beacon block gossip: snappy: %w", err)
	}
	if len(sszBytes) < signedBlockHeaderOffset+blockHeaderFixedLen {
		return nil, fmt.Errorf("decode beacon block gossip: payload too short")
	}
	if off := binary.LittleEndian.Uint32(sszBytes[:gossipPrefixLen]); off != signedBlockHeaderOffset {
		return nil, fmt.Errorf("decode beacon block gossip: unexpected message offset %d", off)
	}
	block := new(generatedspecs.BeaconBlockHeader)
	if _, err := block.UnmarshalSSZTail(sszBytes[signedBlockHeaderOffset:]); err != nil {
		return nil, fmt.Errorf("decode beacon block gossip: ssz: %w", err)
	}
	block.BodyRoot = nil // body offset, not a real root
	return &generatedspecs.SignedBeaconBlockHeader{
		Header:    block,
		Signature: sszBytes[gossipPrefixLen:signedBlockHeaderOffset],
	}, nil
}
