# ADR-0006: Gateway Health Check Endpoint

**Status:** Accepted 
**Date:** 2026-03-18


## Context

Machine-level alerts (CPU, memory, disk) do not catch application-specific failures that directly impact data quality. We have encountered several incidents that were only detected via manual dashboard inspection or ad-hoc API queries:

| Incident                                       | Root Cause                                                | Detection Method            |
| ---------------------------------------------- | --------------------------------------------------------- | --------------------------- |
| Gateway not receiving blocks via libp2p        | CL client OOM — lost gossip peers on restart              | Manual dashboard inspection |
| `region-example-1` missing all libp2p data | CL peer not connected (`t_eth_seen_ms = 0`)               | Manual API query            |
| Gateway connected but CL not forwarding gossip | CL-side gossip stall — CL peers present but no blocks arriving | Log analysis            |

The gateway currently exposes `GET /metrics` (Prometheus) and `GET /api/v1/self_info` (peer counts, topics). There is no unified health endpoint and no alerting on gateway application state.

The existing peer/topic functions in the gateway service already provide the signals we need:

```go
// pkg/service/gossipsub-gateway/bg_stat.go
func (s *Service) GetLibP2PPeers() (totalPeers int, perTopicPeers map[string]int, ...)
func (s *Service) GetMumP2PPeers() (totalPeers int, perTopicPeers map[string]int, ...)

// pkg/service/gossipsub-gateway/service.go
s.libP2PTopics  *commonSyncx.RWMap[string, *libp2ppubsub.Topic]
```

What is missing is a `lastBlockReceivedAt` timestamp to detect "CL peers present but no blocks arriving" - the silent-CL scenario.


## Decision

Add a `GET /health` endpoint on the gateway's telemetry port (48123) that returns `200 OK` when healthy and `503 Service Unavailable` when degraded.

### New Application State

Store a single `lastBlockReceivedAt` as `atomic.Int64` (Unix millisecond timestamp), updated on every block received from any source (ethp2p or mump2p).

### Health Check Logic

| #   | Check                | Source                            | ok    | fail   |
| --- | -------------------- | --------------------------------- | ----- | ------ |
| 1   | `cl_peers`           | `GetLibP2PPeers()` total          | >= 1  | 0      |
| 2   | `mump2p_peers`       | `GetMumP2PPeers()` total          | >= 1  | 0      |
| 3   | `subscribed_topics`  | `len(s.libP2PTopics.Keys())`      | >= 1  | 0      |
| 4   | `last_block_age_sec` | `time.Since(lastBlockReceivedAt)` | < 60s | >= 60s |

Status roll-up: `healthy` if all checks pass; `degraded` if any check fails.

> **Note on `cl_silent`:** An earlier draft of this ADR proposed a 5th composite check (`cl_silent = cl_peers >= 1 AND last_block_age_sec >= 60s`) to catch the CL silent-failure case. It was **dropped as redundant** — when blocks stop arriving for any reason (including silent-CL), `last_block_age_sec` alone already fails and marks the gateway degraded. The composite check added no new alerting signal.

### Response Format

```json
{
  "status": "healthy",
  "gateway_id": "optimum-hoodi-gateway-eu-frankfurt-example-1-prod",
  "uptime_seconds": 86400,
  "checks": {
    "cl_peers":           { "status": "ok", "value": 47 },
    "mump2p_peers":       { "status": "ok", "value": 12 },
    "subscribed_topics":  { "status": "ok", "value": 6 },
    "last_block_age_sec": { "status": "ok", "value": 8 }
  }
}
```

```json
{
  "status": "degraded",
  "gateway_id": "optimum-hoodi-gateway-eu-west-example-1-prod",
  "uptime_seconds": 3600,
  "checks": {
    "cl_peers":           { "status": "fail", "value": 0 },
    "mump2p_peers":       { "status": "ok",   "value": 12 },
    "subscribed_topics":  { "status": "ok",   "value": 6 },
    "last_block_age_sec": { "status": "ok",   "value": 14 }
  },
  "failing": ["cl_peers"]
}
```

### New Prometheus Metrics

| Metric                                                         | Type  | Description                                                                                                          |
| -------------------------------------------------------------- | ----- | -------------------------------------------------------------------------------------------------------------------- |
| `mump2p_gateway_last_block_received_timestamp` | Gauge | Unix timestamp of last block received (any source). `last_block_age_sec` derived at query time via `time() - value`. |

### 2.5 Registration in Router

```go
// pkg/routes/base.go — inside initRoutes()
s.httpEngine.Get("/health", s.handleHealth)
```

The handler calls `GetLibP2PPeers()`, `GetMumP2PPeers()`, reads `libP2PTopics.Keys()`, and computes `time.Since(lastBlockReceivedAt)`. No external dependencies.


## Consequences

* One new `atomic.Int64` field on the `Service` struct — negligible overhead
* One new HTTP route - no middleware or external dependency
* All data sources (`GetLibP2PPeers`, `GetMumP2PPeers`, `libP2PTopics`) already exist; only `lastBlockReceivedAt` is new
* `last_block_age_sec` alone catches the silent-CL incident class (CL gossip stalled while peers connected) that machine-level alerts miss — no composite check required
