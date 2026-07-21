# Optimum Gateway Integration Guide (for Validators)

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

```yaml
log_level: debug
gateway_cluster_id: optimum_hoodi_v0_3
gateway_id: yourorg-prod-hoodi-us-east-gw-1

identity_libp2p_dir: /tmp/libp2p
identity_mump2p_dir: /tmp/mump2p
agent_lib_p2p_port: 33212
agent_mump2p_port: 33213
telemetry_enable: true
telemetry_port: 48123

# Optional Bootstrap override
# remote_bootstrap_url: bootstrap.getoptimum.io
```

Short topic names are recommended. The gateway fetches the current fork digest from Bootstrap and builds the full `/eth2/<fork_digest>/.../ssz_snappy` topic internally. Full topic strings are also accepted.

## Environment Variables

The gateway accepts configuration through environment variables as well. Environment variables override values from `config/app_conf.yml`.

- `OPT_LOG_LEVEL`: Log level: `debug`, `info`, `warn`, `error`
- `OPT_GATEWAY_CLUSTER_ID`: Optimum cluster to join, for example `optimum_hoodi_v0_3`
- `OPT_GATEWAY_ID`: Gateway identifier in `org-env-chain-region-service-suffix` form
- `OPT_CHAIN`: Chain name, for example `hoodi` or `mainnet`
- `OPT_IDENTITY_LIBP2P_DIR`: Directory for the CL-facing libp2p identity
- `OPT_IDENTITY_MUMP2P_DIR`: Directory for the mump2p mesh identity
- `OPT_AGENT_LIB_P2P_PORT`: TCP port used for the CL-facing libp2p listener
- `OPT_AGENT_MUMP2P_PORT`: TCP port used for the gateway-to-gateway mesh
- `OPT_ENABLE_TELEMETRY`: Enable local API and Prometheus metrics
- `OPT_TELEMETRY_PORT`: HTTP port for `/api/v1/*` and `/metrics`
- `OPT_REMOTE_BOOTSTRAP_URL`: Optional Bootstrap override

Example `.env`:

```sh
OPT_LOG_LEVEL=info
OPT_GATEWAY_CLUSTER_ID=optimum_hoodi_v0_3
OPT_GATEWAY_ID=yourorg-prod-hoodi-us-east-gw-1
OPT_CHAIN=hoodi
OPT_IDENTITY_LIBP2P_DIR=/opt/optimum/libp2p
OPT_IDENTITY_MUMP2P_DIR=/opt/optimum/mump2p
OPT_AGENT_LIB_P2P_PORT=33212
OPT_AGENT_MUMP2P_PORT=33213
OPT_ENABLE_TELEMETRY=true
OPT_TELEMETRY_PORT=48123

# Optional overrides
# OPT_REMOTE_BOOTSTRAP_URL=bootstrap.getoptimum.io
```

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
  -p 33213:33213/tcp \
  -p 48123:48123/tcp \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v0.0.1-rc11 \
  -config=/app/config/app_conf.yml
```

`agent_mump2p_port` must be reachable by other gateways in the mesh. It is no longer an outbound-only path.

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

## Verify the Mesh

Check version:

```sh
curl -s http://localhost:48123/api/v1/version
```

Check mesh peers:

```sh
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers'
```

Check startup logs:

```sh
docker logs optimum-gateway | grep -E "fork digest updated from bootstrap|registered gateway to bootstrap server|got mump2p peers from bootstrap server"
```

