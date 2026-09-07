# Consumer Block Stream

The consumer block stream lets a gateway expose the beacon blocks it already
decodes to your own downstream consumers, over **WebSocket** or **gRPC**. It is
read-only, opt-in, and off by default. Each consumer authenticates with its own
short-lived token, separate from the gateway's API key.

<svg viewBox="0 0 1340 940" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Consumer block-stream flow: mint an osc_ key, exchange it for a stream JWT, then connect over WebSocket or gRPC" style="width:100%;height:auto;max-width:1100px;display:block;margin:1.5rem auto;">
  <defs>
    <marker id="stream-op-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse" markerUnits="userSpaceOnUse">
      <path d="M0,0 L10,5 L0,10 L2.2,5 Z" fill="currentColor" fill-opacity="0.55"></path>
    </marker>
  </defs>

  <line x1="312" y1="170" x2="388" y2="170" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="350" y="156" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">mints</text>

  <line x1="592" y1="170" x2="708" y2="170" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="650" y="140" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">POST</text>
  <text x="650" y="157" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="500" fill="currentColor" fill-opacity="0.55">/stream/token</text>

  <line x1="972" y1="170" x2="1048" y2="170" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="1010" y="156" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">issues</text>

  <line x1="1150" y1="272" x2="1150" y2="548" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="1166" y="400" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">Bearer token</text>
  <text x="1166" y="420" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="500" fill="currentColor" fill-opacity="0.55">Authorization header</text>

  <line x1="1018" y1="650" x2="892" y2="650" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="955" y="636" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">connect</text>

  <line x1="468" y1="600" x2="312" y2="538" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="400" y="550" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">valid</text>

  <line x1="468" y1="740" x2="312" y2="764" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#stream-op-arrow)"></line>
  <text x="400" y="736" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.7" style="text-transform:uppercase">invalid</text>

  <path d="M 80,80 L 280,80 A 20,20 0 0 1 300,100 L 300,240 A 20,20 0 0 1 280,260 L 80,260 A 20,20 0 0 1 60,240 L 60,100 A 20,20 0 0 1 80,80 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="180" y="114" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Onboarding</text>
  <text x="180" y="152" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Partner Console</text>
  <text x="180" y="192" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">console.getoptimum.io</text>
  <text x="180" y="214" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">per-gateway scope</text>

  <path d="M 400,100 A 90,20 0 0 1 580,100 L 580,240 A 90,20 0 0 1 400,240 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <path d="M 400,100 A 90,20 0 0 0 580,100" fill="none" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="490" y="152" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Credential</text>
  <text x="490" y="182" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">osc_ key</text>
  <text x="490" y="208" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">stream scope</text>

  <path d="M 740,80 L 940,80 A 20,20 0 0 1 960,100 L 960,240 A 20,20 0 0 1 940,260 L 740,260 A 20,20 0 0 1 720,240 L 720,100 A 20,20 0 0 1 740,80 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="840" y="114" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Auth service</text>
  <text x="840" y="152" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">auth</text>
  <text x="840" y="192" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">verifies osc_ key</text>
  <text x="840" y="214" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">signs stream JWT</text>

  <path d="M 1060,100 A 90,20 0 0 1 1240,100 L 1240,240 A 90,20 0 0 1 1060,240 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <path d="M 1060,100 A 90,20 0 0 0 1240,100" fill="none" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="1150" y="152" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Token</text>
  <text x="1150" y="182" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Stream JWT</text>
  <text x="1150" y="208" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">aud=stream · ~1h</text>

  <path d="M 1050,560 L 1250,560 A 20,20 0 0 1 1270,580 L 1270,720 A 20,20 0 0 1 1250,740 L 1050,740 A 20,20 0 0 1 1030,720 L 1030,580 A 20,20 0 0 1 1050,560 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="1150" y="594" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Consumer</text>
  <text x="1150" y="632" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Client connects</text>
  <text x="1150" y="672" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">WS :9600</text>
  <text x="1150" y="694" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.72">gRPC :9601</text>

  <path d="M 560,490 L 860,490 A 20,20 0 0 1 880,510 L 880,730 A 80,80 0 0 1 800,810 L 500,810 A 20,20 0 0 1 480,790 L 480,570 A 80,80 0 0 1 560,490 Z" fill="#B87CFF" fill-opacity="0.07" stroke="#B87CFF" stroke-opacity="1" stroke-width="2"></path>
  <text x="516" y="532" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="#B87CFF" fill-opacity="1" style="text-transform:uppercase">Gateway</text>
  <text x="516" y="578" text-anchor="start" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="28" font-weight="400" letter-spacing="-0.5" fill="currentColor">Optimum Gateway</text>
  <circle cx="524" cy="628" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="540" y="632" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Reads Authorization header</text>
  <circle cx="524" cy="660" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="540" y="664" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Verifies signature via JWKS</text>
  <circle cx="524" cy="692" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="540" y="696" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Checks aud=stream · exp</text>
  <circle cx="524" cy="724" r="2.5" fill="#B87CFF" fill-opacity="0.85"></circle>
  <text x="540" y="728" text-anchor="start" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="15" font-weight="500" fill="currentColor" fill-opacity="0.78">Admits or rejects the stream</text>

  <path d="M 80,440 L 280,440 A 20,20 0 0 1 300,460 L 300,600 A 20,20 0 0 1 280,620 L 80,620 A 20,20 0 0 1 60,600 L 60,460 A 20,20 0 0 1 80,440 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="180" y="482" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Allowed</text>
  <text x="180" y="524" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">Block stream</text>
  <text x="180" y="566" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">blocks · blobs</text>

  <path d="M 80,690 L 280,690 A 20,20 0 0 1 300,710 L 300,850 A 20,20 0 0 1 280,870 L 80,870 A 20,20 0 0 1 60,850 L 60,710 A 20,20 0 0 1 80,690 Z" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32" stroke-width="1.25"></path>
  <text x="180" y="732" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="11" font-weight="700" letter-spacing="1.1" fill="currentColor" fill-opacity="0.55" style="text-transform:uppercase">Rejected</text>
  <text x="180" y="774" text-anchor="middle" font-family="&#34;ABC Diatype&#34;, &#34;Inter Tight&#34;, system-ui, sans-serif" font-size="22" font-weight="400" letter-spacing="-0.5" fill="currentColor">401</text>
  <text x="180" y="816" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="14" font-weight="500" fill="currentColor" fill-opacity="0.6">Unauthenticated</text>
