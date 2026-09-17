# Optimum Gateway - Version History & Changelog

**Latest Release:** [v1.3.2](./versions/v1.3.2/release_notes.md)  
**Latest Docs:** [v1.3.2 Documentation](./versions/v1.3.2/index.md)

## Supported Versions

| Version | Status             | Docker Image                |
| ------- | ------------------ | --------------------------- |
| v1.3.2  | CURRENT — required | `getoptimum/gateway:v1.3.2` |

## v1.3.2 (Current)

**Docker Image:** `getoptimum/gateway:v1.3.2`

Required upgrade for everyone not on v1.3.2. Networking and CL peering are unchanged — same ports and firewall rules.

### Highlights

* **Improved beacon-block acceleration.** Partner gateways now accelerate beacon blocks on every slot. No partner configuration change.
* **Gateway self-enrollment.** Fleet operators can use one org-wide join key (`ojk_`) instead of one API key per host. See [Gateway Self-Enrollment](./versions/v1.3.2/07_gateway_self_enrollment.md).
* **Legacy API keys unchanged.** The `ogw_` quick-start path is still the default for single-gateway deployments.

[Full release notes](./versions/v1.3.2/release_notes.md) · [Documentation](./versions/v1.3.2/index.md)

## v1.3.1 (Deprecated)

**v1.3.1 is deprecated.** Docs for this release have been removed. Partners still on v1.3.1 must upgrade to **[v1.3.2](./versions/v1.3.2/release_notes.md)** (`getoptimum/gateway:v1.3.2`).

## v1.3.0 (Deprecated)

**v1.3.0 is deprecated.** Docs for this release have been removed. Partners still on v1.3.0 must upgrade to **[v1.3.2](./versions/v1.3.2/release_notes.md)** (`getoptimum/gateway:v1.3.2`).

## v1.2.0 (Deprecated)

**v1.2.0 is deprecated.** Docs for this release have been removed. Partners still on v1.2.0 must upgrade to **[v1.3.2](./versions/v1.3.2/release_notes.md)** (`getoptimum/gateway:v1.3.2`).

## v1.1.1 (Deprecated)

**v1.1.1 is deprecated.** Docs for this release have been removed. Partners still on v1.1.1 must upgrade to **[v1.3.2](./versions/v1.3.2/release_notes.md)** (`getoptimum/gateway:v1.3.2`).

## v1.0.2 (Deprecated)

**v1.0.2 is deprecated.** Docs for this release have been removed. Partners still on v1.0.2 must upgrade to **[v1.3.2](./versions/v1.3.2/release_notes.md)** (`getoptimum/gateway:v1.3.2`).

## Important: Deprecated Versions

**The following versions are deprecated and no longer supported. Upgrade to v1.3.2.**

| Version     | Status     |
| ----------- | ---------- |
| v1.3.1      | DEPRECATED |
| v1.3.0      | DEPRECATED |
| v1.2.0      | DEPRECATED |
| v1.1.1      | DEPRECATED |
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

## Support

Contact the Optimum team through your provided support channels.
