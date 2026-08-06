# ADR-0002: Beacon block latency and mump2p propagation

**Status:** Accepted  
**Date:** 2025-12-04  

---

> **Historical-accuracy note (added for public release).** This ADR captures the original (Dec 2025) design. The implementation has since been refactored and superseded by [ADR-0003](./0003-validator-metrics.md), [ADR-0004](./0004-hop-by-hop-latency-tracking.md), and [ADR-0007](./0007-slot-based-block-arrival-tracking.md). Several symbols, files, and metric names below no longer match the code. Current equivalents:
>
> * Functions `handleBeaconBlockFrom`, `recordMessageFetchedAt`, `calculateBlockDelay` and the file `gateway_exchanges.go` **no longer exist**; the block decode/telemetry path is `processBeaconBlockArrival` (`pkg/service/gossipsub-gateway/beacon_block_measures.go`) plus tracking in `pkg/service/bootstrapper`. `sendTrackedSlots` now lives in `pkg/service/bootstrapper`.
> * The `LatencyComparator` schema shown here (`eth_p2p_received_at`, `mum_p2p_received_at`, and the derived `*_latency_ms` fields) evolved: the current struct uses `t_eth_seen_ms` / `t_mum_seen_ms` / `t_mum_published_ms` (see ADR-0004/0007), and the derived latency fields were **not** added to the gateway struct — those are computed on the bootstrap side.
> * The gateway's per-source arrival metrics are `block_arrival_libp2p_ms` / `block_arrival_mump2p_ms` (ADR-0007), **not** `block_arrival_latency_ms` / `eth_block_latency_ms`.

## Decision

**Option 1 (side-channel via bootstrap) is the adopted model.** Option 2 (embedding `IngressTimeMs` in `P2PMessage`) was considered and rejected — the wire format change would have required coordinated redeploys of all components that handle `P2PMessage` (gateway, proxy, p2p nodes) for a metric that is already computable server-side.

> **Endpoint note:** The `/api/v1/handle_block_latency` URL referenced throughout this ADR is **no longer in use**. It was superseded by `/api/v2/handle_block_latency` from [ADR-003](./0003-validator-metrics.md) onwards, when the payload schema was extended with stable KPI inputs. The v1 path predates the schema change and is retained in this document only for historical accuracy.

---

## Context

The gateway already exposes several latency metrics related to beacon
blocks:

* “block arrival” latency from theoretical slot start to when the CL
  delivers a block to the gateway.
* “block propagation” latency per source (`ethp2p` vs `mump2p`) as
  observed at the gateway.
* Per‑slot arrival timestamps reported to a remote API for further
  analysis.

However, those measurements are taken from the gateway’s point of view
only. The CL itself is a passive consumer of Eth gossip, and the gateway
is a passive consumer of the CL:

* Slot start → Eth gossip → CL → gateway (Eth path).
* Slot start → Eth gossip → CL → origin gateway → mump2p → destination
  gateway (Mum path).

This makes it easy to mix:

* Total end‑to‑end latency (slot start to destination gateway), and
* The internal propagation time inside mump2p only.

We want to:

* make mump2p propagation time explicit, and
* be able to compare “which path was faster” for a given slot (Eth vs
  Mum), while clearly documenting what each metric represents.

---

## Existing measurements

Today the gateway already records several related metrics.

### 1.1. Per-slot JSON payload

`pkg/entities/latency_comparator.go`

```go
type LatencyComparator struct {
    GatewayID        string `json:"gateway_id"`
    BlockSlot        uint64 `json:"block_slot"`
    ValidatorIndex   uint64 `json:"validator_index"`
    SlotTime         int64  `json:"slot_time"`
    EthP2PReceivedAt int64  `json:"eth_p2p_received_at"`
    MumP2PReceivedAt int64  `json:"mum_p2p_received_at"`
}
```

This struct is populated in `pkg/service/gossipsub-gateway/beacon_block_measures.go`
whenever a beacon block is seen at the gateway from:

* CL (Eth gossip path), or
* mump2p (mump2p mesh path).

It is sent to the remote service at:

* `remoteURL = "https://bootstrap.getoptimum.io/api/v1/handle_block_latency"`

via `sendTrackedSlots`.

### 1.2. Gateway-level Prometheus metrics

`pkg/service/telemetry` provides:

