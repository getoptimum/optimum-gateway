# Optimum Gateway - Version History & Changelog

**Latest Release:** [v1.3.4](./versions/v1.3.4/release_notes.md)  
**Latest Docs:** [v1.3.4 Documentation](./versions/v1.3.4/index.md)

## Supported Versions

| Version | Status                          | Docker Image                |
| ------- | ------------------------------- | --------------------------- |
| v1.3.4  | CURRENT                         | `getoptimum/gateway:v1.3.4` |
| v1.3.3  | supported / upgrade recommended | `getoptimum/gateway:v1.3.3` |
| v1.3.2  | supported / upgrade recommended | `getoptimum/gateway:v1.3.2` |

## v1.3.4 (Current)

**Docker Image:** `getoptimum/gateway:v1.3.4`

v1.3.4 is the current release. v1.3.3 and v1.3.2 remain supported; if you run with host networking, upgrade and take the `48123` action in the release notes ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)). v1.3.2 is also affected by [GHSA-rcrq-vcj2-m787](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-rcrq-vcj2-m787). CL peering is unchanged.

### Highlights

* **Telemetry stays on the host ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)).** With `--network host` or the Helm chart's `hostNetwork`, the read-only telemetry endpoints on `48123` listened on every host interface. Gateways started with the current Console command are not affected; the v1.3.2 and v1.3.3 self-enrollment docs and a Console command copied before 29 September 2026 used `--network host`. Publish `-p 127.0.0.1:48123:48123`, or set the new `telemetry_addr: 127.0.0.1` with host networking.
* **New `telemetry_addr` option** (`OPT_TELEMETRY_ADDR`) sets the telemetry bind address. The default is unchanged.
* **Helm chart 0.5.0** binds telemetry to node loopback and makes the join key the default credential. It needs gateway v1.3.4; older images ignore its loopback bind.
* **Registration no longer sends the unused `gateway_http_addr`.**

[Full release notes](./versions/v1.3.4/release_notes.md) · [Documentation](./versions/v1.3.4/index.md)

## v1.3.3 (Supported)

**Docker Image:** `getoptimum/gateway:v1.3.3`

v1.3.3 remains supported; if you run with host networking, upgrade and set `telemetry_addr: 127.0.0.1`, or with Helm move to chart 0.5.0 and gateway v1.3.4 together ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)). Networking and CL peering are unchanged: same ports and firewall rules.

### Highlights

* **Certificate-trust fix (medium severity).** The gateway is built with Go 1.27.3, which stops trusting a set of compromised certificate-authority intermediates (CVE-2026-111943). HTTPS calls from the gateway reject those chains. No partner configuration change.
* **Traffic stays on the current fork until your CL uses the next one.** An early subscription to the next fork no longer moves attestations onto that topic. The gateway follows once a message arrives on it.
* **Turning propagation off stops attestations too.** Attestations from mump2p are no longer forwarded to your CL when propagation is disabled, the same as blocks.
* **Health works with telemetry off.** `GET /health` answers when the metrics exporter is disabled, and `mump2p_gateway_last_block_received_timestamp` is set when a block arrives.
* **Large beacon blocks are accepted.** The CL connection accepts a block up to the compressed size the gateway can decode.
* **Memory-exhaustion fix for aggregated attestations (medium severity, [GHSA-rcrq-vcj2-m787](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-rcrq-vcj2-m787)).** A pack with any subnet outside 0–63 is rejected whole. v1.3.2 and earlier are affected.
* **The access token refreshes about every 15 minutes.** A change to gateway type or validator set shows up well before the six-hour token expires.

[Full release notes](./versions/v1.3.3/release_notes.md) · [Documentation](./versions/v1.3.3/index.md)

## v1.3.2 (Supported)

**Docker Image:** `getoptimum/gateway:v1.3.2`

v1.3.2 remains supported, but upgrading is recommended because it is affected by [GHSA-rcrq-vcj2-m787](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-rcrq-vcj2-m787). If you run with host networking, upgrade and set `telemetry_addr: 127.0.0.1`, or with Helm move to chart 0.5.0 and gateway v1.3.4 together ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)). Networking and CL peering are unchanged: same ports and firewall rules.

### Highlights

* **Beacon blocks propagated on every slot.** Partner gateways now propagate beacon blocks over mump2p on every slot. On v1.3.0 and v1.3.1 some slots were skipped. No partner configuration change.
* **Gateway self-enrollment.** Fleet operators can use one org-wide join key (`ojk_`) instead of one API key per host. See [Gateway Self-Enrollment](./versions/v1.3.2/07_gateway_self_enrollment.md).
* **Legacy API keys unchanged.** The `ogw_` quick-start path is still the default for single-gateway deployments.

[Full release notes](./versions/v1.3.2/release_notes.md) · [Documentation](./versions/v1.3.2/index.md)

## v1.3.1 (Deprecated)

**v1.3.1 is deprecated.** Docs for this release have been removed. Partners still on v1.3.1 must upgrade to **[v1.3.4](./versions/v1.3.4/release_notes.md)** (`getoptimum/gateway:v1.3.4`). v1.3.3 and v1.3.2 remain supported.

## v1.3.0 (Deprecated)

**v1.3.0 is deprecated.** Docs for this release have been removed. Partners still on v1.3.0 must upgrade to **[v1.3.4](./versions/v1.3.4/release_notes.md)** (`getoptimum/gateway:v1.3.4`). v1.3.3 and v1.3.2 remain supported.

## v1.2.0 (Deprecated)

**v1.2.0 is deprecated.** Docs for this release have been removed. Partners still on v1.2.0 must upgrade to **[v1.3.4](./versions/v1.3.4/release_notes.md)** (`getoptimum/gateway:v1.3.4`). v1.3.3 and v1.3.2 remain supported.

## v1.1.1 (Deprecated)

**v1.1.1 is deprecated.** Docs for this release have been removed. Partners still on v1.1.1 must upgrade to **[v1.3.4](./versions/v1.3.4/release_notes.md)** (`getoptimum/gateway:v1.3.4`). v1.3.3 and v1.3.2 remain supported.

## v1.0.2 (Deprecated)

**v1.0.2 is deprecated.** Docs for this release have been removed. Partners still on v1.0.2 must upgrade to **[v1.3.4](./versions/v1.3.4/release_notes.md)** (`getoptimum/gateway:v1.3.4`). v1.3.3 and v1.3.2 remain supported.

## Important: Deprecated Versions

**The following versions are deprecated and no longer supported. Upgrade to v1.3.4. v1.3.3 and v1.3.2 remain supported.**

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
docker pull getoptimum/gateway:v1.3.4
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 33213:33213/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.3.4 \
  -config=/app/config/app_conf.yml
```

## Support

Contact the Optimum team through your provided support channels.
