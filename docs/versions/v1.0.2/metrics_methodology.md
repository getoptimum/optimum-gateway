# Metrics Methodology

Optimum Gateway telemetry follows a two-layer design: **gateways emit raw per-block and per-attestation events**, and **Bootstrap computes stable KPIs** from those events.


## Capture the Critical Path

The system sits in the hot path of validator rewards: blocks, attestations, and gossip messages passing between Ethereum CL clients and the Optimum network acceleration layer. We need visibility into three things:

* **Throughput** -> Are we carrying the expected traffic volume?
* **Latency** -> Are we delivering faster than vanilla libp2p?
* **Reliability** -> Are we dropping or mangling messages?

Hence metrics are anchored at the ingress (CL -> Gateway) and egress (Gateway -> Optimum) points, and mirrored on the reverse path.

## Two-Layer Architecture

### Gateway (raw events)

Gateways report **raw timestamps** per block:

* `t_eth_seen_ms` - First time this gateway saw the block via libp2p
* `t_mum_seen_ms` - First time this gateway saw the block via mump2p
* `t_mum_published_ms` - Time this gateway published the block into mump2p (if publisher)

And per attestation:

* `attestation_evaluated_total` / `attestation_forwarded_mump2p_total` - Router evaluation vs forward (inclusion rate)
* `attestation_dropped_total` - Drops by reason
* `attestation_inclusion_delay_slots` - Slot distance between attestation `data.slot` and the wall-clock slot at receive
* `attestation_propagation_latency_ms` - End-to-end from sender emit to receiver decode
* Attestation pack metrics - items, size, latency, unique data keys (dedup effectiveness)

Plus:

* Per-topic message counters (libp2p, mump2p)
* Peer connectivity gauges
* Block/attestation metadata (slot, proposer/attester index, size)
* Attestation subnet distribution counters

These are operational signals. Raw libp2p vs mump2p deltas are **debug only** — they are not stable KPIs for validator outcomes.

### Bootstrap (computed KPIs)

Bootstrap aggregates raw events and computes **stable baselines**. It publishes histograms (p50/p95/p99) and coverage ratios.

#### Global first-seen: gap to best-in-population

For each block, Bootstrap defines:

* `t_any_seen(g)` = earliest time gateway `g` saw the block (libp2p or mump2p)
* `t_global_first_seen` = earliest time any gateway saw the block
* **gap_to_best_ms(g)** = `t_any_seen(g) - t_global_first_seen`

*Answers: How close is this gateway to best-in-population?* Independent of proposer publish timing.

#### mump2p spread from first publisher (transport quality)

Once a block has entered mump2p, Bootstrap measures how fast other gateways see it:

* `t_mum_enter_first` = time the first gateway published the block into mump2p
* **mum_spread_ms(g)** = `t_mum_seen(g) - t_mum_enter_first` (only when gateway received via mump2p)

*Answers: After the block enters Optimum, how quickly do other gateways receive it?*

## Measure Each Layer Separately

### Message Counters

Counters per topic (libp2p, mump2p) to isolate heavy topics.

### Latency KPIs (Bootstrap)

* **gap_to_best_ms** - Delay vs best observed first-seen time in the gateway population (independent of proposer publish timing)
* **mum_spread_ms** - Delay from first mump2p publish to receiver (Optimum transport quality)

### Attestation KPIs

* **Inclusion rate** - `forwarded / evaluated` — what fraction of CL attestations pass the router filter
* **Propagation latency** - `attestation_propagation_latency_ms` (p50/p95) — sender emit to receiver decode across mump2p
* **Inclusion delay** - `attestation_inclusion_delay_slots` — slot freshness of attestations at receive time
* **Pack efficiency** - pack items / unique data keys — how effectively attestations are batched and deduped

### Peer Connectivity

Gauges for peer counts, counters for connect/disconnect events (gateway-level).

### Error Signals

Counters for bad messages (decode failures, failed publishes, pack errors).

## Enable Operations & Research

Metrics support uptime dashboards, validator ROI analysis, and protocol research.

See [Metrics Reference](metrics.md).
