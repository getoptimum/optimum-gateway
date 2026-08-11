// Package streamhub fans decoded beacon-block observations out to downstream
// consumers (ADR-0011). Emit is non-blocking: a slow consumer's oldest events
// are dropped rather than backpressuring the ingest goroutines.
package streamhub

import "github.com/getoptimum/optimum-gateway/pkg/entities"

// BlockEvent is one beacon-block observation. It is produced once per source,
// so (Slot, ProposerIndex) is the block identity and Source tells the libp2p
// and mump2p views apart. Raw holds the verbatim ssz_snappy bytes for raw-mode
// consumers; metadata-mode transports omit it.
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
