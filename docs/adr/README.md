# Architecture Decision Records

This directory holds Architecture Decision Records (ADRs) for Optimum Gateway.
An ADR captures a single significant architectural decision, the context that
forced it, the options weighed, and the consequences we accept.

Format is loosely [MADR](https://adr.github.io/madr/). One file per decision,
numbered and immutable once `Accepted` — supersede rather than rewrite.

| ADR                                                    | Title                                                          | Status                            | Date       |
| ------------------------------------------------------ | -------------------------------------------------------------- | --------------------------------- | ---------- |
| [0001](./0001-gateway-architecture.md)                 | Optimum Gateway architecture and message flow                  | Accepted                          | 2025-12-04 |
| [0002](./0002-beacon-block-latency.md)                 | Beacon block latency and mump2p propagation                    | Accepted                          | 2025-12-04 |
| [0003](./0003-validator-metrics.md)                    | Redesign gateway metrics around validator outcomes             | Accepted                          | 2026-01-07 |
| [0004](./0004-hop-by-hop-latency-tracking.md)          | Hop-by-hop latency tracking for mump2p routing                 | Accepted                          | 2026-02-09 |
| [0005](./0005-block-ingestion-tracing-and-analysis.md) | Block ingestion tracing and source impact analysis             | Approved                          | 2026-02-17 |
| [0006](./0006-gateway-health-check.md)                 | Gateway health check endpoint                                  | Accepted                          | 2026-03-18 |
| [0007](./0007-slot-based-block-arrival-tracking.md)    | Slot-based block arrival tracking with libp2p peer attribution | Accepted                          | 2026-04-14 |
| [0008](./0008-attestation-subnet-boost.md)             | Attestation subnet boost via validator-scoped filtering        | Accepted                          | 2026-04-17 |
| [0009](./0009-slot-aware-attestation-gate.md)          | Slot-aware attestation aggregation gate                        | Accepted                          | 2026-04-27 |
| [0010](./0010-attestation-synchronization.md)          | Deterministic attestation synchronization for partner clusters | Approved (implementation pending) | 2026-06-10 |

> **Note:** ADRs 0001–0010 were migrated from the pre-open-source gateway repository and lightly edited for public release (path references updated to `pkg/…`, cross-links renumbered, and internal/operational details generalized). They record the design history; where a decision was later revised, the change is noted in-document rather than by rewriting history.
