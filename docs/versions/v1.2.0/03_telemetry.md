# Telemetry & Monitoring

> **Prerequisites:** [Quick Start](01_quick_start.md) with `telemetry_enable: true`.

## Endpoints

| Endpoint                | Description                                       |
| ----------------------- | ------------------------------------------------- |
| `GET /health`           | Structured health check with 200/503 status       |
| `GET /api/v1/self_info` | Gateway identity, peers, config, and pairing mode |
| `GET /metrics`          | Prometheus metrics                                |

**`/api/v1/version` is removed.** Version info is available in `/health` and `/api/v1/self_info`.

## Health Endpoint

`GET /health` returns 200 (healthy) or 503 (degraded) based on six checks:

```json
{
  "status": "healthy",
  "gateway_id": "optimum-dev-hoodi-kubernetes-validator-lighthouse",
  "version": "v1.2.0",
  "commit_hash": "a0b2bc1",
  "uptime_seconds": 1639,
  "checks": {
    "cl_health": {"status": "ok"},
    "cl_peers": {"status": "ok", "value": 1},
    "last_block_age_sec": {"status": "ok", "value": 1},
    "mump2p_health": {"status": "ok"},
    "mump2p_peers": {"status": "ok", "value": 13},
    "subscribed_topics": {"status": "ok", "value": 65}
  }
}
```

| Check                | Passes when                    |
| -------------------- | ------------------------------ |
| `cl_peers`           | ≥ 1 CL peer connected          |
| `mump2p_peers`       | ≥ 1 mump2p peer connected      |
| `subscribed_topics`  | ≥ 1 topic subscribed           |
| `last_block_age_sec` | Last block received < 60s ago  |
| `cl_health`          | CL gossip traffic in last 30s  |
| `mump2p_health`      | Mesh traffic in last 30s       |

If any check fails, `status` becomes `"degraded"` and the failing checks are listed in `"failing"`.

A check can also be `skipped`, meaning it does not apply to this node's mode. A `stream_only` gateway never starts the CL host, so `cl_peers`, `cl_health` and `subscribed_topics` are `skipped` and left out of `failing` and of the 200/503 roll-up. The `mump2p_gateway_cl_health_status` and `mump2p_gateway_cl_peers` gauges have no such notion and still read 0 on those nodes, so exclude them from alerts there, including the "Status" panel below, which multiplies `cl_peers` by `mump2p_peers`.

**Propagation:** `mump2p_gateway_propagation_state` reports whether the gateway is relaying mump2p traffic to your CL (`1` = on, `0` = disabled via Optimum dynamic config). The same state appears as `propagation_enabled` in `/api/v1/self_info`.

## Self Info

`GET /api/v1/self_info` returns gateway identity and peer information for CL client connection and operational visibility.

**Example response:**

