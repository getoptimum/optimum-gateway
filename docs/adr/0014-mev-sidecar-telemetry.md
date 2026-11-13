# ADR-0014: MEV sidecar telemetry collection (mev-boost / Commit-Boost)

**Status:** Draft
**Date:** 2026-10-16

---

## Context

MumBoost recommends MEV sidecar config changes (relay set, timeouts, timing
games, min bid) to onboarded operators. The console's copy of an operator's
config is self-declared and unchecked, so recommendations may rest on stale
data.

This ADR makes it checkable: **do the config an operator runs, and the
behaviour it produces, match what the console stores?** That needs
per-proposal evidence: which relays were asked and when, which answered, how
requests were timed, and which settings took effect. Metrics cannot provide
it, because no sidecar metric has a `slot` label.

The gateway already runs next to the sidecar and already pushes to Mimir
(`telemetry/remote_write.go`) and Loki (`telemetry/loki_writer.go`). That
makes it the cheapest place to collect from.

### What each source can tell us

| Source | Gives | Lacks |
| --- | --- | --- |
| Sidecar metrics | Per-relay health, status codes, latency and bid aggregates | `slot`; configured values |
| Sidecar logs | Per-slot events: slot, proposer pubkey, relay, bid | A stable format: messages can change in any release |
| Sidecar config file | What the operator wrote | Behaviour; mev-boost's CLI-only flags; proof the process loaded it |

### Relay integration setups

The sidecar talks to each relay in one of four ways. That decides what
"behaves as configured" looks like:

| # | Setup | Selected by | Shows in telemetry as |
| --- | --- | --- | --- |
| 1 | Websocket stream | Commit-Boost `get_header = "stream"` | `received new header from ws stream` (`first_bid_latency`, `updates`) |
| 2 | Single request; the relay holds it (timing games as a service) | Default; relay behaviour | One `received new header` just before the deadline |
| 3 | Polling (timing games in the sidecar) | `enable_timing_games`, `frequency_get_header_ms` | Commit-Boost: several `received new header` per relay per slot. mev-boost: one `requesting header` per poll, debug only |
| 4 | Single request; the relay answers immediately | Default; relay behaviour | One `received new header` shortly after the request |

Setups 2 and 4 look identical in config. Only the answer latency against the
deadline tells them apart.

Config and behaviour can also diverge:

- One partner's streaming relay pushes a single bid about 4 ms before the
  deadline, so it is effectively setup 2.
- A stream that fails to connect falls back to HTTP.

Surfacing gaps like these is the point of this ADR. The comparison runs in
the backend.

### What the sidecars log

