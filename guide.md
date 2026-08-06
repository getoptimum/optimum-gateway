# Optimum Gateway Integration Guide (for Validators)

> **Partner docs:** For API keys, Docker quick start, CL peering, and troubleshooting, use the [versioned documentation](https://getoptimum.github.io/optimum-gateway/versions/latest/) (current release: **v1.1.1**). This guide is a short technical overview.

## Overview

Optimum Gateway connects your Ethereum Consensus Layer (CL) client to the **mump2p mesh** — a high-speed, RLNC-enhanced libp2p cluster optimized for data propagation.

It listens to `gossipsub` traffic from your CL client, forwards it into the mump2p mesh, and republishes mesh traffic back into your local CL network.

## Purpose

1. Accelerate message propagation using the mump2p mesh.
2. Improve resilience and redundancy across validator networks.
3. Requires no changes to the CL client codebase – just a local gossipsub peer.

## Current Topology

- Your beacon node peers with the gateway over libp2p.
- The gateway embeds its own Optimum mump2p host.
- Bootstrap provides peer discovery, registration, fork digest, and telemetry collection.
- Gateways connect directly to other gateways over the mesh.

## Minimal Configuration

See [Configuration](https://getoptimum.github.io/optimum-gateway/versions/latest/02_configuration) for the full partner config. Minimal `config/app_conf.yml`:

```yaml
log_level: info
gateway_cluster_id: optimum_ethereum_hoodi_v0_1   # assigned by Optimum

identity_libp2p_dir: /tmp/libp2p
identity_mump2p_dir: /tmp/mump2p
agent_lib_p2p_port: 33212
agent_mump2p_port: 43213
telemetry_enable: true
telemetry_port: 48123
```

Set **`OPT_API_KEY=ogw_live_...`** in the environment (not YAML). Chain, gateway ID, and validator scope come from the key.

## Environment Variables

The gateway accepts configuration through environment variables as well. Environment variables override values from `config/app_conf.yml`.

- `OPT_API_KEY`: Gateway API key (`ogw_live_...`) — **required for partners**
- `OPT_LOG_LEVEL`: Log level: `debug`, `info`, `warn`, `error`
- `OPT_GATEWAY_CLUSTER_ID`: Optimum cluster to join (assigned during onboarding)
- `OPT_IDENTITY_LIBP2P_DIR`: Directory for the CL-facing libp2p identity
- `OPT_IDENTITY_MUMP2P_DIR`: Directory for the mump2p mesh identity
- `OPT_AGENT_LIB_P2P_PORT`: TCP port used for the CL-facing libp2p listener
- `OPT_AGENT_MUMP2P_PORT`: TCP port used for the gateway-to-gateway mesh
- `OPT_ENABLE_TELEMETRY`: Enable local API and Prometheus metrics
- `OPT_TELEMETRY_PORT`: HTTP port for `/health`, `/metrics`, `/api/v1/self_info`
- `OPT_REMOTE_PUSH_ENABLE`: Push metrics/logs to Optimum (requires telemetry + API key)

## Startup Flow

1. The gateway starts its CL-facing libp2p listener.
2. The gateway resolves the fork digest from Bootstrap.
3. The gateway registers its advertised Optimum mesh address with Bootstrap.
4. Bootstrap returns peer addresses for the same chain and cluster.
5. The gateway joins the mesh and begins forwarding traffic.

## Run from Source

```sh
git clone https://github.com/getoptimum/optimum-gateway
cd optimum-gateway
cp config/sample.app_conf.yml config/app_conf.yml
make build
make run
```

## Run with Docker

```sh
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=ogw_live_xxx \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.1.1 \
  -config=/app/config/app_conf.yml
```

`agent_mump2p_port` (sample `43213`) is used for gateway-to-gateway mesh egress.

## Connect Your CL Client

Get the gateway peer info:

```sh
curl -s http://localhost:48123/api/v1/self_info | jq
```

Use:

- `peer_id`
- a reachable entry from `libp2p.multiaddrs`

Example Prysm flag:

```sh
--peer=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID
```

See [Quick Start — Connect CL Client](https://getoptimum.github.io/optimum-gateway/versions/latest/01_quick_start#connect-cl-client) for Teku, Lighthouse, Nimbus, and Lodestar.

## Verify

```sh
curl -s http://localhost:48123/health | jq
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers'
```

## Metrics

When `telemetry_enable: true`, metrics are at `http://localhost:48123/metrics` (prefix `mump2p_gateway_`). See [Metrics & Grafana](https://getoptimum.github.io/optimum-gateway/versions/latest/03_telemetry).
