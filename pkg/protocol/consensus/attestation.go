package consensus

import (
	"encoding/binary"
	"fmt"
)

const (
	checkpointSize        = 40
	attestationDataSize   = 128
	singleAttestationSize = 240
	signatureSize         = 96
)

// Checkpoint is an SSZ (epoch, root) pair.
type Checkpoint struct {
	Epoch Epoch
	Root  [32]byte
}

func (c Checkpoint) appendTo(b []byte) []byte {
	b = binary.LittleEndian.AppendUint64(b, uint64(c.Epoch))
	return append(b, c.Root[:]...)
}

func (c *Checkpoint) unmarshal(b []byte) {
	c.Epoch = Epoch(binary.LittleEndian.Uint64(b[:8]))
	copy(c.Root[:], b[8:checkpointSize])
}

// AttestationData is the SSZ attestation-data container (fixed 128 bytes).
type AttestationData struct {
	Slot            Slot
	CommitteeIndex  CommitteeIndex
	BeaconBlockRoot [32]byte
	Source          Checkpoint
	Target          Checkpoint
}

func (d *AttestationData) MarshalSSZ() ([]byte, error) {
	b := make([]byte, 0, attestationDataSize)
	b = binary.LittleEndian.AppendUint64(b, uint64(d.Slot))
	b = binary.LittleEndian.AppendUint64(b, uint64(d.CommitteeIndex))
	b = append(b, d.BeaconBlockRoot[:]...)
	b = d.Source.appendTo(b)
	b = d.Target.appendTo(b)
	return b, nil
}

func (d *AttestationData) UnmarshalSSZ(b []byte) error {
	if len(b) != attestationDataSize {
		return fmt.Errorf("consensus: AttestationData expects %d bytes, got %d", attestationDataSize, len(b))
	}
	d.Slot = Slot(binary.LittleEndian.Uint64(b[0:8]))
	d.CommitteeIndex = CommitteeIndex(binary.LittleEndian.Uint64(b[8:16]))
	copy(d.BeaconBlockRoot[:], b[16:48])
	d.Source.unmarshal(b[48:88])
	d.Target.unmarshal(b[88:128])
	return nil
}

// SingleAttestation is the Electra single-attestation gossip message (fixed 240 bytes).
type SingleAttestation struct {
	CommitteeIndex CommitteeIndex
	AttesterIndex  ValidatorIndex
	Data           AttestationData
	Signature      [signatureSize]byte
}

func (s *SingleAttestation) MarshalSSZ() ([]byte, error) {
	b := make([]byte, 0, singleAttestationSize)
	b = binary.LittleEndian.AppendUint64(b, uint64(s.CommitteeIndex))
	b = binary.LittleEndian.AppendUint64(b, uint64(s.AttesterIndex))
	data, err := s.Data.MarshalSSZ()
	if err != nil {
		return nil, err
	}
	b = append(b, data...)
	return append(b, s.Signature[:]...), nil
}

func (s *SingleAttestation) UnmarshalSSZ(b []byte) error {
	if len(b) != singleAttestationSize {
		return fmt.Errorf("consensus: SingleAttestation expects %d bytes, got %d", singleAttestationSize, len(b))
	}
	s.CommitteeIndex = CommitteeIndex(binary.LittleEndian.Uint64(b[0:8]))
	s.AttesterIndex = ValidatorIndex(binary.LittleEndian.Uint64(b[8:16]))
	if err := s.Data.UnmarshalSSZ(b[16:144]); err != nil {
		return err
	}
	copy(s.Signature[:], b[144:singleAttestationSize])
	return nil
}