</svg>

## Enable it

Add the stream fields to `config/app_conf.yml` and restart. Only `stream_enable`
is required; the rest have the defaults shown.

```yaml
stream_enable: true            # default: false
stream_only: false             # default: false — skip CL; never publishes (requires stream_enable)
stream_addr: 127.0.0.1:9600    # default — WebSocket listener (own port, off /metrics)
stream_grpc_addr: 127.0.0.1:9601 # default — gRPC listener
stream_require_auth: true      # verify consumer JWTs; false = loopback only
stream_max_conns: 256          # global connection cap
stream_max_conns_per_sub: 8    # per-consumer-key connection cap
stream_buffer_size: 64         # per-connection ring buffer (drop-on-overflow)
```

Set `stream_only: true` if you only want the stream and do not run a consensus
client. The gateway still joins the mesh but does not publish. Because it never
starts the CL host, `/health` reports `cl_peers`, `cl_health` and
`subscribed_topics` as `skipped` and returns 200 on the mesh signals alone
(with `telemetry_enable: true`, which drives the `mump2p_health` check).

The gateway verifies consumer tokens against the JWKS at your `remote_auth_url`,
so it must point at the same auth service that mints the stream tokens.

> **Exposure.** Listeners default to loopback (`127.0.0.1:9600` /
> `127.0.0.1:9601`), separate from the `/metrics` and `/health` port. Enabling
> the stream does not bind every interface. A non-loopback bind is an operator
> choice and must sit behind a trusted TLS-terminating proxy — the gateway does
> not terminate TLS. Disabling auth is allowed only on a loopback bind.

