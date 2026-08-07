# ADR-0001: Optimum Gateway architecture and message flow

**Status:** Accepted  
**Date:** 2025-12-04  

---

## Context

The Optimum Gateway sits between:

* an Ethereum consensus client (CL) running libp2p gossip, and
* the Optimum mump2p mesh (embedded `pkg/service/mum_p2p` node).

The gateway’s responsibilities are:

* Mirror selected CL gossip topics into mump2p and vice versa.
* Apply topic‑level aggregation for high‑volume topics to reduce bandwidth.
* Provide observability on message volume, size, latency, and peer health.
* Enforce basic safety and compatibility constraints (fork digests, topic
  mapping, AB testing for latency experiments).

This ADR documents the current design and architecture so that later
feature‑specific ADRs (such as ADR‑0002 for beacon block latency) have a
stable reference point.

---

## Decision

We keep a **single gossipsub gateway process** with the following
high‑level structure:

1. **Core service**
   * `pkg/service/gossipsub-gateway.Service` is the main in‑process
     component.
   * Created from `cmd/main.go` and owned for the whole process lifetime.

2. **P2P**
   * **libp2p / CL side**:
     * A libp2p host that subscribes to CL gossip topics via gossipsub.
     * Manages:
       * topic handles (`libP2PTopics`),
       * subscriptions and their contexts (`libP2PSubs`, `libP2PSubsCtx`).
   * **mump2p / Optimum side**:
     * An embedded mump2p node (`nodeMumP2P *mum_p2p.Node` in
       `pkg/service/mum_p2p`).
     * Responsible for publishing messages to, and consuming messages
       from, the mump2p mesh.

3. **Message bridges**
   * **CL → mump2p** (`handleMessagesFromCL`):
     * Receives `entities.CLMessage` from the CL node.
     * Decodes messages according to Prysm’s `GossipTopicMappings`.
     * For beacon blocks:
       * Performs slot‑level validation and telemetry
         (`processBeaconBlockArrival`).
       * Optionally enqueues into the aggregator (for non‑block topics).
     * Publishes to the mump2p node (`nodeMumP2P.PublishMessage`).
   * **mump2p → CL** (`handleMessagesFromMumP2PNode`):
     * Receives `commonEntities.P2PMessage` from the local mump2p node.
     * Skips self messages if configured (`GetSkipMessagesFromSelf`).
     * Decodes using Prysm’s gossip mapping and re‑encodes using the
       same SSZ encoder for libp2p.
     * Publishes to local libp2p topics if subscribed.

4. **Aggregation pipeline**
   * For high‑volume topics, the gateway can aggregate multiple messages
     into a single protobuf container:
     * Implemented in `pkg/service/aggregator`.
     * `Service` accepts individual messages via `Enqueue(topic, data)`.
     * Periodically (every ~25 ms) batches messages per topic into
       `Msg{Tms, Container}`.
     * Uses an `Emitter` interface so the gossipsub gateway can send
       aggregated blobs over a dedicated mump2p topic.
   * On the receive side, `handleAggregatedMessages` unpacks the
     container and replays the individual messages toward the CL.

5. **Telemetry & metrics**
   * The gateway uses `pkg/service/telemetry` to expose:
     * Message counts per direction and topic.
     * Message size distributions.
     * Latency metrics for CL and beacon blocks (see ADR‑0002).
     * Peer and validator health metrics.
   * Telemetry is configurable via `AppConfig.TelemetryEnable` and
     exported on `/metrics` if enabled.

6. **HTTP API**
   * HTTP endpoints are registered in `pkg/routes/base.go` (`initRoutes`) and
     provide:
     * Self peer info, including version, at `/api/v1/self_info`,
     * Health at `/health`,
     * Prometheus metrics (`/metrics`) when enabled.
   * The gateway does not currently expose a consumer-facing gRPC service; a
     read-only streaming API (WebSocket + gRPC) is proposed separately in
     [ADR-0011](./0011-gateway-consumer-block-stream.md).

7. **AB testing**
   * Slot‑level AB testing is supported via `cfg.PropagationEnabled()` (dynamic-config rotator):
     * Even slots can be excluded from certain tracking/reporting to
       compare different configurations.

---

## Rationale

* **Single process with dual P2P frontends**
    * Simplifies cross‑protocol message transformation (SSZ encode/decode,
    topic mapping).
    * Centralizes logging and metrics, reducing the number of moving parts.

* **Aggregator as a separate service**
    * The aggregator has a clear, composable interface (`Emitter`) and can
    be kept mostly independent from gossipsub details.
    * It can be tuned (interval, buffer sizes) without deep changes to the
    message bridge logic.

* **Telemetry as a shared subsystem**
    * A single telemetry package (`pkg/service/telemetry`) is reused
    across the gateway, aggregators, and validator‑like features.
    * This keeps metric naming and labels consistent.

* **Config‑driven behavior**
    * Features such as aggregation, telemetry, and AB testing are toggled
    via `AppConfig`, so deployments can gradually enable more complex
    behavior.

---

## Consequences

### Positive

* Clear separation of responsibilities:
    * libp2p ↔ mump2p bridging,
    * Aggregation,
    * Telemetry and metrics,
    * GRPC / HTTP APIs.
* Easy to extend with additional topic‑specific behavior:
    * Example: beacon block latency (ADR‑0002),
    * Example: future attestation‑ or blob‑specific logic.
* Operational simplicity: a single binary manages both CL and mump2p
  connectivity for a gateway.

### Negative / Trade‑offs

* The gateway process is a critical path for both CL and mump2p; any bug
  can affect both directions.
* Tight coupling with specific CL implementations (via Prysm topic
  mappings) may require updates when CL versions change.
* Shared telemetry subsystem means namespace and label decisions are
  “global”; mistakes are harder to undo once exported metrics are in use.

---

## Notes and references

* Main service entrypoint: `cmd/main.go`.
* Gossipsub gateway service: `pkg/service/gossipsub-gateway/service.go`.
* Message bridges and topic mapping:
    * `pkg/service/gossipsub-gateway/messages_proxy.go`
    * `pkg/service/gossipsub-gateway/messages_proxy_aggregated.go`
    * `pkg/service/gossipsub-gateway/subscribe_nodes.go`
* Aggregator: `pkg/service/aggregator/aggregator.go`.
* Telemetry: `pkg/service/telemetry/*`.
* Beacon block latency design: [ADR-0002](./0002-beacon-block-latency.md).

