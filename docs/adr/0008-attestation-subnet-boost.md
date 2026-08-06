# ADR-0008: Attestation Subnet Boost via Validator-Scoped Filtering

**Status:** Accepted
**Date:** 2026-04-17

---

## Context

This ADR builds on [ADR-001](./0001-gateway-architecture.md) (gateway message flow) and complements [ADR-003](./0003-validator-metrics.md) / [ADR-004](./0004-hop-by-hop-latency-tracking.md) (validator-outcome metrics and hop tracking).

### The problem

Before this change, the gateway forwarded **every** attestation it received from the CL into mump2p. With 64 attestation subnets, ~30,000 attestations per slot across the Ethereum network, and N gateways each doing this independently, the network saw massive duplication:

```sh
Gateway A's CL sees: [att_1, att_2, att_3, att_4, att_5]
Gateway B's CL sees: [att_1, att_2, att_3, att_6, att_7]
Gateway C's CL sees: [att_1, att_2, att_3, att_8, att_9]

All three publish their full set → att_1/2/3 cross mump2p 3× each
Receiving gateways XXHash-dedupe on arrival → ~80% wasted bandwidth
```

The receiver-side dedup (`isDuplicateMessage` via XXHash) prevented duplicate delivery to the CL, but the bytes had already traveled over mump2p.

Additionally, forwarding attestations from **non-partner validators** provides no value to the mump2p mesh — other gateways' CLs already see those attestations through normal Ethereum gossip. The gateway's role is to accelerate the partner's validators, not to re-broadcast the entire Ethereum network.

### Goal

Forward **only attestations from known partner validators** to mump2p. Trust that other gateways do the same for their own partners — the union across all gateways covers exactly the partners collectively hosted by the network.

---

## Decision

### New service: `message_router`

A dedicated service decides, per message, whether to forward in each direction. Two methods:

```go
ShouldForwardMessageToMumP2P(topic, payload) bool   // CL → mump2p
ShouldForwardMessageToCLP2P(topic, payload) bool    // mump2p → CL
```

### Validator scope from the auth token

The router maintains an in-memory `knownValidators` set (validator indices the partner operates). In the current implementation this is **not** a dedicated bootstrap endpoint — the validator indices arrive as a `validator_indexes` claim inside the JWT the gateway mints from the auth service (`POST {RemoteAuthURL}/api/v1/auth/token`, in `pkg/service/auth_token`). The router then keeps `knownValidators` in sync locally:

* `pkg/service/message_router/bg_sync.go` polls `authMgr.ValidatorIndexes()` every **30s** and replaces the `knownValidators` map.
* The underlying token (and therefore the index list) is refreshed by the auth manager roughly every **~3h** (token lifetime).
* On error / empty: keep the current list (fail-open — better stale than empty).

> **Note:** An earlier draft of this ADR described a dedicated `GET /api/v1/validators?chain_id=… ` endpoint with `X-Current-Hash` / `304 Not Modified` hash-caching. That endpoint was not built; the `validator_indexes`-in-JWT mechanism above is what ships.

### Asymmetric filtering

| Direction       | Beacon block                               | Attestation                                      | Other |
| --------------- | ------------------------------------------ | ------------------------------------------------ | ----- |
| **CL → mump2p** | forward always                             | forward **only if `attester ∈ knownValidators`** | drop  |
| **mump2p → CL** | forward only when `paired_with == partner` | forward always (trust upstream filter)           | drop  |

**Why asymmetric:** inbound attestations from mump2p already passed another gateway's validator filter — it's the partner's attestation that some *other* gateway shouldered. Filtering again is wasted work; CL gossipsub validates on receipt.

### Lightweight SSZ parsing before full decode

`ShouldForwardMessageToMumP2P` runs **before** `DecodeGossip`. For attestations it peeks at the SSZ bytes to extract `attesterIndex` and `slot` without a full decode:

```go
attester, slot, err := utils.ParseAttestationSSZTopic(payload)
```

