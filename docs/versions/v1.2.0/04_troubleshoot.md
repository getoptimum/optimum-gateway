# Gateway Diagnosis & Troubleshooting

> **Prerequisites:** [Quick Start](01_quick_start.md) and [Configuration](02_configuration.md).

This guide covers the operational issues you can hit running the Optimum Gateway binary: config, network, CL pairing, and telemetry. Start with the first-line diagnosis below — it resolves most issues.

## What you control vs what comes from the API key

### Provided by Optimum

* Gateway **Docker image / binary**
* An **API key** (`ogw_live_...`) — binds `gateway_id`, `chain`, operator, and validator scope
* An assigned **`gateway_cluster_id`** (e.g. `optimum_ethereum_hoodi_v0_1` for Hoodi; Mainnet ID provided during onboarding)

### You configure (operational only)

| Field                                         | Notes                                                                      |
| --------------------------------------------- | -------------------------------------------------------------------------- |
| `api_key` (env `OPT_API_KEY`)                 | Wrong/revoked key -> gateway crashes at startup. **Set via env, not YAML** |
| `gateway_cluster_id`                          | Must match onboarding (Hoodi vs Mainnet)                                   |
| `identity_libp2p_dir` / `identity_mump2p_dir` | **Persist as volumes** — without them, peer ID changes every restart       |
| `agent_lib_p2p_port`                          | Default `33212`; CL connects here                                          |
| `agent_mump2p_port`                          | Default `33213`; Optimum network peers connect here (inbound)              |
| `telemetry_enable` / `telemetry_port`         | Default port `48123`                                                       |
| `direct_cl_peers`                             | Optional, **strongly recommended** for Lighthouse / Nimbus                 |
| `remote_push_enable`                          | Optional; pushes logs/metrics to Optimum for support visibility            |
| `log_level`                                   | `debug` / `info`                                                           |

### Derived from the API key — you do NOT configure these

* **`gateway_id`** — from the JWT `sub` claim
* **`chain`** — from the JWT `chain_id` claim; **not a YAML field**
* Validator list — from the auth mint, refreshed periodically
* Gossip topics (`beacon_block` + 64 attestation subnets) — baked into the binary

> If `chain` looks wrong (e.g. "I want mainnet but it's hoodi"), the **API key is wrong** — chain cannot be changed via YAML. Get the matching key from Optimum.


## First-line diagnosis (run this first)

```bash
curl -s http://localhost:48123/health | jq
curl -s http://localhost:48123/api/v1/self_info | jq
docker logs optimum-gateway --tail=50
```

| Endpoint                | What it gives you                                                                                     |
| ----------------------- | ----------------------------------------------------------------------------------------------------- |
| `GET /health`           | Pass/fail checks, `failing[]` list, HTTP 200 (healthy) or 503 (degraded)                              |
| `GET /`                 | Lightweight liveness — `{"status":"ok"}` when the process and HTTP server are up                      |
| `GET /api/v1/self_info` | Identity, peer IDs, per-topic peer counts, fork digest, chain, cluster, propagation flags, multiaddrs |
| `docker logs`           | Startup fatals, auth mint errors, bootstrap retries, subscribe logs                                   |

**Healthy target:** `cl_peers` ≥ 1, `mump2p_peers` ≥ 1, `subscribed_topics` ≈ 65, `last_block_age_sec` < 60.


## `/health` checks -> meaning -> fix

`GET /health` returns 200 (healthy) or 503 (degraded). Each check covers a different part of the pipeline.