* `block_arrival_latency_ms` and `eth_block_latency_ms` in
  `validator.go` via:

  ```go
  ObserveBlockArrival(latencyMs int64)
  ObserveEthLatency(topic string, latencyMs int64)
  ```

  These are invoked from `recordMessageFetchedAt` in
  `pkg/service/gossipsub-gateway/gateway_exchanges.go` when a
  beacon block is first fetched from CL.

* `beacon_block_propagation_ms{source="ethp2p"|"mump2p"}` via:

  ```go
  ObserveBlockPropagation(source string, latencyMs int64)
  ```

  This is called from `calculateBlockDelay` in
  `pkg/service/gossipsub-gateway/beacon_block_measures.go` when a
  block is seen via ethp2p or mump2p.

### 1.3. Integration points

Messages flow through the gateway in two directions:

* From CL → gateway → mump2p:
    * `handleMessagesFromCL` in
    `pkg/service/gossipsub-gateway/messages_proxy.go`
    * Calls `handleBeaconBlockFrom(entities.SourceEthP2P, ...)` for beacon blocks.

* From mump2p → gateway → CL:
    * `handleMessagesFromMumP2PNode` in the same file.
    * Calls `handleBeaconBlockFrom(entities.SourceMumP2P, ...)` for beacon blocks.

Both sides share the common slot handling logic in:

* `handleBeaconBlockFrom` in
  `pkg/service/gossipsub-gateway/beacon_block_measures.go`

This ensures that for each slot we can see:

* when it first appeared from ethp2p (via CL), and
* when it first appeared from mump2p.

---

## Latency model

For a given beacon block / slot, define the following timestamps:

* `T_slot`: slot start time as computed by the gateway
  (`SlotStartTime(slot)`).
* `T_eth_dest`: time the destination gateway is notified of the block by
  its local CL client (“Eth path”).
* `T_mum_enter`: origin gateway sends the block into mump2p (i.e. time
  of injection into the Mum network as observed by that origin
  gateway).
* `T_mum_dest`: destination gateway first receives the block from
  mump2p.
* (Optional) `T_eth_orig`: time the origin gateway is notified of the
  block by its local CL client.

From these timestamps we derive the following latencies:

* `L_eth_dest = T_eth_dest - T_slot`
    * Eth path: slot start → destination gateway arrival via CL
      (including gossip → CL → gateway).
* `L_mum_dest = T_mum_dest - T_slot`
    * Mum path: slot start → destination gateway arrival via mump2p
      (including Eth path up to the origin gateway, plus mump2p).
* `L_mum_p2p = T_mum_dest - T_mum_enter`
    * Pure mump2p propagation time between a given origin gateway (where
      the block was injected) and a given destination gateway.
* `Δ_mum_vs_eth = T_mum_dest - T_eth_dest`
    * Difference between Mum and Eth arrival at the destination.
    * `< 0`: Mum is faster by `abs(Δ_mum_vs_eth)` ms.
    * `> 0`: Eth is faster by `Δ_mum_vs_eth` ms.

If both `T_eth_orig` and `T_eth_dest` are recorded, the difference
`T_eth_dest - T_eth_orig` approximates additional Eth / CL propagation
between the origin’s and destination’s vantage points. This is mainly a
diagnostic metric rather than a primary KPI.

Optionally, a global network view of mump2p propagation could use the
earliest injection time across all gateways that injected the block:

* `L_mum_p2p_global = T_mum_dest - min_p(T_mum_enter)`

where `min_p(T_mum_enter)` is computed centrally by the remote service
aggregating reports from multiple gateways.

The objective is to:

1. Record `T_mum_enter` and `T_mum_dest` in addition to `T_eth_dest`.
2. Compute the latencies above.
3. Export them both in JSON (to the remote service) and via Prometheus.

---

## Where timestamps should be taken

### Destination arrival timestamps (already implemented)

**Eth path (CL → gateway)**:

* `handleMessagesFromCL` (`messages_proxy.go`) calls:

  ```go
  slot := s.handleBeaconBlockFrom(entities.SourceEthP2P, msg.Topic, msg.Message, time.Now().UnixMilli())
  ```

  This uses `recvAt` as `T_eth_dest` in `handleBeaconBlockFrom`.

