package consensus

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/golang/snappy"

	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

type (
	Marshaler interface {
		MarshalSSZ() ([]byte, error)
	}
	Unmarshaler interface {
		UnmarshalSSZ([]byte) error
	}
)

// SSZSnappyCodec encodes and decodes eth2 gossip and req/resp messages as SSZ
// with snappy compression, matching the consensus p2p spec.
type SSZSnappyCodec struct{}

// EncodeGossip writes msg as snappy-block-compressed SSZ.
func (SSZSnappyCodec) EncodeGossip(w io.Writer, msg Marshaler) (int, error) {
	b, err := msg.MarshalSSZ()
	if err != nil {
		return 0, err
	}
	if len(b) > utils.MaxGossipPayloadSize {
		return 0, fmt.Errorf("consensus: gossip message %d bytes exceeds max %d", len(b), utils.MaxGossipPayloadSize)
	}
	return w.Write(snappy.Encode(nil, b))
}

// DecodeGossip decompresses a snappy-block gossip payload and unmarshals it.
func (SSZSnappyCodec) DecodeGossip(b []byte, msg Unmarshaler) error {
	if len(b) > maxCompressedLen(utils.MaxGossipPayloadSize) {
		return fmt.Errorf("consensus: gossip message exceeds max compressed size %d", maxCompressedLen(utils.MaxGossipPayloadSize))
	}
	raw, err := utils.DecodeSnappy(b, utils.MaxGossipPayloadSize)
	if err != nil {
		return err
	}
	return msg.UnmarshalSSZ(raw)
}

// DecodeGossipRaw decompresses a gossip payload to its raw SSZ bytes without
// unmarshaling, applying the same size bounds as DecodeGossip.
func (SSZSnappyCodec) DecodeGossipRaw(b []byte) ([]byte, error) {
	if len(b) > maxCompressedLen(utils.MaxGossipPayloadSize) {
		return nil, fmt.Errorf("consensus: gossip message exceeds max compressed size %d", maxCompressedLen(utils.MaxGossipPayloadSize))
	}
	return utils.DecodeSnappy(b, utils.MaxGossipPayloadSize)
}

// EncodeWithMaxLength writes msg as an uncompressed-length uvarint followed by the snappy-framed SSZ.
func (SSZSnappyCodec) EncodeWithMaxLength(w io.Writer, msg Marshaler) (int, error) {
	b, err := msg.MarshalSSZ()
	if err != nil {
		return 0, err
	}
	if len(b) > utils.MaxGossipPayloadSize {
		return 0, fmt.Errorf("consensus: message %d bytes exceeds max %d", len(b), utils.MaxGossipPayloadSize)
	}
	if _, err := w.Write(binary.AppendUvarint(nil, uint64(len(b)))); err != nil {
		return 0, err
	}
	sw := snappy.NewBufferedWriter(w)
	n, err := sw.Write(b)
	if err != nil {
		_ = sw.Close()
		return 0, err
	}
	return n, sw.Close()
}

// DecodeWithMaxLength reads a req/resp chunk (uvarint length + snappy frame) and unmarshals it.
func (SSZSnappyCodec) DecodeWithMaxLength(r io.Reader, msg Unmarshaler) error {
	msgLen, err := binary.ReadUvarint(oneByteReader{r})
	if err != nil {
		return err
	}
	if msgLen > utils.MaxGossipPayloadSize {
		return fmt.Errorf("consensus: message length %d exceeds max %d", msgLen, utils.MaxGossipPayloadSize)
	}

	maxCompressed := snappy.MaxEncodedLen(int(msgLen))
	if maxCompressed < 0 {
		return fmt.Errorf("consensus: message length %d too large to decode", msgLen)
	}
	sr := snappy.NewReader(io.LimitReader(r, int64(maxCompressed)))
	buf := make([]byte, msgLen)
	if _, err := io.ReadFull(sr, buf); err != nil {
		return err
	}
	return msg.UnmarshalSSZ(buf)
}

func maxCompressedLen(n int) int { return 32 + n + n/6 }

// oneByteReader adapts an io.Reader to io.ByteReader, reading exactly one byte
// per call so the varint decode never consumes bytes past the length prefix.
type oneByteReader struct{ r io.Reader }

func (o oneByteReader) ReadByte() (byte, error) {
	var p [1]byte
	n, err := o.r.Read(p[:])
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, io.ErrNoProgress
	}
	return p[0], nil
}