| Failing check        | What it means                          | Fix                                                                                                                                          |
| -------------------- | -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `cl_peers`           | No CL client connected on `:33212`     | Configure CL peering; add `direct_cl_peers`; open **33212 inbound**; use a reachable IP in the multiaddr; restart CL after a gateway restart |
| `mump2p_peers`       | Not connected to the Optimum mesh      | Check **33213 inbound** + HTTPS to bootstrap; verify `api_key` + `gateway_cluster_id`; wait 2-5 min after start                              |
| `subscribed_topics`  | Topic subscription failed (expect ~65) | Usually a chain/cluster mismatch (wrong API key for the network); check logs for subscribe errors                                            |
| `last_block_age_sec` | No beacon block in ~60s                | CL is connected but **silent** — CL not synced, EL stuck, or CL OOM/restart; fix the CL first                                                |
| `cl_health`          | No CL gossip traffic in the last 30s   | Same as silent CL — peer count alone can be misleading                                                                                       |
| `mump2p_health`      | No mesh traffic in the last 30s        | Mesh/auth/bootstrap issue; can be briefly normal right after restart                                                                         |

> `cl_peers` can be **OK** while `last_block_age_sec` **fails**: the CL is connected but not delivering blocks (silent CL, EL not synced, Prysm stuck). Don't stop at peer count.

### Example healthy response

```json
{
  "status": "healthy",
  "gateway_id": "optimum-eu-hoodi-01",
  "version": "v1.2.0",
  "uptime_seconds": 1639,
  "checks": {
    "cl_peers": {"status": "ok", "value": 1},
    "mump2p_peers": {"status": "ok", "value": 13},
    "subscribed_topics": {"status": "ok", "value": 65},
    "last_block_age_sec": {"status": "ok", "value": 1},
    "cl_health": {"status": "ok"},
    "mump2p_health": {"status": "ok"}
  }
}
```

### Example degraded response

Read the `failing[]` list first — it names which checks to fix next.

```json
{
  "status": "degraded",
  "gateway_id": "optimum-eu-hoodi-01",
  "checks": {
    "cl_peers": {"status": "ok", "value": 1},
    "last_block_age_sec": {"status": "fail", "value": 120},
    "cl_health": {"status": "fail"}
  },
  "failing": ["last_block_age_sec", "cl_health"]
}
```

### `/api/v1/self_info` fields worth reading

| Field                                 | Tells you                                                                                             |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `chain` / `fork_digest`               | Network match (hoodi `c6ecb76c`, mainnet `8c9f62fe`)                                                  |
| `gateway_cluster_id`                  | You set this; must match the assignment                                                               |
| `gateway_id`                          | From the API key; use it in dashboards and support tickets                                            |
| `libp2p.multiaddrs` + `peer_id`       | Give these to the CL for `--peer` / `--direct-peer`                                                   |
| `libp2p.total_peers` / `direct_peers` | CL connectivity detail                                                                                |
| `mump2p.total_peers`                  | Mesh connectivity                                                                                     |
| `libp2p.peers_per_topic`              | Which topics have CL peers when the CL is healthy                                                     |
| `propagation_enabled`                 | Optimum dynamic config; `false` can explain "not propagating". Same state as `mump2p_gateway_propagation_state=0` |
| `paired_with`                         | Gateway type from the API key: `partner`, `hermes`, or `relay` — only `partner` forwards attestations |

> For support tickets, attach the full output of `curl -s http://localhost:48123/api/v1/self_info | jq`.


## Gateway won't start / crashloops

