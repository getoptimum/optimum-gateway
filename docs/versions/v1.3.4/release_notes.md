# v1.3.4

> **Current release.** v1.3.4 is the current release. It adds `telemetry_addr` so gateways on host networking can keep telemetry on loopback ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)). The default bind is unchanged, so host-networking setups also need the action below. CL peering is unchanged.

## Highlights

* **Telemetry stays on the host ([GHSA-gr6w-cqh2-3c5g](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-gr6w-cqh2-3c5g)).** With `--network host` or the Helm chart's `hostNetwork`, the read-only telemetry endpoints on `48123` (`/`, `/health`, `/metrics`, `/api/v1/self_info`) listened on every host interface rather than only loopback. Gateways started with the current Console command publish `48123` on loopback and are not affected. The join-key `docker run` in the v1.3.2 and v1.3.3 self-enrollment docs, and a Console command copied before 29 September 2026, used `--network host`. Optimum never connects to `48123` inbound. Upgrading alone does not change the bind, so if you use host networking, also take the action below.
* **Action: keep `48123` on the host.** Publish it with `-p 127.0.0.1:48123:48123`, or, with `--network host`, set `telemetry_addr: 127.0.0.1`. If a Prometheus container on Linux scraped the bridge gateway (`172.17.0.1:48123`), it can no longer reach a loopback publish: run the gateway with `--network optimum` and scrape `optimum-gateway:48123` instead. A `--network host` gateway on loopback is reachable only from a Prometheus running directly on the host (`localhost:48123`). With the Helm chart, move to chart 0.5.0 and gateway v1.3.4 together. See [Metrics & Grafana](03_telemetry.md#step-3-prometheus-configuration).
* **New `telemetry_addr` option** (`OPT_TELEMETRY_ADDR`). Sets the address the telemetry server (`/health`, `/metrics`, `/api/v1/self_info`) binds. The default is unchanged (all IPv4 interfaces). With `--network host`, set `telemetry_addr: 127.0.0.1`; with the Helm chart, use chart 0.5.0 (`gateway.telemetryAddr`). See [Network Requirements](00_network_requirements.md).
* **Helm chart 0.5.0.** Binds telemetry to node loopback by default, probes it from inside the pod, and makes the org-wide join key the default credential. It needs gateway v1.3.4; older images ignore its loopback bind. See [Kubernetes](05_kubernetes.md).
* **Registration no longer sends `gateway_http_addr`.** Bootstrap did not use it. No partner configuration change.

Upgrading from v1.3.2 or earlier? Also read the [v1.3.3 release notes](../v1.3.3/release_notes.md).

## Upgrade from an earlier release

`docker restart` alone keeps the old image. Recreate the container. The command below is the quick-start layout; keep your own `--network`, volumes and credential env (for example `OPT_JOIN_KEY`), and change only the image tag and the `48123` publish. With `--network host`, `-p` does not apply: add `-e OPT_TELEMETRY_ADDR=127.0.0.1` instead.

```bash
export OPT_API_KEY=ogw_live_xxx   # unchanged if you use API keys
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

Confirm health and mesh:

```bash
curl -s http://localhost:48123/health | jq '.status, .checks'
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
```

## Version Status

| Version     | Status                          |
| ----------- | ------------------------------- |
| v1.3.4      | **CURRENT**                     |
| v1.3.3      | supported / upgrade recommended |
| v1.3.2      | supported / upgrade recommended |
| v1.3.1      | DEPRECATED / unsupported        |
| v1.3.0      | DEPRECATED / unsupported        |
| v1.2.0      | DEPRECATED / unsupported        |
| v1.1.1      | DEPRECATED / unsupported        |
| v1.0.2      | DEPRECATED / unsupported        |
| v0.0.1-rc12 | REMOVED / unsupported           |
| v0.0.1-rc11 | REMOVED / unsupported           |
