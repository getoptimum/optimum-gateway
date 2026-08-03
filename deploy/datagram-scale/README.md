# Mid-scale dissemination tier

A local stack that runs the same encrypted UDP data plane as `../datagram-demo` at
10 to 20 gateways, to reproduce and measure a **dissemination stall**: a state where
no node accumulates enough of a generation to be allowed to forward any of it, so
nothing propagates and delivery is 0% for the whole fleet at once.

The five-gateway stack cannot show this. It delivers 100% of everything with an
order of magnitude more symbols than it needs, which is exactly why it is a good
smoke test and a useless dissemination test.

```sh
make datagram-scale                                     # 12 gateways, served defaults
make datagram-scale ARGS="--gateways 20 -f 0.9"         # the stall, 0% delivery

deploy/datagram-scale/run.sh --gateways 12              # same thing without make
SCALE_FORWARD_THRESHOLD=0.9 deploy/datagram-scale/sweep.sh gateways 5 12 16 20
SCALE_GATEWAYS=20 deploy/datagram-scale/sweep.sh threshold 0.75 0.85 0.9
```

**This tier has no pass gate.** `run.sh` publishes, measures how far each generation
got, and prints the numbers. A run that delivers nothing is a successful run: it has
reproduced the thing being studied. The only failures it reports are the ones that
would make the measurement meaningless (an unapplied config, an unauthenticated mesh,
an unconfirmed datagram path, symbols on the stream fallback).

## Read delivery and rank. Do not read latency.

20 to 40 containers on an 8-core machine is oversubscription by design: the coder
sidecars alone will saturate the box. **Every timing number this tier produces is an
artifact of that contention and means nothing.** Rank and delivery, on the other
hand, are decided by the protocol's own arithmetic and not by how fast the host is:
a node either accumulates enough independent symbols to cross the forwarding
threshold or it does not, and no amount of CPU changes which side of that line it
lands on. That is why `report.py` prints no latency at all.

## What decides whether a generation propagates

Three rules, all in `mump2p-protocol/pkg/router/router.go`:

1. **The publisher round-robins `n = ceil(p*k)` coded symbols one per target**, and
   its targets are its *mesh* (`meshFirstTargets`), not every topic peer. With the
   served defaults that is `n = ceil(2.5*16) = 40` symbols over `D = 6` mesh peers,
   so a first-hop node receives about **6.7 of the 16 it needs** and every other node
   receives **nothing** directly.
2. **A node forwards recoded symbols only once its rank strictly exceeds
   `int(k*f)`**, which for `k=16, f=0.75` means rank 13 of 16. Below that it is
   silent; it does not gossip, it does not request, it waits.
3. **The one relay that is not gated** is `fastForwardSymbolAsIs`: a *systematic*
   symbol received *directly from the publisher* is relayed as-is to the receiver's
   mesh. It fires exactly one hop, because the relayed copy keeps the publisher as
   its author, so the next node sees `from != author` and does not relay again.

So the whole fleet's fate rests on one quantity: **the margin between the rank a
first-hop node reaches on its own and the gate it has to cross**. Its rank is its
direct share plus the systematic symbols relayed to it by the *other* first-hop
nodes that happen to have it in their mesh, and that second term thins out as the
fleet grows, because the publisher's mesh is a shrinking fraction of it.

When the margin goes negative the failure is not gradual. Nothing forwards, so
nothing reaches anyone, so nothing forwards: every node loses the same generation at
the same moment. Below the cliff the fleet looks perfect.

## What was observed

All runs: one publishing ingress, `k=16`, `p=2.5`, `D=6`, a full mesh (`P = N-1`;
gateways ask the bootstrap for at most 7 peers but DHT discovery fills in the rest),
and a lossless loopback network.

**Growing the fleet alone did not reproduce it.** Up to 20 gateways the cascade
always completed:

| N | P | f | gate | delivery | rank p50 | rank max | senders | recoders | unnecessary symbols |
|---|---|---|---|---|---|---|---|---|---|
| 5 | 4 | 0.75 | >12 | 100% | 16 | 16 | 5/5 | 4 | 84% |
| 12 | 11 | 0.75 | >12 | 100% | 16 | 16 | 12/12 | 11 | 91% |
| 16 | 15 | 0.75 | >12 | 100% | 16 | 16 | 16/16 | 15 | 92% |
| 20 | 19 | 0.75 | >12 | 100% | 16 | 16 | 20/20 | 19 | 92% |

