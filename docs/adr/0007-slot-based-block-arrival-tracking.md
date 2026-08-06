# ADR-0007: Slot-Based Block Arrival Tracking with libp2p Peer Attribution

**Status:** Accepted
**Date:** 2026-04-14

---

## Context

This ADR extends [ADR-004](./0004-hop-by-hop-latency-tracking.md)'s hop-by-hop latency tracking to the **libp2p / Ethereum CL side**, and adds **gateway-local arrival metrics** that previously only existed on the bootstrap service.

### Current State

ADR-004 added routing fields for mump2p messages:

* `OriginGatewayID` — the original publisher's peer ID (from `P2PMessage.SourceNodeID`)
* `UpstreamPeerID` — the immediate peer that relayed it (from `P2PMessage.UpstreamPeerID`)

However, on the **libp2p/CL side**, the peer that delivered a beacon block (`msg.ReceivedFrom`) is available from the gossipsub subscription but is **discarded** when constructing the `CLMessage`:

```go
// subscribe_nodes.go — current code
s.clMessages <- &entities.CLMessage{
    MessageID: msg.ID,
    Topic:     topicName,
    Message:   msg.Data,
    // msg.ReceivedFrom is available here but not passed through
}
```

### Problem Statement

1. We cannot identify **which CL peer** delivered a block to the gateway — useful for debugging slow CL connections and understanding the CL peering topology.
2. Block arrival performance metrics (first-seen %, arrival latency distribution) exist only on bootstrap, not on the gateway itself.

---

## Decision

### Add libp2p Peer Attribution to CLMessage

Extend `CLMessage` to carry the peer that delivered the message:

```go
type CLMessage struct {
    MessageID    string `json:"message_id"`
    Topic        string `json:"topic"`
    Message      []byte `json:"message"`
    ReceivedFrom string `json:"received_from,omitempty"` // libp2p peer.ID as string
}
```

### Extend LatencyComparator with libp2p Peer

Add a field to record which CL peer delivered the block:

```go
type LatencyComparator struct {
    // ... existing fields ...
    EthUpstreamPeerID string `json:"eth_upstream_peer_id,omitempty"` // CL peer that delivered the block
}
```

This completes the symmetry with the mump2p side:

| Field                    | mump2p            | libp2p (NEW)                                    |
| ------------------------ | ----------------- | ----------------------------------------------- |
| Who originally published | `OriginGatewayID` | N/A (CL proposer, tracked via `ValidatorIndex`) |
| Immediate upstream peer  | `UpstreamPeerID`  | `EthUpstreamPeerID`                             |

### Gateway-Local Prometheus Metrics

#### Counters — First Seen

```go
blocks_first_seen_mump2p_total   // incremented when MumSeenAtMs < EthSeenAtMs for a slot
blocks_first_seen_libp2p_total   // incremented when EthSeenAtMs <= MumSeenAtMs for a slot
```

Emitted once per slot (guarded by `firstSeenSlots` TTLMap) when both timestamps are available.

#### Histograms — Arrival Latency

```go
block_arrival_mump2p_ms   // MumSeenAtMs - SlotStartMs
block_arrival_libp2p_ms   // EthSeenAtMs - SlotStartMs
```

Bucket boundaries: `[50, 100, 150, 200, 300, 500, 750, 1000, 2000, 5000]`

Enables `% seen < 200ms` queries:

```promql
sum(rate(block_arrival_mump2p_ms_bucket{le="200"}[5m]))
/ sum(rate(block_arrival_mump2p_ms_count[5m])) * 100
```

#### Counters — Per-Peer Block Delivery

Symmetric with the existing mump2p hop tracking counters:

| Metric                                | Labels                 | Purpose                                                   |
| ------------------------------------- | ---------------------- | --------------------------------------------------------- |
| `mump2p_messages_from_upstream_total` | `upstream_peer_id`     | Which mump2p peers relay blocks to us (existing, ADR-004) |
| `mump2p_messages_from_origin_total`   | `origin_gateway_id`    | Which gateway originally published (existing, ADR-004)    |
| `libp2p_messages_from_upstream_total` | `eth_upstream_peer_id` | Which CL peers deliver blocks to us (NEW)                 |

The new `libp2p_messages_from_upstream_total` counter lets operators see which CL peers are most active and identify slow or disconnected CL connections.

---
