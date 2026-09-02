# v1.1.1

> **Recommended upgrade.** v1.1.1 is the current release for all partners on v1.0.2. Networking and CL peering are unchanged - same ports and firewall rules as v1.0.2.

## Highlights

* **Improved remote telemetry push.** When `remote_push_enable: true`, the gateway pushes metrics and logs to Optimum using standard Prometheus remote write. Setup is the same as v1.0.2 (API-key JWT, no separate push credentials) with improved reliability under load.
* **Propagation state metric.** New gauge **`mump2p_gateway_propagation_state`**: `1` = propagating mump2p messages to your CL, `0` = propagation disabled via Optimum dynamic config. Same state as `propagation_enabled` in `/api/v1/self_info`. See [Metrics](metrics.md).
* **Config field names.** Partner YAML now uses **`agent_mump2p_port`** and **`identity_mump2p_dir`** (replacing `agent_opt_p2p_port` / `identity_optp2p_dir`). Defaults and sample configs are updated; see [Configuration](02_configuration.md).
* **Reliability.** Token mint retry with jitter on startup; mump2p publish waits for peer handshake completion.

## Upgrade from v1.0.2

1. Update `app_conf.yml` if you still use the old field names (`agent_opt_p2p_port` -> `agent_mump2p_port`, `identity_optp2p_dir` -> `identity_mump2p_dir`). Mount identity volumes at `/tmp/libp2p` and **`/tmp/mump2p`**.
2. Pull and restart:

   ```bash
   export OPT_API_KEY=ogw_live_xxx
   docker pull getoptimum/gateway:v1.1.1
   docker restart optimum-gateway
   ```

3. Confirm health and mesh:

   ```bash
   curl -s http://localhost:48123/health | jq '.status, .checks'
   curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
   ```

No firewall or CL peering changes are required.

## Version Status

| Version     | Status                    |
| ----------- | ------------------------- |
| v1.1.1      | **CURRENT - recommended** |
| v1.0.2      | DEPRECATED / unsupported  |
| v0.0.1-rc12 | REMOVED / unsupported     |
| v0.0.1-rc11 | REMOVED / unsupported     |