Key events only. The full list is in
[Appendix A](#appendix-a-sidecar-log-events).

**mev-boost** `v1.12.0` (`@203bb96`) writes flat logrus JSON (`-json`;
`time` in ms, `msg`) to **stdout only**. getPayload lines lack the pubkey, so
they join to getHeader on `slotUID`, or on `slot` + `blockHash`.

| Event (`msg`) | Level | Key fields | Tells us |
| --- | --- | --- | --- |
| `getHeader request start - … into slot …` | INFO | `slot`, `pubkey`, `msIntoSlot`, `slotUID` | When the beacon node asked |
| `bid received` / `ignoring bid below min-bid value` | **DEBUG** | `url`, `value`, `blockHash` (no latency) | Per-relay bid or rejection |
| `error calling getHeader on relay` | WARN | `url`, `error` | Relay timeout or transport error |
| `best bid` / `no bid received` | INFO | `value`, `blockHash`, `relays` | Winner and every relay offering it, or 204 |
| `calling getPayload` | INFO | `url`, `slot`, `blockHash` | Payload requested from each relay |
| `successfully submitted blinded block to relay` / `no payload received from relay!` | INFO / ERROR | **no `url`** | Payload result; delivering relay unknown |
| Bid stream (websocket) | — | — | Not supported |

**Commit-Boost** `v0.11.0` / `v0.12.0-rc1` (`@aaaa136`) writes `tracing`
JSON (`timestamp` in µs, `message`) to daily files `pbs.YYYY-MM-DD`. Span
fields (`req_id`, `slot`, `validator`, `block_hash`) are nested under
`"span"`. getPayload joins to getHeader on `slot` + `block_hash`. Per-relay
payload lines carry no span.

| Event (`message`) | Level | Key fields | Tells us |
| --- | --- | --- | --- |
| `new request` | INFO | `ms_into_slot`, `span.slot`, `span.validator` | When the beacon node asked |
| `received new header` | INFO | `relay_id`, `value_eth`, `block_hash`, `latency` | Each HTTP bid, before validation (one per poll with timing games) |
| `received new header from ws stream` | INFO | `relay_id`, `value_eth`, `first_bid_latency`, `updates` | Final streamed bid only; updates are not logged |
| *(no message)* | ERROR | `err`, `relay_id` | Relay timeout or rejection, including below min bid |
| `no header from relay` | **DEBUG** | `relay_id`, `latency` | Relay 204 |
| `auction winner` / `no header available for slot` | INFO | `relay_id`, `value_eth`, `block_hash` | Winner, or 204 |
| `received unblinded block` | **DEBUG** | `relay_id`, `latency` (**no span**) | Which relay delivered the payload |
| `received unblinded block (v1/v2)` / `CRITICAL: no payload received…` | INFO / ERROR | `block_hash` | Payload result |

### Checking stored config against telemetry

The config file shows what the operator wrote. Telemetry shows whether the
sidecar behaves that way:

| Console setting | Commit-Boost evidence | mev-boost evidence |
| --- | --- | --- |
| Relay set | `relay_id` on metrics, bid and error lines | `relay` on metrics; startup `relay #N` lines; `url` on debug lines |
| `min_bid_eth` | ERROR `bid below minimum: min: X got Y` | Startup `min bid set to …`; `mev_boost_bids_below_min_bid_total`; debug `ignoring bid below min-bid value` |
| `late_in_slot_time_ms` | `late in slot, skipping relay requests` (`threshold`) | `getHeader request skipped …` (`lateInSlotTimeMs`) |
| `timeout_get_header_ms` | `new request.ms_into_slot` + `latency` of the last accepted bid | Request start + `best bid` timestamp; debug for per-relay timing |
| Timing games (`target_first_request_ms`, `frequency_get_header_ms`) | One `received new header` per poll. Send time (timestamp − `latency`) checks the target; spacing checks the frequency | Debug only: `waiting to send header request via timing games` (`targetMs`), `requesting header` per poll |
| `get_header = "stream"` | `received new header from ws stream`. `stream failed, falling back to http` means it did not apply | Not supported |
| `mux` | Debug `using mux config` (`mux_id`) | Not logged; inferred only from `url` on debug lines |
| `timeout_get_payload_ms` | Not directly observable (payload failures only) | Same |

## Decision

The gateway collects sidecar telemetry on three channels, through pipelines it
already owns. **It filters but never rewrites:** log lines ship exactly as
written. Per-slot assembly and the comparison with the console's config happen
in the backend.

```text
mev-boost / Commit-Boost
   /metrics     ──► gateway Prometheus agent ──► Mimir   (aggregates, relay health)
   JSON logs    ──► allowlist filter ──► sidecar LokiWriter ──► Loki (per-slot events)
   config file  ──► field allowlist ──┐
   channel state ─────────────────────┴──► POST bootstrap mev_state  (latest snapshot)
```

### 1. Gateway config (opt-in, off by default)

```yaml
mev_sidecar_kind:  ""    # OPT_MEV_SIDECAR_KIND   "mev-boost" | "commit-boost"
mev_metrics_addrs: []    # OPT_MEV_METRICS_ADDRS  comma-separated host:port
mev_log_path:      ""    # OPT_MEV_LOG_PATH       file or glob, e.g. /mev-logs/pbs.*
mev_config_path:   ""    # OPT_MEV_CONFIG_PATH    YAML (mev-boost) or TOML (Commit-Boost)
```

- Every field defaults to empty, and each one enables one channel. Empty means
  off.
- `mev_sidecar_kind` is required once any other field is set. It selects the
  allowlist and the config parser, and the snapshot needs it before the first
  scrape.
- `mev_metrics_addrs` is a list because some operators run several sidecars.
  Prometheus separates them by `instance`.
- `mev_log_path` accepts a glob because Commit-Boost rotates its log daily.
  The tailer follows the newest match.
- All channels require `remote_push_enable=true` and auth, like the existing
  Loki and Mimir push (`cmd/main.go`). Validation rejects any other
  combination instead of silently doing nothing.
- The log directory and config file are mounted read-only.

### 2. Metrics → Mimir

A second job is added to `ScrapeConfigs` in `buildMimirPromConfig`:

```yaml
job_name:        mev_sidecar
scrape_interval: 15s            # mimirScrapeInterval, same as optimum_gateway
scrape_timeout:  10s
metrics_path:    /metrics
static_configs:
  - targets: <mev_metrics_addrs>
    labels:                     # required, see below
      gateway_id:         <cfg.GatewayID>
      gateway_cluster_id: <cfg.GatewayClusterID>
      org_id:             <cfg.OrgID>
      sidecar:            <mev_sidecar_kind>
      # + cfg.MetaLabels
metric_relabel_configs:
  - source_labels: [__name__]
    action: keep
    regex: (mev_boost_|cb_pbs_).*
```

- **Target labels are required.** The gateway's own series get `gateway_id`,
  `gateway_cluster_id`, `org_id` and the meta labels from
  `prometheus.WrapRegistererWith` (`telemetry/metrics.go`). Scraped series
  bypass that registry and would otherwise reach Mimir with none of them.
- **The `keep` regex is required.** mev-boost also exposes Go and process
  metrics (`WritePrometheus(w, true)`), which would collide with the
  gateway's.

Series kept:

| Sidecar | Series | Labels | Note |
| --- | --- | --- | --- |
| mev-boost | `mev_boost_beacon_node_status_code_total` | `http_status_code`, `endpoint` | What the beacon node received (200/204) |
| | `mev_boost_relay_status_code_total` | `http_status_code`, `endpoint`, `relay` | For getPayload, records the *expected* code |
| | `mev_boost_relay_latency` | `endpoint`, `relay` | ms, every poll included |
| | `mev_boost_relay_last_slot` | `relay` | |
| | `mev_boost_bid_values` | `relay` | Each bid is recorded **twice** (before and after validation) |
| | `mev_boost_bids_below_min_bid_total` | `relay` | |
| | `mev_boost_winning_bid_value` | — | In **wei** |
| | `mev_boost_millisec_into_slot` | `endpoint` | |
| Commit-Boost | `cb_pbs_relay_status_code_total` | `http_status_code`, `endpoint`, `relay_id` | `555` = timeout or request error; `556` = stream transport failure |
| | `cb_pbs_relay_latency` | `endpoint`, `relay_id` | Time to first bid, for streams |
| | `cb_pbs_relay_last_slot` | `relay_id` | |
| | `cb_pbs_relay_header_value` | `relay_id` | gwei, last valid bid |
| | `cb_pbs_relay_stream_{connect_latency,updates,invalid_frames_total,fallback_total}` | `relay_id` | Stream health |
| | `cb_pbs_beacon_node_status_code_total` | `http_status_code`, `endpoint` | |

mev-boost histograms use the VictoriaMetrics default buckets
(`0.005 … 10`). Values in ms and wei land almost entirely in `+Inf`, so only
`_sum` and `_count` are useful. For mev-boost, Mimir gives health and
averages; distributions come from logs.

### 3. Logs → Loki

Filtered lines are stored in Optimum's Loki (`remote_push_loki_url`, the same
instance that holds gateway logs), in their own stream. That is the
per-proposal record the console checks against. The snapshot (§5) holds only
current state. Unfiltered logs never leave the host.

