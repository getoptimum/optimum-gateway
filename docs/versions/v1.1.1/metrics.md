# Optimum Gateway Metrics

When `telemetry_enable: true`, metrics are exposed at `http://localhost:48123/metrics`.

All gateway metrics carry these global labels: `gateway_id`, `gateway_cluster_id`, and `org_id`. Gateways also inherit the metadata labels set on your API key (for example `gateway_label`, and any Region / Consensus Client / Hosting Provider details you selected when generating the key).

> **Metric prefix:** all gateway metrics are prefixed `mump2p_gateway_`. (Earlier releases used the `optp2p_gateway_optimum_gateway_` prefix — update any saved dashboards or queries.)

## Build & Identity

| Metric                                  | Type  | Labels                                      | Description                                                     |
| --------------------------------------- | ----- | ------------------------------------------- | --------------------------------------------------------------- |
| `mump2p_gateway_app_build_info`         | Gauge | version, commit, go, public_ip, paired_with | Build and identity info (value always 1)                        |
| `mump2p_gateway_known_validators_total` | Gauge | —                                           | Number of validator indices synced from the API key / bootstrap |

## Health

| Metric                                | Type  | Description                                                         |
| ------------------------------------- | ----- | ------------------------------------------------------------------- |
| `mump2p_gateway_cl_health_status`     | Gauge | CL health: 1 = healthy (CL path activity in last 30s), 0 = degraded |
| `mump2p_gateway_mump2p_health_status` | Gauge | mump2p health: 1 = healthy (mesh traffic in last 30s), 0 = degraded |
| `mump2p_gateway_propagation_state`    | Gauge | Propagation to CL: 1 = propagating, 0 = disabled (dynamic config)    |

## Peers

| Metric                                          | Type    | Labels         | Description                                           |
| ----------------------------------------------- | ------- | -------------- | ----------------------------------------------------- |
| `mump2p_gateway_cl_peers`                       | Gauge   | —              | CL peers connected to the gateway                     |
| `mump2p_gateway_mump2p_peers`                   | Gauge   | —              | mump2p peers connected to the gateway                 |
| `mump2p_gateway_cl_peers_per_topic`             | Gauge   | topic          | CL peers connected per topic                          |
| `mump2p_gateway_mump2p_peers_per_topic`         | Gauge   | topic          | mump2p peers connected per topic                      |
| `mump2p_gateway_cl_mesh_peer`                   | Gauge   | topic, peer_id | CL peers in the GossipSub mesh per topic (1 per peer) |
| `mump2p_gateway_mump2p_mesh_peer`               | Gauge   | topic, peer_id | mump2p peers in the mesh per topic (1 per peer)       |
| `mump2p_gateway_cl_peer_connected_total`        | Counter | —              | Cumulative CL peer connections established            |
| `mump2p_gateway_cl_peer_disconnected_total`     | Counter | —              | Cumulative CL peer disconnections                     |
| `mump2p_gateway_mump2p_peer_connected_total`    | Counter | —              | Cumulative mump2p peer connections established        |
| `mump2p_gateway_mump2p_peer_disconnected_total` | Counter | —              | Cumulative mump2p peer disconnections                 |

## Messages

| Metric                                                     | Type      | Labels                    | Description                                                              |
| ---------------------------------------------------------- | --------- | ------------------------- | ------------------------------------------------------------------------ |
| `mump2p_gateway_libp2p_messages_from_upstream_total`       | Counter   | —                         | libp2p beacon blocks received from CL peers                              |
| `mump2p_gateway_mump2p_messages_from_upstream_total`       | Counter   | —                         | mump2p messages received from upstream peers                             |
| `mump2p_gateway_mump2p_messages_from_origin_total`         | Counter   | —                         | mump2p messages received from origin gateways                            |
| `mump2p_gateway_mump2p_published_messages_per_topic_total` | Counter   | topic                     | Messages published to mump2p per topic                                   |
| `mump2p_gateway_libp2p_gossipsub_rpc_messages_total`       | Counter   | topic, peer_id, direction | CL libp2p GossipSub RPC messages (direction: recv\|send)                 |
| `mump2p_gateway_bad_messages_to_cl_total`                  | Counter   | direction                 | Bad/undeliverable messages (direction: cl\|mum)                          |
| `mump2p_gateway_processing_time_nanoseconds`               | Histogram | process                   | Per-message processing duration (process: cl_processing\|opt_processing) |

## Block Arrival & First-Seen

| Metric                                          | Type      | Description                                                      |
| ----------------------------------------------- | --------- | ---------------------------------------------------------------- |
| `mump2p_gateway_block_arrival_mump2p_ms`        | Histogram | Block arrival latency via mump2p relative to slot start (ms)     |
| `mump2p_gateway_block_arrival_libp2p_ms`        | Histogram | Block arrival latency via libp2p relative to slot start (ms)     |
| `mump2p_gateway_blocks_first_seen_mump2p_total` | Counter   | Blocks where mump2p delivered strictly before libp2p             |
| `mump2p_gateway_blocks_first_seen_libp2p_total` | Counter   | Blocks where libp2p delivered before or equal to mump2p          |
| `mump2p_gateway_last_block_received_timestamp`  | Gauge     | Unix timestamp of the last beacon block received from any source |