Over 90% of every symbol that arrives at a node arrives *after* that node no longer
needs it. The margin at `f=0.75` is not thin at any of these sizes; it is enormous.

**Moving the gate reproduced it exactly.** At 20 gateways, walking `f` up shrinks the
same margin that fleet growth shrinks, and the collapse is abrupt:

| N | P | f | gate | delivery | rank p50 | rank max | reached k | senders | recoders | helpful share |
|---|---|---|---|---|---|---|---|---|---|---|
| 20 | 19 | 0.75 | >12 | 100% | 16 | 16 | 100% | 20/20 | 19 | 8% |
| 20 | 19 | 0.85 | >13 | **75%** | 16 | 16 | 88% | 20/20 | 19 | 8% |
| 20 | 19 | 0.90 | >14 | **0%** | 8 | 14 | 0% | 8/20 | **0** | **100%** |
| 20 | 19 | 0.95 | >15 | **0%** | 8 | 15 | 0% | 10/20 | **0** | **100%** |

One notch of `f`, from 0.85 to 0.9, is the whole difference between a fleet that
delivers and a fleet that delivers nothing.

**And with the gate held there, fleet size reproduces it on its own.** Holding
`f=0.9` and sweeping the gateway count gives the delivery-versus-P curve the tier was
built for:

| N | P | f | delivery | rank p50 | rank max | reached k | senders | recoders |
|---|---|---|---|---|---|---|---|---|
| 5 | 4 | 0.9 | 100% | 16 | 16 | 100% | 5/5 | 4 |
| 12 | 11 | 0.9 | 100% | 16 | 16 | 100% | 12/12 | 11 |
| 16 | 15 | 0.9 | **50%** | 16 | 16 | 75% | 16/16 | 15 |
| 20 | 19 | 0.9 | **0%** | 8 | 14 | 0% | 8/20 | 0 |

Same cliff, driven by P instead of by `f`, with the knee at P=15: two of four
generations delivered to all 15 receivers and two were lost by all 15. The two axes
are the same axis. Both shrink the margin between the rank a first-hop node reaches
and the gate it has to cross, and either one alone will close it.

The `f=0.9` run is the failure, on every axis at once: **0% delivery**, rank per
receiver-chunk plateauing at p50 8 / max 14 and **never reaching 16**, only **8 of 20**
gateways ever putting a datagram on the wire, **zero** `rlnc.symbol.recode` spans
fleet-wide, and **100% of received symbols still helpful** because nothing ever
arrives late enough to be redundant. Nothing is dropping traffic; there simply is not
enough of it, and the gate is why.

`f=0.85` is the knee, and it shows the shape better than either end: three of four
generations delivered completely and the fourth was lost by all 19 receivers
simultaneously, with the lost chunk's ranks spread from 3 to 15. All-or-nothing per
generation is the signature.

### What this tier does not reproduce

The local network is lossless, the mesh is complete and the RTTs are microseconds.
Real fleets lose datagrams, fragment at unexpected MTUs and form partial meshes, all
of which reduce the supply reaching a first-hop node. That is the most likely reason
a real 25-node fleet stalls at `f=0.75` while 20 local gateways do not: the local
margin is still positive at that gate, so `f` has to be raised by two notches to
close it here. The tier reproduces the *mechanism and its signature* and lets the
margin be measured; it does not reproduce the loss that eats the margin in a
deployment. Read the cliff position, not its absolute coordinates.

The practical consequence points the other way, and it is the reason to have the
knob: if a fleet's measured rank plateaus at p50 `r`, its gate has to open below `r`
for anything to forward at all, so `f` must be under `r/k`. A fleet measured at rank
p50 6 with `k=16` needs `f < 0.375`, not the default 0.75.

## Knobs

