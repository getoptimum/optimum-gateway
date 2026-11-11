package consensus

import (
	"encoding/binary"
	"fmt"
)

type (
	Slot           uint64
	Epoch          uint64
	ValidatorIndex uint64
	CommitteeIndex uint64
)

// SSZUint64 is a uint64 with SSZ (little-endian, fixed 8-byte) encoding.
type SSZUint64 uint64

func (s SSZUint64) MarshalSSZ() ([]byte, error) {
	return binary.LittleEndian.AppendUint64(nil, uint64(s)), nil
}

func (s *SSZUint64) UnmarshalSSZ(buf []byte) error {
	if len(buf) != 8 {
		return fmt.Errorf("consensus: SSZUint64 expects 8 bytes, got %d", len(buf))
	}
	*s = SSZUint64(binary.LittleEndian.Uint64(buf))
	return nil
}