```json
{
  "propagation_enabled": true,
  "chain": "hoodi",
  "commit_hash": "a0b2bc1",
  "fork_digest": "c6ecb76c",
  "gateway_cluster_id": "optimum_hoodi_v0_2",
  "gateway_id": "optimum-dev-hoodi-kubernetes-validator-lighthouse",
  "paired_with": "partner",
  "remote_url": "bootstrap.getoptimum.io",
  "version": "v1.2.0",
  "skip_messages_from_self": true,
  "peer_id": "12D3KooWNKZuPvVw5Sfnbq3nvyukxmhBBPZUXHeqzwcehmmwnKcR",
  "libp2p": {
    "direct_peers": {
      "16Uiu2HAmDv5wLBz48dbi7atvUhcF7sefwQjwKLSrMPqLCYZceYCV": {
        "ID": "16Uiu2HAmDv5wLBz48dbi7atvUhcF7sefwQjwKLSrMPqLCYZceYCV",
        "Addrs": ["/ip4/10.0.0.2/tcp/9000"]
      }
    },
    "multiaddrs": ["/ip4/10.0.0.10/tcp/33212", "/ip4/203.0.113.7/tcp/33212"],
    "peer_ids": ["16Uiu2HAmDv5wLBz48dbi7atvUhcF7sefwQjwKLSrMPqLCYZceYCV"],
    "peers_per_topic": {
      "/eth2/c6ecb76c/beacon_block/ssz_snappy": 1,
      "/eth2/c6ecb76c/beacon_attestation_0/ssz_snappy": 1
    },
    "total_peers": 1
  },
  "mump2p": {
    "peer_ids": ["12D3KooWQKjdLGDYu1b2p4NJ39iuFsVArXmkrA77co7a2gDD94uv", "..."],
    "peers_per_topic": {
      "/eth2/c6ecb76c/beacon_block/ssz_snappy": 5,
      "mump2p_aggregated_messages": 5
    },
    "total_peers": 13
  },
  "rlnc_config": {
    "forward_shard_threshold": 0.75,
    "publisher_shard_multiplier": 1.2,
    "random_message_size_bytes": 512,
    "rlnc_shard_factor": 4
  }
}
```

Use a multiaddr from `libp2p.multiaddrs` that is reachable from your CL host and `peer_id` when configuring your CL client: `--peer=/ip4/YOUR_IP/tcp/33212/p2p/YOUR_PEER_ID`.

## Gateway Metrics

**Endpoint:** `GET /metrics`

Metrics are labeled with `gateway_id` and `gateway_cluster_id`. See [Metrics Reference](metrics.md) for the full list. Consumer block-stream series (`mump2p_stream_*`) appear when `stream_enable` is true.

**CL connected:** When a CL client connects, `mump2p_gateway_cl_peers` goes from 0 to >=1. Messages flow on both block and attestation topics.

## Logs

Gateway logs are JSON lines. Use `docker logs optimum-gateway` to inspect them. Key fields: `fork_digest` (e.g. `c6ecb76c` for hoodi) appears in startup, bootstrap update, and topic names.


## Setting Up the Monitoring Dashboard

This section walks through deploying a local Prometheus + Grafana stack that scrapes your gateway and loads the Partner Dashboard automatically.

### Prerequisites

* Docker and Docker Compose installed
* Optimum Gateway running with `telemetry_enable: true`
* Ports 3000 (Grafana) and 9090 (Prometheus) available

### Step 1: Create Monitoring Directory

```bash
mkdir -p optimum-monitoring/{prometheus,grafana-provisioning/datasources,grafana-provisioning/dashboards,grafana-dashboards}
cd optimum-monitoring
```

Your final structure:

```text
optimum-monitoring/
├── docker-compose.yml
├── prometheus/
│   ├── prometheus.yml
│   └── targets.json
├── grafana-provisioning/
│   ├── datasources/
│   │   └── prometheus.yaml
│   └── dashboards/
│       └── dashboards.yml
└── grafana-dashboards/
    └── partner-dashboard.json
```

### Step 2: Docker Compose

Create `docker-compose.yml`:

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus:/etc/prometheus
      - prometheus-data:/prometheus
    ports:
      - "9090:9090"
    restart: unless-stopped
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"
      - "--storage.tsdb.retention.time=1h"
      - "--storage.tsdb.retention.size=2GB"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/-/healthy"]
      interval: 30s
      timeout: 10s
      retries: 3
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - ./grafana-provisioning:/etc/grafana/provisioning:ro
      - ./grafana-dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    environment:
      - GF_SECURITY_ADMIN_USER=admin
      - GF_SECURITY_ADMIN_PASSWORD=admin
    restart: unless-stopped
    depends_on:
      - prometheus
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/api/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  prometheus-data:
  grafana-data:
```

### Step 3: Prometheus Configuration

Create `prometheus/prometheus.yml`:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'gateway'
    file_sd_configs:
      - files:
          - /etc/prometheus/targets.json
```

