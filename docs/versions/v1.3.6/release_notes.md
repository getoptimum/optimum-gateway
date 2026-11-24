# v1.3.6

> **Current release.** v1.3.6 is the current release. A gateway that is already reachable on `33213` needs no configuration change. v1.3.5 stays supported. v1.3.4 stays supported; upgrade it for [CVE-2026-78661](https://www.cve.org/CVERecord?id=CVE-2026-78661). v1.3.2 and v1.3.3 are unsupported because they contained security vulnerabilities.

## Highlights

* **A gateway behind a load balancer can announce that address.** Set `agent_mump2p_announce_ip` (`OPT_AGENT_MUMP2P_ANNOUNCE_IP`) to the address other gateways should dial, and `agent_mump2p_announce_port` (`OPT_AGENT_MUMP2P_ANNOUNCE_PORT`) when the published port is not `agent_mump2p_port`. The gateway still listens on `agent_mump2p_port` and announces only that address. Leave both unset and it announces the addresses it detects on the host (including IPv6-only hosts). The load balancer must forward plain TCP to this one gateway and preserve the client source IP. See [Mesh address](02_configuration.md#mesh-address).
* **A gateway with no reachable address can join outbound-only.** `agent_mump2p_not_dialable: true` (`OPT_AGENT_MUMP2P_NOT_DIALABLE`) registers the gateway and announces no mesh address, so other gateways do not dial it. It still dials them. Do not set it together with the announce IP or port. Two outbound-only gateways cannot reach each other.
* **Attestations go out on each aggregation tick.** The gateway no longer holds them until a beacon block for that slot has arrived. Attestations outside the current slot are still dropped.

The v1.3.5 consensus behaviour carries forward: custody group count 4, a far-future `earliest_available_slot` on `status/2`, ping sequence 1, and the saved address for a peer in `direct_cl_peers`. The `48123` bind, plain-TCP listeners, beacon blocks on every slot, join-key enrollment, and the consumer block stream are unchanged. CL flags do not change.

## Upgrade from an earlier release

`docker restart` alone keeps the old image. Recreate the container. The command below is the quick-start layout; keep your own `--network`, volumes and credential env (for example `OPT_JOIN_KEY`), and change only the image tag. With `--network host`, `-p` does not apply: add `-e OPT_TELEMETRY_ADDR=127.0.0.1` instead.

```bash
export OPT_API_KEY=ogw_live_xxx   # unchanged if you use API keys
docker pull getoptimum/gateway:v1.3.6
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 33213:33213/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.3.6 \
  -config=/app/config/app_conf.yml
```

Confirm health and mesh:

```bash
curl -s http://localhost:48123/health | jq '.status, .checks'
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
```

No firewall or CL peering changes are required unless you set an announce address (open that published port on the load balancer, forwarding plain TCP to `33213`) or switch to `agent_mump2p_not_dialable: true` (no inbound `33213` needed). Keep `33212` private to the CL. v1.3.2 and v1.3.3 are unsupported because they contained security vulnerabilities.

## Version Status

| Version     | Status                              |
| ----------- | ----------------------------------- |
| v1.3.6      | **CURRENT**                         |
| v1.3.5      | supported                           |
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
