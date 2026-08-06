# ADR-0009: Slot-Aware Attestation Aggregation Gate

**Status:** Accepted (per-gateway jitter removed 2026-05-27)
**Date:** 2026-04-27

---

## 1. Context

[ADR-008](./0008-attestation-subnet-boost.md) introduced the message router and attestation aggregator. By default the aggregator publishes batches every **25 ms** regardless of where we are in the Ethereum slot. The aggregator was designed to optimize bandwidth — and it does. What it does not do is schedule its publishes around the **block propagation window**.

### The Ethereum slot timeline

```sh
t=0s   slot start, proposer publishes block
t=0–2s block propagation phase (mump2p must deliver the block here)
t=4s   attestation deadline (validators publish attestations)
t=4–8s attestation surge — large mump2p volume
t=8s   aggregation deadline (next slot's aggregator must include attestations)
t=12s  next slot starts
```

Two phases share the same mump2p network:

* **t=0–2 s** → block propagation. Latency-critical for validator duties.
* **t=4–8 s** → attestation surge. Bandwidth-heavy.

### Hypothesis

When attestations from the **previous** slot's tail (t=8–12 s) or the current slot's early stragglers are still being aggregated and published every 25 ms, those publishes contend with the **current slot's block** for mump2p mesh CPU/bandwidth between t=0 and t=2 s.

Even with [ADR-008](./0008-attestation-subnet-boost.md)'s validator filter cutting attestation volume by ~95%, the aggregator still wakes up 40 times per second and emits whatever it has buffered. The cost is small per tick; whether that small cost is enough to delay block propagation by even a few milliseconds is **not measured**.

### Why we are doing this without measurements

The mental model says: blocks are latency-critical, attestations are bandwidth-critical, they should not compete during the block propagation window.