Create `prometheus/targets.json` (choose your platform):

**Docker Desktop (macOS / Windows):**

```json
[
  {
    "targets": ["host.docker.internal:48123"],
    "labels": { "job": "gateway" }
  }
]
```

**Linux Docker:**

```json
[
  {
    "targets": ["172.17.0.1:48123"],
    "labels": { "job": "gateway" }
  }
]
```

If you run Hoodi and Mainnet gateways at the same time, add one scrape entry per gateway in `targets.json` (different ports); use the dashboard **Network** dropdown to switch between them.

### Step 4: Grafana Provisioning

Create `grafana-provisioning/datasources/prometheus.yaml`:

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    url: http://prometheus:9090
    access: proxy
    isDefault: true
```

Create `grafana-provisioning/dashboards/dashboards.yml`:

```yaml
apiVersion: 1

providers:
  - name: 'optimum-gateway'
    orgId: 1
    folder: 'Default'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: true
```

### Step 5: Add the Partner Dashboard

Copy the JSON below into `grafana-dashboards/partner-dashboard.json`:

<details>
<summary><strong>Click to expand: Partner Dashboard JSON (v1.2.0)</strong></summary>

```json
{
  "annotations": {
    "list": [
      {
        "builtIn": 1,
        "datasource": {
          "type": "grafana",
          "uid": "-- Grafana --"
        },
        "enable": true,
        "hide": true,
        "iconColor": "rgba(0, 211, 255, 1)",
        "name": "Annotations & Alerts",
        "type": "dashboard"
      }
    ]
  },
  "description": "Partner-facing dashboard for Optimum Gateway v1.2.0. All metrics sourced from the gateway /metrics endpoint. Select your Prometheus datasource and Network (Hoodi or Mainnet), then your gateway (by gateway_label).",
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 1,
  "id": null,
  "links": [],
  "panels": [
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 0
      },
      "id": 1,
      "panels": [],
      "title": "Gateway Info",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "ON when both CL and mump2p peers are connected to the gateway.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [
            {
              "options": {
                "0": {
                  "color": "red",
                  "index": 1,
                  "text": "OFF"
                },
                "1": {
                  "color": "green",
                  "index": 0,
                  "text": "ON"
                }
              },
              "type": "value"
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": 0
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          }
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 4,
        "x": 0,
        "y": 1
      },
      "id": 2,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "clamp_max((max(last_over_time(mump2p_gateway_cl_peers{gateway_label=\"$gateway\"}[$__rate_interval])) or vector(0)) * (max(last_over_time(mump2p_gateway_mump2p_peers{gateway_label=\"$gateway\"}[$__rate_interval])) or vector(0)), 1)",
          "instant": true,
          "legendFormat": "__auto",
          "range": false,
          "refId": "A"
        }
      ],
      "title": "Status",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "cl_health_status. 1 = fork-supported message from CL (libp2p) within last 30s. 0 = silent CL even if cl_peers > 0.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [
            {
              "options": {
                "0": {
                  "color": "red",
                  "index": 1,
                  "text": "DEGRADED"
                },
                "1": {
                  "color": "green",
                  "index": 0,
                  "text": "HEALTHY"
                }
              },
              "type": "value"
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": 0
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          },
          "unit": "none"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 3,
        "w": 3,
        "x": 4,
        "y": 1
      },
      "id": 922,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "mump2p_gateway_cl_health_status{gateway_label=\"$gateway\"}",
          "instant": true,
          "legendFormat": "__auto",
          "range": false,
          "refId": "A"
        }
      ],
      "title": "CL freshness",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "peers connected to the gateway.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": 0
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          }
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 5,
        "x": 7,
        "y": 1
      },
      "id": 3,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "value_and_name",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "mump2p_gateway_cl_peers{gateway_label=\"$gateway\"}",
          "legendFormat": "CL Peers",
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "mump2p_gateway_mump2p_peers{gateway_label=\"$gateway\"}",
          "hide": false,
          "legendFormat": "mump2p Peers",
          "refId": "B"
        }
      ],
      "title": "Peers",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "Gateway type (paired_with), version, commit, and public IP.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "fixedColor": "text",
            "mode": "fixed"
          },
          "custom": {
            "align": "auto",
            "cellOptions": {
              "type": "auto"
            },
            "filterable": false,
            "footer": {
              "reducers": []
            },
            "inspect": false
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": 0
              }
            ]
          }
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 7,
        "x": 12,
        "y": 1
      },
      "id": 5,
      "options": {
        "cellHeight": "sm",
        "frameIndex": 0,
        "showHeader": false
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "mump2p_gateway_app_build_info{gateway_label=\"$gateway\"}",
          "instant": true,
          "legendFormat": "__auto",
          "range": false,
          "refId": "A"
        }
      ],
      "title": "Build Info",
      "transformations": [
        {
          "id": "labelsToFields",
          "options": {
            "keepLabels": [
              "paired_with",
              "version",
              "commit",
              "public_ip"
            ],
            "mode": "rows"
          }
        }
      ],
      "type": "table"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [
            {
              "options": {
                "0": {
                  "color": "orange",
                  "index": 1,
                  "text": "disabled"
                },
                "1": {
                  "color": "green",
                  "index": 0,
                  "text": "enabled"
                }
              },
              "type": "value"
            },
            {
              "options": {
                "match": "null",
                "result": {
                  "color": "text",
                  "index": 2,
                  "text": "unknown cluster"
                }
              },
              "type": "special"
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "orange",
                "value": 0
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          }
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 5,
        "x": 19,
        "y": 1
      },
      "id": 7001,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "mump2p_gateway_propagation_state{gateway_label=\"$gateway\"}",
          "instant": true,
          "legendFormat": "propagation",
          "range": false,
          "refId": "A"
        }
      ],
      "title": "Propagation",
      "type": "stat",
      "description": "Whether this gateway is propagating mump2p messages to CL (from mump2p_gateway_propagation_state: 1=enabled, 0=disabled via Optimum dynamic config)."
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "mump2p_health_status. 1 = fork-supported message from mump2p within last 30s. 0 = mesh connected but no recent traffic.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [
            {
              "options": {
                "0": {
                  "color": "red",
                  "index": 1,
                  "text": "DEGRADED"
                },
                "1": {
                  "color": "green",
                  "index": 0,
                  "text": "HEALTHY"
                }
              },
              "type": "value"
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": 0
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          },
          "unit": "none"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 3,
        "w": 3,
        "x": 4,
        "y": 4
      },
      "id": 923,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "mump2p_gateway_mump2p_health_status{gateway_label=\"$gateway\"}",
          "instant": true,
          "legendFormat": "__auto",
          "range": false,
          "refId": "A"
        }
      ],
      "title": "mump2p freshness",
      "type": "stat"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 7
      },
      "id": 900,
      "panels": [],
      "title": "Authentication Status (JWT) - Gateway: ${gateway}",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "VALID when a JWT has been minted and exp is in the future. INVALID = no token or expired (gateway cannot auth to billing/peers).",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [
            {
              "options": {
                "0": {
                  "color": "red",
                  "index": 1,
                  "text": "INVALID"
                },
                "1": {
                  "color": "green",
                  "index": 0,
                  "text": "VALID"
                }
              },
              "type": "value"
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": 0
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          },
          "unit": "none"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 5,
        "w": 6,
        "x": 0,
        "y": 8
      },
      "id": 902,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "clamp_max((mump2p_gateway_auth_token_expires_at_seconds{gateway_label=\"$gateway\"} > bool(time())) * (mump2p_gateway_auth_token_expires_at_seconds{gateway_label=\"$gateway\"} > 0), 1)",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Token valid now",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "How long the cached JWT remains valid. steps down, then jumps back up in the chart below = background refresh from billing (POST /api/v1/auth/token).",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": 0
              },
              {
                "color": "orange",
                "value": 1800
              },
              {
                "color": "green",
                "value": 7200
              }
            ]
          },
          "unit": "dtdhms"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 5,
        "w": 6,
        "x": 6,
        "y": 8
      },
      "id": 901,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "clamp_min(mump2p_gateway_auth_token_expires_at_seconds{gateway_label=\"$gateway\"} - time(), 0)",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Time until token expires",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "Success share over the last $auth_prom_interval (6h). If no mints occurred in that window but the token is still valid, shows 100% (healthy idle). JWT refresh runs every ~3h.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": 0
              },
              {
                "color": "orange",
                "value": 90
              },
              {
                "color": "green",
                "value": 99
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 5,
        "w": 6,
        "x": 12,
        "y": 8
      },
      "id": 903,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "(\n  sum(increase(mump2p_gateway_auth_token_mint_total{gateway_label=\"$gateway\"}[$auth_prom_interval])) == 0\n  and mump2p_gateway_auth_token_expires_at_seconds{gateway_label=\"$gateway\"} > time()\n  and mump2p_gateway_auth_token_expires_at_seconds{gateway_label=\"$gateway\"} > 0\n) * 100\nor\n100 * sum(increase(mump2p_gateway_auth_token_mint_total{gateway_label=\"$gateway\", result=\"success\"}[$auth_prom_interval]))\n  / clamp_min(sum(increase(mump2p_gateway_auth_token_mint_total{gateway_label=\"$gateway\"}[$auth_prom_interval])), 1)",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Mint success rate",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "Failed mint attempts",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "decimals": 0,
          "mappings": [],
          "noValue": "0",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": 0
              },
              {
                "color": "orange",
                "value": 1
              },
              {
                "color": "red",
                "value": 3
              }
            ]
          },
          "unit": "short"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 5,
        "w": 6,
        "x": 18,
        "y": 8
      },
      "id": 909,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "auto",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "sum(increase(mump2p_gateway_auth_token_mint_total{gateway_label=\"$gateway\", result!=\"success\"}[$auth_prom_interval])) or vector(0)",
          "legendFormat": "__auto",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Failed mints (last 6h)",
      "type": "stat"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 13
      },
      "id": 100,
      "panels": [],
      "title": "Block Arrival Performance",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "Percentage of blocks first seen via mump2p vs libp2p over the last 5 minutes.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "max": 100,
          "min": 0,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "yellow",
                "value": 0
              },
              {
                "color": "green",
                "value": 60
              },
              {
                "color": "dark-green",
                "value": 90
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 8,
        "x": 0,
        "y": 14
      },
      "id": 101,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "percentChangeColorMode": "standard",
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "showPercentChange": false,
        "textMode": "value",
        "wideLayout": true
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "100 * sum(rate(mump2p_gateway_blocks_first_seen_mump2p_total{gateway_label=\"$gateway\"}[5m]))\n/\n(\n  sum(rate(mump2p_gateway_blocks_first_seen_mump2p_total{gateway_label=\"$gateway\"}[5m]))\n  +\n  sum(rate(mump2p_gateway_blocks_first_seen_libp2p_total{gateway_label=\"$gateway\"}[5m]))\n)",
          "legendFormat": "mump2p first",
          "refId": "A"
        }
      ],
      "title": "Accelerated slots (last 5m)",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "Percentage of slots where mump2p was strictly faster than libp2p over time for this gateway. Greener = higher share.",
      "fieldConfig": {
        "defaults": {
          "custom": {
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "scaleDistribution": {
              "type": "linear"
            }
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 16,
        "x": 8,
        "y": 14
      },
      "id": 105,
      "interval": "30s",
      "options": {
        "calculate": false,
        "cellGap": 1,
        "color": {
          "exponent": 0.5,
          "fill": "dark-orange",
          "max": 100,
          "min": 0,
          "mode": "scheme",
          "reverse": true,
          "scale": "exponential",
          "scheme": "Greens",
          "steps": 64
        },
        "exemplars": {
          "color": "rgba(255,0,255,0.7)"
        },
        "filterValues": {
          "le": 1e-09
        },
        "legend": {
          "show": true
        },
        "rowsFrame": {
          "layout": "auto"
        },
        "tooltip": {
          "mode": "single",
          "showColorScale": false,
          "yHistogram": false
        },
        "yAxis": {
          "axisPlacement": "left",
          "reverse": false
        }
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "100 * sum(rate(mump2p_gateway_blocks_first_seen_mump2p_total{gateway_label=\"$gateway\"}[5m])) / (sum(rate(mump2p_gateway_blocks_first_seen_mump2p_total{gateway_label=\"$gateway\"}[5m])) + sum(rate(mump2p_gateway_blocks_first_seen_libp2p_total{gateway_label=\"$gateway\"}[5m])))",
          "legendFormat": "mump2p boost",
          "range": true,
          "refId": "A"
        }
      ],
      "title": "Accelerated slots over time",
      "type": "heatmap"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "Median block arrival time via mump2p from slot start for this gateway over time.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "never",
            "showValues": false,
            "spanNulls": true,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": 0
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          },
          "unit": "ms"
        },
        "overrides": [
          {
            "matcher": {
              "id": "byName",
              "options": "mump2p"
            },
            "properties": [
              {
                "id": "color",
                "value": {
                  "fixedColor": "#73BF69",
                  "mode": "fixed"
                }
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 8,
        "w": 24,
        "x": 0,
        "y": 20
      },
      "id": 104,
      "options": {
        "legend": {
          "calcs": [
            "mean",
            "lastNotNull"
          ],
          "displayMode": "table",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "multi",
          "sort": "desc"
        }
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.5, sum by(le) (rate(mump2p_gateway_block_arrival_mump2p_ms_bucket{gateway_label=\"$gateway\"}[5m])))",
          "legendFormat": "mump2p",
          "refId": "A"
        }
      ],
      "title": "mump2p arrival over time (median)",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 28
      },
      "id": 300,
      "panels": [],
      "title": "Attestation Performance: Gateway Level",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "End-to-end attestation propagation latency across the mump2p network over time. Measures time from sender gateway emit to receiver gateway decode.",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "ms",
            "axisPlacement": "left",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "never",
            "showValues": false,
            "spanNulls": true,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": 0
              }
            ]
          },
          "unit": "ms"
        },
        "overrides": [
          {
            "matcher": {
              "id": "byName",
              "options": "p50"
            },
            "properties": [
              {
                "id": "color",
                "value": {
                  "fixedColor": "green",
                  "mode": "fixed"
                }
              }
            ]
          },
          {
            "matcher": {
              "id": "byName",
              "options": "p95"
            },
            "properties": [
              {
                "id": "color",
                "value": {
                  "fixedColor": "yellow",
                  "mode": "fixed"
                }
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 7,
        "w": 24,
        "x": 0,
        "y": 29
      },
      "id": 310,
      "options": {
        "legend": {
          "calcs": [
            "mean",
            "lastNotNull"
          ],
          "displayMode": "table",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "multi",
          "sort": "desc"
        }
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.5, sum by(le) (rate(mump2p_gateway_attestation_propagation_latency_ms_bucket{gateway_label=\"$gateway\"}[5m])))",
          "legendFormat": "p50",
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.95, sum by(le) (rate(mump2p_gateway_attestation_propagation_latency_ms_bucket{gateway_label=\"$gateway\"}[5m])))",
          "hide": false,
          "legendFormat": "p95",
          "refId": "B"
        }
      ],
      "title": "Attestation propagation at $gateway",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": {
        "h": 1,
        "w": 24,
        "x": 0,
        "y": 36
      },
      "id": 507,
      "panels": [],
      "title": "Attestation Performance: Fleet Level",
      "type": "row"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
      "description": "How long it takes for attestations to reach 50% and 95% of the nodes in mump2p once they are injected in the network. ",
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisBorderShow": false,
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "ms",
            "axisPlacement": "left",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {
              "legend": false,
              "tooltip": false,
              "viz": false
            },
            "insertNulls": false,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "never",
            "showValues": false,
            "spanNulls": true,
            "stacking": {
              "group": "A",
              "mode": "none"
            },
            "thresholdsStyle": {
              "mode": "off"
            }
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": 0
              }
            ]
          },
          "unit": "ms"
        },
        "overrides": [
          {
            "matcher": {
              "id": "byName",
              "options": "p50"
            },
            "properties": [
              {
                "id": "color",
                "value": {
                  "fixedColor": "green",
                  "mode": "fixed"
                }
              }
            ]
          },
          {
            "matcher": {
              "id": "byName",
              "options": "p95"
            },
            "properties": [
              {
                "id": "color",
                "value": {
                  "fixedColor": "yellow",
                  "mode": "fixed"
                }
              }
            ]
          }
        ]
      },
      "gridPos": {
        "h": 7,
        "w": 24,
        "x": 0,
        "y": 37
      },
      "id": 506,
      "options": {
        "legend": {
          "calcs": [
            "mean",
            "lastNotNull"
          ],
          "displayMode": "table",
          "placement": "bottom",
          "showLegend": true
        },
        "tooltip": {
          "hideZeros": false,
          "mode": "multi",
          "sort": "desc"
        }
      },
      "pluginVersion": "12.3.1",
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.5, sum by(le) (rate(mump2p_gateway_attestation_propagation_latency_ms_bucket{gateway_cluster_id=~\"$network\"}[5m])))",
          "legendFormat": "p50",
          "range": true,
          "refId": "A"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${ds_prometheus}"
          },
          "editorMode": "code",
          "expr": "histogram_quantile(0.95, sum by(le) (rate(mump2p_gateway_attestation_propagation_latency_ms_bucket{gateway_cluster_id=~\"$network\"}[5m])))",
          "hide": false,
          "legendFormat": "p95",
          "range": true,
          "refId": "B"
        }
      ],
      "title": "mump2p network attestation propagation",
      "type": "timeseries"
    }
  ],
  "preload": false,
  "refresh": "5s",
  "schemaVersion": 42,
  "tags": [
    "optimum",
    "partner",
    "v1.2.0"
  ],
  "templating": {
    "list": [
      {
        "current": {},
        "includeAll": false,
        "label": "Prometheus",
        "name": "ds_prometheus",
        "options": [],
        "query": "prometheus",
        "refresh": 1,
        "regex": "",
        "type": "datasource",
        "hide": 0
      },
      {
        "current": {
          "text": "Hoodi",
          "value": "optimum_hoodi_.*"
        },
        "includeAll": false,
        "label": "Network",
        "name": "network",
        "options": [
          {
            "selected": true,
            "text": "Hoodi",
            "value": "optimum_hoodi_.*"
          },
          {
            "selected": false,
            "text": "Mainnet",
            "value": "optimum_ethereum_mainnet_.*"
          }
        ],
        "query": "Hoodi : optimum_hoodi_.*, Mainnet : optimum_ethereum_mainnet_.*",
        "type": "custom"
      },
      {
        "current": {},
        "datasource": {
          "type": "prometheus",
          "uid": "${ds_prometheus}"
        },
        "definition": "label_values(mump2p_gateway_app_build_info{gateway_cluster_id=~\"$network\", gateway_label=~\".+\"}, gateway_label)",
        "includeAll": false,
        "label": "Gateway",
        "name": "gateway",
        "options": [],
        "query": {
          "qryType": 1,
          "query": "label_values(mump2p_gateway_app_build_info{gateway_cluster_id=~\"$network\", gateway_label=~\".+\"}, gateway_label)",
          "refId": "PrometheusVariableQueryEditor-VariableQuery"
        },
        "refresh": 2,
        "regex": "",
        "sort": 1,
        "type": "query"
      },
      {
        "current": {
          "text": "6h",
          "value": "6h"
        },
        "description": "Prometheus range vector for JWT mint queries (hidden; fixed at 6h).",
        "hide": 2,
        "label": "Auth rate window",
        "name": "auth_prom_interval",
        "query": "6h",
        "skipUrlSync": true,
        "type": "constant"
      }
    ]
  },
  "time": {
    "from": "now-15m",
    "to": "now"
  },
  "timepicker": {},
  "timezone": "browser",
  "title": "Optimum Gateway - Partner Dashboard (v1.2.0)",
  "uid": "partner-gateway-v1"
}
```

</details>

### Step 6: Start the Stack

```bash
docker compose up -d
docker compose ps
```

You should see both `prometheus` and `grafana` with status `Up`.

### Step 7: Access Grafana

1. Open `http://localhost:3000`
2. Login: `admin` / `admin` (skip password change)
3. Go to **Dashboards** > **Default** > select the partner dashboard

