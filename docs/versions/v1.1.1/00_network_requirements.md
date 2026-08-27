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
  getoptimum/gateway:v1.1.1
```

If Prometheus runs in another container on the same Docker network, omit the `48123` publish and scrape the gateway by container name instead.

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