| Issue                                  | Logs (if applicable)                                                  | Fix                                                                                                                            |
| -------------------------------------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Container exits immediately            | `unable to load config`                                               | Check config path and YAML validity                                                                                            |
| Auth fatal at startup                  | `unable to initialize auth_token manager` / `initial JWT mint failed` | Bad/missing/revoked `api_key`, or no outbound HTTPS to `auth.getoptimum.io`. Verify the key in the console; check firewall/DNS |
| Port bind error                        | `bind: address already in use`                                        | `lsof -i :33212` (and 48123); stop the conflicting process or change ports consistently in `docker run -p`                     |
| Config not loaded                      | `unable to load config`                                               | Mount config correctly: `-v $(pwd)/config:/app/config` and `-config=/app/config/app_conf.yml`                                  |
| Invalid YAML                           | `failed to validate config`                                           | Fix the YAML syntax                                                                                                            |
| Identity dir not writable              | `failed to create identity directory`                                 | `mkdir -p data/libp2p data/mump2p` and mount both volumes                                                                      |
| `remote_push_enable` without telemetry | `remote_push_enable requires telemetry_enable=true`                   | Set `telemetry_enable: true` or disable remote push                                                                            |
| Missing cluster ID                     | `OPT_GATEWAY_CLUSTER_ID is required`                                  | Set the assigned `gateway_cluster_id`                                                                                          |
| Gateway ID conflict                    | `gateway with same ID already exists`                                 | One gateway per API key. Preserve identity volumes on redeploy, or wait ~1h for the old bootstrap registration to expire       |
| API key revoked / suspended            | `api key revoked (403)` / `api key suspended (403)`                   | Generate a new key in the console                                                                                              |
| Auth refresh stopped                   | `api key terminal failure — refresh loop exiting`                     | Key permanently invalid — fix the key and restart                                                                              |
| Clock skew                             | JWT verify failures with no obvious key issue                         | Host clock must be within ~30s of UTC (`timedatectl`, NTP)                                                                     |


## API key / network mismatch

The chain comes from the **API key**, not YAML. A Hoodi key on a Mainnet cluster (or vice versa) breaks things quietly.

| Issue                                               | What you see                                                                                  | Fix                                                                                                                  |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `self_info.chain` is `hoodi` but cluster is mainnet | `chain: hoodi`, `fork_digest: c6ecb76c`, but `gateway_cluster_id: optimum_ethereum_mainnet_*` | Generate a **Mainnet API key** in the console; set the matching **Mainnet `gateway_cluster_id`**; restart            |
| Wrong fork digest in topics                         | mump2p topics use `/eth2/c6ecb76c/...` on mainnet                                             | Fix key + cluster, then **restart**; confirm `self_info.chain` and `fork_digest`                                     |
| Gateway not visible on bootstrap / no mesh peers    | `mump2p_peers: 0`                                                                             | Registered on the wrong chain or never registered. Fix the mismatch; confirm outbound HTTPS                          |
| Attestations never forwarded                        | validator list empty                                                                          | Validators come from the auth mint; wait for sync. If persistent, verify the key's validator assignment with Optimum |

Verify after restart:

```bash
curl -s http://localhost:48123/api/v1/self_info | jq '.chain, .fork_digest'
# mainnet -> "mainnet"  "8c9f62fe"
```


## CL client connection (most common issue)

| Issue                                    | Fix                                                                                                                                                                                                          |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `cl_peers: 0`                            | Point the CL at the `self_info` multiaddr + `peer_id`; open **33212 inbound**                                                                                                                                |
| Disconnected after a **gateway restart** | Lighthouse/Nimbus don't re-dial. Add the CL to `direct_cl_peers` so the gateway keeps retrying                                                                                                               |
| Wrong peer ID in CL config               | Identity dirs not persisted -> new peer ID each run. Mount volumes; refresh CL config from current `self_info.peer_id`                                                                                       |
| Lighthouse v8.x drops gateway            | PeerDAS change. Use a fixed gateway version; on Lighthouse add `--semi-supernode`, `--target-peers=500`, `--trusted-peers=<gateway_peer_id>`                                                                 |
| Teku disconnects the gateway             | Upgrade Teku to **v26.4.0+**; use `--p2p-direct-peers` (not just `--p2p-static-peers`)                                                                                                                       |
| Nimbus won't stay connected              | Use a stable `--netkey-file` (not `random`); add Nimbus to `direct_cl_peers` with its public IP:port; open Nimbus P2P port                                                                                   |
| Prysm shows connected but no blocks      | Silent CL / EL not synced. Prysm can follow in optimistic sync while Geth catches up — `cl_peers` OK but `last_block_age_sec` / `cl_health` fail. Check EL logs; wait for EL, then Prysm `/healthz` goes 200 |
| Docker NAT / wrong IP in multiaddr       | CL on host + gateway in bridge network -> use the host IP or `--network host`                                                                                                                                |

