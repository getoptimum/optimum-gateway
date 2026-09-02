# v1.2.0

> **Recommended upgrade.** v1.2.0 is the current release for all partners on v1.1.1. Networking and CL peering are unchanged — same ports and firewall rules as v1.1.1.

## Highlights

* **Consumer block stream.** Opt-in, read-only WebSocket and gRPC feed of the beacon blocks the gateway already decodes. Off by default (`stream_enable: false`). Consumers authenticate with their own `osc_` keys from the [Partner Console](https://console.getoptimum.io/), exchanged for a short-lived stream JWT. Live on **Hoodi and Mainnet**. See [Consumer Block Stream](06_block_stream.md).
* **Stream metrics.** When the stream is enabled, the telemetry port exports `mump2p_stream_*` series (open connections, events sent, events dropped on lag, auth failures). See [Metrics](metrics.md#consumer-block-stream).

## Upgrade from v1.1.1

1. Pull and restart:

   ```bash
   export OPT_API_KEY=ogw_live_xxx
   docker pull getoptimum/gateway:v1.2.0
   docker restart optimum-gateway
   ```

2. Confirm health and mesh:

   ```bash
   curl -s http://localhost:48123/health | jq '.status, .checks'
   curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
   ```

No firewall or CL peering changes are required. To enable the consumer stream, add `stream_enable: true` and follow [Consumer Block Stream](06_block_stream.md). Listeners default to loopback (`127.0.0.1:9600` / `127.0.0.1:9601`).

## Version Status

| Version     | Status                    |
| ----------- | ------------------------- |
| v1.2.0      | **CURRENT - recommended** |
| v1.1.1      | Previous                  |
| v1.0.2      | DEPRECATED / unsupported  |
| v0.0.1-rc12 | REMOVED / unsupported     |
| v0.0.1-rc11 | REMOVED / unsupported     |
