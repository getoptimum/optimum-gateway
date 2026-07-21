# Optimum Gateway Integration

Local development and testing setup for the Optimum Gateway with Ethereum EL/CL clients and monitoring.

Partner install, API keys, and production config: **[Gateway documentation (latest)](https://getoptimum.github.io/optimum-gateway/versions/latest/)**.

## Prerequisites

* Docker and Docker Compose installed
* Optimum **API key** (`ogw_live_...`) — set as **`OPT_API_KEY`** in `ethereum/.env` (see [Generate your API key](https://getoptimum.github.io/optimum-gateway/versions/latest/01_quick_start#generate-your-api-key))
* Ports available: gateway `33212`, `48123`; monitoring `9090`, `3000` (lite/full); CL-specific ports below

## Setup

```bash
cd ethereum
cp .env.example .env   # if first run; set OPT_API_KEY=ogw_live_...
make init
```

This will:

* Create data directories
* Generate a `jwt.hex` for EL ↔ CL authentication
* Start the Optimum Gateway and fetch its peer info
* Write `GATEWAY_PEER`, `ADDR`, and `PEER_ID` to `.env`

`GATEWAY_PEER` is used by CL clients (Prysm `--peer`, Teku/Lighthouse/Nimbus/Lodestar direct peer flags).

## Running the Stack

### With Makefile

| Command               | Description                                               |
| --------------------- | --------------------------------------------------------- |
| `make init`           | Initialize project, generate jwt, discover `GATEWAY_PEER` |
| `make run`            | Geth + Prysm + monitoring                                 |
| `make run_prysm`      | Nethermind + Prysm + monitoring                           |
| `make run_teku`       | Nethermind + Teku + monitoring                            |
| `make run_lighthouse` | Nethermind + Lighthouse + monitoring                      |
| `make run_nimbus`     | Nethermind + Nimbus + monitoring                          |
| `make run_lodestar`   | Nethermind + Lodestar + monitoring                        |
| `make lite`           | Gateway + monitoring only (no EL/CL)                      |
| `make stop`           | Stop all services                                         |
| `make reset`          | Wipe data dirs and re-init                                |
| `make clean`          | Stop services and remove local data                       |

If port `9090` is already in use, run without the monitoring profile:

```bash
docker compose --profile nethermind --profile nimbus up -d
```

### With Docker Compose

| Stack                   | Command                                                                         |
| ----------------------- | ------------------------------------------------------------------------------- |
| Geth + Prysm            | `docker compose --profile full --profile geth --profile prysm up -d`            |
| Nethermind + Prysm      | `docker compose --profile full --profile nethermind --profile prysm up -d`      |
| Nethermind + Teku       | `docker compose --profile full --profile nethermind --profile teku up -d`       |
| Nethermind + Lighthouse | `docker compose --profile full --profile nethermind --profile lighthouse up -d` |
| Nethermind + Nimbus     | `docker compose --profile full --profile nethermind --profile nimbus up -d`     |
| Nethermind + Lodestar   | `docker compose --profile full --profile nethermind --profile lodestar up -d`   |
| Gateway only            | `docker compose --profile lite up -d`                                           |

## CL client notes

| Client     | Gateway connection                                    | Local REST (host)                                |
| ---------- | ----------------------------------------------------- | ------------------------------------------------ |
| Prysm      | `--peer=${GATEWAY_PEER}`                              | `http://localhost:3500`                          |
| Teku       | `--p2p-direct-peers=${GATEWAY_PEER}`                  | `http://localhost:3500`                          |
| Lighthouse | `--trusted-peers=${PEER_ID}` + `--boot-nodes=${ADDR}` | `http://localhost:5052`                          |
| Nimbus     | `--direct-peer=${GATEWAY_PEER}`                       | `http://localhost:13500` (maps container `3500`) |
| Lodestar   | `--directPeers=${GATEWAY_PEER}`                       | `http://localhost:9596`                          |

Nimbus P2P is published on host ports `19000` (tcp/udp). Lodestar P2P uses `19001` (tcp/udp) to avoid clashes with Lighthouse on `9000`.

Optional: add Nimbus to gateway `config/app_conf.yml` after it is running:

```yaml
direct_cl_peers:
  - /ip4/172.29.0.2/tcp/19000/p2p/<NIMBUS_PEER_ID>
```

Get `<NIMBUS_PEER_ID>` from:

```bash
curl -s http://localhost:13500/eth/v1/node/identity | jq -r '.data.peer_id'
```

Use the Docker network address from `p2p_addresses` (not `127.0.0.1`), then restart the gateway.

Optional: add Lodestar to gateway `config/app_conf.yml` after `make run_lodestar`:

```yaml
direct_cl_peers:
  - /ip4/172.29.0.x/tcp/19001/p2p/<LODESTAR_PEER_ID>
```

```bash
curl -s http://localhost:9596/eth/v1/node/identity | jq -r '.data.p2p_addresses[0]'
```

## Configuration

Gateway config: `ethereum/config/app_conf.yml` (created from `config/sample.app_conf.yml` on first `make init`).

* **`OPT_API_KEY`** — set in `ethereum/.env`, not in YAML. Required for auth and validator scope.
* **`gateway_cluster_id`** — must match the cluster assigned during onboarding (sample uses `optimum_hoodi_v0_2` for Hoodi dev).
* Image versions (`GATEWAY_VERSION`, CL/EL tags) and `GATEWAY_PEER` live in `ethereum/.env` (from `.env.example`).

Recommended CL versions and PeerDAS flags: [Gateway quick start](https://getoptimum.github.io/optimum-gateway/versions/latest/01_quick_start) and [Troubleshooting](https://getoptimum.github.io/optimum-gateway/versions/latest/04_troubleshoot).

## Monitoring

| Service     | URL                                   |
| ----------- | ------------------------------------- |
| Prometheus  | <http://localhost:9090>               |
| Grafana     | <http://localhost:3000> (admin/admin) |
| Gateway API | <http://localhost:48123>              |

## Verify

```bash
# Gateway health and peers
curl -s http://localhost:48123/health | jq '{status, checks: .checks.cl_peers}'
curl -s http://localhost:48123/api/v1/self_info | jq '{peer_id, libp2p: .libp2p.total_peers}'

# Nimbus sees gateway (after make run_nimbus)
curl -s http://localhost:13500/eth/v1/node/peers/$(grep PEER_ID .env | cut -d= -f2) | jq '.data | {state, agent}'

# Lodestar sees gateway (after make run_lodestar)
curl -s http://localhost:9596/eth/v1/node/peers/$(grep PEER_ID .env | cut -d= -f2) | jq '.data | {state, agent}'
```

When CL is connected: gateway `checks.cl_peers` ≥ 1 and CL peer `state` is `connected` (brief `disconnected` can occur during churn).

## Structure

```text
integration/
├── README.md
├── ethereum/
│   ├── docker-compose.yml
│   ├── Makefile
│   ├── config/
│   │   └── sample.app_conf.yml
│   ├── .env.example
│   └── prysm.sh
└── grafana/
    ├── docker-compose-grafana.yml
    ├── prometheus/
    ├── grafana-provisioning/
    └── grafana-dashboards/
        └── partner-dashboard.json   # mump2p_gateway_* metrics (v1.0.2+)
```

## Important Notes

* Set **`OPT_API_KEY`** in `.env` before `make init` — the gateway will not authenticate without it
* Only one EL profile at a time (`geth` or `nethermind`)
* Only one CL profile at a time (`prysm`, `teku`, `lighthouse`, `nimbus`, or `lodestar`)
* `GATEWAY_PEER`, `ADDR`, and `PEER_ID` are set by `make init`
* Sync issues: `make reset`

## Troubleshooting

```bash
docker logs optimum-gateway
docker logs nimbus
docker logs lodestar
make stop && docker compose --profile nethermind --profile lodestar up -d   # skip monitoring if 9090 busy
```

CL startup can take several minutes (checkpoint sync). Gateway bootstrap errors to remote mump2p nodes during local dev are expected if outbound mesh is unreachable.
