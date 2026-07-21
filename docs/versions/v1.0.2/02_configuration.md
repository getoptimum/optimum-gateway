# Configuration

> **Prerequisites:** [Quick Start](01_quick_start.md) complete, including an [API key](01_quick_start.md#generate-your-api-key).

## Basic Setup

Everything that identifies your gateway — `gateway_id`, `chain`, and validator scope — comes from your **API key**. The only things you set are operational: the cluster ID, ports, telemetry, and identity directories.

Create `config/app_conf.yml`:

```yaml
log_level: info
gateway_cluster_id: optimum_ethereum_hoodi_v0_1   # REQUIRED — assigned by Optimum

agent_lib_p2p_port: 33212
agent_opt_p2p_port: 43213
telemetry_enable: true
telemetry_port: 48123
identity_libp2p_dir: /tmp/libp2p
identity_optp2p_dir: /tmp/optp2p
```

> **API key via environment, not YAML.** Set `OPT_API_KEY=ogw_live_...` in the environment. Keep it out of the config file and image layers. The key determines your chain (Hoodi vs Mainnet), gateway ID, and validator list — there is **no** `chain` or `gateway_id` field for partners to set.

## Authentication

Your gateway sends its API key only to Optimum's auth service, never to peer gateways or other Optimum services. On start (and periodically after), it exchanges the key there for two short-lived JWTs and keeps them refreshed automatically. There is nothing to configure and no token to manage yourself.

* **Services token:** attached as an `Authorization: Bearer` header on calls to Optimum's central services, namely bootstrap registration and heartbeat, and (when enabled) Loki/Mimir remote push.
* **Peer handshake token:** presented during peer-to-peer (libp2p) handshakes with other gateways, so each side can confirm the other is a legitimate gateway on the same chain before exchanging traffic.

Both tokens are ES256-signed and valid for 6 hours; the gateway refreshes them well before expiry. Peers verify each other's handshake token locally against Optimum's published JWKS, with no per-connection callback to the auth service. If your API key is revoked or suspended, refresh stops and the gateway loses access at the next token expiry.

## Networks (Hoodi / Mainnet)

The network is selected by your **API key**, not by config. A Hoodi key runs Hoodi; a Mainnet key runs Mainnet. Set the matching `gateway_cluster_id` you were assigned during onboarding (Hoodi partners use `optimum_ethereum_hoodi_v0_1`; Mainnet cluster ID is provided by Optimum during onboarding). To move a gateway to Mainnet, obtain a Mainnet API key from Optimum and set the assigned Mainnet `gateway_cluster_id`, then restart.

Confirm the active network after start:

```sh
curl -s http://localhost:48123/api/v1/self_info | jq '.chain, .fork_digest'
```

## Direct CL Peers (Recommended for All Partners)

Lighthouse and Nimbus do not automatically reconnect to the gateway after a **gateway** restart. Configure `direct_cl_peers` so the gateway runs a dedicated goroutine that retries the connection until the CL peer is reachable again.

```yaml
direct_cl_peers:
  - /ip4/192.168.1.2/tcp/9000/p2p/16Uiu2HAmGj6AoMKe7fNrghXwwRgivXLpji3Hkm4QEGVpsHZYKwPQ
```

Replace the IP, port, and peer ID with your CL node's libp2p multiaddr.

**Why this matters:** Prysm re-dials peers on its own, but Lighthouse and Nimbus do not reliably hold the gateway link after a gateway restart. Without `direct_cl_peers`, the CL can stay disconnected until the next manual intervention. We recommend all partners add their CL nodes as direct peers.

**Peer allowlist:** When `direct_cl_peers` is set, the gateway disconnects any libp2p peer whose ID is not in that list. When empty, no peer-ID filtering is applied; firewall `agent_lib_p2p_port` (default `33212`) to your CL client.

**Lighthouse beacon node CLI:** upstream help marks `--libp2p-addresses` as **deprecated**; use **`--boot-nodes`** for the same comma-delimited multiaddrs (multiaddr or ENR). See [Beacon Node help](https://lighthouse-book.sigmaprime.io/help_bn.html).

To find your CL node's multiaddr:

* **Lighthouse:** `curl -s http://localhost:5052/eth/v1/node/identity | jq '.data.p2p_addresses[0]'`
* **Prysm:** `curl -s http://localhost:3500/eth/v1/node/identity | jq '.data.p2p_addresses[0]'`
* **Nimbus:** `curl -s http://localhost:9596/eth/v1/node/identity | jq '.data.p2p_addresses[0]'`

## Topics

Topic subscription is **baked into the binary** — `beacon_block` plus all 64 attestation subnets (`beacon_attestation_0` through `beacon_attestation_63`). There is no `eth_topics_subscribe` field to configure. The full topic paths (with fork digest) are resolved automatically.

## Gateway Pairing Mode

The gateway exposes a `paired_with` field (visible in `/api/v1/self_info`) that declares what the gateway is paired with. This is derived from your API key type and controls whether inbound beacon blocks from mump2p are re-forwarded to the local CL. For partner deployments the value is `partner` — no action needed.

Attestation forwarding is identical in all modes.

## Attestation Subnet Boost

When attestation subnets are subscribed, the gateway forwards to mump2p **only attestations from your partner's known validators** — not every attestation the CL sees. The partner validator list is derived from your API key (via the auth mint) and refreshed periodically.

* Non-partner attestations: dropped before reaching mump2p (saves bandwidth vs forwarding everything)
* Stale attestations (older than the current slot gate): dropped
* Inbound attestations from mump2p: always forwarded to CL (trust upstream filter)

This is fully automatic — no extra config. Until the first successful validator sync, the gateway forwards no attestations.

## Remote Push

Remote push streams your gateway's logs to Optimum's Loki and metrics to Optimum's Mimir, giving the Optimum team visibility to help support you. Both `telemetry_enable` and `remote_push_enable` must be `true`.

```yaml
telemetry_enable: true
remote_push_enable: true
```

Remote push authenticates with the gateway's **services token** (the short-lived JWT minted from your API key; see [Authentication](#authentication)), attached as an `Authorization: Bearer` header. There are **no** separate `remote_push_client_id` / `remote_push_client_secret` to configure anymore. The push endpoints are baked into the binary. Requirements: `telemetry_enable: true`, `remote_push_enable: true`, a valid API key, and outbound HTTPS (443).

## Dynamic Configuration

The gateway receives automatic config updates from bootstrap.

* Polls for updates periodically
* Changes apply without restart
* Dynamic config includes: propagation toggle, self-message skip, aggregation interval

## Config Reference

| Key | Env Variable | Default | Description |
|---|---|---|---|
| `api_key` | `OPT_API_KEY` | *(required)* | Gateway API key (`ogw_live_...`). **Set via env, not YAML.** Drives gateway_id, chain, and validator scope |
| `gateway_cluster_id` | `OPT_GATEWAY_CLUSTER_ID` | *(required)* | Cluster ID assigned by Optimum during onboarding |
| `agent_lib_p2p_port` | `OPT_AGENT_LIB_P2P_PORT` | 33212 | CL clients connect here (inbound) |
| `agent_opt_p2p_port` | `OPT_AGENT_OPT_P2P_PORT` | 33213 | mump2p agent port (outbound). Sample config uses `43213` |
| `telemetry_enable` | `OPT_ENABLE_TELEMETRY` | false | Enable metrics / health endpoint |
| `telemetry_port` | `OPT_TELEMETRY_PORT` | 48123 | Telemetry HTTP port (`/health`, `/metrics`, `/api/v1/self_info`) |
| `identity_libp2p_dir` | `OPT_IDENTITY_LIBP2P_DIR` | /tmp/libp2p | libp2p identity dir — **persist as a volume** |
| `identity_optp2p_dir` | `OPT_IDENTITY_OPT_P2P_DIR` | /tmp/optimum | mump2p identity dir — **persist as a volume** |
| `direct_cl_peers` | `OPT_DIRECT_CL_PEERS` | *(empty)* | CL node multiaddrs for auto-reconnect; when set, only listed peer IDs may stay connected |
| `log_level` | `OPT_LOG_LEVEL` | debug | `debug` / `info` |
| `remote_push_enable` | `OPT_REMOTE_PUSH_ENABLE` | false | Optional Loki/Mimir push (requires `telemetry_enable: true`) |

## Validation

```sh
curl http://localhost:48123/health
curl http://localhost:48123/metrics | grep gateway_id
docker logs optimum-gateway | grep "subscribed to topic"
```

## Common Issues

* **Wrong network** — Chain comes from the API key, not YAML. If `/api/v1/self_info` shows the wrong `chain`, you are using the wrong key. Get the right key from Optimum
* **Missing `gateway_cluster_id`** — Required; use the ID assigned during onboarding
* **API key in YAML / image** — Move it to `OPT_API_KEY` in the environment
* **Port conflicts** — Ensure 33212, 43213, 48123 are free
* **Peer ID changes after restart** — Persist `identity_libp2p_dir` and `identity_optp2p_dir` as volumes

See [Troubleshooting](04_troubleshoot.md) for more.
