# Gateway Diagnosis & Troubleshooting

> **Prerequisites:** [Quick Start](01_quick_start.md) and [Configuration](02_configuration.md).

This guide covers the operational issues you can hit running the Optimum Gateway binary: config, network, CL pairing, and telemetry. Start with the first-line diagnosis below — it resolves most issues.

## What you control vs what comes from your credential

### Provided by Optimum

* Gateway **Docker image / binary**
* A credential — either an **API key** (`ogw_live_...`), which binds `gateway_id`, `chain`, operator, and validator scope, or an org **join key** (`ojk_live_...`) the gateway enrolls with, which binds chain, type, and cluster scope. See [Gateway Self-Enrollment](07_gateway_self_enrollment.md)
* An assigned **`gateway_cluster_id`** (e.g. `optimum_ethereum_hoodi_v0_1` for Hoodi; Mainnet ID provided during onboarding)

### You configure (operational only)

| Field                                         | Notes                                                                      |
| --------------------------------------------- | -------------------------------------------------------------------------- |
| `api_key` (env `OPT_API_KEY`)                 | Wrong/revoked key -> gateway crashes at startup. **Set via env, not YAML** |
| `join_key` (env `OPT_JOIN_KEY`)               | Fleet alternative to `api_key`; the two are mutually exclusive. **Set via env, not YAML** |
| `gateway_id` (env `OPT_GATEWAY_ID`)           | Join-key path only: the enrollment label, unique per host. Ignored on the API-key path |
| `enroll_cred_dir`                             | Join-key path only; **persist as a volume** — losing it re-enrolls and consumes a join-key use |
| `gateway_cluster_id`                          | Must match onboarding (Hoodi vs Mainnet)                                   |
| `identity_libp2p_dir` / `identity_mump2p_dir` | **Persist as volumes** — without them, peer ID changes every restart       |
| `agent_lib_p2p_port`                          | Default `33212`; CL connects here                                          |
| `agent_mump2p_port`                          | Default `33213`; Optimum network peers connect here (inbound)              |
| `telemetry_enable` / `telemetry_port`         | Default port `48123`                                                       |
| `direct_cl_peers`                             | CL multiaddr the gateway dials; also an allowlist when set. See [Connecting your CL client](08_cl_clients.md) |
| `remote_push_enable`                          | Optional; pushes logs/metrics to Optimum for support visibility            |
| `log_level`                                   | `debug` / `info`                                                           |

### Derived from your credential — you do NOT configure these

* **`gateway_id`** — from the JWT `sub` claim
* **`chain`** — from the JWT `chain_id` claim; **not a YAML field**
* Validator list — from the auth mint, refreshed periodically
* Gossip topics (`beacon_block` + 64 attestation subnets) — baked into the binary

> If `chain` looks wrong (e.g. "I want mainnet but it's hoodi"), the **credential is wrong** — chain cannot be changed via YAML. Get the matching API key or join key from Optimum.


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

On a `stream_only` gateway the target is `mump2p_peers` ≥ 1, `mump2p_health` ok, `last_block_age_sec` < 60; only the three CL checks read `skipped`, so a stale mesh still returns 503.


## `/health` checks -> meaning -> fix

`GET /health` returns 200 (healthy) or 503 (degraded). Each check covers a different part of the pipeline.

A check is `ok`, `fail`, or `skipped`. `skipped` means the check does not apply to this node's mode: a `stream_only` gateway never starts the CL host, so `cl_peers`, `cl_health` and `subscribed_topics` are reported as `skipped` and are left out of `failing` and of the 200/503 roll-up.

| Failing check        | What it means                          | Fix                                                                                                                                          |
| -------------------- | -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `cl_peers`           | No CL client connected on `:33212`     | [Connecting your CL client](08_cl_clients.md): reachable multiaddr, open **33212**, and a matching `direct_cl_peers` entry when that list is set |
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
  "version": "v1.3.2",
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

### Example `stream_only` response