This saves the cost of a full SSZ decode on every attestation that gets dropped.

### Staleness drop

Attestations more than 3 slots old (past or future) are dropped regardless of validator membership:

```go
if diff := utils.DiffUint64(slot, utils.CurrentSlot(time.Now())); diff > 3 {
    return false
}
```

### `PairedWith` modes

A new config field `paired_with` determines gateway deployment intent:

| Value               | Meaning                                     | Block forwarding mump2p → CL |
| ------------------- | ------------------------------------------- | ---------------------------- |
| `partner` (default) | Paired with a partner CL running validators | **yes**                      |
| `hermes`            | Paired with a Hermes lightweight peer       | no                           |
| `relay`             | Paired with a relay node                    | no                           |

Attestation forwarding is identical in all modes (always forward mump2p → CL). Only beacon block re-forwarding to CL differs.

---

## Data flow

> **Note:** The function names in the diagrams below are design-time labels. In the current code the CL-side handling is split into `processCLBeaconBlock` / `processCLAttestation` (not a single `processCLMessage`), and the CL publish step is `publishToCLTopic` (not `decodeEncodePublish`). `isDuplicateMessage`, `handleAggregatedMessages`, `ShouldForwardMessageToMumP2P`, and `ShouldForwardMessageToCLP2P` match the code.

### Outbound (CL → mump2p)

```sh
CL gossipsub delivers beacon_attestation_31
        ↓
processCLMessage()
        ├─ shouldProcessBeaconBlock() — AB testing, slot staleness
        ├─ isDuplicateMessage() — XXHash dedup
        ├─ ShouldForwardMessageToMumP2P(topic, payload)      ← NEW
        │   ├─ lightweight SSZ parse → (attester, slot)
        │   ├─ slot > current+3 → drop (stale)
        │   ├─ attester ∉ knownValidators → drop (non-partner)
        │   └─ else → forward
        │
        ├─ full DecodeGossip (only reached for partner attestations)
        │
        └─ EnableAggregation?
            ├─ yes → aggregator.Enqueue(topic, payload)   — 25ms batching
            └─ no  → nodeMumP2P.PublishMessage()          — direct
```

### Inbound (mump2p → CL)

```sh
mump2p delivers aggregated message OR direct message
        ↓
processMumP2PMessage()
        ├─ messageFromSelf check
        ├─ isDuplicateMessage() — XXHash dedup
        ├─ if aggregated topic → handleAggregatedMessages (decompose)
        │
        └─ for each decomposed message:
             decodeEncodePublish()
               ├─ ShouldForwardMessageToCLP2P(topic, _)    ← NEW
               │   ├─ attestation → yes
               │   ├─ beacon_block && paired_with==partner → yes
               │   └─ else → drop
               │
               └─ publish to CL libp2p topic
```

---

## Bandwidth impact

Before this change:

```sh
3 gateways × ~2000 attestations/slot published each
≈ 6000 mump2p publishes/slot, ~4800 (80%) deduped on arrival
```

After:

```sh
3 gateways × only partner attestations published
≈ variable depending on partner validator count
If each partner owns ~700 validators: 3 × ~30 attestations/slot ≈ 90 publishes/slot
```

**~98% reduction in mump2p attestation traffic** with equivalent coverage of partner validators.

Combined with aggregator batching (25ms buckets), the mump2p attestation load is further reduced to a small number of aggregated messages per slot.

---

## Telemetry

New Prometheus counters (gateway-local):

| Metric                              | Labels        | Purpose                                           |
| ----------------------------------- | ------------- | ------------------------------------------------- |
| `attestation_evaluated_total`       | —             | Every attestation passed through the router       |
| `attestation_forwarded_mump2p_total` | —            | Forwarded to mump2p                               |
| `attestation_dropped_total`         | `reason`      | Dropped — reason ∈ {parse_error, stale, rejected} |
| `attestation_inclusion_delay_slots` | — (histogram) | Distribution of slot diff at filter time          |
