# Optimum Gateway - Version History & Changelog

**Latest Release:** [v1.0.2](./versions/v1.0.2/release_notes.md)  
**Latest Docs:** [v1.0.2 Documentation](./versions/v1.0.2/index.md)

## Important: Deprecated Versions

**The following versions are deprecated and no longer supported:**

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

**All users on RC10 or earlier must upgrade to RC11 or RC12.**

```bash
docker pull getoptimum/gateway:v0.0.1-rc12
docker restart optimum-gateway
```

## v0.0.1-rc12 (Deprecated)

**Docker Image:** `getoptimum/gateway:v0.0.1-rc12`

### Highlights

**Attestation subnet support** – Subscribes to all 64 attestation subnets, aggregates and propagates via mump2p.  
**Health endpoint** – `GET /health` returns structured health checks with 200/503 for load balancer integration.  
**Attestation performance metrics** – New histograms for arrival timing, first-seen race, and propagation latency.  
**Gateway pairing mode** – `paired_with` field controls inbound block re-forwarding to the local CL.

Full Release Notes (release notes not published) · Documentation (not published)

## v0.0.1-rc11 (Deprecated)

**Docker Image:** `getoptimum/gateway:v0.0.1-rc11`

### Highlights

**Bootstrap-driven peer discovery** – No proxy hosts. Gateway uses Bootstrap for peers and fork digest.  
**Simplified topic config** – Short topic names (e.g. `beacon_block`); fork digest from Bootstrap.  
**Stricter validation** – Messages from unsupported forks rejected early.  
**Config migration required** – Remove `proxy_host`, use new structure.

Full Release Notes (release notes not published) · Documentation (not published)

## Support

Contact the Optimum team through your provided support channels.