#### 3.1 Tailer

- Follows `mev_log_path`, or the newest file matching the glob, across
  rotation and truncation.
- Starts at end-of-file. There is no backfill, and delivery is at most once.
  Gaps show up as `logs.last_line_ms` in the snapshot.
- Caps lines at 16 KiB. Longer lines are dropped and counted.
- The Loki timestamp is the read time. The sidecar's own timestamp stays in
  the line, and timing analysis uses that one.

#### 3.2 Allowlist filter

Each line is decoded only far enough to read its message key and check a few
fields. Matching lines ship unchanged; everything else is dropped.

The rules are data in `pkg/service/mevsidecar/allowlist.go`, keyed by sidecar
kind and tagged with the versions they were checked against. The full list is
in Appendix A.

| Kind | Key | Keep when |
| --- | --- | --- |
| mev-boost | `msg` | Exact or prefix match on the getHeader, bid, getPayload and startup messages (A.1). Also the access-log lines `http: GET /eth/v1/builder/header/…` and `http: POST /eth/v{1,2}/builder/blinded_blocks…` |
| Commit-Boost | `message` | Exact or prefix match on the messages in A.2. `new request` only with `ms_into_slot`, which excludes the register and status routes. `Responded with …` only when `span.method` is the header or blinded_blocks route |
| Commit-Boost | — | No `message`, but `relay_id` and `err` present (relay timeouts and rejections) |
| both | — | Lines that are not JSON are dropped and counted |