The dashboard auto-selects your Prometheus datasource and discovers gateway(s) via the `gateway_id` label.


## Dashboard Panels

The Partner Dashboard includes the following sections:

### Gateway Info

* **Status** - ON/OFF based on CL + mump2p peer connectivity
* **CL Peers** / **mump2p Peers** - current peer counts
* **Hoodi Slot** - live slot number from Hoodi genesis
* **Hoodi Epoch** - epoch index (32 slots per epoch)
* **Build Info** - version, commit, Go, public IP
* **Subscribed Topics** - number of subscribed topics

### Block Arrival Performance

* **Arrival via mump2p (median)** - median block arrival time via mump2p from slot start
* **Accelerated slots** - percentage of slots where mump2p delivered the block strictly before libp2p
* **Accelerated slots over time** - heatmap of accelerated slot percentage over time
* **mump2p arrival over time (median)** - arrival time trend

### Attestation Performance

* **Accelerated attestations** - percentage of attestations where mump2p delivered first
* **Accelerated attestations over time** - trend of attestation race wins
* **Attestations delivered before 8s deadline** - percentage of mump2p attestations arriving within the 8-second slot deadline


## Prometheus Queries (Quick Reference)

All queries use gateway-local metrics from the `/metrics` endpoint.

| What                               | PromQL                                                                                                                                                |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| CL peers                           | `mump2p_gateway_cl_peers{gateway_id="$gateway"}`                                                                                      |
| mump2p peers                       | `mump2p_gateway_mump2p_peers{gateway_id="$gateway"}`                                                                                  |
| Block arrival p50 via mump2p       | `histogram_quantile(0.50, sum by(le) (rate(mump2p_gateway_block_arrival_mump2p_ms_bucket{gateway_id="$gateway"}[5m])))`               |
| Accelerated slots %                | `rate(mump2p_gateway_blocks_first_seen_mump2p_total[5m]) / (rate(mump2p_gateway_blocks_first_seen_mump2p_total[5m]) + rate(mump2p_gateway_blocks_first_seen_libp2p_total[5m])) * 100`              |


## Stopping the Stack

```bash
docker compose down        # stop, keep data
docker compose down -v     # stop, delete all data
```

## References

* [Metrics Reference](metrics.md) - metric names and types
* [Metrics Methodology](metrics_methodology.md) - why and how we measure
* [Configuration](02_configuration.md) - gateway settings
* [Troubleshooting](04_troubleshoot.md) - fixing dashboard / gateway issues
