# ADR-0012: Selective Block Forwarding on Partner-Proposed Slots

**Status:** Approved (implementation pending)
**Date:** 2026-08-18

---

## Context

Block forwarding from mump2p into the local CL is decided per gateway type
([ADR-0008](./0008-attestation-subnet-boost.md) `PairedWith` modes, now carried as the `type`
claim on the gateway token):

| Type      | Block forwarding mump2p → CL |
| --------- | ---------------------------- |
| `partner` | yes                          |
| `hermes`  | no                           |
| `relay`   | no                           |

A `hermes` gateway is paired with a [Hermes](https://github.com/probe-lab/hermes) peer, which
participates in the public Ethereum gossip network rather than running validators. A block
handed to it therefore propagates outward, to the network, rather than inward to a validator
client. That is the opposite direction from a `partner` pairing, and it is why the two are
treated differently.

### Problem

The choice is all-or-nothing per gateway, and the useful case is narrow. When one of our
partners proposes a block, getting that block onto the public network quickly is valuable: it is
the block's own propagation race, and losing it risks the proposal being reorged out. When
anyone else proposes, forwarding the block to a Hermes peer adds nothing — the network already
carries it, and the peer would receive it through normal gossip regardless.

So `hermes` gateways withhold everything, and the one case worth forwarding is withheld with it.

---

## Decision

A `hermes` gateway forwards a mump2p block into its CL **iff the block's slot appears in an
allowlist supplied by the control plane**. `partner` and `relay` behaviour is unchanged.

| Type      | Block forwarding mump2p → CL      |
| --------- | --------------------------------- |
| `partner` | yes                               |
| `hermes`  | **only for allowlisted slots**    |
| `relay`   | no                                |

The allowlist contains upcoming slots whose proposer is one of our partners' validators. It is a
rolling forward window: the control plane computes it from proposer duties and the gateway polls
for it.

### Why the allowlist is keyed on slots, not validators

The gateway already has the slot: it is decoded from the block header on the inbound path and
currently discarded. Matching a slot against a set is a lookup on data in hand.

Keying on validators instead would mean shipping the partner validator set to every gateway and
keeping it current. The gateway holds such a set already for attestations (ADR-0008), and it is
the larger and more sensitive of the two — a slot allowlist conveys only *when* to act, and
nothing about whose validators are involved.

### Why proposer duties, and not attester duties

Both are knowable in advance, and only one is selective. The difference is structural rather
than a property of our current holdings:

* **Every active validator attests exactly once per epoch.** For any validator set of
  non-trivial size, "does this set have an attestation duty in this slot" is true for
  essentially every slot. As a gate it degenerates to always-on and carries no information.
* **Exactly one validator proposes each slot.** The gate's selectivity is therefore the set's
  share of the active validator set — sparse by construction, and sparse for any set that is not
  most of the network.

This also explains why the reasoning does not carry over from attestation handling. Attestation
forwarding asks whether *we* need a message. This asks whether the network needs *ours*.

### Fail-closed

An empty allowlist, a stale one, or an unreachable control plane means **withhold**. That is the
current `hermes` behaviour, so a control-plane failure degrades to today rather than to
forward-everything. The allowlist response carries the time it was computed so the gateway can
tell "nothing to forward right now" from "nothing has been computed for a while", and report the
two differently.

---

## Client sketch

Not implemented in this ADR. The shape:

* Poll the control plane for the allowlist into a set, swapped wholesale, alongside the
  computation time. Modelled on the existing validator-set sync in `message_router`.
* Bind the slot already returned by `processBeaconBlockArrival` on the mump2p path instead of
  discarding it, and pass it to the gate. No second decode.
* Extend the block branch of `ShouldForwardMessageToCLP2P` with the slot, or add a sibling that
  takes it — the existing signature accepts a payload argument it does not use.
* Count forwarded, skipped-not-allowlisted and skipped-stale-allowlist separately. Without that
  split a stalled allowlist is indistinguishable from a quiet one.

Polling is sufficient because the signal is predictive: proposer duties are knowable about two
epochs ahead, so the window is always well ahead of the slots it covers. No push channel is
required.

---

## Rejected alternatives

* **Carry the allowlist in the gateway token.** It changes every epoch, and the token is minted
  on a much longer cycle. Refreshing the token to refresh a slot list couples two unrelated
  lifetimes.
* **Push the signal over the mesh.** mump2p carries block and attestation topics; there is no
  control channel, and a predictive window removes the need for one.
* **Reuse the fleet-level propagation toggle.** Wrong granularity — it is a per-cluster setting,
  not per-slot — and it is evaluated before the type gate, so it cannot express "off in general,
  on for these slots".
* **Forward every block from `hermes` gateways.** Simple, but it re-broadcasts traffic the
  network already carries, for no gain on the slots that matter.

---

## Consequences

* `hermes` becomes a conditional mode rather than a fixed one. The type alone no longer
  determines block forwarding, so anything reasoning about gateway behaviour from the type must
  also consider the allowlist.
* A new control-plane dependency on the block path, made safe by failing closed.
* The gate is inert until a client ships and an allowlist is served, so it can land ahead of
  either.
* Any mechanism that sets the `type` claim fleet-wide will change which gateways consult the
  allowlist at all. Selective forwarding and a fleet-level type switch overlap and should not be
  changed independently.

---