When a sidecar release renames a message, its lines are dropped rather than
misread, and the drift metric (§3.4) shows it.

#### 3.3 Separate Loki stream

Sidecar lines get their own `LokiWriter`:

- The existing writer's `streamLabels` fix `job=optimum-gateway`, so sidecar
  lines would be indistinguishable from gateway logs.
- Its 1000-entry channel drops silently when full, so a sidecar burst could
  push out gateway lines.

`NewLokiWriter` takes the stream labels as a parameter. The second instance
uses:

```text
gateway_id, operator_id, cluster, job=mev-sidecar, sidecar=<kind>
```

`slot`, `pubkey` and `relay` stay in the line, never in labels (cardinality).
LogQL `| json` extracts them at query time.

#### 3.4 Self-observability

```text
mump2p_gateway_mev_log_lines_total{result="shipped|filtered|not_json|too_long"}
mump2p_gateway_loki_dropped_lines_total{job="optimum-gateway|mev-sidecar"}
```

If `shipped` drops to zero while `filtered` keeps rising, the sidecar was
upgraded and the allowlist has drifted. The drop counter also covers the
existing gateway writer.

### 4. Config file → snapshot

The gateway extracts an allowlist of fields from the config file. This is the
only place it parses, and the reason is to keep secrets on the host:
Commit-Boost `[[relays]].headers` holds relay API keys, and `[[mux]].loader`
can contain URLs with credentials.

| Field | mev-boost | Commit-Boost |
| --- | --- | --- |
| `min_bid_eth` | CLI `-min-bid`; also the startup line `min bid set to …` | `[pbs]` (global only) |
| `timeout_get_header_ms` | YAML | `[pbs]`, mux override |
| `timeout_get_payload_ms` | CLI `-request-timeout-getpayload` | `[pbs]` |
| `late_in_slot_time_ms` | YAML | `[pbs]`, mux override |
| `relays[].id`, `.url` | YAML `relays` (or CLI `-relays`) | `[[relays]]` |
| `relays[].get_header` (`http` \| `stream`) | — | `[[relays]]` |
| `relays[].enable_timing_games`, `.target_first_request_ms`, `.frequency_get_header_ms` | YAML, per relay | `[[relays]]`, per relay |
| `mux[]`: `id`, validator **count**, `relays`, timeout overrides | YAML `mux` | `[[mux]]` |

