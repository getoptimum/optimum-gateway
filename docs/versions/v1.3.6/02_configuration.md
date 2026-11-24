# Configuration

> **Prerequisites:** [Quick Start](01_quick_start.md) complete, including a credential — an [API key](01_quick_start.md#generate-your-api-key), or a join key if you use [Gateway Self-Enrollment](07_gateway_self_enrollment.md).

## Basic Setup

Everything that identifies your gateway — `gateway_id`, `chain`, and validator scope — comes from your **credential**. Use an **API key** (`ogw_`) for single-gateway deployments, or a **join key** (`ojk_`) for fleet self-enrollment. The only operational fields you set in YAML are cluster ID, ports, telemetry, and identity directories.

Create `config/app_conf.yml`:

```yaml
log_level: info
gateway_cluster_id: optimum_ethereum_hoodi_v0_1   # REQUIRED — assigned by Optimum

agent_lib_p2p_port: 33212
agent_mump2p_port: 33213
# Behind a load balancer: the address other gateways dial. See Mesh address below.
# agent_mump2p_announce_ip: 203.0.113.7
# agent_mump2p_announce_port: 40001
# No address others can dial: register, but announce nothing. Do not set with announce_ip.
# agent_mump2p_not_dialable: true
telemetry_enable: true
telemetry_port: 48123
identity_libp2p_dir: /tmp/libp2p
identity_mump2p_dir: /tmp/mump2p
```

> **Credentials via environment, not YAML.** Set `OPT_API_KEY=ogw_live_...` or `OPT_JOIN_KEY=ojk_live_...` in the environment — never in YAML or image layers. `api_key` and `join_key` are **mutually exclusive**; setting both is a startup error.

## Authentication

Your gateway authenticates only with Optimum's auth service (`https://auth.getoptimum.io`), never with peer gateways. On start (and periodically after), it obtains two short-lived JWTs and keeps them refreshed automatically. There is no token to manage yourself.

### API key (default)

Your gateway sends its API key to `POST /api/v1/auth/token`. The key determines your chain (Hoodi vs Mainnet), gateway ID, and validator list — there is **no** `chain` or `gateway_id` field for partners to set on this path.

### Join key (fleet self-enrollment)

With `OPT_JOIN_KEY`, the gateway **enrolls once** on first boot (`POST /api/v1/gateways/enroll`), persists an asymmetric credential to disk, and mints JWTs with a client assertion on every boot after that. Chain, type, and cluster scope come from the join key. Set a unique `OPT_GATEWAY_ID` per host as the enrollment label; runtime `gateway_id` in metrics and `/health` comes from the JWT `sub` claim after enroll.

See [Gateway Self-Enrollment](07_gateway_self_enrollment.md) for minting, storage, and fleet rollout.

* **Services token:** attached as an `Authorization: Bearer` header on calls to Optimum's central services, namely bootstrap registration and heartbeat, and (when enabled) Loki/Mimir remote push.
* **Peer handshake token:** presented during peer-to-peer (libp2p) handshakes with other gateways, so each side can confirm the other is a legitimate gateway on the same chain before exchanging traffic.

Both tokens are ES256-signed and valid for 6 hours; the gateway refreshes them well before expiry. Peers verify each other's handshake token locally against Optimum's published JWKS, with no per-connection callback to the auth service. If your API key or enrolled credential is revoked or suspended, refresh stops and the gateway loses access at the next token expiry.

## Networks (Hoodi / Mainnet)

The network is selected by your **credential**, not by config. A Hoodi API key or join key runs Hoodi; a Mainnet one runs Mainnet. Set the matching `gateway_cluster_id` you were assigned during onboarding (Hoodi partners use `optimum_ethereum_hoodi_v0_1`; Mainnet cluster ID is provided by Optimum during onboarding). To move a gateway to Mainnet, obtain a Mainnet credential from Optimum and set the assigned Mainnet `gateway_cluster_id`, then restart.

Confirm the active network after start:

```sh
curl -s http://localhost:48123/api/v1/self_info | jq '.chain, .fork_digest'
```

## Direct CL Peers

`direct_cl_peers` is the CL multiaddr the gateway dials and keeps retrying. When the list is set, it is also an allowlist: any other libp2p peer is disconnected. When it is empty, no peer-ID filter is applied.

```yaml
direct_cl_peers:
  - /ip4/192.168.1.2/tcp/9000/p2p/16Uiu2HAmGj6AoMKe7fNrghXwwRgivXLpji3Hkm4QEGVpsHZYKwPQ
```

Which peer ID goes in that multiaddr, and the flag on each CL, is in [Connecting your CL client](08_cl_clients.md). Firewall `agent_lib_p2p_port` (default `33212`) to your CL either way.

## Mesh address

By default the gateway listens on `agent_mump2p_port` (`33213`) and announces the public IP (IPv4 and/or IPv6) and interface IPs it detects on the host. Remote gateways dial that address. The CL port is separate and stays `33212`.

### Load balancer

When inbound mesh traffic reaches the gateway through a TCP load balancer or port forward, set the external address other gateways should dial:

```yaml
agent_mump2p_announce_ip: 203.0.113.7
agent_mump2p_announce_port: 40001   # omit (or 0) to announce agent_mump2p_port
```

Or pass them via environment variables:

```sh
-e OPT_AGENT_MUMP2P_ANNOUNCE_IP=203.0.113.7 \
-e OPT_AGENT_MUMP2P_ANNOUNCE_PORT=40001
```

* **Announces only that address.** The gateway still listens on `agent_mump2p_port` (`33213`), and announces only the configured IP and port (no detected host, interface, or IPv6 addresses), so peers on the same private network also connect through the load balancer. Leave both unset when the host is already reachable directly on `33213`.
* **Validation.** `agent_mump2p_announce_ip` (`OPT_AGENT_MUMP2P_ANNOUNCE_IP`) accepts any global or private unicast IPv4 or IPv6 address (a private IP logs a startup notice that gateways outside that network cannot dial it). Loopback, link-local, multicast, and unspecified addresses (`0.0.0.0`, `::`) fail startup. `agent_mump2p_announce_port` (`OPT_AGENT_MUMP2P_ANNOUNCE_PORT`) must be `0`–`65535`; `0` announces `agent_mump2p_port`, and setting a non-zero port without an announce IP fails startup.
* **Plain TCP to one gateway.** HTTP and TLS termination do not carry the session. One external address maps to one gateway: peers authenticate by peer ID, and a balancer that routes connections to another process fails dials. Gateways can share one IP on different ports, up to 8 per remote peer (libp2p allows 8 connections per remote IPv4 address or IPv6 `/56`).
* **Preserve client source IP.** The mesh allows 8 connections per remote IPv4 address (or IPv6 `/56`). A balancer that rewrites source IP — such as an AWS NLB with client IP preservation off, or a Kubernetes `LoadBalancer` Service with `externalTrafficPolicy: Cluster` — caps the gateway at 8 inbound mesh peers.
* **Verify.** Check startup logs for `using configured mump2p announce address` and `mump2p advertised addresses`, and confirm `mump2p.total_peers` in `GET /api/v1/self_info`. Note that `libp2p.multiaddrs` in `self_info` lists the CL listener (`33212`), not the mump2p announce address.

### Outbound-only

When a gateway can dial out and has no address other gateways can dial, register without announcing any mesh address:

```yaml
agent_mump2p_not_dialable: true
```

Or via environment variable:

```sh
-e OPT_AGENT_MUMP2P_NOT_DIALABLE=true
```

* **Behavior.** The gateway registers its peer ID with bootstrap (`not_dialable: true`) so bootstrap omits it from `expose-nodes`, and starts its mump2p host announcing no mesh addresses. Other gateways do not dial it; it still dials the dialable peers bootstrap returns.
* **Constraints.** Cannot be combined with `agent_mump2p_announce_ip` or `agent_mump2p_announce_port` (startup error). Two gateways that are both outbound-only cannot reach each other.
* **Verify.** Startup logs print `mump2p outbound-only: registering without announcing a dialable address` and `mump2p outbound-only: announcing no mesh address`. Confirm `mump2p.total_peers >= 1` in `GET /api/v1/self_info`.

## Topics

Topic subscription is **baked into the binary** — `beacon_block` plus all 64 attestation subnets (`beacon_attestation_0` through `beacon_attestation_63`). There is no `eth_topics_subscribe` field to configure. The full topic paths (with fork digest) are resolved automatically.

## Gateway Pairing Mode

The gateway exposes a `paired_with` field (visible in `/api/v1/self_info`) that declares what the gateway is paired with. This is derived from your API key type and controls whether inbound beacon blocks from mump2p are re-forwarded to the local CL. For partner deployments the value is `partner` — no action needed.

Attestation forwarding is identical in all modes.

## Attestation Subnet Boost

When attestation subnets are subscribed, the gateway forwards to mump2p **only attestations from your partner's known validators** — not every attestation the CL sees. The partner validator list is derived from your API key (via the auth mint) and refreshed periodically.

* Non-partner attestations: dropped before reaching mump2p (saves bandwidth vs forwarding everything)
* Stale attestations (outside the current slot): dropped
* Inbound attestations from mump2p: always forwarded to CL (trust upstream filter)

This is fully automatic — no extra config. Until the first successful validator sync, the gateway forwards no attestations.

## Remote Push

Remote push streams your gateway's logs to Optimum's Loki and metrics to Optimum's Mimir, giving the Optimum team visibility to help support you. Both `telemetry_enable` and `remote_push_enable` must be `true`. v1.3.6 uses standard **Prometheus remote write** for metrics push, the same setup as v1.1.1.

```yaml
telemetry_enable: true
remote_push_enable: true
```

Remote push authenticates with the gateway's **services token** (the short-lived JWT minted from your API key or enrolled credential; see [Authentication](#authentication)), attached as an `Authorization: Bearer` header. There are **no** separate `remote_push_client_id` / `remote_push_client_secret` to configure anymore. The push endpoints are baked into the binary. Requirements: `telemetry_enable: true`, `remote_push_enable: true`, a valid credential (`OPT_API_KEY` or `OPT_JOIN_KEY`), and outbound HTTPS (443).

## Dynamic Configuration

The gateway receives automatic config updates from bootstrap.

* Polls for updates periodically
* Changes apply without restart
* Dynamic config includes: propagation toggle, self-message skip

## Config Reference

| Key | Env Variable | Default | Description |
|---|---|---|---|
| `api_key` | `OPT_API_KEY` | *(empty)* | Gateway API key (`ogw_live_...`). **Set via env, not YAML.** Set this or `join_key`; with neither, the gateway starts with authentication disabled and cannot join the Optimum mesh. Drives gateway_id, chain, and validator scope |
| `join_key` | `OPT_JOIN_KEY` | *(empty)* | Org-wide join key (`ojk_live_...`). **Set via env, not YAML.** Mutually exclusive with `api_key`. See [Gateway Self-Enrollment](07_gateway_self_enrollment.md) |
| `enroll_cred_dir` | `OPT_ENROLL_CRED_DIR` | `identity_mump2p_dir` | Enrollment credential directory (`enrollment.json`). **Must be persistent** |
| `gateway_id` | `OPT_GATEWAY_ID` | `dev-gateway` | Join-key path only: enrollment label at first boot (unique per host). Overwritten by JWT `sub` after mint. Ignored for API-key path |
| `chain` | `OPT_DEV_CHAIN` | *(empty)* | Auth-off development only: `hoodi`, `mainnet`, or a numeric chain ID. Ignored when a credential is set |
| `gateway_cluster_id` | `OPT_GATEWAY_CLUSTER_ID` | *(required)* | Cluster ID assigned by Optimum during onboarding |
| `agent_lib_p2p_port` | `OPT_AGENT_LIB_P2P_PORT` | 33212 | CL clients connect here (inbound) |
| `agent_mump2p_port` | `OPT_AGENT_MUMP2P_PORT` | 33213 | mump2p listen port (inbound). Still the listen port when an announce address is set |
| `agent_mump2p_announce_ip` | `OPT_AGENT_MUMP2P_ANNOUNCE_IP` | *(empty)* | Address other gateways dial. Empty announces the detected host address. Global or private unicast; not loopback, link-local, multicast, or unspecified |
| `agent_mump2p_announce_port` | `OPT_AGENT_MUMP2P_ANNOUNCE_PORT` | 0 | Port published with the announce IP. `0` publishes `agent_mump2p_port`. Requires `agent_mump2p_announce_ip` |
| `agent_mump2p_not_dialable` | `OPT_AGENT_MUMP2P_NOT_DIALABLE` | false | Register and announce no mesh address. Cannot be set with the announce IP or port |
| `telemetry_enable` | `OPT_ENABLE_TELEMETRY` | false | Enable the `/metrics` endpoint. `/health` and `/api/v1/self_info` stay available when this is false |
| `telemetry_port` | `OPT_TELEMETRY_PORT` | 48123 | Telemetry HTTP port (`/health`, `/metrics`, `/api/v1/self_info`) |
| `telemetry_addr` | `OPT_TELEMETRY_ADDR` | *(empty)* | IP (or `localhost`) the telemetry server binds, without a port. Empty binds all IPv4 interfaces; `::` binds IPv4 and IPv6 (needs IPv6 enabled on the host). Set `127.0.0.1` only with `--network host` or `hostNetwork`; in a bridged container it breaks the `-p` publish |
| `identity_libp2p_dir` | `OPT_IDENTITY_LIBP2P_DIR` | /tmp/libp2p | libp2p identity dir — **persist as a volume** |
| `identity_mump2p_dir` | `OPT_IDENTITY_MUMP2P_DIR` | /tmp/mump2p | mump2p identity dir — **persist as a volume** |
| `direct_cl_peers` | `OPT_DIRECT_CL_PEERS` | *(empty)* | CL node multiaddrs for auto-reconnect. When set, only listed peer IDs may stay connected, and a listed peer keeps its address after a disconnect |
| `log_level` | `OPT_LOG_LEVEL` | debug | `debug` / `info` |
| `remote_push_enable` | `OPT_REMOTE_PUSH_ENABLE` | false | Optional Loki/Mimir push (requires `telemetry_enable: true`) |
| `stream_enable` | `OPT_STREAM_ENABLE` | false | Enable the consumer block stream (WebSocket + gRPC) |
| `stream_only` | `OPT_STREAM_ONLY` | false | Skip CL host/ingest; never publishes. Requires `stream_enable` |
| `stream_addr` | `OPT_STREAM_ADDR` | 127.0.0.1:9600 | WebSocket listener (own port, off `/metrics`); loopback by default |
| `stream_grpc_addr` | `OPT_STREAM_GRPC_ADDR` | 127.0.0.1:9601 | gRPC listener; loopback by default |
| `stream_require_auth` | `OPT_STREAM_REQUIRE_AUTH` | true | Verify consumer JWTs; `false` only on a loopback bind |
| `stream_max_conns` | `OPT_STREAM_MAX_CONNS` | 256 | Global connection cap |
| `stream_max_conns_per_sub` | `OPT_STREAM_MAX_CONNS_PER_SUB` | 8 | Per-consumer-key connection cap |
| `stream_buffer_size` | `OPT_STREAM_BUFFER_SIZE` | 64 | Per-connection ring buffer (drop-on-overflow) |
| `stream_heartbeat_interval_sec` | `OPT_STREAM_HEARTBEAT_INTERVAL_SEC` | 20 | In-band liveness frame; `0` disables, leaving a stalled feed indistinguishable from a quiet one |
| `stream_keepalive_min_time_sec` | `OPT_STREAM_KEEPALIVE_MIN_TIME_SEC` | 20 | Shortest accepted client ping interval; faster pings get GOAWAY `too_many_pings` |
| `stream_reauth_mode` | `OPT_STREAM_REAUTH_MODE` | observe | `off` \| `observe` \| `enforce`; re-verify the presented token mid-stream |
| `stream_reauth_interval_sec` | `OPT_STREAM_REAUTH_INTERVAL_SEC` | 60 | How often the presented token is re-verified |

See [Consumer Block Stream](06_block_stream.md) for minting consumer tokens and
opening a stream.

## Validation

```sh
curl http://localhost:48123/health
curl http://localhost:48123/metrics | grep gateway_id
docker logs optimum-gateway | grep "subscribed to topic"
```

## Common Issues

* **Wrong network** — Chain comes from the credential, not YAML. If `/api/v1/self_info` shows the wrong `chain`, you are using the wrong API key or join key. Get the right credential from Optimum
* **Missing `gateway_cluster_id`** — Required; use the ID assigned during onboarding
* **API key or join key in YAML / image** — Move credentials to `OPT_API_KEY` or `OPT_JOIN_KEY` in the environment
* **Both `OPT_API_KEY` and `OPT_JOIN_KEY` set** — Startup error; use exactly one credential mode
* **Port conflicts** — Ensure 33212, 33213, 48123 are free
* **Mesh address validation error** — `agent_mump2p_announce_ip` must be a global or private unicast IP; `agent_mump2p_announce_port` requires `agent_mump2p_announce_ip`; `agent_mump2p_not_dialable` cannot be combined with either
* **Peer ID changes after restart** — Persist `identity_libp2p_dir` and `identity_mump2p_dir` as volumes

See [Troubleshooting](04_troubleshoot.md) for more.