* `recordMessageFetchedAt` (`gateway_exchanges.go`) is called with the
  message hash and the topic, where it:

    * Computes `slot := utils.CurrentSlot(time.Now())`.
    * Computes `latency := nowMs - SlotStartTime(slot)`.
    * Emits:

    ```go
    telemetry.ObserveBlockArrival(latency)
    telemetry.ObserveEthLatency("beacon_block", latency)
    ```

  This is equivalent to `L_eth_dest` at the destination gateway.

**Mum path (mump2p → destination gateway)**:

* `handleMessagesFromMumP2PNode` (`messages_proxy.go`) calls:

  ```go
  slot := s.handleBeaconBlockFrom(entities.SourceMumP2P, msg.Topic, msg.Message, time.Now().UnixMilli())
  ```

  This uses `recvAt` as `T_mum_dest` in `handleBeaconBlockFrom`.

So the existing fields in `LatencyComparator` have the following meaning:

* `EthP2PReceivedAt` ≈ `T_eth_dest`
* `MumP2PReceivedAt` ≈ `T_mum_dest`
* `SlotTime` = `T_slot`

### mump2p ingress timestamp (`T_mum_enter`)

To measure pure mump2p propagation, we need a timestamp when the block
first enters mump2p, at the *origin gateway*.

This should be taken in:

* `handleMessagesFromCL` (`messages_proxy.go`), right before publishing
  the message to the mump2p node (`nodeMumP2P.PublishMessage`).

Example:

```go
tEnter := time.Now().UnixMilli() // T_mum_enter
// ... record this against the slot/message ...

if s.nodeMumP2P == nil {
    continue
}
if err := s.nodeMumP2P.PublishMessage(s.ctx, msg.Topic, msg.Message); err != nil {
    // existing error handling
}
```

The challenge is transporting `T_mum_enter` so that the destination
gateway (or the remote analytics backend) can correlate it with
`T_mum_dest` and `T_eth_dest` for the same slot.

There are two main options:

1. Store `T_mum_enter` per-slot in a TTL map at the origin, and have
   the remote service join origin and destination payloads.
2. Embed `T_mum_enter` into the `P2PMessage` that is sent over mump2p,
   so the destination gateway can compute `L_mum_p2p` locally.

Both designs are described in detail below.

---

## Data model changes (JSON payload)

Regardless of how to obtain `T_mum_enter`, we can extend the JSON we
send to `remoteURL` to include:

* Raw timestamp for when the block entered mump2p.
* Derived latencies: `L_eth_dest`, `L_mum_dest`, `L_mum_p2p`,
  and `Δ_mum_vs_eth`.

### Extended `LatencyComparator`

In `pkg/entities/latency_comparator.go`, extend the struct:

```go
type LatencyComparator struct {
    GatewayID        string `json:"gateway_id"`
    BlockSlot        uint64 `json:"block_slot"`
    ValidatorIndex   uint64 `json:"validator_index"`
    SlotTime         int64  `json:"slot_time"`
    EthP2PReceivedAt int64  `json:"eth_p2p_received_at"`
    MumP2PReceivedAt int64  `json:"mum_p2p_received_at"`

    // New fields
    MumP2PEnterAt int64 `json:"mum_p2p_enter_at,omitempty"`

    EthLatencyMs  int64 `json:"eth_latency_ms,omitempty"`      // L_eth_dest
    MumLatencyMs  int64 `json:"mum_latency_ms,omitempty"`      // L_mum_dest
    MumP2POnlyMs  int64 `json:"mum_p2p_only_ms,omitempty"`     // L_mum_p2p
    MumMinusEthMs int64 `json:"mum_minus_eth_ms,omitempty"`    // Δ_mum_vs_eth
}
```

The expectation is:

* `EthLatencyMs` and `MumLatencyMs` are always computed as
  `receivedAt - SlotTime` on the gateway that sends the payload.
* `MumP2POnlyMs` and `MumMinusEthMs` are set if there is enough data to
  compute them.

**Note:** if the remote service is strict on payload shape, we should
co‑ordinate this change with the service before deploying.

---

## Option 1: side-channel via remote service (minimal wire changes)

This option keeps the mump2p protocol unchanged. All correlation is done
by the remote analytics service running on the bootstrap nodes (the HTTP
endpoint at `remoteURL`), which receives per-slot records from both
origin and destination gateways.

### 5.1. Origin gateway

At the origin gateway, when a beacon block arrives from CL and is about
to be published to mump2p:

