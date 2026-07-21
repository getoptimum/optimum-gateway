package consensus

import (
	"encoding/binary"
	"fmt"
)

const (
	forkDigestSize = 4
	attnetsSize    = 8
	syncnetsSize   = 1
	statusSize     = 84
	statusV2Size   = 92
	metaDataV0Size = 16
	metaDataV1Size = 17
	metaDataV2Size = 25
)

// StatusMessage is an eth2 status req/resp payload.
type StatusMessage interface {
	Unmarshaler
	Marshaler
	GetForkDigest() [forkDigestSize]byte
}

// Status is the eth2 req/resp status handshake message (fixed 84 bytes).
type Status struct {
	ForkDigest     [forkDigestSize]byte
	FinalizedRoot  [32]byte
	FinalizedEpoch Epoch
	HeadRoot       [32]byte
	HeadSlot       Slot
}

func (s *Status) MarshalSSZ() ([]byte, error) {
	b := make([]byte, 0, statusSize)
	b = append(b, s.ForkDigest[:]...)
	b = append(b, s.FinalizedRoot[:]...)
	b = binary.LittleEndian.AppendUint64(b, uint64(s.FinalizedEpoch))
	b = append(b, s.HeadRoot[:]...)
	b = binary.LittleEndian.AppendUint64(b, uint64(s.HeadSlot))
	return b, nil
}

func (s *Status) UnmarshalSSZ(b []byte) error {
	if len(b) != statusSize {
		return fmt.Errorf("consensus: Status expects %d bytes, got %d", statusSize, len(b))
	}
	copy(s.ForkDigest[:], b[0:4])
	copy(s.FinalizedRoot[:], b[4:36])
	s.FinalizedEpoch = Epoch(binary.LittleEndian.Uint64(b[36:44]))
	copy(s.HeadRoot[:], b[44:76])
	s.HeadSlot = Slot(binary.LittleEndian.Uint64(b[76:84]))
	return nil
}

func (s *Status) GetForkDigest() [forkDigestSize]byte { return s.ForkDigest }

// StatusV2 is the Fulu status handshake message (fixed 92 bytes).
type StatusV2 struct {
	ForkDigest            [forkDigestSize]byte
	FinalizedRoot         [32]byte
	FinalizedEpoch        Epoch
	HeadRoot              [32]byte
	HeadSlot              Slot
	EarliestAvailableSlot Slot
}

func (s *StatusV2) MarshalSSZ() ([]byte, error) {
	b := make([]byte, 0, statusV2Size)
	b = append(b, s.ForkDigest[:]...)
	b = append(b, s.FinalizedRoot[:]...)
	b = binary.LittleEndian.AppendUint64(b, uint64(s.FinalizedEpoch))
	b = append(b, s.HeadRoot[:]...)
	b = binary.LittleEndian.AppendUint64(b, uint64(s.HeadSlot))
	b = binary.LittleEndian.AppendUint64(b, uint64(s.EarliestAvailableSlot))
	return b, nil
}

func (s *StatusV2) UnmarshalSSZ(b []byte) error {
	if len(b) != statusV2Size {
		return fmt.Errorf("consensus: StatusV2 expects %d bytes, got %d", statusV2Size, len(b))
	}
	copy(s.ForkDigest[:], b[0:4])
	copy(s.FinalizedRoot[:], b[4:36])
	s.FinalizedEpoch = Epoch(binary.LittleEndian.Uint64(b[36:44]))
	copy(s.HeadRoot[:], b[44:76])
	s.HeadSlot = Slot(binary.LittleEndian.Uint64(b[76:84]))
	s.EarliestAvailableSlot = Slot(binary.LittleEndian.Uint64(b[84:92]))
	return nil
}

func (s *StatusV2) GetForkDigest() [forkDigestSize]byte { return s.ForkDigest }

// MetaDataV0 is the phase0 metadata message (fixed 16 bytes).
type MetaDataV0 struct {
	SeqNumber uint64
	Attnets   [attnetsSize]byte
}

func (m *MetaDataV0) MarshalSSZ() ([]byte, error) {
	b := binary.LittleEndian.AppendUint64(nil, m.SeqNumber)
	return append(b, m.Attnets[:]...), nil
}

func (m *MetaDataV0) UnmarshalSSZ(b []byte) error {
	if len(b) != metaDataV0Size {
		return fmt.Errorf("consensus: MetaDataV0 expects %d bytes, got %d", metaDataV0Size, len(b))
	}
	m.SeqNumber = binary.LittleEndian.Uint64(b[0:8])
	copy(m.Attnets[:], b[8:16])
	return nil
}

// MetaDataV1 is the Altair metadata message (fixed 17 bytes).
type MetaDataV1 struct {
	SeqNumber uint64
	Attnets   [attnetsSize]byte
	Syncnets  [syncnetsSize]byte
}

func (m *MetaDataV1) MarshalSSZ() ([]byte, error) {
	b := binary.LittleEndian.AppendUint64(nil, m.SeqNumber)
	b = append(b, m.Attnets[:]...)
	return append(b, m.Syncnets[:]...), nil
}

func (m *MetaDataV1) UnmarshalSSZ(b []byte) error {
	if len(b) != metaDataV1Size {
		return fmt.Errorf("consensus: MetaDataV1 expects %d bytes, got %d", metaDataV1Size, len(b))
	}
	m.SeqNumber = binary.LittleEndian.Uint64(b[0:8])
	copy(m.Attnets[:], b[8:16])
	copy(m.Syncnets[:], b[16:17])
	return nil
}

// MetaDataV2 is the Fulu/PeerDAS metadata message (fixed 25 bytes).
type MetaDataV2 struct {
	SeqNumber         uint64
	Attnets           [attnetsSize]byte
	Syncnets          [syncnetsSize]byte
	CustodyGroupCount uint64
}

func (m *MetaDataV2) MarshalSSZ() ([]byte, error) {
	b := binary.LittleEndian.AppendUint64(nil, m.SeqNumber)
	b = append(b, m.Attnets[:]...)
	b = append(b, m.Syncnets[:]...)
	return binary.LittleEndian.AppendUint64(b, m.CustodyGroupCount), nil
}

func (m *MetaDataV2) UnmarshalSSZ(b []byte) error {
	if len(b) != metaDataV2Size {
		return fmt.Errorf("consensus: MetaDataV2 expects %d bytes, got %d", metaDataV2Size, len(b))
	}
	m.SeqNumber = binary.LittleEndian.Uint64(b[0:8])
	copy(m.Attnets[:], b[8:16])
	copy(m.Syncnets[:], b[16:17])
	m.CustodyGroupCount = binary.LittleEndian.Uint64(b[17:25])
	return nil
}
