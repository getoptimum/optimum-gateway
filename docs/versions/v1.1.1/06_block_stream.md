# Consumer Block Stream

The consumer block stream lets a gateway expose the beacon blocks it already
decodes to your own downstream consumers, over **WebSocket** or **gRPC**. It is
read-only, opt-in, and off by default. Each consumer authenticates with its own
short-lived token, separate from the gateway's API key.

![Consumer block-stream flow: mint an osc_ key, exchange it for a stream JWT, then connect over WebSocket or gRPC](/block_stream.png)

## Enable it

Add the stream fields to `config/app_conf.yml` and restart. Only `stream_enable`
is required; the rest have the defaults shown.

```yaml
stream_enable: true            # default: false
stream_only: false             # default: false — skip CL; never publishes (requires stream_enable)
stream_addr: 0.0.0.0:9600      # WebSocket listener (own port, off /metrics)
stream_grpc_addr: 0.0.0.0:9601 # gRPC listener
stream_require_auth: true      # verify consumer JWTs; false = loopback dev only
stream_max_conns: 256          # global connection cap
stream_max_conns_per_sub: 8    # per-consumer-key connection cap
stream_buffer_size: 64         # per-connection ring buffer (drop-on-overflow)
```

Set `stream_only: true` if you only want the stream and do not run a consensus
client. The gateway still joins the mesh but does not publish.

The gateway verifies consumer tokens against the JWKS at your `remote_auth_url`,
so it must point at the same auth service that mints the stream tokens.

> **Exposure.** The stream listeners are separate from the `/metrics` and
> `/health` port. Any non-loopback bind must sit behind TLS (native or a
> terminating proxy); disabling auth is allowed only on a loopback bind.

## Step 1 — mint a stream key (console)

Open [dev-console.getoptimum.io](https://dev-console.getoptimum.io) and select
the operator you are minting for. **Check the network picker (top right) first** —
the key is minted for the selected chain. Open the API keys section (labelled
**Manage Gateways** for a customer-role login), choose the **Stream consumers**
tab, create a key, name it after the downstream consumer, and copy the value.

* The raw `osc_` key is shown **once** and cannot be retrieved again — copy it
  before dismissing the dialog.
* Mint **one key per consumer**. Revocation and the per-connection cap are both
  per key, so a shared key cannot be cut off individually.

## Step 2 — exchange the key for a JWT

The consumer swaps its `osc_` key for a JWT. The key is the request body, so
there is no `Authorization` header on this call:

```bash
curl -X POST https://dev-auth.getoptimum.io/api/v1/stream/token \
  -H "content-type: application/json" \
  -d '{"stream_key":"osc_test_..."}'
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
(`as_<streamKeyId>`):

```json
{
  "aud": "stream",
  "iss": "https://dev-auth.getoptimum.io",
  "sub": "as_6772872d87e3e0e1f6a6c97630413fa9",
  "operator_id": "1",
  "chain_id": "560048",
  "cluster_ids": ["optimum_hoodi_v0_2"],
  "iat": 1786709057,
  "exp": 1786712657
}
```

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
TOKEN=$(curl -s -X POST https://dev-auth.getoptimum.io/api/v1/stream/token \
  -H "content-type: application/json" \
  -d '{"stream_key":"osc_test_..."}' | jq -r .access_token)
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

| Condition | WebSocket | gRPC |
| --- | --- | --- |
| Missing / bad token | `401 Unauthorized` | `Unauthenticated` |
| Invalid `mode` | `400 Bad Request` | `InvalidArgument` |
| Connection cap reached | `503 Service Unavailable` | `ResourceExhausted` |
| Unknown / suspended / revoked key (at mint) | `401 invalid_key` | `401 invalid_key` |

## Metrics

The gateway exports these on the telemetry port (`telemetry_port`, default
`48123`):

| Metric | Type | Meaning |
| --- | --- | --- |
| `mump2p_stream_connections` | gauge | currently open consumer connections |
| `mump2p_stream_events_sent_total` | counter | events written to consumers |
| `mump2p_stream_events_dropped_total` | counter | events dropped on buffer overflow (lag) |
| `mump2p_stream_auth_failures_total` | counter | connections rejected for bad/missing tokens |

```bash
curl -s http://GATEWAY_HOST:48123/metrics | grep mump2p_stream_
```

## Availability

The stream token endpoint is available on **dev** only for now
(`https://dev-auth.getoptimum.io`). The production endpoint is not live yet.
