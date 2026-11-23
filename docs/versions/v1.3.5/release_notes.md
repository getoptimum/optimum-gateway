# v1.3.5

> **Current release.** v1.3.5 is the current release. v1.3.2 and v1.3.3 are unsupported because they contained security vulnerabilities. v1.3.4 remains supported; upgrade it for [CVE-2026-78661](https://www.cve.org/CVERecord?id=CVE-2026-78661). No partner configuration change.

## Highlights

* **HTML-parser denial of service ([CVE-2026-78661](https://www.cve.org/CVERecord?id=CVE-2026-78661)).** The gateway is built with `golang.org/x/net` v0.62.0. v1.3.4 is built with v0.60.0, which can be made to spend unbounded time on crafted HTML. The gateway does not parse HTML itself; the bump clears the advisory from the image. No partner configuration change.
* **Oversized consensus requests are rejected before they are allocated.** A peer could declare a length larger than the message type on a request/response exchange and make the gateway allocate up to the gossip cap. Each type is now capped at its own SSZ size: 8 bytes for ping and goodbye, 16 to 25 for metadata, 84 or 92 for status. An idle inbound exchange is closed after 10 seconds, the spec response timeout. No partner configuration change.
* **Consensus clients stop asking the gateway for data columns.** The gateway serves no blocks or column sidecars. v1.3.4 answered `metadata/3` with custody group count 8 and echoed the peer's `earliest_available_slot` on `status/2`. Beacon nodes then sent column requests that always fail, which can stall sync when the gateway is a trusted peer. v1.3.5 answers `metadata/3` with custody group count 4, the Fulu `CUSTODY_REQUIREMENT`, and sets `earliest_available_slot` on `status/2` to 2^53−1, past any real slot. `status/1` has no such field and stays an echo. PeerDAS clients that honor the field keep the peer and skip column requests. No partner configuration change. See [Connecting your CL client](08_cl_clients.md).
* **Ping reports the gateway's own metadata sequence.** v1.3.4 answered a ping with the requester's sequence number, while its metadata sequence number was 0, so a peer could not tell that the metadata had changed. v1.3.5 sends sequence number 1 on the metadata response and on the ping reply. No partner configuration change.
* **A configured CL peer can reconnect after a disconnect.** v1.3.4 deleted that peer's saved address, so a later dial by peer ID had nowhere to go. v1.3.5 keeps the address for peers in the configured CL allowlist. No partner configuration change.

### Telemetry changes

* `mump2p_gateway_cl_peer_disconnected_by_client_total{peer_client}` counts CL disconnects by client.
* `mump2p_gateway_cl_peer_goodbye_total{peer_client,reason}` counts goodbye messages by client and reason. `reason` is the goodbye code as a decimal number, or `other` when the code is not one the gateway recognizes. `peer_client` is the agent's first segment (`lighthouse`, `prysm`, `teku`, `nimbus`, `lodestar`, `grandine`, `erigon`, `hermes`), `other` for any other identified agent, or `unknown` when the agent string is missing. `mump2p_gateway_cl_peer_disconnected_total` stays unlabeled and still pairs with `mump2p_gateway_cl_peer_connected_total`. Peers the allowlist rejects are closed and are not counted.

A stream-only gateway follows a fork-digest change, and a cancelled mump2p subscription no longer logs a retry. The `48123` bind and plain-TCP listeners from v1.3.4 are unchanged. Beacon blocks on every slot, join-key enrollment, and the consumer block stream carry forward.

## Upgrade from an earlier release

`docker restart` alone keeps the old image. Recreate the container. The command below is the quick-start layout; keep your own `--network`, volumes and credential env (for example `OPT_JOIN_KEY`), and change only the image tag. With `--network host`, `-p` does not apply: add `-e OPT_TELEMETRY_ADDR=127.0.0.1` instead.

```bash
export OPT_API_KEY=ogw_live_xxx   # unchanged if you use API keys
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

Confirm health and mesh:

```bash
curl -s http://localhost:48123/health | jq '.status, .checks'
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
```

No firewall or CL peering changes are required. v1.3.2 and v1.3.3 are unsupported because they contained security vulnerabilities.

## Version Status

| Version     | Status                              |
| ----------- | ----------------------------------- |
| v1.3.5      | **CURRENT**                         |
| v1.3.4      | supported / upgrade recommended     |
| v1.3.3      | DEPRECATED / unsupported            |
| v1.3.2      | DEPRECATED / unsupported            |
| v1.3.1      | DEPRECATED / unsupported            |
| v1.3.0      | DEPRECATED / unsupported            |
| v1.2.0      | DEPRECATED / unsupported            |
| v1.1.1      | DEPRECATED / unsupported            |
| v1.0.2      | DEPRECATED / unsupported            |
| v0.0.1-rc12 | REMOVED / unsupported               |
| v0.0.1-rc11 | REMOVED / unsupported               |
