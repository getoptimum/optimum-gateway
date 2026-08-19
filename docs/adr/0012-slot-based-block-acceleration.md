# ADR-0012: Slot-based beacon block acceleration

**Status:** Draft
**Date:** 2026-08-18
**Related:** [ADR-0008](./0008-attestation-subnet-boost.md), [ADR-0009](./0009-slot-aware-attestation-gate.md), [ADR-0011](./0011-gateway-consumer-block-stream.md)

---

## Context

Every beacon block currently goes on the Optimum fast path. We want that to be selective: only the slots Optimum has decided are worth accelerating.

The gateway is the wrong place to make that decision. It can't work it out from the block, and even if it could, deciding once the block has arrived is already too late. We want gateways armed for a slot before the block exists.

So the decision is made centrally and gateways are told which slots to accelerate.

## Decision

Bootstrap publishes a list of slot numbers. Gateways poll it and accelerate blocks whose slot is on the list.

```sh
GET /api/v2/:chain/accelerate_slots
Authorization: Bearer <services token>

{
  "chain_id":        "hoodi",
  "to_slot":         39519,
  "slots":           [39488, 39501, 39515],
  "generated_at_ms": 1755500000000
}
```

Slot numbers and nothing else. How the list is chosen is Optimum's business and can change without touching a gateway. The gateway never learns why a slot is on it.

`slots` covers at most two epochs, so 64 entries. It stays that size regardless of how the list grows in future.

### to_slot

`to_slot` is how far ahead the list looks, and it's what makes the list expire:

```text
slot > to_slot   →  don't know yet  →  accelerate
slot in slots    →  on the list     →  accelerate
otherwise        →  not on it       →  normal propagation
```

Without it, a stale or empty list is indistinguishable from "nothing to accelerate right now", so a broken pipeline would silently switch acceleration off everywhere. With it, once `to_slot` is behind us everything reads as unknown and we fall back to accelerating everything, which is what we do today.

A plain TTL doesn't work here: a list built at the start of an epoch is good for nearly 13 minutes, one built at the end for barely 6, so any fixed age either throws away good lists or trusts dead ones. `to_slot` says it exactly.

`generated_at_ms` is for monitoring. If the pipeline stops, `to_slot` still looks healthy for a couple of epochs, and the timestamp is what surfaces it sooner.

### Using it in the gateway

Check the slot on the block header, not the clock:

```text
accelerate(block) = verdict(block.slot) != not_on_list
```

The slot is already decoded before any forwarding decision, so this is one set lookup. Using the header rather than the clock also means a block that turns up a slot or two late is still judged against the slot it belongs to.

The check goes in both directions, because any gateway can be where a block enters the network:

* **mesh → local CL**: do I hand this to my consensus client?
* **local CL → mesh**: do I put this on the fast path at all?

Same answer both times, and it applies to every gateway role.

The fleet-wide propagation switch still runs first and still wins. This gate can only narrow what that switch already allows.

Measurement stays outside the gate. Arrival timing, latency tracking and the ADR-0011 stream all run regardless of the verdict, otherwise we have nothing to compare accelerated slots against.

### Refreshing

The gateway polls on a short interval and swaps the whole list at once. `to_slot` and the slots have to move together, or there's a moment where `to_slot` accepts a slot the set doesn't have yet and we get a wrong "not on the list".

A failed poll keeps the old list rather than clearing it. `to_slot` already handles expiry, so there's no separate TTL to get wrong.

No change detection. The list is small enough that just replacing it is cheaper than working out whether to.

Until the endpoint exists, every slot is unknown and every block gets accelerated, which is exactly what happens today.

## Data flow

Only three things about the serving side affect how the gateway behaves:

* **Never publish a half-finished list.** If a build can't complete, leave the previous list alone. Publishing a short list with a healthy `to_slot` turns acceleration off silently, and it's the one failure the gateway can't spot.
* **`to_slot` only covers what was actually looked at.** Push it further and "unknown" turns into a wrong "not on the list".
* **`to_slot` stays at least an epoch ahead.** That gives every gateway plenty of polls to pick up coverage before those slots arrive.

Manual overrides should apply when the list is served rather than when it's built, so we can force acceleration on or off for a slot range during an incident without waiting for the next build.

### Timing

An epoch is 6.4 minutes. Since Fulu, the slots for the current and next epoch are both determinable ahead of time, which is the headroom this design spends.

```text
list refresh interval + gateway poll < 6.4 min
```

Whatever produces the list today doesn't refresh fast enough to keep `to_slot` an epoch ahead. Blocks in the gap fail open so nothing breaks, but acceleration stops being selective for part of every cycle and looks exactly like it's working. This needs fixing before the feature is meaningfully selective.

## Failure modes

| What happened                                     | What the gateway does                                                                                                                                 |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| No list, stale list, or slot past `to_slot`       | Accelerates, and records it as fail-open rather than a normal decision                                                                                |
| List stops being updated                          | `to_slot` falls behind, coverage lapses, everything fails open. Fixes itself, but it has to alert — the timestamp goes bad well before `to_slot` does |
| Block header won't decode                         | Already dropped before the gate. No slot, no decision                                                                                                 |
| List doesn't match what actually happens on-chain | Gateway can't tell, by design. Reconciled centrally                                                                                                   |

Two things have to be easy to tell apart, because both look like success from the block path: *covered, nothing to accelerate right now*, and *not covered, so everything is failing open*. The horizon and the list age both need to be visible, and the decision counts need to keep on-list, not-on-list and fail-open separate.

## Consequences

* Acceleration becomes selective without the gateway knowing anything about validators or operators.
* The gateway's contract is one endpoint. The selection logic behind it can change freely.
* A stale or missing list degrades to today's behaviour rather than to something worse.
* New dependency on the block path, though only a local lookup.
* Mesh block volume drops to accelerated slots only. Shard distribution and peer scoring were tuned on all-blocks traffic, so this wants validating on Hoodi first.
* Partners lose accelerated delivery on slots that aren't on the list. That's the intent, but it's a change in what we deliver. The propagation switch is the way back if we get it wrong.

## Non-goals

Trader-facing API, Edge signal format, pricing, partner comms, and how the slot list is chosen. Those belong in product specs or follow-up ADRs.
