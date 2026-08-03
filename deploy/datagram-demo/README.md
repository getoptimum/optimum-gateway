# Encrypted UDP data-plane demo

A reproducible five-gateway stack that proves the gateway carries RLNC traffic over
the **encrypted UDP datagram data plane**, with the per-peer keys bootstrapped over
the **authenticated libp2p control connection**, and that nothing quietly fell back
to the stream path.

```sh
make datagram-demo          # from the repo root
# or
deploy/datagram-demo/run.sh
```

One run takes about four minutes and ends in `PASS` or a list of the gate items
that did not hold.

## What it proves

Five gateways, mutually authenticated by real JWT handshakes: one publishing
ingress fed by a real consensus-layer publisher, four subscribers. Every gateway
has its own `rlnc-server` shared-memory sidecar, so the coding is out of process
exactly as it is in a deployment.

The pass gate, all of which must hold:

| # | Gate | Evidence |
|---|------|----------|
| 1 | Applied config matches the served config, including the derived shard size | `/api/v1/self_info` `rlnc_config` |
| 2 | 20 verified handshakes (5 gateways x 4 peers), 0 rejected | `mump2p_gateway_p2p_handshake_cluster_claim_total{result}` |
| 3 | `paths_confirmed == peers_total` on every gateway | `/api/v1/self_info` `datagram` |
| 4 | `mump2p_datagram_sends_total{path="hook"} > 0` and `{path="fallback"} == 0` | per-gateway `/metrics` |
| 5 | Every subscriber decoded every published message | `mump2p_mump2p_delivered_messages_count` vs `mump2p_gateway_mump2p_published_messages_per_topic_total` |
| 6 | Traced generations and a measurable end-to-end latency | OTel spans through `parse_v2_1.py` |

Gate 4 is the one that makes the rest non-vacuous. The hybrid forwarder falls back
to the libp2p stream path *silently* whenever the datagram path refuses a message,
so a run can deliver 100% of its traffic without a single byte having crossed UDP.
Delivery alone proves nothing about the transport; the `path` split does.

### The security claim, precisely

The datagram keys are bootstrapped over the **authenticated libp2p control
connection**, which in this stack is **QUIC on port 33213**, not TCP. (The
consensus-layer ingress on 33212 is TCP; that is a different connection carrying
different traffic.) `baseNodeConfig` pins `TransportQUIC` and the gateway
advertises `/ip4/<addr>/udp/33213/quic-v1`.

The ordering is what matters, and it is enforced in code:
`handshakeHandler` verifies an ES256 JWT whose `cnf.peer_id` equals the connection's
peer ID and whose `cluster_ids` contains this gateway's cluster; only then does
`markHandshakeValid` call `establishDatagramSession`. The key exchange itself is
`/optimum/v1/udp-session`: X25519 ephemerals and HKDF-SHA256, one key per
direction, receiver-allocated key ids, and path tokens that every probe must
carry. Nothing in it is signed, because the stream underneath is Noise
authenticated and the peer ID is already cryptographically proven there.

The session is capped by the token that authorised it: the handshake returns the
JWT's `exp` and the session expiry is bounded by it, so datagram keys cannot
outlive the credential that admitted the peer. You can see this in the gateway log:

```
INFO datagram session installed module=udp_session peer_id=16Uiu2H... initiator=true
     rx_key_id=468987952 tx_key_id=1263427275 expires_at=2026-08-03T03:44:20.000Z
```

## Why `max_shard_size` is not set anywhere, and why 1136 is asserted

`OPT_DATAGRAM_MAX_PAYLOAD` and any shard-size override are deliberately left
unset. With the datagram path on, the shard size is *derived* from the transport's
plaintext budget and from the topics the gateway declares it publishes on:

```
 1382  transport default MaxPayload (datagram.DefaultPathMTU of 1460, less a
       40 byte IPv6 header, an 8 byte UDP header and the sealed datagram's
       own 30 bytes of framing)
 -192  engine.SymbolFramingOverhead
 - 38  len("/eth2/<8 hex digest>/beacon_block/ssz_snappy"), the longest of
       topics.MumP2PPublishTopics()
 - 16  k coefficient bytes (= rlnc_shard_factor)
 = 1136
```

`verify.py` asserts `rlnc_config.max_shard_size == 1136` read back from
`/api/v1/self_info`, which is the value the RLNC engine is actually coding at.
That single assertion catches three distinct silent failures:

- **the derived size never reaching the engine.** A datagram node whose engine was
  built from the undecorated config shards at the 64-byte protocol default instead
  of 1136. The demo's own negative control shows the shape of it: 20 chunks per
  message instead of 2, ~2065 symbols per delivered node instead of ~205, and
  end-to-end p50 of 76ms instead of 8ms.
- **an accidental override.** Anything that pins a shard size wins over the
  derivation and the number moves.
- **a config-proxy 404.** A failed config fetch is *not* an error the gateway
  reports: it keeps its built-in defaults, so k stays 4, the derivation lands on
  1148 rather than 1136, and the run looks healthy until delivery is measured.

The 1382 targets a 1460 byte path MTU, not the 1500 an Ethernet link
advertises: an overlay network encapsulates, and the encapsulation comes out of
the same 1500 bytes. Sizing above the real MTU fragments every full-size symbol,
and losing one fragment loses the whole symbol, which is exactly the loss the
coding cannot absorb.

A path whose true MTU is the 1280 bytes IPv6 guarantees will still fragment at
1382; `datagram.ConservativeMaxPayload` is the value for such a path, set
through `OPT_DATAGRAM_MAX_PAYLOAD`. That is the only knob, and
`datagram.MaxPayloadForMTU` is how to derive a value for it. Sender and receiver
must be moved together: the receive bound comes from each node's own
`max_payload` and nothing on the wire negotiates it, so a one-sided change costs
the node every symbol its peers send at full size.

Do not pin `max_shard_size` to make a run pass. If the number is wrong, the number
is the finding.

Do not read the shard size out of the telemetry `config_info` label either:
telemetry initialises before config resolution, so that label reports the
pre-resolution value. `self_info` reports what the node runs on.

## Layout

```
docker-compose.yml     the stack: mock control plane, config proxy, collector,
                       5 x (rlnc-server + gateway), publisher
Dockerfile             one image, three entrypoints (gateway, mock, publisher)
gateway-entrypoint.sh  seeds gateway1's fixed CL identity, then execs the gateway
dynamic-config.json    the served DynamicConfig, verbatim
nginx.conf             config proxy: serves that file on the config path,
                       forwards everything else to the mock
otel-collector.yaml    OTLP/HTTP in, debug exporter out
run.sh                 build -> staged bring-up -> publish -> verify -> stop
verify.py              scrapes all five gateways and evaluates the gate
parse_v2_1.py          turns the collector's span log into per-generation numbers
keys/                  two committed test-only libp2p identities
out/                   run artifacts (git-ignored)
```

## Services

| Service | What it is |
|---|---|
| `bootstrap` | One mock binary replacing optimum-auth, optimum-bootstrap and its Postgres: registration, peer discovery, fork digest, JWKS and the JWT mint. |
| `configproxy` | nginx. Serves `dynamic-config.json` on `/api/v1/<chain>/<cluster>/config` and proxies everything else to `bootstrap`. |
| `otel-collector` | OTLP/HTTP receiver, debug exporter. Its stdout is the span record. |
| `rlnc1..5` | One `getoptimum/rlnc-server` per gateway, sharing a tmpfs `/dev/shm` volume with it. |
| `gateway1..5` | The gateways. `gateway1` is the publishing ingress. Telemetry is published on `127.0.0.1:48131..48135`. |
| `publisher` | A real libp2p CL peer: eth2 status handshake, then valid hoodi `beacon_block` messages, one per 12s slot. |

## Things that are load bearing