1. Compute `T_mum_enter = time.Now().UnixMilli()`.
2. Determine the block slot from the decoded message (similar to
   `handleBeaconBlockFrom`).
3. Store `MumP2PEnterAt` in an in-memory TTL map keyed by `slot`.
4. When the gateway itself later calls `sendTrackedSlots(slot)`, populate:

   * `MumP2PEnterAt` from the TTL map.

Pseudo-code changes:

```go
// 1) New TTL map in Service (origin gateway)
// messagesMap exists already; we can add a similar map:
// mumIngressBySlot *commonUtils.TTLMap[uint64, int64]

// 2) In NewService, initialize the TTL map with a reasonable TTL, e.g. 2-3 slots.

// 3) In handleMessagesFromCL, when topic contains "beacon_block":
blkSlot := ... // decode slot from msg.Message, similar to getBlockObject
tEnter := time.Now().UnixMilli()
s.mumIngressBySlot.Put(blkSlot, tEnter)
```

### Destination gateways

Destination gateways already fill:

* `EthP2PReceivedAt` and `MumP2PReceivedAt` in `handleBeaconBlockFrom`.

No change is required on the destination side; they simply keep
reporting their arrival times for each slot.

### 5.3. Computing and exporting latencies

In `sendTrackedSlots(slot uint64)` in
`pkg/service/gossipsub-gateway/beacon_block_measures.go`, compute
the derived metrics just before sending to `remoteURL`:

```go
func (s *Service) sendTrackedSlots(slot uint64) {
    data, ok := trackedSlots.Load(slot)
    if !ok {
        return
    }

    // AB testing logic unchanged
    // ...

    // compute derived metrics
    ethLatency := int64(0)
    mumLatency := int64(0)
    mumP2POnly := int64(0)
    mumMinusEth := int64(0)

    if data.EthP2PReceivedAt > 0 {
        ethLatency = data.EthP2PReceivedAt - data.SlotTime
    }
    if data.MumP2PReceivedAt > 0 {
        mumLatency = data.MumP2PReceivedAt - data.SlotTime
    }

    // fetch MumP2PEnterAt from the origin’s side-channel map if this gateway is the origin
    if enterAt, ok := s.mumIngressBySlot.Get(slot); ok && data.MumP2PReceivedAt > 0 {
        data.MumP2PEnterAt = enterAt
        mumP2POnly = data.MumP2PReceivedAt - enterAt
    }

    if data.EthP2PReceivedAt > 0 && data.MumP2PReceivedAt > 0 {
        mumMinusEth = data.MumP2PReceivedAt - data.EthP2PReceivedAt
    }

    data.EthLatencyMs = ethLatency
    data.MumLatencyMs = mumLatency
    data.MumP2POnlyMs = mumP2POnly
    data.MumMinusEthMs = mumMinusEth

    ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
    defer cancel()
    _, _, _ = commonUtils.PostCurl[any](ctx, remoteURL, data, nil)
    trackedSlots.Delete(slot)
}
```

The remote service can then:

* For origin gateways:
    * Read `MumP2PEnterAt` and `SlotTime`.
    * Use this as `T_mum_enter` and `T_slot`.
* For destination gateways:
    * Read `EthP2PReceivedAt` and `MumP2PReceivedAt`.
    * Use these as `T_eth_dest` and `T_mum_dest`.

If the remote service correlates origin/destination records by
`gateway_id` + `block_slot`, it can recompute:

* `L_eth_dest`, `L_mum_dest`, `L_mum_p2p`, and `Δ_mum_vs_eth`,
  or simply rely on the precomputed fields if present.

**Pros:**

* No changes to the P2P protocol.
* Only the gateway and remote service need to be updated.

**Cons:**

* Requires backend logic to match origin and destination entries.
* Pure mump2p latency is only visible in the backend, not per-gateway
  Prometheus.

---

## Option 2: embed ingress time in P2PMessage (full end-to-end)

This option changes the wire format so that every mump2p message carries
its ingress timestamp from the origin gateway. Then any destination
gateway can compute `L_mum_p2p` locally.

### Extend P2PMessage

In `../optimum-common/pkg/entities/p2p_messages.go`:

```go
type P2PMessage struct {
    SourceNodeID   string `json:"source_node_id"`
    UpstreamPeerID string `json:"upstream_peer_id,omitempty"`
    Topic          string `json:"topic"`
    MessageID      string `json:"message_id"`
    Message        []byte `json:"message"`

    IngressTimeMs  int64  `json:"ingress_time_ms,omitempty"` // new field
}
```

