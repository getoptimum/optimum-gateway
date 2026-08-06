# ADR-0003: Redesign Optimum Gateway metrics around validator outcomes

**Status:** Accepted  
**Date:** 2026-01-07

## Context

[ADR002](./0002-beacon-block-latency.md) metrics have some problems. 
Current `arrival latency` style metrics (e.g., `recv_time - slot_start`, and `mum_minus_eth_ms`) have 3 structural issues:

1. Slot-start latency mixes two different things
   1. **Proposer/relay publish timing** (MEV-Boost relays, proposer timing games) can delay blocks deep into the slot (e.g., ~2–3.5s is common in timing games). That delay is not under our control. [ref](https://ethresear.ch/t/on-attestations-block-propagation-and-timing-games/20272)
   2. **Propagation latency** (what Optimum improves) is only a part of what we are measuring.
2. `mum_minus_eth_ms` is biased for **publisher gateways** If a gateway received from `ethp2p` first and published into `mump2p`, then for that gateway:
   1. Eth will always be first.
   2. mump2p `arrival` at the same gateway is meaningless for win/loss.
3. **Cross-gateway comparisons are time-sync fragile** (clock drift) If we compare timestamps across machines, we need explicit handling of clock drift.

Additional product reality:

1. **Validator-as-proposer slots behave differently**
When the validator is the proposer for a slot, publish timing is dominated by their own pipeline (MEV-Boost, signing, BN load), not network propagation. A validator dashboard must identify proposer slots and avoid mixing proposer slots into **attestation outcome** interpretation.
2. **Validator dashboard should not query bootstrap directly**
We want open dashboard and it works without Prometheus, scraping, or interactive queries to bootstrap. Bootstrap should publish stable KPI snapshots (publication/snapshot API is **not yet implemented** — planned for a future change).

## Decision

We redesign metrics around a strict separation:

1. **Gateway emits raw per-block events** (source timestamps). These are not KPIs.
2. **Bootstrap collector computes baselines + stable KPIs** from those raw events.
3. **Dashboards are driven by stable KPIs** (not raw Eth vs Mum deltas).
   1. Validator dashboard (what validators care about)
   2. Global dashboard (operator view + product KPI)
4. **Proposer-awareness is computed at bootstrap (not in gateways)**
   1. Bootstrap extracts `proposer_index` from each block.
   2. Bootstrap joins `proposer_index ↔ partner` using Postgres mapping tables maintained by Optimum.
   3. Bootstrap logs and publishes additional proposer-specific KPIs when a partner is proposer.
5. **Bootstrap publishes KPI outputs to a snapshot API** *(not yet implemented — planned for a future change)*
   1. Public global snapshot (everyone)
   2. Partner snapshot (token-based approach to be evaluated later)
   3. Dashboards read from snapshots by default; Prometheus is optional for operators.
   
## Definitions

Let block `b` be identified by slot `s`. Gateways do not send `block_root`; bootstrap treats `(slot, observed_proposer_index)` as the unique block identity if multiple blocks appear for the same slot.

### Raw timestamps (gateway → bootstrap)

Each gateway `g` reports these raw fields:

* `gateway_id`
* `block_slot`
* `validator_index` — proposer index inside the observed block
* `block_size` — size of the block message in bytes
* `t_eth_seen_ms(g,b)` — first time gateway `g` saw `b` via `ethp2p`
* `t_mum_seen_ms(g,b)` — first time gateway `g` saw `b` via `mump2p`
* `t_mum_published_ms(g,b)` — time gateway `g` published `b` into `mump2p (only if publisher)`

Helper definitions (raw → derived per gateway)

* `t_any_seen_ms(g,b)` = `min_nonzero(t_eth_seen_ms(g,b), t_mum_seen_ms(g,b))`
* `mum_minus_eth_ms(g,b)` = `t_mum_seen_ms(g,b) - t_eth_seen_ms(g,b)` (debug only)

### Transport / Network KPIs (Optimum-controlled)

**Goal:** Measure what Optimum actually controls (its transport + routing), without getting fooled when ethp2p becomes “Optimum-fed Eth” after gateways publish blocks into the CL mesh.

**Why the old `mum_minus_eth_ms` is unstable**

The core issue is not `Optimum-fed Eth` as a certainty, it’s that after the first few gateways observe a block, the system becomes coupled and multi-path:
  
* A block can reach a gateway over ethp2p, mump2p, or both.
* Gateways themselves can re-publish into CL gossip.
* Therefore, for any gateway `g`, the first-seen transport can flip even if Optimum improved overall network propagation.

So `MumMinusEthMs = t_mum_seen(g) - t_eth_seen(g)` can become positive because:

* Eth might reach `g` from any peer that has the block earlier (regardless of how that peer got it),
* while Mum might arrive slightly later to `g`,
* and the sign does not tell us `Optimum lost`, it only tells us `this gateway saw Eth before Mum`.

Therefore: raw `Eth vs Mum delta` is a debug signal, not a **stable KPI**.

### What we measure instead: stable relative propagation KPIs

We introduce two types of `baselines`, `computed at the bootstrap collector`.

#### Baseline 1 — Global First-Seen baseline (stable "competitiveness vs best")

For each block `b`:

* Define, per gateway `g`: `t_any_seen(g,b) = min_nonzero(t_eth_seen(g,b), t_mum_seen(g,b))`
* Define: `t_global_first_seen(b) = min_g t_any_seen(g,b)`
* stable KPI: `gap_to_best_ms(g,b) = t_any_seen(g,b) - t_global_first_seen(b)`

This gives **how far behind best-in-population was this gateway for this block**, independent of proposer publish time.

#### Baseline 2 — “Spread from first publisher into Optimum” (stable Optimum transport KPI)

To isolate Optimum routing/transport, we need a baseline that starts when the block enters Optimum.

For each block `b` define: 

* `t_mum_enter_first(b) = min_g t_mum_published_ms(g,b) where t_mum_published_ms(g,b) > 0`
* `mum_spread_ms(g,b) = t_mum_seen(g,b) - t_mum_enter_first(b)`. computed only if `t_mum_seen(g,b)>0` and `t_mum_enter_first(b)>0` (so it exists only for blocks that entered mump2p and were observed via `mump2p`)
* Optionally (for debugging, not KPI): 
    * `t_eth_first_seen(b) = min_g t_eth_seen(g,b) where t_eth_seen(g,b)>0`
    * `eth_spread_ms(g,b) = t_eth_seen(g,b) - t_eth_first_seen(b)`

Important: `t_mum_published_ms(g,b)` must be timestamped at the actual publish to mump2p, not inferred from "eth received" time.

This answers: **Once any gateway publishes the block into Optimum, how quickly do other gateways receive it via mump2p?**

### Proposer-slot event logging (partner is scheduled proposer)

We must log proposer status based on **scheduled proposer duties**, not only observed blocks.

For each slot `s` where `scheduled_proposer_partner_id(s) != null`, bootstrap logs a proposer-slot event row.

`proposer_slot_events`

Per slot `s`:

* `slot`
* `scheduled_proposer_index`
* `scheduled_proposer_partner_id`

`Observed block linkage:`

* `observed_block_root`
* `observed_proposer_index`
* `observed_proposer_partner_id`
* `did_propose` (bool, only if observed block exists): `did_propose = (observed_proposer_index == scheduled_proposer_index)`


### Examples (why mum_minus_eth_ms flips sign, and why KPIs stay stable)

For a block b and gateway g:

* `t_eth_seen(g,b)` = when `g` first sees `b` from ethp2p
* `t_mum_seen(g,b)` = when `g` first sees `b` from mump2p
* `t_mum_published(g,b)` = when `g` publish `b` into mump2p (publish-to-mump2p)

why `mum_minus_eth_ms` flips sign even when Optimum helps, and how the redesigned metrics stay stable.

#### Scenario 1 — Eth-first everywhere (Optimum irrelevant for this block)

Block reaches everyone via Eth fast; Mum arrives later or not at all.

| gateway | t_eth_seen | t_mum_published| t_mum_seen |
| ------- | ---------: | -------------: | ---------: |
| g1      |        120 |              0 |          0 | 
| g2      |        170 |              0 |        400 |
| g3      |        210 |              0 |          0 | 


**Compute:**

* `t_any_seen`: g1=120, g2=170, g3=210
* `t_global_first_seen` = 120
* `gap_to_best_ms`: g1=0, g2=50, g3=90

`t_mum_enter_first` exists? only if any `t_mum_published>0 → none`, so `mum_spread_ms` is `N/A` for this block.

**Interpretation:**

* `Network outcome:` g2 and g3 are behind best by 50/90ms.
* `Optimum transport KPI:` not applicable (no publisher), which is correct.

#### Scenario 2 - Optimum helps where it can: Mum-first at non-publisher (fast spread)

| gateway       | t_eth_seen | t_mum_published| t_mum_seen |
| ------------- | ---------: | -------------: | ---------: |
| g1 (publish)  |        100 |            115 |        125 |
| g2            |        210 |              0 |        150 |
| g3            |        240 |              0 |        165 |

* `t_any_seen`:
    * g1 = min(100,125)=100
    * g2 = min(210,150)=150
    * g3 = min(240,165)=165
* `t_global_first_seen` = min(100,150,165)=100
* `gap_to_best_ms`:
    * g1 = 0
    * g2 = 150-100 = 50
    * g3 = 165-100 = 65
* `t_mum_enter_first` = 115
* `mum_spread_ms`:
    * g1 = 125-115 = 10
    * g2 = 150-115 = 35
    * g3 = 165-115 = 50

This is the real **Optimum helps** story for today’s design: **spread from first publisher**.

#### Scenario 3 - Mixed paths (Eth-first at some non-publisher even when Optimum is good)

Because the network is coupled + multipath, `mum_minus_eth_m`s can flip sign at non-publisher too.

| gateway       | t_eth_seen | t_mum_published| t_mum_seen |
| ------------- | ---------: | -------------: | ---------: |
| g1 (publish)  |        100 |            115 |        125 |
| g2            |        145 |              0 |        160 |
| g3            |        230 |              0 |        155 |


Debug deltas:

* g2: `mum_minus_eth_ms` = 160-145 = +15 (Eth-first)
* g3: `mum_minus_eth_ms` = 155-230 = -75 (Mum-first)

Stable KPI:

* `t_any_seen`:
    * g1=100
    * g2=145
    * g3=155
* `t_global_first_seen`=100
* `gap_to_best_ms`: g2=45, g3=55

Optimum spread:

* `t_mum_enter_first`=115
* `mum_spread_ms`: g2=45, g3=40

g2 being Eth-first **does not mean Optimum lost**; it just means g2’s Eth path beat its mump2p path for that block.

#### Scenario 4 — Multi-publisher race (two gateways publish same block)

| gateway | t_eth_seen | t_mum_published| t_mum_seen |
| ------- | ---------: | -------------: | ---------: |
| g1      |        100 |            140 |        150 |
| g2      |        120 |            130 |        145 |
| g3      |        220 |              0 |        170 |

Compute:

* `t_mum_enter_first` = min(140,130)=130 (g2 published first)
* `mum_spread_ms`:
    * g1 = 150-130 = 20
    * g2 = 145-130 = 15
    * g3 = 170-130 = 40

Stable KPI:

* `t_any_seen`: g1=100, g2=120, g3=170
* `t_global_first_seen`=100
* `gap_to_best_ms`: g2=20, g3=70

Multi-publisher is fine as long as the baseline is `first publisher`.

#### Scenario 5 — Partial observability (missing Eth or missing Mum)

| gateway       | t_eth_seen | t_mum_published| t_mum_seen |
| ------------- | ---------: | -------------: | ---------: |
| g1 (publisher)|        100 |            115 |        125 |
| g2            |          0 |              0 |        155 |
| g3            |        210 |              0 |          0 |

* `t_any_seen`: g1=100, g2=155, g3=210
* `t_global_first_seen`=100
* `gap_to_best_ms`: g2=55, g3=110
* `t_mum_enter_first`=115
* `mum_spread_ms`: g2=40, g3=N/A

If `t_mum_seen(g,b)=0`, then:

* `mum_spread_ms(g,b)` is undefined (no mump2p receipt)
* `gap_to_best_ms(g,b)` still works using Eth if present

**Takeaway:** `gap_to_best_ms` remains a **population KPI**, `mum_spread_ms` remains a **transport KPI** wherever Mum receipts exist.

#### Scenario 6 — Clock drift (why bootstrap must compute baselines)

* Cross-gateway timestamp comparisons are unsafe unless corrected.
* here `t_global_first_seen` naively comparing raw gateway wall-clock timestamps may create issue (clock-drift — open item).
* Bootstrap calculation can help.

### The actual new "KPI outputs" (what bootstrap publishes as metrics)

Bootstrap produces KPIs aggregated over a time window.

> **Implementation note (verified against code):** The metric names in the groups below are *design-time* names, and they conflate two different layers. In the current bootstrap code:
> - The **JSON snapshot** struct (`internal/entities`) uses percentile-suffixed keys: `opt_gateway_gap_to_best_ms_{50,95,99}`, `opt_gateway_mum_spread_ms_{50,95,99}`, `opt_mum_spread_coverage_{200,500,1000}`, `opt_mum_publish_rate`, `opt_missing_eth_rate`, `opt_missing_mum_rate` (partner-scoped `mum_seen_rate` is **un-prefixed**).
> - The **Prometheus** layer (namespace `optp2p_bootstrap` / subsystem `optimum_bootstrap`) uses **un-prefixed base names**: `gap_to_best_ms`, `mum_spread_ms`, `mum_spread_coverage_{200,500,1000}`, `missing_eth_rate`, `missing_mum_rate`, `mum_publish_rate`, `gap_to_best_ms_max`. The `opt_`/`opt_gateway_` prefix and the `_50/_95/_99` split exist only in the JSON snapshot, not at the Prometheus layer.
> - Names below that appear in **neither** layer (e.g. `opt_gateway_gap_to_best_p95_ms`, `opt_gateway_gap_to_best_within_ms`, `opt_gateway_event_missing_rate`, and the clock-drift group `opt_gateway_clock_offset_ms` / `opt_gateway_clock_rtt_ms`) are **proposed, not yet implemented**.

#### KPI group A — Gateway competitiveness vs best (per gateway)

From per-block `gap_to_best_ms(g,b)`:

* `opt_gateway_gap_to_best_ms{gateway_id}` → histogram
    * Grafana shows p50/p95/p99
* `opt_gateway_gap_to_best_p95_ms{gateway_id}` → gauge (derived from histogram quantile)
* `opt_gateway_gap_to_best_within_ms{gateway_id,threshold="50|100|200"}` → ratio
    * `% blocks where gap_to_best` <= threshold

Interpretation: **How close is this gateway to the best observed first-seen time inside our population?**

#### KPI group B — Optimum spread quality (global + per gateway)

From per-block `mum_spread_ms(g,b)` (only blocks that had a publish event):

* `opt_mum_spread_ms{gateway_id}` → histogram
    * show p50/p95/p99 per gateway
* `opt_mum_spread_coverage{threshold="200|500|1000"}` → ratio (global)
    * For each block, compute `% gateways with mum_spread <= threshold`, then average over window
* `opt_mum_publish_rate` → ratio (global)
    * `% blocks where t_mum_enter_first exists`
* `opt_mum_seen_rate{gateway_id}` → ratio
    * `% published blocks where this gateway actually saw block via Mum`

Interpretation:

* Publish rate tells: **How often the network is getting blocks into mump2p**
* Spread + coverage tells: **Once in mump2p, how fast and how broadly it propagates**

#### KPI group C — Data quality + time sync safety (clock drift — open item)

* `opt_gateway_event_missing_rate{gateway_id,source="eth|mum"}`, example:
    * missing eth seen
    * missing mum seen
* `opt_gateway_clock_offset_ms{gateway_id}` (estimated offset to bootstrap clock)
* `opt_gateway_clock_rtt_ms{gateway_id}` (for health)

Open item: clock-drift handling.

### What we downgrade (debug only)

* `mum_minus_eth_ms(g,b) = t_mum_seen - t_eth_seen` is debug only
    * it answers: “which path won locally on this gateway for this block”
    * it does not answer: “did Optimum win globally”
* `recv_time - slot_start` metrics remain for debugging slot timing games, but not KPI.

## End visualization (what dashboards actually show)

### Validator dashboard (single gateway / validator region view)

What validators want:

* Am I seeing heads near-best, consistently?
* Is Optimum helping my delivery path?
* Do I have reliability issues?
* Does this translate to rewards? (validator-rewards mapping — planned, not yet written)

1. Competitiveness vs best (PRIMARY) that tells **Your gateway is within `X ms` of the best observed `first-seen` time for `95%` of blocks.**
   1. `gap_to_best_p95_ms`
   2. `gap_to_best_p50_ms`
   3. `% within 100ms`
2. Optimum delivery speed after publish (Optimum-controlled) that tells **After first Optimum publish, you receive via Optimum within `Y ms` p95**
   1. `mum_spread_p95_ms`
   2. `mum_spread <= N ms (as % of publish blocks)`
3. Reliability (missing rates):
   1. `missing_mum_rate` (only on published block)
   2. `missing_eth_rate`
4. Reward-related metrics will be covered in a future validator-rewards ADR (planned, not yet written)

### Global dashboard (operator + product KPI)

**What we want globally:**

* How close to best are gateways (network positioning + peering)?
* How fast does Optimum spread once it has the block?
* Is publish happening consistently?
* Any region degraded?

1. Gateway ranking table
   1. `gap_to_best_p95_ms`
   2. `% within X ms`
   3. `missing_mum_rate`
   4. sort by `gap_to_best_p95_ms`
2. Population competitiveness distribution
   1. histogram/quantile timeseries global `p50/p95` of `gap_to_best_ms` across all gateways.
3. Optimum spread quality
   1. global timeseries `mum_spread_p50/p95/p99` (across gateways, published blocks only)
   2. coverage timeseries `coverage_200ms`, `coverage_500ms`
4. Publish rate, `opt_mum_publish_rate` and top publisher (`% blocks where gateway publish first`)
5. Data quality: `missing rates per region`