> **Recommended:** Prysm **v7.1.8**; Lighthouse **v8.2.1+**; Teku **v26.6.0+** (minimum v26.4.0); Nimbus **v26.7.0+**.

### Getting the gateway's peer info for the CL

```bash
curl -s http://localhost:48123/api/v1/self_info | jq -r '.peer_id'
curl -s http://localhost:48123/api/v1/self_info | jq -r '.libp2p.multiaddrs'
```

### `direct_cl_peers` (recommended for all partners)

Lighthouse and Nimbus do not reliably re-dial the gateway after a **gateway** restart. Add the CL node so the gateway runs a background goroutine that keeps retrying:

```yaml
direct_cl_peers:
  - /ip4/YOUR_CL_IP/tcp/9000/p2p/YOUR_CL_PEER_ID
```

### PeerDAS / custody metadata

The gateway advertises a **custody group count of 8** (Fulu `VALIDATOR_CUSTODY_REQUIREMENT`) in its libp2p metadata so PeerDAS-aware clients — Lighthouse in particular — keep the gateway as a useful peer. See the [Fulu p2p metadata spec](https://github.com/ethereum/consensus-specs/blob/master/specs/fulu/p2p-interface.md#metadata).

### Lighthouse v8.x PeerDAS Configuration (Important)

Use Lighthouse **v8.2.1+** with:

```yaml
command:
  - lighthouse
  - beacon_node
  - --network=hoodi
  - --semi-supernode
  - --target-peers=500
  - --boot-nodes=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID
  - --trusted-peers=YOUR_GATEWAY_PEER_ID
```

**Why:** PeerDAS changes peer requirements; `--semi-supernode` and `--target-peers=500` prevent pruning.

[Lighthouse v8.2.0 Release Notes](https://github.com/sigp/lighthouse/releases/tag/v8.2.0)

### Teku PeerDAS Configuration (Important)

**Minimum version:** Teku **v26.4.0** or later. Older versions have a known bug (`NoSuchElementException` at `MetadataDasPeerCustodyTracker.onPeerMetadataUpdate`) that causes recurring errors when exchanging metadata with peers.

Use **`--p2p-direct-peers`** (not just `--p2p-static-peers`): static peers can be pruned during peer management; direct peers maintain a persistent, protected connection.

```sh
teku \
  --network=hoodi \
  --p2p-direct-peers=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --p2p-static-peers=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --p2p-subscribe-all-subnets-enabled \
  --p2p-subscribe-all-custody-subnets-enabled \
  ...
```

| Symptom                                                     | Cause                           | Fix                       |
| ----------------------------------------------------------- | ------------------------------- | ------------------------- |
| `NoSuchElementException` at `MetadataDasPeerCustodyTracker` | Teku version too old            | Upgrade to v26.4.0+       |
| Gateway peer keeps disconnecting                            | Using `--p2p-static-peers` only | Add `--p2p-direct-peers`  |
| `Payload marked as invalid by Execution Client`             | EL still syncing                | Wait for the EL to finish |

### Nimbus

Connect Nimbus to the gateway and add Nimbus to the gateway's `direct_cl_peers`:

```sh
nimbus_beacon_node \
  --direct-peer=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --netkey-file=/data/netkey \
  ...
```

Verify Nimbus sees the gateway:

```sh
curl -s http://localhost:9596/eth/v1/node/peers/YOUR_GATEWAY_PEER_ID | jq '.data | {state, agent}'
```

Expected: `"state": "connected"` with an agent containing `optimum-gateway`.

**Nimbus behavior — what is normal (not a gateway bug):**

1. **Warmup is normal.** Nimbus may take a few hours after start to settle. Gossip/scoring can look off early but stabilizes on its own — don't treat early instability as a failure.
2. **Peer drop + reconnect is expected.** Nimbus drops peers when a connection closes or peer score drops low, then reconnects with fresh scores. Transient `cl_peers` flapping is normal.
3. **The connection itself is reliable.** On a gateway restart you immediately see `mump2p_gateway_cl_peers 1`; Nimbus reconnects fast and stays connected (held over a full weekend in testing).
4. **Behavior differs from Prysm/Lighthouse.** Prysm doesn't show this drop/reconnect pattern and Lighthouse syncs in parallel — don't benchmark Nimbus's warmup/scoring against them.
5. **Ignore the stale issue tracker.** Nimbus has many old open issues (2020-2022), but its commit history is active; those open issues don't indicate a broken integration.

| Symptom                                                   | Cause                                        | Fix                                                                                                               |
| --------------------------------------------------------- | -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `Adding privileged direct peer` in logs but no connection | Firewall or wrong IP/port                    | Open 33212 (Nimbus -> gateway) and your Nimbus P2P port (gateway -> Nimbus); use a public IP in `direct_cl_peers` |
| Gateway peer always `disconnected` in REST                | Missing `direct_cl_peers` or unstable netkey | Add `direct_cl_peers`; use a stable `--netkey-file`                                                               |

### Lodestar

Lodestar is supported. Add the gateway as a trusted/direct peer in Lodestar, and add the Lodestar node to the gateway's `direct_cl_peers` so the gateway re-dials after restarts. Verify with `curl -s http://localhost:48123/health | jq '.checks.cl_peers'`.


## Network / firewall / Docker

| Port    | Direction | Action                                                                                              |
| ------- | --------- | --------------------------------------------------------------------------------------------------- |
| `33212` | Inbound   | CL clients connect here                                                                             |
| `33213` | Inbound   | Optimum network (mump2p) peers connect here                                                         |
| `48123` | Localhost | `/health`, `/metrics`, `/api/v1/self_info` — see [Network Requirements](00_network_requirements.md) |
| `443`   | Outbound  | auth, bootstrap, Loki, Mimir                                                                        |

```bash
sudo lsof -i :33212 -i :33213 -i :48123
sudo ufw allow 33212/tcp
sudo ufw allow 33213/tcp
```

**33213** must be reachable from the internet for the mesh. **33212** is for your CL (usually private). **Docker tip:** `--network host` exposes all ports on the host network interface.


## Identity / persistence

| Issue                                     | What you see                     | Fix                                                                                                   |
| ----------------------------------------- | -------------------------------- | ----------------------------------------------------------------------------------------------------- |
| CL config "suddenly wrong" after redeploy | `peer_id` changed in `self_info` | New libp2p identity each run. Persist `identity_libp2p_dir` and `identity_mump2p_dir` as volumes      |
| Auth/mesh issues after a wipe             | —                                | New mump2p peer_id in the mint payload. Persist the identity dir; Optimum may need to re-link the key |
| Two gateways with the same identity       | —                                | Copied volume. One identity per gateway instance                                                      |

```bash
docker exec optimum-gateway ls -la /tmp/libp2p /tmp/mump2p
```


## Local dashboard (Prometheus + Grafana)

Partners scrape the gateway `/metrics` **locally** with their own Prometheus + Grafana. See [Metrics & Grafana](03_telemetry.md) for the full stack setup.

| Issue                          | Fix                                                                                  |
| ------------------------------ | ------------------------------------------------------------------------------------ |
| Grafana empty / "no data"      | `telemetry_enable: false`. Set it to `true` and restart                              |
| Prometheus can't scrape        | Wrong target. macOS/Windows: `host.docker.internal:48123`; Linux: `172.17.0.1:48123` |
| Gateway not in the dropdown    | No successful scrape yet. Confirm `curl localhost:48123/metrics \| grep gateway_id`  |
| Block/attestation panels empty | Gateway isn't receiving blocks/attestations. Fix CL + mesh first via `/health`       |
| "Accelerated slots" always 0   | No block race data yet. Needs a healthy CL + mesh + time on the network              |

```bash
curl -s http://localhost:48123/metrics | grep mump2p_gateway
```


## Message flow / performance (gateway runs but "isn't helping")

| Symptom                                     | Cause                                 | Fix                                                                                                                                                                           |
| ------------------------------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Blocks not faster than baseline             | CL or mesh unhealthy                  | Fix `/health` first                                                                                                                                                           |
| Attestations not on the mesh                | Validators not yet in the JWT list    | Wait for sync. Only `partner` gateways forward attestations (`self_info.paired_with`). Check `mump2p_gateway_known_validators_total` — 0 means no validators synced from auth |
| Attestations dropped (expected)             | Non-partner validators on the same CL | Normal — only your own validators are forwarded                                                                                                                               |
| `propagation_enabled: false` in `self_info` | Optimum dynamic config                | Not a partner YAML knob. Metrics: `mump2p_gateway_propagation_state=0`                                                                                                        |


## Bootstrap / mesh (partner-visible symptoms only)

| Symptom                                                            | Action                                                       |
| ------------------------------------------------------------------ | ------------------------------------------------------------ |
| Startup logs: `failed to connect to bootstrap node... i/o timeout` | **Ignore** if transient (first few minutes)                  |
| Persistent `mump2p_peers: 0`                                       | Check `api_key`, `gateway_cluster_id`, outbound network      |
| Handshake errors in logs                                           | Usually peer churn; persistent -> contact Optimum            |
| Gateway missing from the bootstrap list                            | Usually chain/cluster/key mismatch or no heartbeat (TTL ~1h) |


## CL client-specific quick reference

| Client         | Recurring issue                     | Fix                                                                        |
| -------------- | ----------------------------------- | -------------------------------------------------------------------------- |
| Lighthouse     | No reconnect after gateway restart  | `direct_cl_peers` + `--boot-nodes` / trusted peer                          |
| Lighthouse v8+ | Goodbye / custody metadata mismatch | Updated gateway + PeerDAS flags (`--semi-supernode`, `--target-peers=500`) |
| Prysm          | Connected but no gossip             | EL sync; restart beacon node. Use v7.1.8+                                  |
| Teku           | `NoSuchElementException` metadata   | Upgrade to v26.4.0+                                                        |
| Teku           | Gateway peer pruned                 | Use `--p2p-direct-peers` (not just `--p2p-static-peers`)                   |
| Nimbus         | Privileged peer fails               | Stable `--netkey-file` + public IP in `direct_cl_peers`                    |



## Operational mistakes

| Issue                                       | Fix                                                                                                       |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Wrong Docker image tag                      | Use `getoptimum/gateway:v1.2.0`                                                                           |
| Config edited but container not restarted   | `docker restart optimum-gateway`                                                                          |
| Running hoodi + mainnet on the same ports   | The second instance needs different ports + its own config + its own API key                              |
| Duplicate gateway (same API key, two hosts) | One key -> one gateway; generate a second key                                                             |
| Checking health on the wrong host/port      | Confirm `telemetry_port` and Docker port mapping — see [Network Requirements](00_network_requirements.md) |
| Exposed `/metrics` to the internet          | Use `-p 127.0.0.1:48123:48123`, not `-p 48123:48123`                                                      |


## Normal Log Noise (Safe to Ignore)

```text
failed to connect to bootstrap node... i/o timeout
failed to send handshake for peer... connection closed
failed to load topics data... file does not exist
```

* Expected during the first minutes of startup or on first run.
* A brief `503` on `/health` right after restart (before CL/mesh settle) is also normal — allow **30-60s** after boot.
* `stale_data, skipping publish of beacon_block` while the CL is still syncing — expected, not a gateway bug.


## Highest-frequency real-world issues (prioritize)

1. **CL not connected** — missing `direct_cl_peers`, firewall, or wrong multiaddr
2. **CL connected, no blocks** — silent CL / EL sync (`last_block_age_sec`)
3. **Wrong API key for the network** — Hoodi key on a Mainnet cluster (or vice versa)
4. **Identity not persisted** — peer ID drift breaks CL config
5. **Local Grafana empty** — telemetry off or Prometheus target wrong
6. **Lighthouse v8 PeerDAS** — custody/metadata disconnect
