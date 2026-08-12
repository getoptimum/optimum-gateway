// Package streamhub fans decoded beacon-block observations to consumers without backpressure.
// It drops new events when a consumer buffer is full (ADR-0011).
package streamhub

import "github.com/getoptimum/optimum-gateway/pkg/entities"

// BlockEvent identifies an observation by Slot, ProposerIndex, and Source (ADR-0011).
// Raw contains verbatim SSZ-Snappy data in raw mode and is omitted in metadata mode.
type BlockEvent struct {
	Slot           uint64          `json:"slot"`
	ProposerIndex  uint64          `json:"proposer_index"`
	ParentRoot     []byte          `json:"parent_root"`
	StateRoot      []byte          `json:"state_root"`
	BlockSizeBytes uint64          `json:"block_size_bytes"`
	Topic          string          `json:"topic"`
	Source         entities.Source `json:"source"`
	ReceivedAtMs   int64           `json:"received_at_ms"`
	GatewayID      string          `json:"gateway_id"`
	ForkDigest     string          `json:"fork_digest"`
	Stale          bool            `json:"stale"`
	Raw            []byte          `json:"raw,omitempty"`
}
