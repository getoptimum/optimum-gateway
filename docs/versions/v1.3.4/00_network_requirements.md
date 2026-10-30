# Network Requirements

Before starting the Optimum Gateway, ensure your firewall allows these ports:

## Required Ports

| Port  | Direction | Purpose                                                             |
| ----- | --------- | ------------------------------------------------------------------- |
| 33212 | Inbound   | libp2p agent - CL clients connect here                              |
| 33213 | Inbound   | mump2p agent - Optimum network peers connect here                   |
| 48123 | Localhost | Telemetry / Health / API — **local access only** (see Docker below) |

## Docker

Publish **33212** (CL clients) and **33213** (Optimum mump2p mesh) to the network. Bind **48123** to localhost only — metrics and health stay available on the host (e.g. local Grafana) but are not reachable from the public internet. Optimum receives telemetry via remote push; you do not need to expose 48123 externally.

```bash
docker run -p 33212:33212 -p 33213:33213 -p 127.0.0.1:48123:48123 \
  --name optimum-gateway \
  -e OPT_API_KEY=ogw_live_xxx \
  getoptimum/gateway:v1.3.4
```

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