- Absent fields are reported as absent, never filled with defaults.
- Only the listed fields leave the host: never the raw file, never unknown
  keys.
- The file is re-read when its mtime changes. It can still differ from the
  running process: mev-boost reloads only with `-watch-config`, and flags may
  differ. That is why it is checked against behaviour.

### 5. Bootstrap snapshot

```text
POST /api/v2/gateways/mev_state
Authorization: Bearer <services token>
```

- Identity comes from the token's `sub`. Each post is an idempotent upsert on
  `gateway_id`.
- Sent when the config changes, when a channel changes state (target up or
  down, log gone stale), and every 5 min regardless. The register heartbeat
  (every 5–30 min, jittered) is too infrequent to reuse.

```json
{
  "mev": {
    "sidecar_kind": "commit-boost",
    "sidecar_version": "v0.11.0",
    "metrics": { "configured": true, "targets": 2, "targets_up": 1, "last_scrape_ms": 1791051083169 },
    "logs":    { "configured": true, "last_line_ms": 1791051082400, "shipped_1h": 412, "filtered_1h": 9120 },
    "config": {
      "read_ok": true,
      "read_at_ms": 1791051000000,
      "min_bid_eth": "0.05",
      "timeout_get_header_ms": 950,
      "timeout_get_payload_ms": 4000,
      "late_in_slot_time_ms": 2000,
      "relays": [
        { "id": "relay-a", "url": "https://0xabc…@relay-a.example", "get_header": "stream" },
        { "id": "relay-b", "url": "https://0xdef…@relay-b.example", "get_header": "http",
          "enable_timing_games": true, "target_first_request_ms": 1500, "frequency_get_header_ms": 300 }
      ],
      "mux": [
        { "id": "high-value", "validators": 128, "late_in_slot_time_ms": 2500, "relays": ["relay-a"] }
      ]
    }
  }
}
```

- `sidecar_version` is the last version seen in a shipped line. mev-boost
  puts it on every line; Commit-Boost only in `starting PBS service`. It is
  `null` until seen.
- The snapshot has no observed relay set. The backend derives it from the
  `relay` / `relay_id` labels in Mimir.

### 6. Operator setup

| | mev-boost | Commit-Boost |
| --- | --- | --- |
| Metrics | `-metrics` (off by default) and `-metrics-addr 0.0.0.0:18551` (the default `localhost` is unreachable from the gateway container) | On by default at `[metrics] start_port` (10000) |
| Logs | `-json`. Stdout only: needs a tee or logging driver that writes a file the gateway can mount | `[logs.file] enabled = true` (JSON by default) |
| Per-relay bids | `-loglevel debug` | Default INFO |
| Delivering relay | Not logged at any level | `[logs.file] level = "debug"` |
| Config | `-config <yaml>`, mounted read-only. `-min-bid` and request timeouts are CLI-only | `config.toml`, mounted read-only |

## Consequences

- New package `pkg/service/mevsidecar` (tailer, allowlist, config reader,
  snapshot reporter), plus one scrape job, one `LokiWriter` instance and one
  bootstrap endpoint. All off unless configured.
- **Commit-Boost** at default log levels: every stored setting can be checked
  except `mux` and `timeout_get_payload_ms`.
- **mev-boost** at INFO: only the relay set, min bid and late-in-slot cut-off
  can be checked. Timing games and per-relay timing need `debug`. The active
  mux cannot be confirmed.
- The allowlist must track sidecar releases. The drift metric flags a
  mismatch but cannot prevent one.
- The existing gateway Loki writer gains a drop counter.

---

## Appendix A: Sidecar log events

Levels: **I** info, **W** warn, **E** error, **D** debug. ✓ = kept by the allowlist.

