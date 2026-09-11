# v1.3.1

> **Recommended upgrade.** v1.3.1 is the current release. Networking and CL peering are unchanged from v1.3.0 — same ports and firewall rules.

## Highlights

* **Gateway self-enrollment.** Fleet operators can mint one org-wide **join key** (`ojk_`) and configure gateways with `OPT_JOIN_KEY` instead of distributing one `ogw_` API key per host. Each gateway enrolls once on first boot and mints JWTs with a local keypair thereafter. See [Gateway Self-Enrollment](07_gateway_self_enrollment.md).
* **Legacy API keys unchanged.** The `ogw_` path in [Quick Start](01_quick_start.md) is still the default for single-gateway deployments.

Everything in [v1.3.0](https://github.com/getoptimum/optimum-gateway/releases/tag/v1.3.0) — long-running consumer streams, slot-prioritized acceleration, stream metrics — remains in v1.3.1.

## Upgrade from v1.3.0

1. Pull and restart:

   ```bash
   export OPT_API_KEY=ogw_live_xxx   # unchanged if you use API keys
   docker pull getoptimum/gateway:v1.3.1
   docker restart optimum-gateway
   ```

2. Confirm health and mesh:

   ```bash
   curl -s http://localhost:48123/health | jq '.status, .checks'
   curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
   ```

No firewall or CL peering changes are required. Join-key enrollment is **opt-in** — you only change credentials if you adopt the new fleet path.

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