```json
{
  "status": "healthy",
  "gateway_id": "optimum-eu-mainnet-stream-01",
  "checks": {
    "cl_peers": {"status": "skipped"},
    "cl_health": {"status": "skipped"},
    "subscribed_topics": {"status": "skipped"},
    "mump2p_peers": {"status": "ok", "value": 25},
    "mump2p_health": {"status": "ok"},
    "last_block_age_sec": {"status": "ok", "value": 11}
  }
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
| Auth fatal at startup                  | `unable to initialize auth_token manager` / `initial JWT mint failed` / `enroll gateway` | Bad/missing/revoked credential, enroll failure, or no outbound HTTPS to `auth.getoptimum.io`. Verify API key or join key in the console; check firewall/DNS |
| Both credentials set                   | `api_key and join_key are mutually exclusive`                         | Use exactly one: `OPT_API_KEY` or `OPT_JOIN_KEY`                                                                               |
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
| `self_info.chain` is `hoodi` but cluster is mainnet | `chain: hoodi`, `fork_digest: c6ecb76c`, but `gateway_cluster_id: optimum_ethereum_mainnet_*` | Generate a Mainnet key in the console — the network picker calls it **Ethereum** — and set the matching Mainnet `gateway_cluster_id`; restart |
| Wrong fork digest in topics                         | mump2p topics use `/eth2/c6ecb76c/...` on mainnet                                             | Fix key + cluster, then **restart**; confirm `self_info.chain` and `fork_digest`                                     |
| Gateway not visible on bootstrap / no mesh peers    | `mump2p_peers: 0`                                                                             | Registered on the wrong chain or never registered. Fix the mismatch; confirm outbound HTTPS                          |
| Attestations never forwarded                        | validator list empty                                                                          | Validators come from the auth mint; wait for sync. If persistent, verify the key's validator assignment with Optimum |

Verify after restart:

```bash
curl -s http://localhost:48123/api/v1/self_info | jq '.chain, .fork_digest'
# mainnet -> "mainnet"  "8c9f62fe"
```


## CL client connection (most common issue)

Flags, versions, and the per-client fixes are in [Connecting your CL client](08_cl_clients.md#if-the-link-fails). The short version:

| Issue                                    | Fix                                                                                                                                                          |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `cl_peers: 0`                            | CL flag uses `self_info.peer_id` and an address the CL can route. Open **33212**.                                                                            |
| Disconnected after a **gateway restart** | Lighthouse and Nimbus do not re-dial. Add the CL to `direct_cl_peers`.                                                                                       |
| Wrong peer ID in CL config               | Identity dirs not persisted, or the beacon `.data.peer_id` / `mump2p.peer_ids` was pasted into the CL flag.                                                 |
| Lighthouse drops the gateway             | `--boot-nodes` plus `--trusted-peers=<gateway peer_id>`. `--semi-supernode` is custody, not the session.                                                     |
| Teku disconnects the gateway             | Teku **26.4.0+**, and `--p2p-direct-peers` rather than `--p2p-static-peers` alone.                                                                          |
| Nimbus won't stay connected              | Stable `--netkey-file`, Nimbus in `direct_cl_peers`, both P2P ports open.                                                                                   |
| Connected but no blocks                  | Execution client still syncing. `cl_peers` can be ok while `last_block_age_sec` fails.                                                                      |
| Docker NAT / wrong IP in multiaddr       | `libp2p.multiaddrs[0]` is often a bridge address. Use the host IP or `--network host`.                                                                      |


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
| "mump2p-first slots" always 0  | No block race data yet. Needs a healthy CL + mesh + time on the network              |
| Consumer stream quiet, connection still open | A quiet chain and a stalled feed look the same without liveness frames. Alert on **missing** heartbeats and send client keepalives. See [Consumer Block Stream](06_block_stream.md#holding-a-stream-open-for-weeks) |

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


## Gateway self-enrollment

Applies when `OPT_JOIN_KEY` is set. See [Gateway Self-Enrollment](07_gateway_self_enrollment.md).

| Symptom | Likely cause | Fix |
|---|---|---|
| Startup fails with `enroll gateway` / `401` | Join key unknown, expired, exhausted, or revoked; or host clock >~2 min slow | The `401` is deliberately the same for all four, so check the key under **Manage Gateways** → **Enrollment keys**: expired and exhausted show as a badge, revoked keys are removed from the list. Sync NTP. Generate a new key if needed |
| Startup fails with `label_conflict` / `409` | Enrollment label already live in the org | Set a unique `OPT_GATEWAY_ID` per host, or revoke the orphan under **Manage Gateways** → **Gateway** tab, finding it by that label |
| Startup fails with `gateway_key_limit` / `409` | Org at the 1000-gateway cap | Revoke unused credentials or contact Optimum |
| Startup fails: peer ID mismatch | mumP2P identity changed under an existing credential. Raised by the gateway, not by auth | Restore the original `identity_mump2p_dir` volume, or revoke the enrolled credential and enroll fresh |
| Corrupt credential on disk | `enrollment.json` unreadable, or the thumbprint recorded in it does not match the private key it carries. Raised by the gateway, not by auth | Do not delete and re-enroll blindly — that burns a join-key use. Restore `enrollment.json` from backup, or revoke the credential in the console first |
| `auth_enrollment_total{result="success"}` on every restart | Credential directory not persisting | Mount `identity_mump2p_dir` (or `enroll_cred_dir`) as a volume. Repeated `success` across a fleet means enrollments are not being reused |
| Log line `enrolling without a label` at startup | `OPT_GATEWAY_ID` left at default (`dev-gateway`), which sends an empty label | Set a unique `OPT_GATEWAY_ID` per host before first enroll. An empty label is exempt from the conflict check, so the host silently re-enrolls and burns a join-key use every time it loses its credential dir |
| Mesh peers stay at 0 after enroll | Join key minted without matching `cluster_ids` | Mint a join key whose cluster scope includes your `gateway_cluster_id` |
| Revoked join key, gateway still runs | Revoking a join key stops new enrollments only | Revoke the enrolled gateway credential separately if you need to cut access |

Runtime `gateway_id` in `/health` and metrics is the enrolled `client_id` (JWT `sub`), not the `OPT_GATEWAY_ID` enrollment label.

## Bootstrap / mesh (partner-visible symptoms only)

| Symptom                                                            | Action                                                       |
| ------------------------------------------------------------------ | ------------------------------------------------------------ |
| Startup logs: `failed to connect to bootstrap node... i/o timeout` | **Ignore** if transient (first few minutes)                  |
| Persistent `mump2p_peers: 0`                                       | Check credential (`OPT_API_KEY` or `OPT_JOIN_KEY`), `gateway_cluster_id`, outbound network |
| Handshake errors in logs                                           | Usually peer churn; persistent -> contact Optimum            |
| Gateway missing from the bootstrap list                            | Usually chain/cluster/key mismatch or no heartbeat (TTL ~1h) |


## Operational mistakes

| Issue                                       | Fix                                                                                                       |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Wrong Docker image tag                      | Use `getoptimum/gateway:v1.3.2`                                                                           |
| Config edited but container not restarted   | `docker restart optimum-gateway`                                                                          |
| Running hoodi + mainnet on the same ports   | The second instance needs different ports + its own config + its own credential                           |
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

1. **CL not connected** — [Connecting your CL client](08_cl_clients.md): wrong peer ID, firewall, or a multiaddr the CL cannot route
2. **CL connected, no blocks** — silent CL / EL sync (`last_block_age_sec`)
3. **Wrong API key for the network** — Hoodi key on a Mainnet cluster (or vice versa)
4. **Identity not persisted** — peer ID drift breaks CL config
5. **Local Grafana empty** — telemetry off or Prometheus target wrong
6. **Lighthouse drops the gateway** — `--boot-nodes` and `--trusted-peers`, in [Connecting your CL client](08_cl_clients.md#lighthouse)