Any component that marshals/unmarshals `P2PMessage` must be updated and
redeployed (gateway, proxy, p2p nodes).

### Set ingress time at origin

At the origin gateway, when a beacon block is received from CL and a
`P2PMessage` is created for mump2p, set:

```go
msg.IngressTimeMs = time.Now().UnixMilli()
```

This is `T_mum_enter`.

### Use ingress time at destination

At the destination gateway, in `handleMessagesFromMumP2PNode`:

```go
receiveAt := time.Now().UnixMilli()

// Pure mump2p propagation
if msg.IngressTimeMs > 0 {
    propagationMs := receiveAt - msg.IngressTimeMs
    telemetry.ObserveMumP2POnly(propagationMs)
}

// Existing beacon block handling
slot := s.handleBeaconBlockFrom(entities.SourceMumP2P, msg.Topic, msg.Message, receiveAt)
```

This gives us:

* `L_mum_p2p = receiveAt - msg.IngressTimeMs` for every message.
* `L_mum_dest = receiveAt - SlotStartTime(slot)` via `sendTrackedSlots`.

We can also extend `LatencyComparator` as in Option 1 so the JSON
payload carries both raw timestamps and precomputed latencies.

**Pros:**

* Each gateway can expose pure mump2p latency in Prometheus.
* No need for backend correlation between origin and destination.

**Cons:**

* Requires coordinated changes across all components that handle
  `P2PMessage`.
* Slightly increases wire payload size.

---

## Making Mum vs Eth latency “clearly visible”

Once we have at least `EthLatencyMs` and `MumLatencyMs` computed (either
side-channel or embedded), our dashboards and remote analytics can show:

* At the level of a given destination gateway, the primary comparison of
  interest is “time from slot start to arrival via Eth” versus “time
  from mump2p injection to arrival via Mum”, as exposed by
  `eth_latency_ms` versus either `mum_p2p_only_ms` (if available) or
  `mum_latency_ms`.

* **Total path latency from slot start:**
    * `eth_latency_ms` vs `mum_latency_ms` per slot.
    * Histograms of both per gateway and globally.
* **Pure mump2p time:**
    * `mum_p2p_only_ms` as a separate histogram / time series.
* **Winner per slot:**
    * `mum_minus_eth_ms`:
        * Negative values ⇒ Mum faster by `abs(value)` ms.
        * Positive values ⇒ Eth faster by `value` ms.

On the Prometheus side, `blockPropagation` already gives:

* `beacon_block_propagation_ms{source="ethp2p"}` and
* `beacon_block_propagation_ms{source="mump2p"}`,

which are effectively `L_eth_dest` and `L_mum_dest`. Option 2 allows us
to add a dedicated histogram:

```go
mumPropagation = NewHistogramWithBuckets(
    "mum_p2p_only_latency_ms",
    subsystem,
    "Propagation time inside mump2p only",
    nil,
    prometheus.ExponentialBuckets(10, 2, 12),
)
```

and a helper:

```go
func ObserveMumP2POnly(latencyMs int64) {
    if enabledMetrics {
        mumPropagation.WithLabelValues().Observe(float64(latencyMs))
    }
}
```

to make pure mump2p latency explicitly visible in metrics.

---

## Clarifying limitations (passive CL listener)

Because the gateway is a passive listener on CL:

* Any metric that uses `SlotStartTime(slot)` measures from *theoretical*
  slot start, not from when the block entered Eth gossip.
* Gateways do not observe the true global “first injection” into Eth
  gossip or mump2p; any network-wide minimum such as `min_p(T_mum_enter)`
  must be computed by the remote service aggregating reports from many
  nodes.
* “Eth latency” at the gateway always includes:
    * Gossip → CL,
    * CL internal processing,
    * CL → gateway (local pubsub / RPC).
* “Mum latency” (`L_mum_dest`) includes:
    * Everything in the Eth path up to the origin gateway,
    * Plus mump2p propagation between origin and destination gateways.

This is why it is important to separate:

* Total path latencies (`EthLatencyMs`, `MumLatencyMs`), from
* Pure mump2p time (`MumP2POnlyMs`).

Document this behavior in dashboards and external docs so users interpret
the graphs correctly.
