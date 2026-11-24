# Optimum Gateway Integration Guide (for Validators)

Short technical overview for validator operators integrating the gateway with their CL client.

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

Minimal `config/app_conf.yml`:

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
- `OPT_AGENT_MUMP2P_ANNOUNCE_IP`: IP other gateways dial for the mesh, instead of the autodetected public IP. See [Mesh behind a load balancer](#mesh-behind-a-load-balancer)
- `OPT_AGENT_MUMP2P_ANNOUNCE_PORT`: Port announced with `OPT_AGENT_MUMP2P_ANNOUNCE_IP` (default: `OPT_AGENT_MUMP2P_PORT`)
- `OPT_ENABLE_TELEMETRY`: Enable local API and Prometheus metrics
- `OPT_TELEMETRY_PORT`: HTTP port for `/health`, `/metrics`, `/api/v1/self_info`
- `OPT_TELEMETRY_ADDR`: IP the telemetry server binds (default: all IPv4 interfaces). Set `127.0.0.1` under host networking
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
  getoptimum/gateway:${version} \
  -config=/app/config/app_conf.yml
```

`${version}` is the latest tag on the [releases](https://github.com/getoptimum/optimum-gateway/releases/latest) page.

`agent_mump2p_port` (sample `43213`) is used for gateway-to-gateway mesh egress.

## Mesh Behind a Load Balancer

By default the gateway announces its autodetected public IP and interface IPs on `agent_mump2p_port` to the mesh. If inbound mesh traffic reaches it through a load balancer or port forward instead, set `agent_mump2p_announce_ip` (and `agent_mump2p_announce_port` if the external port differs) to the address other gateways should dial. The gateway then announces only that address, not its interface or IPv6 addresses, so peers on the same private network also connect through the load balancer. It still listens on `agent_mump2p_port`.

The load balancer must:

- **Forward plain TCP to exactly one gateway.** Peers authenticate by peer ID, so an address shared by several gateways fails the dial. Gateways can share one IP on different ports, but each peer connects to at most 8 of them, because libp2p allows 8 connections per remote IP (per IPv4 address, or per IPv6 /56).
- **Preserve the client source IP.** The mesh allows 8 connections per source IPv4 address. A load balancer that rewrites the source address (for example an AWS NLB with client IP preservation off, or a Kubernetes Service with `externalTrafficPolicy: Cluster`) caps the gateway at 8 inbound mesh peers.

## Outbound-only

Set `agent_mump2p_not_dialable: true` when nothing can dial this gateway. It still registers `mump2p_addr` (that address carries the peer ID) and still dials the peers bootstrap returns, but it announces no mesh address and bootstrap leaves it out of `expose-nodes`. This needs a bootstrap that understands `not_dialable`. Do not set it together with `agent_mump2p_announce_ip`. Two outbound-only gateways cannot dial each other.

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

For Teku, Lighthouse, Nimbus, and Lodestar, configure a direct/trusted peer to the gateway multiaddr and add the CL node to the gateway's `direct_cl_peers` where needed (Lighthouse and Nimbus).

## Verify

```sh
curl -s http://localhost:48123/health | jq
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers'
```

## Metrics

When `telemetry_enable: true`, metrics are at `http://localhost:48123/metrics` (prefix `mump2p_gateway_`).
