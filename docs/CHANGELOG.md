# Optimum Gateway - Version History & Changelog

**Latest Release:** [v1.2.0](./versions/v1.2.0/release_notes.md)  
**Latest Docs:** [v1.2.0 Documentation](./versions/v1.2.0/index.md)

## Supported Versions

| Version | Status                | Docker Image                |
| ------- | --------------------- | --------------------------- |
| v1.2.0  | CURRENT — recommended | `getoptimum/gateway:v1.2.0` |
| v1.1.1  | Previous — supported  | `getoptimum/gateway:v1.1.1` |

## v1.2.0 (Current)

**Docker Image:** `getoptimum/gateway:v1.2.0`

Recommended upgrade for everyone on v1.1.1. Networking and CL peering are unchanged — same ports and firewall rules.

### Highlights

* **Consumer block stream.** Opt-in WebSocket + gRPC feed of decoded beacon blocks (`stream_enable: true`). Consumers use `osc_` keys from the [Partner Console](https://console.getoptimum.io/) (Hoodi and Mainnet). See [Consumer Block Stream](./versions/v1.2.0/06_block_stream.md).
* **Stream metrics.** `mump2p_stream_*` series on the telemetry port when the stream is enabled. See [Metrics](./versions/v1.2.0/metrics.md#consumer-block-stream).

[Full release notes](./versions/v1.2.0/release_notes.md) · [Documentation](./versions/v1.2.0/index.md)

## v1.1.1

**Docker Image:** `getoptimum/gateway:v1.1.1`

Recommended upgrade for everyone on v1.0.2. Networking and CL peering are unchanged — same ports and firewall rules.

### Highlights

* **Remote telemetry push.** More reliable Prometheus remote-write of metrics and logs under load when `remote_push_enable: true` (same API-key JWT as v1.0.2, no separate push credentials).
* **Propagation-state metric.** New gauge `mump2p_gateway_propagation_state`: `1` = propagating mump2p messages to your CL, `0` = disabled via Optimum dynamic config. Mirrors `propagation_enabled` in `/api/v1/self_info`.
* **Config field renames.** Partner YAML now uses `agent_mump2p_port` and `identity_mump2p_dir` (replacing `agent_opt_p2p_port` / `identity_optp2p_dir`). Mount the identity volume at `/tmp/mump2p`.
* **Reliability.** Token-mint retry with jitter on startup; mump2p publish waits for peer-handshake completion.

[Full release notes](./versions/v1.1.1/release_notes.md) · [Documentation](./versions/v1.1.1/index.md)

## v1.0.2 (Deprecated)

**v1.0.2 is deprecated.** Docs for this release have been removed. Partners still on v1.0.2 should upgrade to **[v1.2.0](./versions/v1.2.0/release_notes.md)** (`getoptimum/gateway:v1.2.0`). v1.1.1 remains supported as a previous release.

## Important: Deprecated Versions

**The following versions are deprecated and no longer supported. Upgrade to v1.2.0.**

| Version     | Status     |
| ----------- | ---------- |
| v1.0.2      | DEPRECATED |
| v0.0.1-rc1  | DEPRECATED |
| v0.0.1-rc2  | DEPRECATED |
| v0.0.1-rc3  | DEPRECATED |
| v0.0.1-rc4  | DEPRECATED |
| v0.0.1-rc5  | DEPRECATED |
| v0.0.1-rc6  | DEPRECATED |
| v0.0.1-rc7  | DEPRECATED |
| v0.0.1-rc8  | DEPRECATED |
| v0.0.1-rc9  | DEPRECATED |
| v0.0.1-rc10 | DEPRECATED |
| v0.0.1-rc11 | DEPRECATED |
| v0.0.1-rc12 | DEPRECATED |

### Required Action

Move to the current release. `docker restart` alone keeps the old image, so
recreate the container:

```bash
export OPT_API_KEY=ogw_live_xxx
docker pull getoptimum/gateway:v1.2.0
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.2.0 \
  -config=/app/config/app_conf.yml
```

## Support

Contact the Optimum team through your provided support channels.