### A.1 mev-boost v1.12.0

Source: [`flashbots/mev-boost@203bb96`](https://github.com/flashbots/mev-boost/tree/203bb9659eea613caefd198c67df4c6a8e6bf5d6)

Fields on every getHeader line: `method=getHeader`, `slot`, `parentHash`,
`pubkey`, `ua`, plus `slotUID` inside `getHeader`, plus `url` per relay.
Every line also carries `version` unless `-log-no-version` is set.

| Level | `msg` | Key fields | Meaning |
| --- | --- | --- | --- |
| I ✓ | `getHeader request start - <ms> milliseconds into slot <slot>` | `msIntoSlot`, `slotUID` | getHeader arrival in the slot |
| W ✓ | `getHeader request skipped because we are already past the lateInSlotTimeMs deadline` | `msIntoSlot`, `lateInSlotTimeMs` | Late skip; beacon node gets 204 |
| D ✓ | `waiting to send header request via timing games` | `url`, `targetMs`, `delayMs` | TG delay before first request |
| D ✓ | `sending multiple header requests via timing games` | `url`, `frequencyMs`, `timeoutLeftMs` | TG polling starts |
| D ✓ | `requesting header` | `url` | One per HTTP request, so one per poll |
| W ✓ | `error calling getHeader on relay` | `url`, `error` | Transport error or timeout (`context deadline exceeded`) |
| D ✓ | `no-content response` | `url` | Relay 204 |
| W ✓ | `error status code` | `url`, `error` | Relay non-200/204 |
| W ✓ | `no headers received via timing games` | `url` | |
| D ✓ | `received headers from relay via timing games` | `url`, `totalBids` | Polled bids, collapsed to the latest received |
| E ✓ | `bid pubkey mismatch…` / `error verifying relay signature` / `failed to verify relay signature` / `proposer and relay parent hashes are not the same` / `error decoding bid` / `relay responded with empty block hash` | `url`, `value`, `blockHash` | Bid rejected |
| W ✓ | `ignoring bid with 0 value` | | |
| D ✓ | `bid received` | `url`, `value`, `blockHash`, `blockNumber` | Per-relay bid (no latency) |
| D ✓ | `ignoring bid below min-bid value` | same | Min-bid rejection |
| D ✓ | `ignoring less profitable bid` / `equally profitable bid lost tiebreaker` / `new best bid` | same | Auction steps |
| I ✓ | `no bid received` | chain (no `slotUID`) | Beacon node gets 204 |
| I ✓ | `best bid` | `value`, `blockHash`, `relays` | Winner and every relay that offered it |
| I ✓ | `http: GET /eth/v1/builder/header/… <status>` | `status`, `duration` | Status and total time to the beacon node |
| I ✓ | `submitBlindedBlock request start - <ms> milliseconds into slot <slot>` | `slot`, `blockHash`, `slotUID`, `msIntoSlot` | getPayload arrival |
| W ✓ | `latest slotUID is for slot X rather than payload slot Y` / `no bid for this payload found, was getHeader called before?` | | getPayload without a matching getHeader |
| I ✓ | `calling getPayload` | `url`, `version` | One per relay; sent to all relays |
| W ✓ | `error calling getPayloadV1\|V2 on relay` / `unexpected status code …` / `failed to submit signed blinded block after retries` | `url`, `error` | Per-relay payload failure |
| I ✓ | `successfully submitted blinded block to relay` (V1) / `successfully submitted signed blinded beacon block to relay` (V2) | **no `url`** | Payload delivered |
| E ✓ | `no payload received from relay!` / `no relay accepted the signed blinded beacon block submission!` | `relaysWithBid` (no slot) | Payload failure |
| I ✓ | `starting mev-boost` / `relay #N: … [timing games: enabled]` / `min bid set to …` / `using N relays` / `mux config loaded with …` / `config file changed, reloading...` / `successfully loaded new config…` | | Startup and config |
| D/I ✗ | `handling request`, `calling registerValidator on relays`, `sending the registerValidator request` (dumps the full request), relay status checks, content negotiation, `http: GET /eth/v1/builder/status` | | Dropped |

### A.2 Commit-Boost v0.11.0 / v0.12.0-rc1

Source: [`Commit-Boost/commit-boost-client@aaaa136`](https://github.com/Commit-Boost/commit-boost-client/tree/aaaa136178921698013b9e9f09a2dfd64b6f70ab)

Span fields under `"span"`: `method` (route template), `req_id`, `slot`,
`parent_hash`, `validator` on get_header; `slot`, `block_hash`,
`block_number`, `parent_hash` on submit_block.

| Level | `message` | Key fields | Meaning |
| --- | --- | --- | --- |
| I ✓ | `new request` (with `ms_into_slot`) | `ua`, `ms_into_slot` | get_header or submit_block arrival |
| D ✓ | `using mux config` / `using default config` | `mux_id`, `relays`, `pubkey` | Which relay set applied |
| W ✓ | `late in slot, skipping relay requests` | `ms_into_slot`, `threshold` | Late skip |
| W ✓ | `invalid user-supplied timeout header, using …ms` | `header` | Bad `X-Timeout-Ms` from the beacon node |
| D ✓ | `TG: waiting to send first header request` / `TG: sending first header request now` / `TG: sending multiple header requests` / `TG: received headers from relay` | `relay_id`, `target_ms`, `send_freq_ms`, `n_headers` | Timing games |
| E/W ✓ | `TG: error sending header request` / `TG: no headers received` | `relay_id`, `err` | |
| I ✓ | `received new header` | `relay_id`, `value_eth`, `block_hash`, `latency`, `version`, `header_size_bytes` | One per HTTP bid, logged **before** validation |
| D ✓ | `no header from relay` | `relay_id`, `code`, `latency` | Relay 204 |
| D ✓ | `ws connected` / `no header` | `relay_id`, `connect_latency` | Stream lifecycle |
| W ✓ | `ws stream error` / `invalid ws frame, skipping` / `stream failed, falling back to http get_header` | `relay_id`, `err` | Stream failure or fallback |
| I ✓ | `received new header from ws stream` | `relay_id`, `value_eth`, `block_hash`, `connect_latency`, `first_bid_latency`, `validate_latency`, `updates`, `invalid_frames` | Final streamed bid only |
| E ✓ | *(no message)* | `err`, `relay_id` | `err="Timed Out"`, or a rejection such as below-min-bid, signature or parent-hash failure, non-2xx, decode error |
| I ✓ | `auction winner` | `relay_id`, `value_eth`, `block_hash` | Best bid |
| I ✓ | `received header` | `value_eth`, `block_hash` | Returned to the beacon node |
| I ✓ | `no header available for slot` | | 204 to the beacon node |
| W ✓ | `client timed out` | | Beacon node gave up before the response |
| I ✓ | `Responded with <status> in <ms> ms` (header and blinded_blocks routes only) | | Final status to the beacon node |
| D ✓ | `received unblinded block` / `received response for v2 submit_block` | `relay_id`, `latency`, `retry` (**no span**) | Which relay delivered |
| W ✓ | `failed to get payload (this might be ok if other relays have it)` / `relay does not support the v2 submit_block endpoint…` / `relay sent OK response for v2 submit_block, expected 202 Accepted` | `relay_id`, `err` | |
| I ✓ | `received unblinded block (v1)` / `received unblinded block (v2)` | (no `relay_id`) | Payload delivered |
| E ✓ | `CRITICAL: no payload received from relays…` | `err`, `block_hash` | Payload failure |
| I ✓ | `starting PBS service` | `version`, `commit_hash`, `chain` | Startup |
| I/E/D ✗ | register_validator and status events, `Responded with` on other routes, content negotiation, `fetching parent block` | | Dropped |
