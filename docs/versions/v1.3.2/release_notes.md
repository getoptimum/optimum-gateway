# v1.3.2

> **Required upgrade.** v1.3.2 is the current release. Partners on v1.3.1 or v1.3.0 must upgrade. Networking and CL peering are unchanged — same ports and firewall rules.

## Highlights

* **Improved beacon-block acceleration.** Partner gateways now accelerate beacon blocks on every slot. No partner configuration change.
* **Gateway self-enrollment.** Fleet operators can mint one org-wide **join key** (`ojk_`) and configure gateways with `OPT_JOIN_KEY` instead of distributing one `ogw_` API key per host. Each gateway enrolls once on first boot and mints JWTs with a local keypair thereafter. See [Gateway Self-Enrollment](07_gateway_self_enrollment.md).
* **Legacy API keys unchanged.** The `ogw_` path in [Quick Start](01_quick_start.md) is still the default for single-gateway deployments.

Everything in v1.3.1 — gateway self-enrollment, long-running consumer streams, stream metrics — remains in v1.3.2.

## Upgrade from v1.3.1 or v1.3.0

`docker restart` alone keeps the old image. Recreate the container:

```bash
export OPT_API_KEY=ogw_live_xxx   # unchanged if you use API keys
docker pull getoptimum/gateway:v1.3.2
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.3.2 \
  -config=/app/config/app_conf.yml
```

Confirm health and mesh:

```bash
curl -s http://localhost:48123/health | jq '.status, .checks'
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
```

No firewall or CL peering changes are required. Join-key enrollment is **opt-in** — you only change credentials if you adopt the new fleet path.

If you run consumer streams, treat [Holding a stream open for weeks](06_block_stream.md#holding-a-stream-open-for-weeks) as the complete checklist for production always-on feeds. It covers client keepalives and in-band token refresh, and also reconnecting after `GOAWAY`, retrying `Internal` as well as `Unavailable`, and accounting for permanent gaps.

## Version Status

| Version     | Status                    |
| ----------- | ------------------------- |
| v1.3.2      | **CURRENT - required**    |
| v1.2.0      | Previous                  |
| v1.1.1      | Previous                  |
| v1.3.1      | DEPRECATED / unsupported  |
| v1.3.0      | DEPRECATED / unsupported  |
| v1.0.2      | DEPRECATED / unsupported  |
| v0.0.1-rc12 | REMOVED / unsupported     |
| v0.0.1-rc11 | REMOVED / unsupported     |
