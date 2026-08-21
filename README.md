<div align="center">

![Optimum Gateway banner](static/img/banner.png)

[![Docker Image Publish](https://github.com/getoptimum/optimum-gateway/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/getoptimum/optimum-gateway/actions/workflows/docker-publish.yml)
[![Security Scanning](https://github.com/getoptimum/optimum-gateway/actions/workflows/security-scan.yml/badge.svg)](https://github.com/getoptimum/optimum-gateway/actions/workflows/security-scan.yml)
[![Integration](https://github.com/getoptimum/optimum-gateway/actions/workflows/integration.yml/badge.svg)](https://github.com/getoptimum/optimum-gateway/actions/workflows/integration.yml)
[![Latest release](https://img.shields.io/github/v/release/getoptimum/optimum-gateway?include_prereleases&sort=semver)](https://github.com/getoptimum/optimum-gateway/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/getoptimum/optimum-gateway)](go.mod)
[![Coverage](https://img.shields.io/badge/coverage-%E2%89%A571%25-green)](Makefile)
[![Docker image](https://img.shields.io/docker/v/getoptimum/gateway?label=docker&sort=semver)](https://hub.docker.com/r/getoptimum/gateway)
[![Kurtosis Readiness](https://github.com/getoptimum/optimum-gateway/actions/workflows/optimum-package.yml/badge.svg)](https://github.com/getoptimum/optimum-gateway/actions/workflows/optimum-package.yml)
[![Audited by ProbeLab](https://img.shields.io/badge/Audited_by-ProbeLab-1B873F)](https://cdn.probelab.io/media/documents/2026-08-ProbeLab-Security_Audit_Report_Optimum_Gateway.pdf)
[![License](https://img.shields.io/badge/License-MIT-blue)](./LICENSE)

# Optimum Gateway

</div>

**Bridging Ethereum Consensus Layer gossip with the RLNC-enhanced mump2p mesh — for faster block & attestation propagation.**

> **Security:** Independently audited by [ProbeLab](https://probelab.io/blog/security-audit-report-optimum-gateway/)
> ([PDF](https://cdn.probelab.io/media/documents/2026-08-ProbeLab-Security_Audit_Report_Optimum_Gateway.pdf)).
> See [`SECURITY.md`](SECURITY.md) for the trust model, the listener inventory
> with default binds and auth gates, and how to report vulnerabilities.

---

## Overview

The **Optimum Gateway (OG)** connects an Ethereum Consensus Layer (CL) client to the
**mump2p mesh** — a libp2p network running the RLNC-enhanced `mump2p` protocol.

It acts as:

- a **subscriber** to Ethereum libp2p gossip topics (`/eth2/<fork_digest>/.../ssz_snappy`);
- a **publisher/forwarder** of that traffic into the mump2p mesh; and
- a **receiver** of `mump2p` messages, re-encoding and re-injecting them into the local CL libp2p network.

The result is reduced propagation delay, improved validator rewards, and cross-network
latency telemetry — with **no changes required to the CL client** (it just peers with the gateway).

## Architecture

```mermaid
flowchart LR
    CL["Ethereum CL client<br/>(Prysm / Grandine)"]

    subgraph GW["Optimum Gateway"]
        direction TB
        LP["libp2p host<br/>:33212 (CL-facing)"]
        CORE["dedup TTL cache<br/>+ SSZ re-encode"]
        MP["mump2p host<br/>:33213 (mesh)"]
        LP <--> CORE <--> MP
    end

    RLNC["Local rlnc-server<br/>20 shared-memory lanes"]
    MESH["Other gateways<br/>mump2p mesh"]

    CL <-->|"gossip (ssz_snappy)"| LP
    MP <-->|"shared memory"| RLNC
    MP <-->|"RLNC mump2p"| MESH

    classDef ext fill:#eef,stroke:#88a,color:#225;
    class CL,RLNC,MESH ext;
```

The gateway uses `mump2p-protocol` for RLNC-enabled pubsub and delegates RLNC coding to the required local `rlnc-server` process over shared memory.

### Message flow

#### Ethereum CL → mump2p mesh

1. The gateway subscribes to the configured CL gossip topics via its local libp2p host.
2. Each message is fingerprinted (fast XXHash) and stored in a short TTL cache (≈1 min) for dedup.
3. The message is forwarded to connected mump2p peers (with optional aggregation batching for non-block topics).

#### mump2p mesh → Ethereum CL

1. A `mump2p` message arrives from a local or remote mump2p peer.
2. If its hash is already in the TTL cache, it is ignored; otherwise it is SSZ-decoded.
3. It is re-encoded and published to the local libp2p network for CL propagation, and telemetry is recorded.

## Quick start

### Run with Docker

The simplest path is the bundled Compose file, which starts both:

```sh
docker compose -f docker-compose-local.yml up
```

To run the gateway image directly:

```sh
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 33213:33213/tcp \
  -p 48123:48123/tcp \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.1.1 \
  -config=/app/config/app_conf.yml
```

`agent_mump2p_port` (default `33213`) must be reachable by other gateways in the mesh.

### Run from source

Requires **Go 1.26+**, Git, and a working CGO/C toolchain.

```sh
git clone https://github.com/getoptimum/optimum-gateway
cd optimum-gateway
cp config/sample.app_conf.yml config/app_conf.yml
make build                 # builds ./bin/optimum-gateway

# terminal 1; builds ./bin/rlnc-server on first use
make run-rlnc-server

# terminal 2
make run                   # go run cmd/main.go -config config/app_conf.yml
```

### Connect your CL client

Fetch the gateway peer info and point your beacon node at it:

```sh
curl -s http://localhost:48123/api/v1/self_info | jq '{peer_id, multiaddrs: .libp2p.multiaddrs}'
```

```sh
# Example Prysm flag
--peer=/ip4/<YOUR_GATEWAY_IP>/tcp/33212/p2p/<YOUR_GATEWAY_PEER_ID>
```

See [`config/sample.app_conf.yml`](config/sample.app_conf.yml) for the full reference.

## Configuration

The gateway is configured via a YAML file (`config/app_conf.yml`) or environment
variables; **env vars override YAML**. A minimal config:

```yaml
api_key: ogw_live_****         # given by optimum team
gateway_cluster_id: ****       # given by optimum team
log_level: info
chain: hoodi                   # or: mainnet
identity_libp2p_dir: /tmp/libp2p
identity_mump2p_dir: /tmp/mump2p
agent_lib_p2p_port: 33212     # CL-facing libp2p
agent_mump2p_port: 33213     # mump2p mesh (gateway-to-gateway)
telemetry_enable: true
telemetry_port: 48123

```

See [`config/sample.app_conf.yml`](config/sample.app_conf.yml) and [`guide.md`](guide.md) for the full reference.

## APIs

The gateway exposes an HTTP server on `telemetry_port` (default `48123`):

| Endpoint                | Description                                                                                                                 |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `GET /health`           | Structured health check (CL peers, mesh peers, subscribed topics, last block age) — returns `200`/`503` for load balancers. |
| `GET /api/v1/self_info` | Peer info: `peer_id`, multiaddrs, fork digest, chain, peer counts, version/commit.                                          |
| `GET /metrics`          | Prometheus metrics (only when `telemetry_enable: true`).                                                                    |
| `GET /`                 | Liveness root.                                                                                                              |

```sh
curl -s http://localhost:48123/health | jq
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers'
```

## Local development (Prysm)

Generate an identity and run a CL client locally against the gateway:

```sh
go run cmd/generate_identity/main.go
curl https://raw.githubusercontent.com/OffchainLabs/prysm/master/prysm.sh --output prysm.sh && chmod +x prysm.sh && ./prysm.sh beacon-chain generate-auth-secret
mv jwt.hex /home/<USER>/local_cl/local_eth/jwt
make run_cl     # brings up the CL dependency via docker compose
```

Run Prysm as a binary against Hoodi (sync from a checkpoint):

```sh
cd prysm
go run ./cmd/beacon-chain/ \
  --execution-endpoint=http://0.0.0.0:8551 --hoodi \
  --jwt-secret=/home/<USER>/local_cl/local_eth/jwt/jwt.hex \
  --checkpoint-sync-url=https://hoodi.beaconstate.info \
  --genesis-beacon-api-url=https://hoodi.beaconstate.info \
  --peer=<GATEWAY_MULTIADDRESS> --accept-terms-of-use
```

For debugging Prysm from source, patch the flags noted in [`guide.md`](guide.md) and run the `TestGatewayReal` integration test.

## Make targets

```sh
make help               # list all targets
make build              # build the gateway binary
make build-rlnc-server  # build ./bin/rlnc-server from getoptimum/rlnc
make run-rlnc-server    # run the required local RLNC shared-memory server
make test               # unit + integration tests with coverage
make lint               # golangci-lint
make vulcheck           # govulncheck (with documented exception list)
```

Keep `make run-rlnc-server` running in a separate terminal before `make run` or `make test`.

## Documentation

- [Integration guide for validators](guide.md)
- [Architecture Decision Records (ADRs)](docs/adr/README.md)
- [Changelog](docs/CHANGELOG.md)
- [Security model](SECURITY.md)
- [ProbeLab security audit (PDF)](https://cdn.probelab.io/media/documents/2026-08-ProbeLab-Security_Audit_Report_Optimum_Gateway.pdf)

## Contributing

See [`docs/contributing.md`](docs/contributing.md). Please run `make lint` and
`make test` before opening a PR, and report security issues privately per
[`SECURITY.md`](SECURITY.md) rather than via public issues.

## License

Source is provided under the **MIT License**: see [`LICENSE`](./LICENSE).

The MIT License grants copyright permissions only and grants no rights under any
patent. [`PATENTS`](./PATENTS) lists patents and patent applications licensed to
Spice Solutions Inc. by CodeOn; operating this software may involve practicing
patented technology, and any patent rights you require must be obtained from the
relevant patent holder directly. Third-party dependencies are inventoried in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md) with attributions in
[`NOTICE`](./NOTICE).