The cost of being wrong is small (a single config flip in the next release reverts to the old behaviour). The cost of measuring before doing it is non-trivial (build the bench harness first, run scenarios that simulate slot timing, compare). We chose to ship the mechanism **on by default** based on the design hypothesis. Bench validation via [optimum-bench](https://github.com/getoptimum/optimum-bench) confirms or refutes the choice **after** ship; if attestation latency regression outweighs block-propagation gain, the default flips back to `0` in a follow-up release.

---

## 2. Decision

Add a configurable **slot-aware publish gate** to the aggregator. Until the gate releases, the aggregator continues to **accumulate** attestations (in `byTopic` and `packer`) but does **not emit** them to mump2p.

### 2.1 Publish-window parameters

In the current code these are **compile-time constants** in `pkg/config/config.go` (not env/yaml configurable), read through getter methods:

| Constant | Value | Getter | Meaning |
| --- | --- | --- | --- |
| `DefaultAttestationPublishAfterMs` | `4000` | `GetAttestationPublishGate()` | Gate — open the publish window 4s into the slot (after the block-propagation window). |
| `DefaultAttestationPublishCapMs` | `8000` | `GetAttestationPublishCap()` | Cap — close the publish window 8s into the slot (Ethereum's attestation aggregation deadline). |
| `DefaultAttestationMaxSlotAge` | `0` | `GetAttestationMaxSlotAge()` | Max slot age (in slots) of attestations the router forwards to mump2p. `0` = current slot only. |

The publish window per slot is **`[gate, cap)` = `[4s, 8s)`**:

* Before `gate`: aggregator accumulates, no emit (block propagation window — block must not be drowned out by attestation traffic)
* Between `gate` and `cap`: emit normally on every 25 ms tick — attestations flow to mump2p
* At or past `cap`: aggregator holds again. Whatever's accumulated waits until next slot's gate

> **Note:** An earlier draft proposed exposing these as `attestation_publish_after_ms` / `attestation_publish_cap_ms` / `attestation_max_slot_age` env/yaml settings (with `= 0` disabling the gate/cap). They are **not** wired to env/yaml today — the getters return the constants above. The gate-disable path (`gate <= 0`) still exists in code, but there is currently no config surface to reach it; making these runtime-configurable is future work.

**Default selection.** 4s aligns with Ethereum's attestation deadline (1/3 of a 12s slot) — by then the block has typically propagated and validators are publishing attestations. The 8s cap matches Ethereum's attestation aggregation deadline (2/3 of the slot). Past 8s, attestations from the current slot are unlikely to make it into an aggregate before the next proposer needs them, so emitting wastes mump2p bandwidth.

### 2.2 Behaviour

```sh
slot N timeline with gate=4000, cap=8000, jitter=1500 (defaults):

  t=0 ────────── 4–5.5 ────────── 8 ───────── 12
  │ accumulate │ emit (25ms)    │ accumulate │
  │ (no emit)  │ window         │ (no emit)  │
  │            │ open           │            │
  ▼            ▼                ▼            ▼
  slot N       gate              cap         slot N+1
  starts       releases          closes      (gate re-arms)
                (jitter,          window
                 see 2.4)
```

Outside the `[gate, cap)` window, the aggregator continues to accept new attestations into `byTopic` and `packer` — they just sit in the buffer. Whatever's accumulated when the next slot's gate releases gets flushed in that slot's window.

A zero gate (`GetAttestationPublishGate() <= 0`) disables the gate completely — restoring pre-ADR behaviour (publish on every 25 ms tick). In the current code the gate is the constant `4000 ms`, so this disabled path is not reachable without a code change.

### 2.3 Logic

In the aggregator loop's tick handler ([aggregator.go](../../pkg/service/aggregator/aggregator.go)):

```go
case <-t.C:
    if a.shouldHoldForSlotGate(time.Now()) {
        // gate active — accumulate, do not emit
        continue
    }
    buildAndEmit()
```

Where:

```go
func (a *Service) shouldHoldForSlotGate(now time.Time) bool {
    if a.cfg == nil {
        return false
    }
    gate := a.cfg.GetAttestationPublishGate()
    if gate <= 0 {
        return false   // gate disabled
    }
    effectiveGate := gate + a.gateJitter   // per-gateway offset.
    slotStart := utils.SlotStartTime(utils.CurrentSlot(now))
    elapsed := now.Sub(slotStart)

    // Before window opens
    if elapsed < effectiveGate {
        return true
    }
    // After window closes (cap is 0 = disabled)
    if cap := a.cfg.GetAttestationPublishCap(); cap > 0 && elapsed >= cap {
        return true
    }
    return false
}
```

Note: the cap is **fixed across gateways** — only the gate has per-gateway jitter. The cap defines a fleet-wide hard "stop emitting" point so that late-slot attestations don't flood mump2p when they have low chance of being included in time.

The aggregator's existing 25 ms ticker keeps running; the gate just suppresses the **emit** half of the loop. Accumulation through `Enqueue → packer.Add` / `byTopic` continues normally because that path is independent of the ticker.

### 2.4 Per-gateway jitter (anti-burst)

Without jitter, every gateway flushes at exactly t=4000 ms, which would create a synchronised mump2p burst across the fleet. To spread flushes across a small window, the aggregator applies a **deterministic per-gateway offset**:

```go
// configured via OPT_ATTESTATION_PUBLISH_JITTER_MS (default 1500)
jitter := cfg.GetAttestationPublishJitter()

// at construction:
offset := sha256(GatewayID)[:8] mod jitter     // stable across restarts
effectiveGate := configuredGate + offset       // in [4000ms, 4000ms + jitter)
```

Properties:

* **Deterministic** — the same `GatewayID` always lands on the same offset, so the behaviour is reproducible during incident debugging
* **No coordination needed** — gateways pick offsets independently from their own IDs
* **Bounded spread** — every effective gate lands in `[configured, configured + jitter)`
* **Safe floor** — jitter only adds delay, never reduces. An attestation is never published before the configured gate

Why deterministic rather than random per startup:

* Reproducibility — incident logs from the same gateway always show the same flush timing
* No risk of the unlucky case where multiple gateways pick similar offsets after a coordinated restart
* No need for a seeded PRNG or extra state

The default jitter range is **1500 ms**. The 500 ms range we shipped initially still produced a synchronised p99 spike at the gate boundary; widening the spread to 1500 ms smooths it. The knob (`OPT_ATTESTATION_PUBLISH_JITTER_MS`) is exposed so the spread can be retuned without a code change — set to 0 to disable jitter entirely.

---

## 3. Followup: per-gateway jitter removed (2026-05-27)

### What changed

Section 2.4 ("Per-gateway jitter") is no longer in effect. The `gateJitter` field on the aggregator, the `gateJitterForGateway` / `jitterFromCfg` / `gatewayIDFromCfg` helpers, and the `OPT_ATTESTATION_PUBLISH_JITTER_MS` / `DefaultAttestationPublishJitterMs` config knob have all been removed. The aggregator now flushes at the configured gate (`attestation_publish_after_ms`, default 4000 ms) on every gateway with no per-gateway offset. The publish window is `[4 s, 8 s)` fleet-wide.

### Why

The anti-burst rationale assumed every gateway publishes the same attestation set — so synchronised flushes at t=4000 ms would produce a fleet-wide mump2p surge that spreading out across `[4000, 5500)` ms would smooth.

That assumption no longer holds. The auth mint response now returns a `validator_indexes` list scoping each gateway to a specific subset of validators, and the message router only forwards attestations from those validators to mump2p ([`ShouldForwardMessageToMumP2P`](../../pkg/service/message_router/service.go)). At gate-release time, each gateway publishes a **disjoint slice** of the attestation set; there is no thundering-herd shape left to smooth out. The 1500 ms jitter window was paying real cost (up to 1.5 s extra attestation latency at the tail) to solve a problem that doesn't exist anymore.

### If the burst comes back

If validator-scoping doesn't end up evenly distributing publish load (e.g., one operator's gateway serves a disproportionately large validator set), the right response is **bounded jitter on the tick rate** (ms-scale, inside the publish window), not the slot-level offset we just removed. Revisit this section before re-introducing the slot-offset mechanism.
