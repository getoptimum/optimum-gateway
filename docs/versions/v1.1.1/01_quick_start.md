# Quick Start (Docker)

Get the Optimum Gateway running with Docker.

> **Running on Kubernetes?** See [Kubernetes (Helm)](05_kubernetes.md) for the official Helm chart.

> **Prerequisites:** [Requirements](index.md#requirements) and [Network Requirements](00_network_requirements.md). You also need an **API key** — see [Generate your API key](#generate-your-api-key) below.

## Hardware Requirements

**Minimum:** 2+ vCPUs, 512MB RAM, 500MB+ disk  
**Recommended:** 4+ vCPUs

## Generate your API key

Every gateway authenticates with an **API key**. The key binds your gateway's identity, chain, operator, and validator scope — everything the gateway needs comes from this key, so there is no per-network YAML to edit.

> **Access is invite-only.** You cannot self-register. The Optimum team must **onboard you first**. Anyone not invited cannot create an account or generate a key.

1. **Get invited.** The Optimum team adds you as an operator. You receive a **"Welcome to Optimum"** email invite to the [Partner Console](https://console.getoptimum.io/).
2. **Sign in.** Open the console and sign in **with the same email** the invite was sent to, using your **Google or Microsoft** account — no password.
3. **Open API Keys.** In the sidebar go to **API Keys**, then select the **GATEWAY** tab.

### Generate one key

1. **Generate a key.** Click **GENERATE KEY** and fill in:
   * **Type** — choose **Partner** (this fixes the gateway's publish/subscribe role on mump2p and **cannot be changed after creation**).
   * **Gateway Details** — pick from the dropdowns where available: **Region**, **Consensus Client**, **Hosting Provider**, **DVT**. These label the gateway in monitoring.
2. **Copy the key.** The key (format `ogw_live_...`) is **shown only once**. Copy and store it securely. If you lose it, generate a new one and revoke the old.

### Bulk generate many keys

Use **BULK GENERATE** when you need many gateway keys at once (for example a large fleet rollout). Each key is still **one per gateway** — bulk create saves clicking **GENERATE KEY** repeatedly.

1. **Open API Keys.** Same as above: sidebar **API Keys** -> **GATEWAY** tab.
2. **Start bulk generate.** Click **BULK GENERATE**.
3. **Choose how many.** Enter the number of keys to create. Your operator quota is shown in the dialog (for example `0 of 1000 used`).
4. **Gateway details (optional).** **Region**, **Consensus Client**, **Hosting Provider**, and **DVT** apply to **every** key in the batch — the same dropdowns as single-key generation. Keys are **auto-named**; you do not enter a label per key.
5. **Download your keys.** When creation finishes, download the batch as **CSV or JSON**. Raw keys (`ogw_live_...`) are **shown only once** — store the file securely before closing the dialog. **Keep the browser tab open** until the download completes.
6. **Deploy one key per host.** Map each key to a gateway instance and set `OPT_API_KEY` on that host. Do not reuse a key across gateways.

> **All-or-nothing.** If any key in a batch fails to create, the whole batch is rolled back — none of the keys are kept. Fix the issue (for example quota) and try again.

> **One API key per gateway.** Each gateway instance needs its **own** key. Do not share a key across gateways — the gateway registers a single identity per key, and reuse causes registration conflicts. If you run multiple gateways (e.g. Hoodi + Mainnet, or several hosts), generate a separate key for each — use **BULK GENERATE** for large rollouts.

The gateway exchanges this key on startup at `auth.getoptimum.io/api/v1/auth/token` for a short-lived JWT that carries your `gateway_id`, `chain`, and validator scope.

## Installation

```sh
docker pull getoptimum/gateway:v1.1.1
```

## Configuration

Create `config/app_conf.yml`:

```yaml
log_level: info
gateway_cluster_id: optimum_ethereum_hoodi_v0_1   # assigned by Optimum during onboarding

agent_lib_p2p_port: 33212
agent_mump2p_port: 33213
telemetry_enable: true
telemetry_port: 48123
identity_libp2p_dir: /tmp/libp2p
identity_mump2p_dir: /tmp/mump2p
```

> **Set your API key via the environment, not YAML.** Pass it as `OPT_API_KEY` so it never lives in a config file or image layer. Your `gateway_id`, `chain`, and validator scope are all derived from this key — do not set them in YAML.

```sh
export OPT_API_KEY=ogw_live_xxx
```

## Run

```sh
mkdir -p config data/libp2p data/mump2p

docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 33213:33213/tcp \
  -p 33213:33213/udp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.1.1 \
  -config=/app/config/app_conf.yml
```

> Persist the identity volumes (`/tmp/libp2p`, `/tmp/mump2p`) across restarts — otherwise the gateway's peer ID changes on every run and your CL client config breaks.

> `agent_mump2p_port` must be published and allowed through the firewall for both TCP and UDP (QUIC-v1). The example uses port `33213` for both transports.

## Verify

Health check:

```sh
curl http://localhost:48123/health
```

```json
{
  "status": "healthy",
  "gateway_id": "optimum-dev-hoodi-kubernetes-validator-lighthouse",
  "version": "v1.1.1",
  "commit_hash": "a0b2bc1",
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

For a lightweight liveness probe (process + HTTP up), use the root endpoint:

```sh
curl http://localhost:48123/
```

```json
{"status":"ok"}
```

Metrics with your gateway_id:

```sh
curl http://localhost:48123/metrics | grep gateway_id
```

```text
mump2p_gateway_app_build_info{commit="a0b2bc1",gateway_id="your-gateway-id",paired_with="partner",...} 1
mump2p_gateway_mump2p_peers{gateway_id="your-gateway-id"} 13
...
```

Startup logs (fork digest, topics):

```sh
docker logs optimum-gateway
```

```text
{"msg":"initialized fork digest from chain default","fork_digest":"c6ecb76c","chain":"hoodi"}
{"msg":"fork digest updated from bootstrap","fork_digest":"c6ecb76c"}
{"msg":"subscribed to topic","topic":"/eth2/c6ecb76c/beacon_block/ssz_snappy"}
{"msg":"subscribed to topic","topic":"/eth2/c6ecb76c/beacon_attestation_0/ssz_snappy"}
...
```

**Note:** "Failed to connect to bootstrap" during startup is normal. See [Troubleshooting](04_troubleshoot.md#normal-log-noise-safe-to-ignore).

## Connect CL Client

Get gateway peer info:

```sh
curl -s http://localhost:48123/api/v1/self_info
```

Use `libp2p.multiaddrs[0]` (or another reachable multiaddr) for IP and `peer_id` for peer ID.

> **Recommended CL versions:** Use **Prysm v7.1.8** or later. For Teku use **v26.6.0+** (minimum v26.4.0). See [Troubleshooting](04_troubleshoot.md) for client-specific PeerDAS flags.

### Prysm

```sh
./beacon-chain \
  --peer=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  ...
```

### Teku

```sh
teku \
  --p2p-direct-peers=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --p2p-static-peers=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  ...
```

Use `--p2p-direct-peers` (not just `--p2p-static-peers`) — static peers can be pruned. See [Troubleshooting - Teku PeerDAS](04_troubleshoot.md#teku-peerdas-configuration-important) for details.

### Lighthouse

Add your Lighthouse node as a direct peer in the gateway config so the gateway auto-reconnects after restarts:

```yaml
direct_cl_peers:
  - /ip4/YOUR_LIGHTHOUSE_IP/tcp/9000/p2p/YOUR_LIGHTHOUSE_PEER_ID
```

See [Troubleshooting - Lighthouse v8.x](04_troubleshoot.md#lighthouse-v8x-peerdas-configuration-important) for additional required flags.

### Nimbus

Point Nimbus at the gateway (multiaddr or ENR; multiaddr is typical):

```sh
nimbus_beacon_node \
  --direct-peer=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --netkey-file=/data/netkey \
  ...
```

Add your Nimbus node in the gateway config so the gateway reconnects after restarts:

```yaml
direct_cl_peers:
  - /ip4/YOUR_NIMBUS_IP/tcp/YOUR_NIMBUS_P2P_PORT/p2p/YOUR_NIMBUS_PEER_ID
```

Use a stable `--netkey-file` (not `random`) — Nimbus requires it for privileged direct peers. Nimbus drop/reconnect cycles during warmup are normal; see [Troubleshooting - Nimbus](04_troubleshoot.md#nimbus) for verification and expected behavior.

### Lodestar

Lodestar is supported. Point it at the gateway as a trusted/direct peer and add the Lodestar node to the gateway's `direct_cl_peers` so the gateway re-dials after restarts.

## Next Steps

* [Configuration](02_configuration.md) - Ports, direct peers, advanced settings
* [Troubleshooting](04_troubleshoot.md) - Gateway diagnosis and common issues
* [Metrics](metrics.md) - Gateway and mesh metrics