## Attestations

| Metric                                              | Type      | Labels | Description                                                                           |
| --------------------------------------------------- | --------- | ------ | ------------------------------------------------------------------------------------- |
| `mump2p_gateway_attestation_evaluated_total`        | Counter   | —      | Attestations evaluated by the router (forwarded + dropped)                            |
| `mump2p_gateway_attestation_forwarded_mump2p_total` | Counter   | —      | Attestations forwarded to mump2p after the router filter                              |
| `mump2p_gateway_attestation_dropped_total`          | Counter   | reason | Attestations dropped by the router, by reason                                         |
| `mump2p_gateway_attestation_subnet_messages_total`  | Counter   | subnet | Attestation messages received from CL per subnet index                                |
| `mump2p_gateway_attestation_inclusion_delay_slots`  | Histogram | —      | Slot distance between attestation `data.slot` and the wall-clock slot at receive time |
| `mump2p_gateway_attestation_propagation_latency_ms` | Histogram | —      | End-to-end latency from sender gateway emit to receiver gateway decode (ms)           |

### Attestation Packing

| Metric                                             | Type      | Description                                                        |
| -------------------------------------------------- | --------- | ------------------------------------------------------------------ |
| `mump2p_gateway_attestation_pack_total`            | Counter   | Attestation messages successfully packed                           |
| `mump2p_gateway_attestation_pack_errors_total`     | Counter   | Attestation messages that failed packing                           |
| `mump2p_gateway_attestation_pack_items`            | Histogram | Attestation items per packed blob                                  |
| `mump2p_gateway_attestation_pack_size_bytes`       | Histogram | Size of emitted packed attestation blobs (bytes)                   |
| `mump2p_gateway_attestation_pack_latency_ms`       | Histogram | Time to encode and emit one attestation pack cycle (ms)            |
| `mump2p_gateway_attestation_pack_unique_data_keys` | Histogram | Unique AttestationData hashes per pack cycle (dedup effectiveness) |

## Aggregation

| Metric                                                | Type      | Labels    | Description                                       |
| ----------------------------------------------------- | --------- | --------- | ------------------------------------------------- |
| `mump2p_gateway_aggregation_messages_total`           | Counter   | direction | Messages seen by the aggregation topic            |
| `mump2p_gateway_aggregation_message_size_bytes`       | Histogram | direction | Message sizes across aggregation (in/out/expand)  |
| `mump2p_gateway_aggregation_included_total`           | Counter   | —         | Messages included in emitted aggregated protobufs |
| `mump2p_gateway_aggregation_included_per_topic_total` | Counter   | topic     | Messages included in emitted protobufs per topic  |

## Authentication (API key / JWT)

| Metric                                         | Type    | Labels | Description                                                                                                                            |
| ---------------------------------------------- | ------- | ------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `mump2p_gateway_auth_token_mint_total`         | Counter | result | JWT mint outcomes (result: success, unknown_key, revoked, suspended, forbidden, bad_status, network_error, empty_token, verify_failed) |
| `mump2p_gateway_auth_token_expires_at_seconds` | Gauge   | —      | Unix timestamp at which the current JWT expires (0 if never minted)                                                                    |

## mump2p Trace (debug)

These are populated only when the corresponding trace flags are enabled (`OPT_TRACE_MESH`, `OPT_TRACE_RPC`, `OPT_TRACE_SHARD`). All labels are bounded — per-peer and per-message identifiers are deliberately not used.

| Metric                                              | Type    | Labels        | Description                                                            |
| --------------------------------------------------- | ------- | ------------- | ---------------------------------------------------------------------- |
| `mump2p_gateway_mump2p_trace_messages_total`        | Counter | event, topic  | Message-lifecycle events (publish\|deliver\|duplicate\|reject)         |
| `mump2p_gateway_mump2p_trace_message_rejects_total` | Counter | topic, reason | Rejected messages by validation reason                                 |
| `mump2p_gateway_mump2p_trace_shards_total`          | Counter | kind          | RLNC shard events (new\|duplicate\|unhelpful\|unnecessary)             |
| `mump2p_gateway_mump2p_trace_shard_bytes_total`     | Counter | kind          | RLNC shard coefficient bytes by kind                                   |
| `mump2p_gateway_mump2p_trace_rpc_total`             | Counter | direction     | RPC events (recv\|send\|drop)                                          |
| `mump2p_gateway_mump2p_trace_rpc_bytes_total`       | Counter | direction     | RPC bytes moved (recv\|send)                                           |
| `mump2p_gateway_mump2p_trace_mesh_total`            | Counter | event, topic  | Mesh-topology churn (add_peer\|remove_peer\|join\|leave\|graft\|prune) |

## Process Metrics

Standard Go and process metrics (`go_*`, `process_*`) are exposed for resource monitoring.

## Quick Checks

```sh
curl -s http://localhost:48123/metrics | grep mump2p_gateway
```

With a CL connected: `mump2p_gateway_cl_peers` ≥ 1, `mump2p_gateway_mump2p_peers` ≥ 1, and `/health` reports `subscribed_topics` = 65 (1 block + 64 attestation subnets).
