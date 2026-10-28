# ADR-0014: Benchmarking the slot-aware attestation gate

**Status:** Accepted (implemented)
**Date:** 2026-10-28
**Related:** [ADR-0008](./0008-attestation-subnet-boost.md), [ADR-0009](./0009-slot-aware-attestation-gate.md)
**Supersedes:** [ADR-0009](./0009-slot-aware-attestation-gate.md)

---

## 1. Context

[ADR-0009](./0009-slot-aware-attestation-gate.md) holds the aggregator's mump2p emit back during the early part of each slot. Its rationale is a hypothesis: attestation publishes compete with the current slot's block for mump2p bandwidth and CPU, so delaying them protects block propagation. ADR-0009 shipped the gate **without measurements** and asked for bench validation after the fact.

The gate that is in the code today is the block-aware release from ADR-0009 §4:

```text
release = last_block + 2s   when a beacon block arrived in this slot
          slot_start + 4s   otherwise
emit while release <= now < slot_start + 8s
```

This ADR records the first measurement of that hypothesis. It compares the gate against emitting on every 25 ms aggregation tick (gate disabled), under synthetic attestation load, on the [optimum-bench](https://github.com/getoptimum/optimum-bench) Shadow harness.

---

## 2. Experiment

### 2.1 Harness

[optimum-bench](https://github.com/getoptimum/optimum-bench) runs real gateway binaries on the [Shadow](https://shadow.github.io) discrete-event network simulator, against a mock bootstrap. Shadow replays the same run deterministically for a given configuration.

The topology is `07-wan-fanout`:

* 8 gateways in one mump2p mesh, plus the mock bootstrap and a publisher.
* Every host link: 75 ms one-way latency (±5 ms jitter), 100 Mbit/s up and down, 0.5% packet loss.
* The publisher (`bench-traffic`) is a CL libp2p peer of gateway1. Once per 12 s slot, at the slot start, it publishes a real hoodi beacon block (~20 KB) stamped with the current slot.
* Path under test: publisher → libp2p → gateway1 → mump2p → gateway2..8.

### 2.2 Synthetic attestation load

Signatures and contents of attestations are never verified on the gateway's mump2p path, so the load is synthetic. With `OPT_BENCH_SYNTH_ATTESTATIONS=N`, a gateway does the following once per slot, on the **first** arrival of that slot's block from either path:

* It builds N `SingleAttestation` messages in valid SSZ (snappy-framed) form. They share one `AttestationData` per slot, carry distinct attester indexes (a random per-gateway base plus a counter) and 96 random signature bytes, and are spread round-robin across the 64 attestation subnets.
* It enqueues them straight into the aggregator (`Enqueue`). This bypasses the validator filter from ADR-0008. It runs in its own goroutine, so block forwarding is not delayed.

This mimics the local CL handing over the gateway's validators' attestations right after block import. All 8 gateways inject, so the mesh carries 8·N attestations per slot. They go through the real packer: one shared `AttestationData` per pack, and per item the committee index, attester index, topic index and signature.

### 2.3 Variants

Only the gate setting differs between the A and B runs. The binary, topology, seed and traffic are identical.

| Scenario | `OPT_ATTESTATION_PUBLISH_AFTER_MS` | Behaviour |
| --- | --- | --- |
| `07-wan-fanout` | n/a, no synthetic load | Baseline: blocks only |
| `09-wan-attest-gate` | `4000` (production default) | Block-aware gate from ADR-0009 §4, 8 s cap |
| `10-wan-attest-nogate` | `-1` | Gate disabled: emit on every 25 ms tick, no cap |

Three load levels were run:

| Run | N per gateway per slot | Mesh total per slot | Slots |
| --- | ---: | ---: | ---: |
| Stress | 5000 | 40 000 | 10 |
| Realistic, short | 800 | 6 400 | 10 |
| Realistic, long | 800 | 6 400 | 1000 |

**Why 800.** A validator attests once per 32-slot epoch. A large operator with ~25 600 validators produces ~800 attestations per slot. Smaller partners produce tens; ADR-0008 estimates ~22 for a 700-validator partner. **Why 5000 is a stress level.** 8 × 5000 = 40 000 attestations per slot exceeds what the whole Ethereum network produces per slot (~30 000 with ~1 M validators). That run only asks whether contention can appear at all.

### 2.4 Code modifications used by the bench

The experiment ran on patched builds of both repositories. **None of these patches is merged.** [Appendix A](#appendix-a-modifying-the-code-to-run-the-bench) has them in full.

The gateway patch is bench-only: everything sits behind env variables that are absent from yaml, so with the variables unset the binary behaves like production.

| Env | Effect |
| --- | --- |
| `OPT_BENCH_SYNTH_ATTESTATIONS` | Synthetic load from §2.2; `0` (default) = off |
| `OPT_ATTESTATION_PUBLISH_AFTER_MS` | No-block gate fallback; `0` (default) = the compiled-in default, negative = gate disabled |
| `OPT_GENESIS_TIME` | Overrides the chain genesis time; Shadow's simulated clock starts in 2000, before hoodi genesis, so without it every slot is `0` and every block is dropped as stale |

`optimum-bench` (at `3377e9d`) needed three fixes to run against the gateway at all:

* The mock mint has to issue the `cluster_ids` claim. Since #707 the mump2p handshake rejects tokens without it, and the mesh never forms.
* The publisher has to stamp each block with the slot current at publish time. As committed, it anchors a base slot before the first tick, so blocks arrive one slot late and are never "this slot's block".
* The scenarios `09`/`10` are new. They add a `scraper` host that `curl`s every gateway's `/metrics` at the end of the run.

---

## 3. Calculation methodology

### 3.1 Block propagation latency

Every gateway posts a per-slot `LatencyComparator` to the mock bootstrap (`/api/v2/handle_block_latency`). The mock re-emits each one on stdout, and `bench-report` joins them by `(chain, slot, gateway)`:

* gateway1 (the publisher's peer) reports `t_mum_published_ms`, the moment it published the block onto mump2p;
* each receiver reports `t_mum_seen_ms`, the moment the block first arrived off mump2p.

One **sample** is one (receiver, slot) pair:

```text
latency_ms = receiver.t_mum_seen_ms − gateway1.t_mum_published_ms
```

Both timestamps come from Shadow's single simulated clock, so there is no clock skew between hosts.

### 3.2 Delivery

```text
expected  = published_slots × receivers        (receivers = 7)
delivery  = samples / expected
```

A missing sample means a receiver never reported the block for that slot through mump2p.

### 3.3 Percentiles

All samples of a run are sorted ascending as `s[0..n−1]`, with the same rule as `bench-report`:

```text
pN = s[⌊(n − 1) · N/100⌋]
```

This picks a sample with no interpolation. On 70 samples (10 slots), p95 and p99 both land on the top one or two samples, so for the 10-slot runs only p50 and the per-slot view are meaningful.

### 3.4 Per-slot tail counts

For each slot, `slot_max` is the largest latency across its receivers, i.e. how late the last gateway saw the block. A row "slots with max > T" counts slots with `slot_max > T`. This is the metric least sensitive to how samples spread inside a slot. It answers the question "in how many slots did at least one gateway get the block late".

### 3.5 Attestation latency

* **Wire latency** comes from the receiving gateway's `mump2p_gateway_attestation_propagation_latency_ms` histogram. The value is `now − SendTsMs`, observed on decoding each `AttestationPack`; `SendTsMs` is set when the sender **emits**. The histograms of all 8 gateways are summed, and the reported values are mean = Σsum / Σcount and the cumulative bucket shares.
* **Gate hold is not in the wire latency**, because it happens before `SendTsMs`. It is derived from the release rule instead. Injection happens on the first block arrival, and the release is 2 s after the latest arrival in the slot, so the hold is about 2 s plus the gap between the first and latest arrival on that gateway. With the gate disabled the hold is at most one 25 ms tick.
* **End-to-end** latency is hold plus wire latency.

### 3.6 Reproduction

[Appendix A](#appendix-a-modifying-the-code-to-run-the-bench) covers the patches, the build and the run. The tables in §4 recompute every run from `bench-report`'s `metrics.json` (`propagation.samples`) with the rules in §3.3–3.4, using the script in [A.6](#a6-analysis). The attestation numbers come from `hosts/scraper/curl.*.stdout`.

---

## 4. Results

### 4.1 Block propagation

| Run | Variant | Samples / expected | Delivery | p50 | p90 | p95 | p99 | max | Slots max > 500 ms | > 1 s | > 2 s | > 10 s |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 slots | baseline | 70 / 70 | 100.0% | 153 | 153 | 227 | 228 | 228 | 0 | 0 | 0 | 0 |
| 10 slots, 5000 | gate | 70 / 70 | 100.0% | 238 | 4540 | 5585 | 5587 | 5587 | 5 | 5 | 4 | 0 |
| 10 slots, 5000 | no gate | 70 / 70 | 100.0% | 163 | 239 | 1470 | 1471 | 1545 | 1 | 1 | 0 | 0 |
| 10 slots, 800 | gate | 70 / 70 | 100.0% | 154 | 229 | 1302 | 1303 | 1303 | 1 | 1 | 0 | 0 |
| 10 slots, 800 | no gate | 70 / 70 | 100.0% | 154 | 154 | 155 | 228 | 229 | 0 | 0 | 0 | 0 |
| 1000 slots | baseline | 6985 / 7000 | 99.8% | 153 | 229 | 324 | 2303 | 16 302 | 39 | 18 | 14 | 5 |
| 1000 slots, 800 | gate | 6929 / 7000 | 99.0% | 154 | 425 | 926 | 9091 | 23 990 | **106** | **52** | 27 | 9 |
| 1000 slots, 800 | no gate | 6950 / 7000 | 99.3% | 154 | 326 | 600 | 9926 | 21 937 | **70** | **31** | 23 | 10 |

Latencies are in milliseconds.

Per-slot pattern in the stress run with the gate: the first 5 blocks propagate in ~162 ms. From the 6th block on, every receiver sees the block 4.3 s, 5.6 s, 4.3 s, 2.6 s and 1.4 s late. This looks like a backlog that each slot's synchronised attestation release adds to and that does not drain before the next block. Without the gate, 9 of 10 blocks stay at ~162 ms and one reaches 1.47 s.

### 4.2 Attestation latency (1000 slots, 800 per gateway)

| Variant | Attestations packed (all gateways) | Packs decoded | Wire mean | Wire ≤ 500 ms | ≤ 800 ms | ≤ 1 s | ≤ 2 s | Gate hold | End-to-end, approx. |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| gate | 6 226 400 | 54 225 | 726 ms | 28.6% | 78.7% | 91.6% | 99.1% | ~2 s | ~2.7 s |
| no gate | 6 224 800 | 54 221 | 814 ms | 21.6% | 72.7% | 89.5% | 98.7% | ≤ 25 ms | ~0.8 s |

No pack errors occurred in any run. Wire latency in the 5000-per-gateway stress run averaged ~4 s in both variants: the 100 Mbit/s links were close to saturation.

---

## 5. Interpretation

1. **The tail above ~2 s is a property of the harness, not of the gate.** The baseline with no attestations already has 14 slots above 2 s and 5 above 10 s out of 1000, and 99.8% delivery. These are blocks recovered a slot or two late over the lossy WAN model. Above 2 s, the gate and no-gate runs differ by noise (27 vs 23 slots, 9 vs 10).
2. **Attestation load degrades block propagation in both variants.** p90 rises from 229 ms to 326–425 ms and delivery drops from 99.8% to 99.0–99.3%.
3. **The gate makes that degradation worse, not better.** Compared with emitting immediately, the gate has 51% more slots with a block later than 500 ms (106 vs 70) and 68% more later than 1 s (52 vs 31), and p90/p95 are 100–330 ms higher. Holding attestations and then releasing them fleet-wide at about the same moment (block + 2 s) loads the mesh in a burst. Spreading them from block arrival on loads it less. The stress run shows the same direction more sharply.
4. **The gate costs attestations about 2 s end to end.** Their wire latency is about the same either way (the no-gate run is ~90 ms slower, because its packs share the mesh with the block). The hold dominates.

In this harness, ADR-0009's hypothesis is not supported: the gate does not protect block propagation and delays attestations by ~2 s.

---

## 6. Limitations

* **CPU is not modelled.** Shadow simulates the network; compute takes effectively zero simulated time. The CPU half of ADR-0009's hypothesis (RLNC block coding competing with attestation packing) is not tested.
* **One run per variant.** At 1000 slots, the 106 vs 70 and 52 vs 31 gaps are consistent across thresholds and with both 10-slot runs, but no significance test was done and no seeds were varied.
* **Synthetic arrival pattern.** All N attestations arrive in one burst at block arrival. A real CL hands them over as validators finish signing, which smooths the no-gate case further and probably favours it more.
* **One topology.** One uniform WAN link model (75 ms, 100 Mbit/s, 0.5% loss) and 8 gateways in one mesh, with one block source.
* **Disabling the gate also disables the 8 s cap**, because `shouldHoldForSlotGate` returns before the cap check. The no-gate variant therefore also forwards late attestations instead of carrying them into the next slot.

---

## 7. Decision

* Remove the slot-aware gate. The aggregator emits whatever it has accumulated on every aggregation tick (`aggregation_interval_ms`, 25 ms by default), at any point in the slot, as it did before ADR-0009. Removed along with it: `shouldHoldForSlotGate`, `blockSeenPublishDelay`, `Emitter.LastBlockReceivedMs`, and the config getters and constants `GetAttestationPublishGate` / `DefaultAttestationPublishAfterMs` and `GetAttestationPublishCap` / `DefaultAttestationPublishCapMs`.
* The router's current-slot filter (`DefaultAttestationMaxSlotAge`) is unchanged.
* After rollout, check the CPU side on real hardware, which this bench cannot model: compare block propagation p50/p90 on hoodi before and after the release.
* The bench patches stay out of the tree. [Appendix A](#appendix-a-modifying-the-code-to-run-the-bench) describes how to re-apply them, for example to try other load levels such as N = 50 and N = 200.

---

## 8. Consequences

### Positive

* Attestations reach the mump2p overlay ~2 s earlier on average.
* In the simulated WAN, fewer blocks are delayed under attestation load.
* The failure modes from ADR-0009 §4 go away: a late duplicate block arrival re-closing an open window, no publish at all in slots whose block arrives after 6 s, and held attestations carried into the next slot.

### Negative / risks

* Attestation and block traffic overlap in the first ~2 s of the slot. If the CPU half of the hypothesis holds on real hardware, block coding may slow down. The post-rollout check in §7 is meant to catch this.
* Without the 8 s cap, attestations that arrive after 8 s are still forwarded. That adds some late traffic, which ADR-0009 considered wasted.

---

## Appendix A: modifying the code to run the bench

The results in §4 come from the patches below. They are not merged, so apply them to local checkouts.

### A.1 Prerequisites and base commits

* **Hardware and tools:** x86_64 Linux (Shadow does not run on ARM), Go 1.27, Shadow on `PATH` (`make install-shadow` in `optimum-bench`), and `curl` at `/usr/bin/curl`.
* **`optimum-gateway`:** `16fc2f5`, the last commit with the gate. The gate-on variant needs `shouldHoldForSlotGate` and the gate constants from ADR-0009 §4, which §7 removes, so apply the patch to that commit rather than to `main`.
* **`optimum-bench`:** `3377e9d`.

### A.2 Gateway patch

**1. `pkg/config/config.go`: env knobs.** Add three fields to `AppConfig`, next to `AggregationIntervalMs`:

```go
// Bench only: no-block attestation gate in ms; 0 = default, negative disables the gate.
AttestationPublishAfterMs int64 `yaml:"-" env:"OPT_ATTESTATION_PUBLISH_AFTER_MS"`
// Bench only: synthetic attestations enqueued once per slot on the first block arrival; 0 = off.
BenchSynthAttestations int `yaml:"-" env:"OPT_BENCH_SYNTH_ATTESTATIONS"`
// Bench only: genesis unix time replacing the chain default (Shadow's simulated clock starts in 2000); 0 = chain default.
GenesisTimeOverride int64 `yaml:"-" env:"OPT_GENESIS_TIME"`
```

Make the gate getter honour the override:

```go
func (c *AppConfig) GetAttestationPublishGate() time.Duration {
	if c.AttestationPublishAfterMs == 0 {
		return time.Duration(DefaultAttestationPublishAfterMs) * time.Millisecond
	}
	return time.Duration(c.AttestationPublishAfterMs) * time.Millisecond
}
```

**2. `pkg/protocol/chain_state/chain_state.go`: genesis override.** Add a parameter to `LoadGenesisState` and apply it after the chain default is resolved:

```go
func LoadGenesisState(log logger.AppLogger, cfgChain chain.Chain, overrideUnix int64) error {
	// ... unchanged up to:
	genesisTime := int64(ts) //nolint:gosec // G115: known chain genesis timestamps are within int64 range
	if overrideUnix > 0 {
		genesisTime = overrideUnix
	}
	// ... unchanged
```

In `pkg/protocol/forks/service.go`, pass the config value:

```go
if err := chain_state.LoadGenesisState(log, srv.appChain, appCfg.GenesisTimeOverride); err != nil {
```

The existing callers in `chain_state_test.go` pass `0`.

**3. `pkg/service/gossipsub-gateway/service.go`: once-per-slot marker.** Add a field to `Service`:

```go
benchSynthSlot atomic.Uint64 // last slot synthetic bench attestations were injected for
```

**4. `pkg/service/gossipsub-gateway/beacon_block_measures.go`: trigger.** In `processBeaconBlockArrival`, right after `currentSlot` is computed:

```go
currentSlot := chainstate.CurrentSlot(time.Now())
if blockDecoded.Header.Slot == currentSlot && s.cfg.BenchSynthAttestations > 0 {
	go s.injectBenchAttestations(topic, currentSlot) // off the block path: forwarding must not wait
}
```

**5. New file `pkg/service/gossipsub-gateway/bench_attestations.go`: the injector.**

```go
package gossipsub_gateway

import (
	"bytes"
	cryptorand "crypto/rand"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
)

// injectBenchAttestations emulates the local CL handing over cfg.BenchSynthAttestations
// attestations right after the slot's block, so a bench can load mump2p alongside
// block propagation. Nothing downstream verifies them: one shared AttestationData
// per slot (so the packer groups them like real ones), distinct attester indexes,
// random signatures.
func (s *Service) injectBenchAttestations(blockTopic string, slot uint64) {
	if prev := s.benchSynthSlot.Load(); prev >= slot || !s.benchSynthSlot.CompareAndSwap(prev, slot) {
		return // once per slot, on the first arrival from either path
	}
	att := consensus.SingleAttestation{Data: consensus.AttestationData{Slot: consensus.Slot(slot)}}
	base := rand.Uint64N(1 << 40) // keeps attester indexes distinct across gateways
	for i := range s.cfg.BenchSynthAttestations {
		att.AttesterIndex = consensus.ValidatorIndex(base + uint64(i)) //nolint:gosec // G115: i is non-negative
		_, _ = cryptorand.Read(att.Signature[:])
		var buf bytes.Buffer
		if _, err := s.sszEncoder.EncodeGossip(&buf, &att); err != nil {
			s.log.Error("bench: failed to encode synthetic attestation", err)
			return
		}
		topic := strings.Replace(blockTopic, "beacon_block", fmt.Sprintf("beacon_attestation_%d", i%64), 1)
		s.aggregatorMessages.Enqueue(topic, topics.TopicMetaFor(topic), buf.Bytes())
	}
}
```

**6. Build** a glibc dynamic binary straight into the bench tree. Shadow rejects musl and static binaries, so the release Docker image cannot be used:

```sh
cd optimum-gateway
CGO_ENABLED=1 go build -ldflags='-linkmode=external' -o ../optimum-bench/bin/optimum-gateway ./cmd
```

### A.3 Bench patch

**1. `mocks/bootstrap/auth.go`: cluster claim.** Add the claim to `mintClaims` and set it in `handleMint`:

```go
type mintClaims struct {
	// ... existing fields
	ClusterIDs []string `json:"cluster_ids"` // the mump2p handshake rejects peers without the receiver's cluster
	// ...
}

// benchClusterID is the OPT_GATEWAY_CLUSTER_ID every scenario gives its gateways.
const benchClusterID = "bench-cluster"

// in handleMint:
claims := mintClaims{
	// ... existing fields
	ClusterIDs: []string{benchClusterID},
}
```

**2. `tools/bench-traffic/main.go`: slot stamping.** In `runPublish`, drop `baseSlot` and stamp each block with the slot current at publish time:

```go
case <-tick.C:
	slot := currentSlot(time.Now(), genesisTime) // was: baseSlot + seq
```

**3. Build the mock and the publisher.** `make bin` skips the gateway binary from A.2 because it already exists:

```sh
cd optimum-bench
make bin
```

### A.4 Scenarios

Generate the A/B pair from `scenarios/07-wan-fanout.yaml`. The script adds the two env knobs to every gateway and appends a `scraper` host that saves each gateway's `/metrics` at the end of the run. `N` is the number of attestations per gateway per slot, and `BLOCKS` is the slot count:

```python
# gen.py — usage: python3 -I gen.py scenarios/07-wan-fanout.yaml scenarios N BLOCKS
import sys

src, out_dir, n, blocks = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
body = open(src).read()
body = body[body.index("general:"):]
stop = 60 + blocks * 12 + 40  # publisher t=45s + 15s warmup, 12s per block, drain + scrape
body = body.replace("  stop_time: 210s\n", f"  stop_time: {stop}s\n")
body = body.replace("-count=10 -rate=12s", f"-count={blocks} -rate=12s")

for name, gate in [("09-wan-attest-gate", "4000"), ("10-wan-attest-nogate", "-1")]:
    out = body.replace(
        '          OPT_REMOTE_PUSH_ENABLE: "false"\n',
        '          OPT_REMOTE_PUSH_ENABLE: "false"\n'
        f'          OPT_BENCH_SYNTH_ATTESTATIONS: "{n}"\n'
        f'          OPT_ATTESTATION_PUBLISH_AFTER_MS: "{gate}"\n',
    )
    assert out.count("OPT_BENCH_SYNTH_ATTESTATIONS") == 8
    out = out.rstrip("\n") + "\n\n  scraper:\n    network_node_id: 0\n    processes:\n"
    for g in range(1, 9):
        out += (f"      - path: /usr/bin/curl\n"
                f"        args: -sS --max-time 5 http://gateway{g}:48123/metrics\n"
                f"        start_time: {stop - 20}s\n"
                f"        expected_final_state: {{ exited: 0 }}\n")
    open(f"{out_dir}/{name}.yaml", "w").write(f"# {name}: ADR-0014, N={n}, {blocks} blocks\n" + out)
```

```sh
python3 -I gen.py scenarios/07-wan-fanout.yaml scenarios 800 1000   # §4 long run
python3 -I gen.py scenarios/07-wan-fanout.yaml scenarios 800 10     # §4 short run
python3 -I gen.py scenarios/07-wan-fanout.yaml scenarios 5000 10    # §4 stress run
```

To get a baseline of the same length without attestations, copy `07-wan-fanout.yaml` with only `stop_time` and `-count` changed, for example `stop_time: 12100s` and `-count=1000`.

### A.5 Run

`make prepare-runtime-dirs` uses `sudo` only to create `/gateway/logs`. The gateway falls back to stdout-only logging without that directory, so the same preparation can be done by hand without `sudo`:

```sh
cd optimum-bench
for d in /tmp/libp2p /tmp/libp2p-1; do mkdir -p $d && cp keys/gateway1-libp2p.p2p.key $d/p2p.key; done
mkdir -p /tmp/libp2p-2 && cp keys/gateway2-libp2p.p2p.key /tmp/libp2p-2/p2p.key
for n in 3 4 5 6 7 8; do mkdir -p /tmp/libp2p-$n /tmp/optimum-$n; done
mkdir -p /tmp/publisher && cp keys/publisher-cl.p2p.key /tmp/publisher/p2p.key
mkdir -p shadow.data

# one at a time: scenarios share /tmp identity and JWKS-cache paths
for s in 09-wan-attest-gate 10-wan-attest-nogate; do
  rm -rf shadow.data/$s
  shadow --data-directory shadow.data/$s scenarios/$s.yaml > shadow.data/$s.log 2>&1
done
go run ./tools/bench-report -data shadow.data -out report/gate-ab -tag gate-ab
```

A 10-slot scenario takes seconds of wall-clock time; a 1000-slot one takes ~8 min.

Sanity checks before reading the results:

* Gateway logs show `"genesis_time":1` and no `stale_data` lines. Otherwise the genesis override is missing.
* Gateway logs show `handshake handled successfully` and no `missing cluster claim`. Otherwise the mock patch is missing.
* `mump2p_gateway_attestation_pack_total` in `hosts/scraper/curl.*.stdout` equals N × slots per gateway, and `pack_errors_total` is 0. Otherwise the injector did not run; the usual cause is the publisher's slot stamping.

### A.6 Analysis

Block propagation (§3.1–3.4), computed from `bench-report`'s per-scenario `metrics.json`:

```python
# analyze.py — usage: python3 -I analyze.py label=report/gate-ab/<scenario>/metrics.json ...
import collections, json, sys

def pct(s, p):
    return s[max(int((len(s) - 1) * p), 0)]

for label, path in (a.split("=", 1) for a in sys.argv[1:]):
    prop = json.load(open(path))["propagation"]
    by_slot = collections.defaultdict(list)
    for x in prop["samples"]:
        by_slot[x["slot"]].append(x["latency_ms"])
    s = sorted(v for l in by_slot.values() for v in l)
    slot_max = [max(l) for l in by_slot.values()]
    over = lambda t: sum(v > t for v in slot_max)
    exp = prop["expected_samples"]
    print(f"{label}: samples={len(s)}/{exp} delivery={len(s) / exp:.1%} "
          f"p50={pct(s, .5)} p90={pct(s, .9)} p95={pct(s, .95)} p99={pct(s, .99)} max={s[-1]} "
          f"slots>500={over(500)} >1000={over(1000)} >2000={over(2000)} >10000={over(10000)}")
```

Attestation wire latency (§3.5), summed over the 8 gateways:

```sh
cat shadow.data/<scenario>/hosts/scraper/curl.*.stdout | awk '
  /^mump2p_gateway_attestation_propagation_latency_ms_sum\{/   { sum += $NF }
  /^mump2p_gateway_attestation_propagation_latency_ms_count\{/ { cnt += $NF }
  /^mump2p_gateway_attestation_propagation_latency_ms_bucket\{/ {
    match($0, /le="[^"]+"/); b[substr($0, RSTART + 4, RLENGTH - 5)] += $NF }
  /^mump2p_gateway_attestation_pack_total\{/ { packed += $NF }
  END { printf "packed=%d packs=%d mean=%.0fms\n", packed, cnt, sum / cnt
        for (le in b) printf "le=%s %.3f\n", le, b[le] / cnt }' | sort -t= -k2 -g
```
