package utils

import (
	"fmt"

	"github.com/golang/snappy"
)

// MaxGossipPayloadSize is the max uncompressed gossip payload size (10 MiB).
const MaxGossipPayloadSize = 10 * 1024 * 1024

// DecodeSnappy decodes a snappy block, rejecting decompressed size over maxSize before allocating.
func DecodeSnappy(msg []byte, maxSize int) ([]byte, error) {
	size, err := snappy.DecodedLen(msg)
	if err != nil {
		return nil, err
	}
	if size > maxSize {
		return nil, fmt.Errorf("snappy message exceeds max size: %d bytes > %d bytes", size, maxSize)
	}
	return snappy.Decode(nil, msg)
}
