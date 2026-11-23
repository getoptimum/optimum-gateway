# Optimum Gateway - Version History & Changelog

**Latest Release:** [v1.3.5](./versions/v1.3.5/release_notes.md)  
**Latest Docs:** [v1.3.5 Documentation](./versions/v1.3.5/index.md)

## Supported Versions

| Version | Status                          | Docker Image                |
| ------- | ------------------------------- | --------------------------- |
| v1.3.5  | CURRENT                         | `getoptimum/gateway:v1.3.5` |
| v1.3.4  | supported / upgrade recommended | `getoptimum/gateway:v1.3.4` |

## v1.3.5 (Current)

**Docker Image:** `getoptimum/gateway:v1.3.5`

v1.3.5 is the current release. v1.3.2 and v1.3.3 are unsupported because they contained security vulnerabilities. v1.3.4 remains supported; upgrade it for [CVE-2026-78661](https://www.cve.org/CVERecord?id=CVE-2026-78661). No partner configuration change.

### Highlights

* **HTML-parser denial of service ([CVE-2026-78661](https://www.cve.org/CVERecord?id=CVE-2026-78661)).** The image is built with `golang.org/x/net` v0.62.0. v1.3.4 carries v0.60.0. The gateway does not parse HTML; the bump clears the advisory from the image.
* **Oversized consensus requests are rejected before they are allocated.** Each request/response type is capped at its own SSZ size (8 bytes for ping and goodbye, 16 to 25 for metadata, 84 or 92 for status), and an idle inbound exchange is closed after 10 seconds.
* **Consensus clients stop asking the gateway for data columns.** `metadata/3` advertises custody group count 4, and `status/2` sets `earliest_available_slot` to 2^53−1. `status/1` stays an echo. PeerDAS clients that honor the field keep the peer and skip column requests.
* **Ping and metadata both use sequence number 1.** v1.3.4 echoed the requester on ping and advertised sequence number 0. A peer listed in `direct_cl_peers` keeps its address after a disconnect.
* **CL disconnect telemetry.** `mump2p_gateway_cl_peer_disconnected_by_client_total{peer_client}` and `mump2p_gateway_cl_peer_goodbye_total{peer_client,reason}`.

[Full release notes](./versions/v1.3.5/release_notes.md) · [Documentation](./versions/v1.3.5/index.md)

## v1.3.4 (Supported)

**Docker Image:** `getoptimum/gateway:v1.3.4`

v1.3.4 remains supported. Upgrade to v1.3.5 for [CVE-2026-78661](https://www.cve.org/CVERecord?id=CVE-2026-78661). If you run with host networking, also take the `48123` action in the release notes ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)).

### Highlights

* **Telemetry stays on the host ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)).** With `--network host` or the Helm chart's `hostNetwork`, the read-only telemetry endpoints on `48123` listened on every host IPv4 interface. Gateways started with the current Console command are not affected. Gateways run with `--network host` are, for example those started from the join-key `docker run` in the v1.3.2 and v1.3.3 docs or a Console command copied before 29 September 2026. With `--network host`, set the new `telemetry_addr: 127.0.0.1` (`-p` does not apply). With Helm, move to chart 0.5.0 and gateway v1.3.4 together.
* **New `telemetry_addr` option** (`OPT_TELEMETRY_ADDR`) sets the telemetry bind address. The default is unchanged.
* **Helm chart 0.5.0** binds telemetry to node loopback and adds join-key enrollment. It needs gateway v1.3.4; older images ignore its loopback bind.
* **Registration no longer sends the unused `gateway_http_addr`.**
* **Listeners use plain TCP ([CVE-2026-98432](https://www.cve.org/CVERecord?id=CVE-2026-98432)).** Go 1.24 and later accept MPTCP by default, the listener the published exploit uses. v1.3.4 turns that off. No partner configuration change. The complete fix lies on the partner's side, in the host kernel update.

[Full release notes](./versions/v1.3.4/release_notes.md) · [Documentation](./versions/v1.3.4/index.md)

## v1.3.3 (Unsupported)

**v1.3.3 is unsupported.** Docs for this release have been removed. It contained security vulnerabilities. Partners still on v1.3.3 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`).

## v1.3.2 (Unsupported)

**v1.3.2 is unsupported.** Docs for this release have been removed. It contained security vulnerabilities. Partners still on v1.3.2 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`).

## v1.3.1 (Deprecated)

**v1.3.1 is deprecated.** Docs for this release have been removed. Partners still on v1.3.1 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`). v1.3.4 remains supported.

## v1.3.0 (Deprecated)

**v1.3.0 is deprecated.** Docs for this release have been removed. Partners still on v1.3.0 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`). v1.3.4 remains supported.

## v1.2.0 (Deprecated)

**v1.2.0 is deprecated.** Docs for this release have been removed. Partners still on v1.2.0 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`). v1.3.4 remains supported.

## v1.1.1 (Deprecated)

**v1.1.1 is deprecated.** Docs for this release have been removed. Partners still on v1.1.1 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`). v1.3.4 remains supported.

## v1.0.2 (Deprecated)

**v1.0.2 is deprecated.** Docs for this release have been removed. Partners still on v1.0.2 must upgrade to **[v1.3.5](./versions/v1.3.5/release_notes.md)** (`getoptimum/gateway:v1.3.5`). v1.3.4 remains supported.

## Important: Deprecated Versions

**The following versions are deprecated and no longer supported. Upgrade to v1.3.5. v1.3.2 and v1.3.3 are unsupported because they contained security vulnerabilities. v1.3.4 remains supported.**

| Version     | Status     |
| ----------- | ---------- |
| v1.3.3      | DEPRECATED |
| v1.3.2      | DEPRECATED |
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
docker pull getoptimum/gateway:v1.3.5
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 33213:33213/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.3.5 \
  -config=/app/config/app_conf.yml
```

If you run with `--network host`, add it to this command, drop the `-p` flags and add `-e OPT_TELEMETRY_ADDR=127.0.0.1`.

## Support

Contact the Optimum team through your provided support channels.