**Auth issuer must match byte for byte.** The gateway pins the JWT issuer to
`OPT_REMOTE_AUTH_URL`, and the mock's default issuer is `http://bootstrap:48124`.
That is why the service is named `bootstrap` and listens on 48124, and why auth
traffic goes direct while bootstrap traffic goes through the proxy.

**Cluster ID must be identical everywhere** and must appear in the mock's
`OPT_BENCH_AUTH_CLUSTER_IDS`. The cluster check happens inside the mump2p
handshake, so a mismatch presents as a mesh with no edges, never as an auth error.

**`OPT_API_KEY` must be non-empty** even though the mock ignores its value. An
empty key disables the auth manager; with no claims the chain ID is empty and the
gateway fatals at `InitRuntime`.

**Startup order is load bearing.** A gateway registers itself and then asks the
bootstrap for the peers registered before it, so `run.sh` starts one gateway at a
time and waits for each to serve HTTP before starting the next. Bring the stack up
with `run.sh`, not `docker compose up`.

**Every DynamicConfig key must be spelled as its JSON tag.** The whole config is
decoded from that one document and applied wholesale, so an unrecognised key lands
on the Go zero value with no fallback: for a bool, silently `false`. Note
`propagation_enabled`, not `propagation_disabled`. `verify.py` therefore checks the
*applied* config from `self_info` rather than trusting what was served.

**Shut down with SIGTERM.** `run.sh` uses `docker compose stop` so the 5s span
flush runs. A `kill` drops the tail of the trace.

**The collector's log driver matters.** The debug exporter prints every span
(~8 MB for a default run), and the default journald driver rate-limits and would
drop them. The compose file pins a file driver with a large cap, and `run.sh`
starts draining the log before any traffic exists.

**The `file` exporter is not a substitute for `debug`.** It writes OTLP JSON,
which the span parser cannot read.

**Public-IP probes are pointed at localhost.** The gateway probes public HTTP
endpoints to learn the address it registers with the bootstrap. On a host with
internet, all five would register the same public address and the mesh would not
form. `extra_hosts` makes each probe fail immediately so the gateway falls back to
interface inspection and registers its own container address.

## Checking that the gate bites

```sh
DEMO_DATAGRAM_ENABLE=false ./run.sh
```

Same stack, same publisher, symbols back on the stream path. It still delivers
100%, which is the point, and it fails:

```
[4/6] transport: which path carried the symbols
  gateway1  sends[hook]=      0 sends[fallback]=    0 ...
  FAIL: gateway1: sends[hook] is 0, so this node put nothing on the datagram path.
        Delivery alone would not have caught this: the forwarder falls back silently
```

## Knobs

| Variable | Default | Meaning |
|---|---|---|
| `DEMO_MSG_COUNT` | `8` | blocks to publish |
| `DEMO_WARMUP` | `20s` | publisher delay after connect, before the first block |
| `DEMO_SETTLE` | `25` | seconds to wait after the last publish |
| `DEMO_DATAGRAM_ENABLE` | `true` | `false` runs the negative control |
| `DEMO_SKIP_BUILD` | unset | reuse the existing binaries and image |
| `DEMO_BENCH_REPO` | `~/github/optimum-bench-v2` | where the mock and publisher sources live |

## Prerequisites

- A sibling `optimum-bench-v2` checkout, for `mocks/bootstrap` and
  `tools/bench-traffic`.
- This repo's `go.mod` `replace` for `mump2p-protocol` resolving locally. That
  replace is why the binaries are built on the host and copied into the image
  rather than built in a builder stage: a filesystem path outside the build
  context cannot resolve inside it.
- `getoptimum/rlnc-server`, `nginx:1.27-alpine` and
  `otel/opentelemetry-collector:0.115.0` pullable or already present.

The two keys under `keys/` are committed test-only identities with no access to
anything. They are fixed so the publisher can dial `gateway1` by peer ID without
discovery, and so `gateway1` can allowlist the publisher.
