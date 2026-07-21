package entities

type Source string

const (
	SourceLibP2P Source = "libp2p"
	SourceMumP2P Source = "mump2p"
)

func (s Source) String() string {
	return string(s)
}

// LatencyComparator correlates libp2p and mump2p arrival timestamps for a single beacon block,
// per message and peer, to compare delivery latency across the two transports.
type LatencyComparator struct {
	GatewayID         string `json:"gateway_id"`
	GatewayPeerID     string `json:"gateway_peer_id,omitempty"` // This gateway's mump2p peer ID
	ChainID           uint64 `json:"chain_id"`
	BlockSlot         uint64 `json:"block_slot"`
	SlotTime          int64  `json:"slot_time"`
	ValidatorIndex    uint64 `json:"validator_index"`
	BlockSize         uint64 `json:"block_size"`
	EthSeenAtMs       int64  `json:"t_eth_seen_ms,omitempty"`
	MumSeenAtMs       int64  `json:"t_mum_seen_ms,omitempty"`
	MumPublishedAtMs  int64  `json:"t_mum_published_ms,omitempty"`
	OriginGatewayID   string `json:"origin_gateway_id,omitempty"`    // Original source gateway (from P2PMessage.SourceNodeID)
	UpstreamPeerID    string `json:"upstream_peer_id,omitempty"`     // Immediate mump2p peer that sent this message (from P2PMessage.UpstreamPeerID)
	EthUpstreamPeerID string `json:"eth_upstream_peer_id,omitempty"` // libp2p peer that delivered the block (from gossipsub msg.ReceivedFrom)
}
