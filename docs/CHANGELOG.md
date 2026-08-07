# Optimum Gateway - Version History & Changelog

**Latest Release:** [v1.1.1](./versions/v1.1.1/release_notes.md)  
**Latest Docs:** [v1.1.1 Documentation](./versions/v1.1.1/index.md)

## Supported Versions

| Version | Status                | Docker Image                |
| ------- | --------------------- | --------------------------- |
| v1.1.1  | CURRENT — recommended | `getoptimum/gateway:v1.1.1` |
| v1.0.2  | Previous — supported  | `getoptimum/gateway:v1.0.2` |

## v1.1.1 (Current)

**Docker Image:** `getoptimum/gateway:v1.1.1`

Recommended upgrade for everyone on v1.0.2. Networking and CL peering are unchanged — same ports and firewall rules.

### Highlights

* **Remote telemetry push.** More reliable Prometheus remote-write of metrics and logs under load when `remote_push_enable: true` (same API-key JWT as v1.0.2, no separate push credentials).
* **Propagation-state metric.** New gauge `mump2p_gateway_propagation_state`: `1` = propagating mump2p messages to your CL, `0` = disabled via Optimum dynamic config. Mirrors `propagation_enabled` in `/api/v1/self_info`.
* **Config field renames.** Partner YAML now uses `agent_mump2p_port` and `identity_mump2p_dir` (replacing `agent_opt_p2p_port` / `identity_optp2p_dir`). Mount the identity volume at `/tmp/mump2p`.
* **Reliability.** Token-mint retry with jitter on startup; mump2p publish waits for peer-handshake completion.

[Full release notes](./versions/v1.1.1/release_notes.md) · [Documentation](./versions/v1.1.1/index.md)

## v1.0.2

**Docker Image:** `getoptimum/gateway:v1.0.2`

Required upgrade that replaces all earlier releases.

### Highlights

* **API-key authentication.** Each gateway authenticates with an `ogw_live_...` key set via the `OPT_API_KEY` environment variable; the key drives `gateway_id`, `chain`, and validator scope (no per-network YAML).
* **More consensus clients.** Adds Nimbus and Lodestar alongside Prysm, Lighthouse, and Teku.
* **Lighthouse / PeerDAS compatibility.** Advertises a custody group count of 8 in libp2p metadata so PeerDAS-aware clients keep the gateway as a peer.
* **Health endpoints.** Structured `GET /health` (200/503 with `cl_peers`, `mump2p_peers`, `subscribed_topics`, `last_block_age_sec`, `cl_health`, `mump2p_health`) plus a lightweight `GET /` liveness probe.
* **Attestation subnet carry + metrics.** Subscribes to all 64 subnets and forwards partner-validator attestations over mump2p, with inclusion and propagation metrics.
* **Metric namespace.** Gateway metrics are now prefixed `mump2p_gateway_` (previously `optp2p_gateway_optimum_gateway_`) — update saved Prometheus/Grafana queries.
* **Simpler config + security hardening.** Removed `enable_aggregation`, the baked-in topic list, the sidecar port, and separate push credentials; bounded JWT lifetime and more frequent JWKS refresh.

[Full release notes](./versions/v1.0.2/release_notes.md) · [Documentation](./versions/v1.0.2/index.md)

## Important: Deprecated Versions

**The following versions are deprecated and no longer supported. Upgrade to v1.1.1.**

| Version     | Status     |
| ----------- | ---------- |
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
docker pull getoptimum/gateway:v1.1.1
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.1.1 \
  -config=/app/config/app_conf.yml
```

## Support

Contact the Optimum team through your provided support channels.
