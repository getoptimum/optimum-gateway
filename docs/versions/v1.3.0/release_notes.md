# v1.3.0

> **Previous release.** Superseded by [v1.3.1](../v1.3.1/release_notes.md). Networking and CL peering are unchanged from v1.3.0 — same ports and firewall rules.

## Highlights

* **Long-running consumer streams.** Consumer block streams are built for always-on connections: in-band liveness signals so you can tell a quiet chain from a stalled feed, guidance on client keepalives through NAT and load balancers, and token refresh without reconnecting. See [Consumer Block Stream](06_block_stream.md).
* **Cleaner block events.** Duplicate same-source blocks from mesh re-encoding are collapsed before they reach your consumers.
* **Slot-prioritized acceleration.** During slots where your validators are scheduled to propose, acceleration is prioritized automatically — no partner action or configuration required. Existing Grafana panels under [Metrics & Grafana](03_telemetry.md) continue to report accelerated slots.
* **Stream-only gateways.** Gateways with `stream_only: true` skip CL health checks; mesh signals alone satisfy `/health`.
* **Stream metrics.** Additional `mump2p_stream_*` series for liveness, re-auth observation, and connection age when the stream is enabled. See [Metrics](metrics.md#consumer-block-stream).
* **Reliability.** Improved stability for gateways that run continuously on the mesh network — fewer unexpected peer disconnects on long uptimes.

## Upgrade from v1.2.0

1. Pull and restart:

   ```bash
   export OPT_API_KEY=ogw_live_xxx
   docker pull getoptimum/gateway:v1.3.0
   docker restart optimum-gateway
   ```

2. Confirm health and mesh:

   ```bash
   curl -s http://localhost:48123/health | jq '.status, .checks'
   curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
   ```

No firewall or CL peering changes are required.

If you run consumer streams, review [Holding a stream open for weeks](06_block_stream.md#holding-a-stream-open-for-weeks) — implement client keepalives and in-band token refresh for production always-on feeds.

## Version Status

| Version     | Status                    |
| ----------- | ------------------------- |
| v1.3.1      | **CURRENT - recommended** |
| v1.3.0      | Previous                  |
| v1.2.0      | Previous                  |
| v1.1.1      | Previous                  |
| v1.0.2      | DEPRECATED / unsupported  |
| v0.0.1-rc12 | REMOVED / unsupported     |
| v0.0.1-rc11 | REMOVED / unsupported     |