`run.sh` takes flags; every flag also has an environment variable, so `sweep.sh` can
drive it either way.

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--gateways` | `SCALE_GATEWAYS` | `12` | gateway count; `P = N-1` |
| `--forward-threshold` | `SCALE_FORWARD_THRESHOLD` | `0.75` | served `f`; the gate opens above `int(k*f)` |
| `--mesh-degree` | `SCALE_MESH_DEGREE` | `6` | served `mesh_degree_target`, which is the publisher's fan-out |
| `--shard-factor` | `SCALE_SHARD_FACTOR` | `16` | served `k` |
| `--publisher-multiplier` | `SCALE_PUBLISHER_MULTIPLIER` | `2.5` | served `p`; `n = ceil(p*k)` |
| `--lanes` | `SCALE_LANES` | `20` | shared-memory lanes per coder; drives the tmpfs size |
| `--messages` | `SCALE_MSG_COUNT` | `8` | blocks to publish |
| `--warmup` | `SCALE_WARMUP` | `20s` | publisher delay after connect |
| `--settle` | `SCALE_SETTLE` | `25` | seconds to wait after the last publish |
| `--base-port` | `SCALE_BASE_PORT` | `48151` | host port of gateway1; the rest follow |
| `--tag` | | derived | artifact directory name under `out/` |
| `--skip-build` | `SCALE_SKIP_BUILD` | unset | reuse the existing binaries and image |
| `--keep-up` | | | leave the stack running to poke at it |
| `--no-datagram` | `SCALE_DATAGRAM_ENABLE` | `true` | run the same thing on the stream path |

Two of these do not do what their name suggests, and both are in the protocol rather
than here:

- **`f = 0`** does not disable the gate through the served config. `applyServedConfig`
  treats a zero as "not served" and leaves the protocol default of 0.75 in place. The
  lowest useful value is the smallest one whose gate still opens at or above
  `MinRecodeRank = 2`.
- **`p` below 1.0 has no effect on this path.** With the datagram transport on,
  `datagramRedundancyFraction` replaces any redundancy still sitting at the stream
  default with the datagram path's own 2.5, so `p` can only be raised, not lowered.

## Sweeping

```sh
./sweep.sh gateways 5 8 12 16 20      # delivery versus P
./sweep.sh threshold 0.5 0.75 0.9     # delivery versus f, at SCALE_GATEWAYS
./sweep.sh mesh 4 6 10                # delivery versus the publisher's fan-out
./summarise.py out/*/report.json      # re-table any set of finished runs
```

Each point is a full bring-up and teardown, so a five-point sweep is the better part
of an hour. Sweeps default to `SCALE_MSG_COUNT=4` because the shape of the curve
comes from the number of runs, not from the number of blocks per run. Every point
leaves a complete artifact directory behind, so `summarise.py` can re-table a sweep
later without re-running anything.

## What the report shows

`report.py` prints nine sections and writes the same numbers to `report.json`. The
ones that matter:

| Section | Why it is there |
|---|---|
| topology | `P` per gateway. It is the independent variable, and it is measured, not assumed. |
| transport | hook vs fallback per gateway, and **how many gateways ever sent a datagram at all**. In a stalled fleet only the publisher and its first hop ever transmit. |
| delivery | decoded/published per subscriber, and the fleet fraction. |
| **rank** | **rank reached per receiver-chunk against the required `k`**, as min/p50/max plus a full histogram. This is the diagnostic: delivery says a generation failed, rank says how far short it stopped and therefore why. |
| recode | `rlnc.symbol.recode` span counts per node, and how many generations and chunk ids each node recoded for. A node that never recodes never crossed the gate. |
| symbols | helpful vs redundant vs unnecessary. The composition tells starvation from congestion: a healthy small fleet is ~85-90% *unnecessary* symbols (they keep arriving after the decode), a starved one is nearly 100% *helpful* (everything that arrives is still needed, there just is not enough of it). |

Receiver-chunks that got no symbol at all produce no spans, so `report.py` computes
the expected count as `generations x chunks x receivers` and counts the difference in
as rank 0 rather than dropping it. Without that, a fleet where half the nodes heard
nothing would report a flatteringly high median rank over the half that did.

## Resource cost

Each gateway carries its own `rlnc-server` sidecar with a tmpfs `/dev/shm`, so the
container count is `2N + 4` and the memory commitment scales with `N`:

| N | containers | tmpfs ceiling | tmpfs actually touched |
|---|---|---|---|
| 12 | 28 | 6.4 GiB | 3.8 GiB |
| 16 | 36 | 8.5 GiB | 5.0 GiB |
| 20 | 44 | 10.6 GiB | 6.3 GiB |

`gen-stack.py` prints those three numbers on every run before anything starts. The
tmpfs is sized at 1.6x the lanes' own footprint, and `--lanes` is the knob to reduce
it: `--lanes 8` cuts the per-coder commitment from 320 MiB to 128 MiB. Fewer lanes
means less coding concurrency, which is a confound for anything timing-shaped, but
rank and delivery are not timing-shaped.

## Layout

```
gen-stack.py       writes stack.yml and served-config.json for N gateways
dynamic-config.json  the base served config; gen-stack.py applies the swept knobs
run.sh             generate -> build -> staged bring-up -> publish -> report -> stop
report.py          scrapes every gateway and reports delivery, rank, recode, composition
summarise.py       one report.json per run -> a delivery-versus-knob table
sweep.sh           runs one axis and summarises it
nginx.conf         config proxy: serves the generated config, proxies the rest
otel-collector.yaml  OTLP/HTTP in, debug exporter out
Dockerfile         one image, three entrypoints (gateway, mock, publisher)
gateway-entrypoint.sh  seeds gateway1's fixed CL identity, then execs the gateway
keys/              two committed test-only libp2p identities
out/<tag>/         per-run artifacts: spans, report.txt, report.json, node-map.json
```

`stack.yml` and `served-config.json` are generated and git-ignored. The compose file
cannot be a committed constant here: every gateway needs its own shared-memory
volume, its own coder sidecar and its own published port, none of which
`docker compose --scale` provides.

## Things that are load bearing

Everything in `../datagram-demo/README.md` under the same heading still applies: the
auth issuer must match byte for byte, the cluster ID must be identical everywhere,
`OPT_API_KEY` must be non-empty, every DynamicConfig key must be spelled as its JSON
tag, shut down with SIGTERM so the span flush runs, and the public-IP probes are
pointed at localhost on purpose. What is different here:

**Start order decides the topology.** A gateway registers with the bootstrap and then
asks for peers exactly once, at startup, and never again. `run.sh` starts one gateway
at a time and waits for each to serve HTTP.

**The topology is waited for, not assumed.** The bootstrap hands out at most 7 peers
per request (`exposeNodesAmount` in the gateway), so above 8 gateways the initial
graph is not a clique; DHT discovery then fills it in to a full mesh. `run.sh`
therefore waits for a peer total that stops changing rather than for a fixed number,
and `report.py` prints the `P` it actually found.

**The span log is large.** 12 gateways for 8 blocks is ~37k spans and hundreds of MB
of collector output. The compose file pins a file log driver with a 4 GiB cap; the
default journald driver would rate-limit and silently drop spans.

**Node identity comes from the container log.** Spans are tagged with the mump2p node
id, which is that node's own peer ID and appears in neither `self_info` nor
`/metrics`. `run.sh` greps it out of each gateway's startup line into
`out/<tag>/node-map.json` and passes it to `report.py`, which is what lets the
per-receiver tables say `gateway7` instead of a peer ID prefix.

**The five-gateway stack is untouched.** `../datagram-demo` stays the fast smoke test
with a real pass gate. This tier does not share its image, its ports, its cluster ID
or its compose project, so both can be run without interfering.

## Prerequisites

Identical to `../datagram-demo`: a sibling `optimum-bench-v2` checkout for
`mocks/bootstrap` and `tools/bench-traffic`, this repo's local `replace` for
`mump2p-protocol`, and `getoptimum/rlnc-server`, `nginx:1.27-alpine` and
`otel/opentelemetry-collector:0.115.0` available.

The two keys under `keys/` are copies of that stack's committed test-only identities,
kept here so this tier builds its own image and stands alone. They grant no access to
anything; do not reuse them anywhere that matters.
