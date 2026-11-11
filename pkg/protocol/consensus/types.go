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

// ErrorMessage is the req/resp error payload, SSZ List[byte, 256].
type ErrorMessage string

const maxErrorMessageLen = 256

func (m ErrorMessage) MarshalSSZ() ([]byte, error) {
	b := []byte(m)
	if len(b) > maxErrorMessageLen {
		b = b[:maxErrorMessageLen]
	}
	return b, nil
}

func (m *ErrorMessage) UnmarshalSSZ(buf []byte) error {
	if len(buf) > maxErrorMessageLen {
		return fmt.Errorf("consensus: ErrorMessage exceeds %d bytes", maxErrorMessageLen)
	}
	*m = ErrorMessage(buf)
	return nil
}

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
