// Package streamhub fans decoded beacon-block observations to consumers without backpressure.
// It drops the oldest event for a slow consumer (ADR-0011).
package streamhub

import "github.com/getoptimum/optimum-gateway/pkg/entities"

// BlockEvent identifies an observation by Slot, ProposerIndex, and Source (ADR-0011).
// Raw contains verbatim SSZ-Snappy data in raw mode and is omitted in metadata mode.
type BlockEvent struct {
	Slot           uint64
	ProposerIndex  uint64
	ParentRoot     []byte
	StateRoot      []byte
	BlockSizeBytes uint64
	Topic          string
	Source         entities.Source
	ReceivedAtMs   int64
	GatewayID      string
	ForkDigest     string
	Stale          bool
	Raw            []byte
}
