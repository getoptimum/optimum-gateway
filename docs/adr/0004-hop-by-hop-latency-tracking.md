# ADR-0004: Hop-by-Hop Latency Tracking for mump2p Gateway Routing

**Status:** Accepted
**Date:** 2026-02-09

---

## Context

This ADR extends [ADR-003](./0003-validator-metrics.md)'s metrics architecture. ADR-003 established:

* Gateways emit raw per-block timestamp events
* Bootstrap collector computes baselines and stable KPIs
* Clear separation between raw events (gateway) and computed metrics (bootstrap)

ADR-003 measures **when** blocks arrive at each gateway. This ADR adds **how** blocks flow through the gateway network by tracking routing paths and enabling hop-by-hop latency analysis.

### Current End-to-End Latency Tracking

The gateway currently measures end-to-end latency for beacon blocks:

* `EthSeenAtMs`: When this gateway receives a block from Ethereum P2P
* `MumSeenAtMs`: When this gateway receives a block from mump2p
* `MumPublishedAtMs`: When this gateway publishes a block to mump2p

However, when multiple gateways are connected in a mump2p network, messages may traverse several hops before reaching their destination. The current telemetry does not provide visibility into **which specific gateway in the chain is introducing latency**.

### Problem Statement

Given a network topology like:

```sh
Origin Gateway A → Gateway B → Gateway C → Destination Gateway D
```

We can measure:

* Total latency from slot start to Gateway D (via `MumSeenAtMs`)
* When Gateway A published to mump2p (via `MumPublishedAtMs`)

But we **cannot identify** if the latency bottleneck is:

* A→B hop (slow propagation from A to B)
* B→C hop (slow propagation from B to C)
* C→D hop (slow propagation from C to D)
* Or processing delay at B or C

This lack of visibility makes it difficult to:

1. Identify problematic gateways causing network delays
2. Optimize routing and peering configurations
3. Debug latency issues in production

---

## Decision

We will implement **hop-by-hop latency tracking** by:

### Extending Telemetry Data Model

Add routing information to `LatencyComparator` that each gateway reports:

```go
type LatencyComparator struct {
    // Existing fields
    GatewayID        string `json:"gateway_id"`
    GatewayPeerID    string `json:"gateway_peer_id,omitempty"` // NEW
    BlockSlot        uint64 `json:"block_slot"`
    EthSeenAtMs      int64  `json:"t_eth_seen_ms,omitempty"`
    MumSeenAtMs      int64  `json:"t_mum_seen_ms,omitempty"`
    MumPublishedAtMs int64  `json:"t_mum_published_ms,omitempty"`
    // Additional fields
    OriginGatewayID string `json:"origin_gateway_id,omitempty"` // Who originally published
    UpstreamPeerID  string `json:"upstream_peer_id,omitempty"`  // Who sent it to us
}
```

### Leveraging Existing P2PMessage Fields

The `P2PMessage` struct (in `optimum-common`) already contains routing information:

```go
type P2PMessage struct {
    SourceNodeID   string // Original publisher's peer ID
    UpstreamPeerID string // Immediate sender's peer ID
    Topic          string
    MessageID      string
    Message        []byte
}
```

We will capture these fields when processing mump2p messages and include them in telemetry reports.

### Bootstrap Service Correlation

The bootstrap service (at `https://bootstrap.getoptimum.io`) will:

1. **Build a peer ID mapping**: Use `GatewayPeerID` to map libp2p peer IDs to logical gateway IDs
2. **Reconstruct routing paths**: Follow the `UpstreamPeerID` chain to trace message flow
3. **Calculate hop latencies**: Match `MumPublishedAtMs` from sender with `MumSeenAtMs` from receiver

Example calculation:

```sh
Gateway A reports: gateway_peer_id="QmXYZ", mum_published_at=T1
Gateway B reports: gateway_id="B", upstream_peer_id="QmXYZ", mum_seen_at=T2
→ Bootstrap infers: A→B hop latency = T2 - T1
```

### Gateway-Level Operational Metrics (Prometheus)

**Important:** Following ADR-003's pattern, these are **operational monitoring counters**, not KPIs. These gateway-local counters (`mump2p_messages_from_upstream_total`, `mump2p_messages_from_origin_total`, `libp2p_messages_from_upstream_total`) **are implemented**. The derived hop-by-hop latency KPIs (p50/p95/p99) that bootstrap would compute from the raw routing data are **not implemented** (proposed — see KPI Groups D/E below).

Add gateway-level metrics for real-time operational visibility:

```go
// Count messages received from each upstream peer
mumP2PMessagesFromUpstream = NewCounterVec(
    "mump2p_messages_from_upstream_total",
    []string{"upstream_peer_id"},
)

// Count messages received from each origin gateway
mumP2PMessagesFromOrigin = NewCounterVec(
    "mump2p_messages_from_origin_total",
    []string{"origin_gateway_id"},
)
```

These metrics help operators identify:

* Which upstream peers are most active
* Message distribution across origin gateways
* Anomalies in routing patterns

### Bootstrap-Computed Hop-by-Hop KPIs (proposed — not yet implemented)

Following ADR-003's pattern of "gateways emit raw events, bootstrap computes stable KPIs", the bootstrap service *could* compute the following KPIs from the raw routing data.

> **Status:** The raw routing inputs described above (`origin_gateway_id`, `upstream_peer_id`, `eth_upstream_peer_id`, and the gateway-local counters `mump2p_messages_from_upstream_total`, `mump2p_messages_from_origin_total`, `libp2p_messages_from_upstream_total`) **are implemented** and emitted by the gateway. The **derived hop-by-hop KPIs below (Groups D and E) are a design proposal and are not yet implemented** in bootstrap. The metric names below are illustrative, not shipped keys. Bootstrap today computes the ADR-0003 KPIs (`opt_gateway_gap_to_best_ms_{50,95,99}`, `opt_gateway_mum_spread_ms_{50,95,99}`, `opt_mum_spread_coverage_{200,500,1000}`, `opt_mum_publish_rate`, `opt_missing_eth_rate`, `opt_missing_mum_rate`); the hop KPIs here remain future work.

#### KPI Group D — Hop-by-Hop Latency Analysis — NOT IMPLEMENTED (proposed)

None of the metric names in this group exist in code (neither gateway nor bootstrap). They are illustrative names for a possible future design. If built, bootstrap would compute per-gateway-pair metrics such as:

* `opt_hop_latency_ms{from_gateway, to_gateway}` → histogram — **not implemented (proposed)**
    * Latency between specific gateway pairs: `T_mum_seen(to) - T_mum_published(from)`
    * Shows p50/p95/p99 for each hop in the network
    * Example: "Gateway A→B hop has p95 latency of 45ms"

* `opt_gateway_processing_delay_ms{gateway_id}` → histogram — **not implemented (proposed)**
    * Time between receiving via mump2p and republishing: `T_mum_published - T_mum_seen`
    * Identifies gateways with slow block processing
    * Example: "Gateway B takes p95 of 12ms to republish blocks"

* `opt_hop_count_distribution{gateway_id}` → histogram — **not implemented (proposed)**
    * Number of hops from origin to this gateway (computed from upstream chain)
    * Identifies routing efficiency and potential multi-hop delays

#### KPI Group E — Bottleneck Identification (Global) — NOT IMPLEMENTED (proposed)

None of the metric names in this group exist in code. They are illustrative names for a possible future design.

* `opt_slowest_hops_p95{from_gateway, to_gateway}` → gauge — **not implemented (proposed)**
    * Identifies top slowest gateway-to-gateway paths
    * Helps pinpoint network routing bottlenecks
    * Used for network optimization decisions

* `opt_gateway_bottleneck_rate{gateway_id}` → ratio — **not implemented (proposed)**
    * Percentage of blocks where this gateway introduces >threshold ms delay
    * Flags gateways causing network-wide slowdowns
    * Threshold configurable (e.g., >100ms delay considered bottleneck)

**Interpretation (of the proposed metrics above):**

* Hop latency: **How fast do blocks propagate between specific gateway pairs?**
* Processing delay: **Which gateways are slow to republish blocks?**
* Bottleneck rate: **Which gateways are causing network-wide latency issues?**

---

## Integration with ADR-003 Data Flow

This section shows how hop-by-hop tracking integrates with ADR-003's established data flow:

### ADR-003 Flow (Existing)

```text
1. Gateway receives block → Records raw timestamps
2. Gateway sends to bootstrap: {gateway_id, slot, t_eth_seen_ms, t_mum_seen_ms, t_mum_published_ms}
3. Bootstrap computes KPIs: gap_to_best_ms, mum_spread_ms
```

### ADR-004 Extension (New)

```text
1. Gateway receives block → Records raw timestamps + routing info
2. Gateway sends to bootstrap: {gateway_id, gateway_peer_id, slot,
                                t_eth_seen_ms, t_mum_seen_ms, t_mum_published_ms,
                                origin_gateway_id, upstream_peer_id}  ← NEW FIELDS
3. Bootstrap computes:
   a. ADR-003 KPIs (implemented): gap_to_best_ms, mum_spread_ms
   b. ADR-004 hop KPIs: hop_latency_ms, processing_delay_ms, bottleneck_rate ← NOT IMPLEMENTED (proposed; see KPI Groups D/E above)
```
