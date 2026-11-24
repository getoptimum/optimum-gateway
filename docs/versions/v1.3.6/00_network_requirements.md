# Network Requirements

Before starting the Optimum Gateway, ensure your firewall allows these ports:

## Required Ports

| Port  | Direction | Purpose                                                             |
| ----- | --------- | ------------------------------------------------------------------- |
| 33212 | Inbound   | libp2p agent - CL clients connect here                              |
| 33213 | Inbound   | mump2p agent - Optimum network peers connect here (or open the announced load-balancer port forwarding to `33213`; omit inbound with `agent_mump2p_not_dialable: true`) |
| 48123 | Localhost | Telemetry / Health / API — **local access only** (see Docker below) |

## Load balancers

`33212` stays on the private network. The beacon node dials that sidecar by peer ID. Keep it off the public internet, and do not put it behind a load balancer.

`33213` is a long-lived raw TCP session. An HTTP load balancer cannot carry it. A TCP load balancer can, when the gateway announces that balancer's address: set `agent_mump2p_announce_ip` and, if the published port is not `33213`, `agent_mump2p_announce_port`. The gateway still listens on `33213`, and announces only that configured address (skipping host, interface, and IPv6 detection) so peers on the same private network also connect through the balancer. Leave both unset and it announces the addresses it detects on the host.

* **One external address per gateway.** Peers authenticate by peer ID, so an address shared across multiple gateways fails dials. Multiple gateways can share one IP on different ports, up to 8 per remote peer (libp2p allows 8 connections per remote IPv4 address or IPv6 `/56`).
* **Preserve the client source IP.** The mesh allows 8 connections per source IPv4 address (or IPv6 `/56`). A balancer that rewrites the source address — for example an AWS NLB with client IP preservation off, or a Kubernetes `LoadBalancer` Service with `externalTrafficPolicy: Cluster` — caps the gateway at 8 inbound mesh peers. Enable client IP preservation (or `externalTrafficPolicy: Local` on Kubernetes).

See [Mesh address](02_configuration.md#mesh-address) for full configuration and validation rules. The [Kubernetes install](05_kubernetes.md#networking-why-hostnetwork-is-required) still defaults to the node's own address.

## Outbound-only (no inbound 33213)

When a gateway can dial out and has no address other gateways can dial, set `agent_mump2p_not_dialable: true` (`OPT_AGENT_MUMP2P_NOT_DIALABLE=true`). It registers with bootstrap without announcing a mesh address, so other gateways do not dial it while it still dials them. Allow outbound TCP egress to mesh peers and outbound HTTPS (`443`) to `auth.getoptimum.io` and `bootstrap.getoptimum.io`. Do not combine it with `agent_mump2p_announce_ip` or `agent_mump2p_announce_port`. Two outbound-only gateways cannot dial each other.

Keep one sidecar per beacon node. A balancer that sends the CL connection to a different process presents a different peer ID, and the beacon node will not stay connected to it.

## Docker

Publish **33212** (CL clients) and **33213** (Optimum mump2p mesh) to the network. Bind **48123** to localhost only — metrics and health stay available on the host (e.g. local Grafana) but are not reachable from the public internet. Optimum receives telemetry via remote push; you do not need to expose 48123 externally.

```bash
docker run -p 33212:33212 -p 33213:33213 -p 127.0.0.1:48123:48123 \
  --name optimum-gateway \
  -e OPT_API_KEY=ogw_live_xxx \
  getoptimum/gateway:v1.3.6
```

Behind a TCP load balancer, also pass `-e OPT_AGENT_MUMP2P_ANNOUNCE_IP=<lb-ip>` (and `-e OPT_AGENT_MUMP2P_ANNOUNCE_PORT=<lb-port>` when the external port is not `33213`). For outbound-only mode, pass `-e OPT_AGENT_MUMP2P_NOT_DIALABLE=true` and you can omit `-p 33213:33213`.

If Prometheus runs in another container, put both on one Docker network and scrape the gateway by container name (see [Metrics & Grafana](03_telemetry.md#step-3-prometheus-configuration)). Keep the loopback publish for local `curl` checks.

Optimum never connects inbound to `48123`. If you run the gateway with `--network host`, `-p` does not apply, so set `telemetry_addr: 127.0.0.1` (or `OPT_TELEMETRY_ADDR=127.0.0.1`) to keep `48123` on loopback.

## Consumer block stream (optional)

Enabling `stream_enable` does **not** add public ports. Listeners default to loopback `127.0.0.1:9600` (WebSocket) and `127.0.0.1:9601` (gRPC). See [Consumer Block Stream](06_block_stream.md).

## Verification

```bash
# Check listening ports (Linux)
netstat -tlnp | grep -E "(33212|33213|48123)"

# Check listening ports (macOS)
netstat -an | grep -E "(33212|33213|48123)"
```

Test health:

```bash
curl http://localhost:48123/health
```