## Step 1 — mint a stream key (console)

Open the [Optimum Partner Console](https://console.getoptimum.io/) and select
the operator you are minting for. **Check the network picker (top right) first** —
the key is minted for the selected chain (Hoodi or Mainnet). Open the API keys
section (labelled **Manage Gateways** for a customer-role login), choose the
**Stream consumers** tab, create a key, name it after the downstream consumer,
and copy the value.

* The raw `osc_` key is shown **once** and cannot be retrieved again — copy it
  before dismissing the dialog.
* Mint **one key per consumer**. Revocation and the per-connection cap are both
  per key, so a shared key cannot be cut off individually.
* Use a **Hoodi** key with a Hoodi gateway, and a **Mainnet** key with a
  Mainnet gateway. The picker must match the gateway's chain, the same rule as
  `ogw_live_` API keys.

## Step 2 — exchange the key for a JWT

The consumer swaps its `osc_` key for a JWT. The key is the request body, so
there is no `Authorization` header on this call:

```bash
curl -X POST https://auth.getoptimum.io/api/v1/stream/token \
  -H "content-type: application/json" \
  -d '{"stream_key":"osc_live_..."}'
```

```json
{
  "access_token": "eyJhbGciOiJFUzI1NiIsImtpZCI6...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "operator_id": "1"
}
```

The `access_token` is the stream JWT. Decoded, its claims look like this — the
gateway checks `aud=stream`, and caps and revocation key off `sub`
(`as_<streamKeyId>`). Hoodi example:

```json
{
  "aud": "stream",
  "iss": "https://auth.getoptimum.io",
  "sub": "as_6772872d87e3e0e1f6a6c97630413fa9",
  "operator_id": "1",
  "chain_id": "560048",
  "cluster_ids": ["optimum_ethereum_hoodi_v0_1"],
  "iat": 1786709057,
  "exp": 1786712657
}
```

A Mainnet token uses `chain_id` `"1"` and
`cluster_ids: ["optimum_ethereum_mainnet_v0_1"]` (or the Mainnet cluster ID
Optimum assigned you at onboarding).

Tokens are short-lived (default `3600s`, ceiling `21600s`). A real consumer
re-runs this call to refresh before expiry — the same way an OAuth client renews
a token. The gateway verifies the signature locally against the published JWKS
and never calls back to auth, so revoking a key stops **new** tokens immediately
but an already-issued token keeps working until it expires. Keep the lifetime
short so a cutoff takes effect quickly.

## Step 3 — open a stream

There are two payload modes:

* **metadata** (default) — the decoded block event only: slot, proposer, roots,
  size, source, timing. No block bytes.
* **raw** — the same event **plus** the verbatim `ssz_snappy` block bytes as a
  base64 `raw` field.

Set the token once, then connect.

```bash
TOKEN=$(curl -s -X POST https://auth.getoptimum.io/api/v1/stream/token \
  -H "content-type: application/json" \
  -d '{"stream_key":"osc_live_..."}' | jq -r .access_token)
```

### WebSocket

```bash
wscat -H "Authorization: Bearer $TOKEN" \
  -c "ws://GATEWAY_HOST:9600/api/v1/stream/blocks?mode=metadata"
```

Each block arrives as one JSON frame (metadata mode):

```json
{
  "type": "block",
  "slot": 3706300,
  "proposer_index": 535778,
  "parent_root": "saTBS/MRxOtn3FMP5vZGLhMjcgDYoJo5ISec1nGpt8M=",
  "state_root": "HgD2msizU7N6A6663xIsK5LiauwtPVGTY4Gnbc9mfIg=",
  "block_size_bytes": 17365,
  "topic": "/eth2/c6ecb76c/beacon_block/ssz_snappy",
  "source": "mump2p",
  "received_at_ms": 1786689003070,
  "gateway_id": "ag_...",
  "fork_digest": "c6ecb76c",
  "stale": false
}
```

For **raw** mode, use `mode=raw`; the frame additionally carries the block bytes:

```json
{ "type": "block", "slot": 3706300, "...": "...", "raw": "8gAA...base64 ssz_snappy..." }
```

Browsers cannot set request headers, so pass the token via the
`Sec-WebSocket-Protocol` header instead — offer two values, the marker
`optimum.stream.v1` and `bearer.<jwt>`. The server negotiates only the marker
and never echoes the token.

### gRPC

```bash
grpcurl -plaintext \
  -import-path proto \
  -proto getoptimum/optimum_gateway/service/stream/v1/stream.proto \
  -H "authorization: Bearer $TOKEN" \
  -d '{"mode":"metadata"}' \
  GATEWAY_HOST:9601 \
  getoptimum.optimum_gateway.service.stream.v1.BlockStreamService/Subscribe
```

Each block is a `block` frame:

```json
{
  "block": {
    "slot": "3706300",
    "proposerIndex": "535778",
    "parentRoot": "saTBS/MRxOtn3FMP5vZGLhMjcgDYoJo5ISec1nGpt8M=",
    "stateRoot": "HgD2msizU7N6A6663xIsK5LiauwtPVGTY4Gnbc9mfIg=",
    "blockSizeBytes": "17365",
    "topic": "/eth2/c6ecb76c/beacon_block/ssz_snappy",
    "source": "mump2p",
    "receivedAtMs": "1786689003070",
    "gatewayId": "ag_...",
    "forkDigest": "c6ecb76c"
  }
}
```

The stream is open-ended, so stop it yourself: Ctrl-C for WebSocket, or
`-max-time <seconds>` for gRPC. A trailing gRPC `DeadlineExceeded` is your own
timeout expiring, not a server error.

### Lag signal

Each connection has a bounded buffer. If a consumer reads too slowly and the
buffer overflows, the gateway drops events and sends a lag frame with the
cumulative dropped count, then resumes. A slow consumer never stalls the gateway.

```json
{ "type": "lagged", "dropped": 12 }
```

Over gRPC the same signal is a `lagged` frame: `{ "lagged": { "dropped": "12" } }`.

## Errors

Connection-time (gateway WebSocket / gRPC):

| Condition | WebSocket | gRPC |
| --- | --- | --- |
| Missing / bad token | `401 Unauthorized` | `Unauthenticated` |
| Invalid `mode` | `400 Bad Request` | `InvalidArgument` |
| Connection cap reached | `503 Service Unavailable` | `ResourceExhausted` |

Token exchange (`POST /api/v1/stream/token` on the auth service) is a different call. Unknown, suspended, or revoked `osc_` keys fail there as `401 invalid_key` — the gateway never sees that status on the stream.

## Metrics

Consumer stream series are exported on the telemetry port (`telemetry_port`,
default `48123`) when `stream_enable` is true. See
[Metrics — Consumer Block Stream](metrics.md#consumer-block-stream) for names
and types (`mump2p_stream_*`).

```bash
curl -s http://localhost:48123/metrics | grep mump2p_stream_
```

## Availability

The stream is live on **Hoodi and Mainnet**. Mint the consumer key in the
[Partner Console](https://console.getoptimum.io/) with the matching network
selected, and exchange it at `https://auth.getoptimum.io/api/v1/stream/token`.

| Network | Console picker | Cluster ID (typical) |
| --- | --- | --- |
| Hoodi | Hoodi | `optimum_ethereum_hoodi_v0_1` |
| Mainnet | Mainnet | `optimum_ethereum_mainnet_v0_1` |

Use the cluster ID Optimum assigned you at onboarding if it differs from the
table.
