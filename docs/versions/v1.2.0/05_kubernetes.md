# Kubernetes (Helm)

Run the Optimum Gateway on Kubernetes with the official Helm chart. The gateway
sits beside your Ethereum consensus-layer (CL) client and bridges it into the
Optimum network.

**One Helm release = one gateway = one CL client.**

> **Running on bare metal or Docker instead?** See [Quick Start (Docker)](./01_quick_start.md).

## What you need from Optimum

Three things — none of them belong in a file you commit:

| | example |
|---|---|
| API key | `ogw_live_…` |
| cluster ID | `optimum_ethereum_mainnet_v0_1` |
| image digest | `sha256:943f6bd4df92…` |

You generate the API key yourself from the Partner Console — see
[Generate your API key](./01_quick_start.md#generate-your-api-key). Ask Optimum
for a **digest**, not a tag: a tag can be repointed, a digest cannot.

## Requirements

* **Kubernetes 1.25+**, **Helm 3.8+**
* A node with a **public IP**
* Inbound **TCP 33213** open to that node from the internet — this is the
  Optimum network (mump2p) port and the gateway is unusable without it
* The CL client able to reach the gateway on **TCP 33212** (usually in-cluster;
  no public firewall hole needed for this)
* A namespace permitting `hostNetwork` and `hostPort`. Under Pod Security
  Admission that means **`privileged`** — `baseline` blocks both
* A default StorageClass supporting `ReadWriteOnce` (two small volumes hold the
  gateway's identities)
* Outbound HTTPS to `bootstrap.getoptimum.io` and `auth.getoptimum.io`

Resource use: requests `150m` CPU / `2Gi` memory, limit `4Gi` memory.

## Networking: why `hostNetwork` is required

The gateway advertises the addresses it detects **in its own network namespace**
to the Optimum bootstrap registry, and has **no announce-address override**
(there is no equivalent of Lighthouse's `--enr-address` / `--nat=extip`).

On a normal pod network it would detect and advertise the internal **pod IP**,
register as "reachable," and then silently receive **no inbound mump2p peers** —
a NodePort or LoadBalancer in front does not help, because there is no way to
tell the gateway to announce that external address. Reachability comes from
`hostNetwork`, not from a Service.

So the chart defaults to `networking.hostNetwork: true` (the K8s equivalent of
Docker host-mode) and one gateway per node via anti-affinity, since it binds host
ports. Schedule it on a node with a public IP and open inbound **33213**.

## Install

```bash
export CHART=oci://registry-1.docker.io/getoptimum/optimum-gateway
export VER=$(helm show chart $CHART | awk '/^version:/{print $2}')

kubectl create namespace optimum
kubectl label namespace optimum pod-security.kubernetes.io/enforce=privileged

# the API key, out of band — never in your values file
kubectl -n optimum create secret generic optimum-gateway-api-key \
  --from-literal=api-key='<your API key>'

helm show values $CHART --version $VER > my-values.yaml
# fill in the four values below

helm install gateway $CHART --version $VER -n optimum -f my-values.yaml
```

## The four values you must set

The chart refuses to install without these, rather than starting a gateway that
does nothing.

```yaml
image:
  digest: sha256:…                  # from Optimum

gateway:
  clusterId: optimum_…              # from Optimum
  directClPeers:
    - /dns4/<cl-service>.<ns>.svc.cluster.local/tcp/<p2p-port>/p2p/<peer-id>
    # CL outside the cluster: /ip4/<cl-host>/tcp/<p2p-port>/p2p/<peer-id>

apiKey:
  existingSecret: optimum-gateway-api-key
```

`directClPeers` is an **allowlist** — the gateway closes any connection from a
peer not on it. A wrong peer ID or port means it talks to nothing.

Get your client's peer ID:

```bash
curl -s localhost:5052/eth/v1/node/identity | jq -r .data.peer_id   # lighthouse / nimbus
curl -s localhost:3500/eth/v1/node/identity | jq -r .data.peer_id   # prysm
curl -s localhost:5051/eth/v1/node/identity | jq -r .data.peer_id   # teku
```

Use the client's **P2P** port in the multiaddr — prysm `13000`, others `9000` —
not the HTTP port you just queried.

> `apiKey.existingSecret` is the one under the top-level `apiKey:` block.

## Point your CL client at the gateway

**Peering is two-way.** The step above tells the gateway about your client. Your
client must **also** be told about the gateway, or it drops the connection and
`cl_peers` stays at 0.

Get the gateway's identity once it is running:

```bash
kubectl -n optimum port-forward svc/gateway-optimum-gateway 48123:48123
curl -s localhost:48123/api/v1/self_info | jq -r '.peer_id, .libp2p.multiaddrs[]'
```

Build the multiaddr from the **public** address and port `33212`:

```text
/ip4/<gateway-node-public-ip>/tcp/33212/p2p/<gateway-peer-id>
```

Add it to your client and restart it:

| client | flag |
|---|---|
| Prysm | `--peer=<multiaddr>` |
| Lighthouse | `--libp2p-addresses=<multiaddr>` and `--trusted-peers=<gateway-peer-id>` |
| Teku | `--p2p-direct-peers=<multiaddr>` |
| Nimbus | `--direct-peer=<multiaddr>` |

> **Nimbus** ignores the direct-peer list when its network key is
> auto-generated. Give Nimbus a persistent netkey or it silently skips the
> gateway.

The gateway's peer ID is stable across restarts. Its **IP is not** — if the pod
moves to a different node, update this multiaddr.

## Check it works

A `Running` pod proves nothing on its own — the gateway can start, register, and
peer with no one.

```bash
kubectl -n optimum port-forward svc/gateway-optimum-gateway 48123:48123

curl -s localhost:48123/health | jq '{status, cl: .checks.cl_health.status, cl_peers: .checks.cl_peers.value, mump2p: .checks.mump2p_health.status, mump2p_peers: .checks.mump2p_peers.value}'
curl -s localhost:48123/api/v1/self_info | jq '{peer_id, multiaddrs: .libp2p.multiaddrs}'
```

Healthy looks like: `status: "healthy"`, `cl: "ok"`, `cl_peers: 1`, and
`mump2p_peers` in the tens. Expect this **within a couple of minutes** of the pod
going ready. On a `stream_only` deployment `cl` reads `skipped` and `cl_peers` is
absent, while `status` is still `"healthy"`.

In `self_info`, `multiaddrs` must contain a **public IP**. If it only shows a
private or pod address (`10.x`), nothing outside your cluster can dial you and
`mump2p_peers` will stay at 0.

For the full meaning of each `/health` and `self_info` field, see
[Telemetry & Monitoring](./03_telemetry.md).

## Upgrade, rollback, uninstall

When Optimum sends a new image digest, put it in `image.digest` in your values
file, then:

```bash
helm upgrade  gateway $CHART --version $VER -n optimum -f my-values.yaml
helm history  gateway -n optimum
helm rollback gateway <REVISION> -n optimum
helm uninstall gateway -n optimum
```

The deployment strategy is `Recreate`, not `RollingUpdate` — two pods must never
hold the same identity volume. Expect a brief gap on every upgrade.

Uninstall **keeps** the identity volumes, so reinstalling keeps the same peer IDs
and your CL client's configured multiaddr still matches. To discard them:

```bash
kubectl -n optimum delete pvc -l app.kubernetes.io/instance=gateway
```

That is permanent. The gateway returns as a new peer and you must update your CL
client.

## Optional

**Send telemetry to Optimum** — lets us help you debug. Authenticated with a
token derived from your API key, so it needs no extra credentials:

```yaml
gateway:
  remotePush:
    enabled: true
```

**Scrape it yourself** — if you run the Prometheus Operator:

```yaml
podMonitor:
  enabled: true
```

Metrics are on `:48123/metrics`. Keep `gateway.logLevel: info`; `debug` is very
noisy. Propagation (forwarding Optimum messages into the CL network) is managed
by Optimum centrally — you do not need to configure it.

**Consumer block stream** is off by default. Enable it in gateway config if you
need a local WebSocket/gRPC feed of decoded blocks; listeners bind loopback.
See [Consumer Block Stream](06_block_stream.md).

## When it doesn't work

Kubernetes-specific symptoms below. For gateway behaviour that isn't K8s-specific
(CL peering, PeerDAS, identity, log noise), see
[Troubleshooting](./04_troubleshoot.md).

| Symptom | Cause |
|---|---|
| `helm install` fails naming a value | That value is required — the message says which |
| `helm install` says deployed but there are no pods | Pod Security is rejecting them. `kubectl -n optimum describe rs -l app.kubernetes.io/instance=gateway` — if it mentions `violates PodSecurity`, the namespace needs `privileged` |
| `CrashLoopBackOff` immediately | Usually a rejected API key — `kubectl logs` will show `api key not recognized (401)` |
| `cl_peers: 0` | `directClPeers` wrong, **or** your CL client was never pointed at the gateway (see above) |
| `mump2p_peers: 0` after a few minutes | Inbound **33213** is not reachable from the internet |
| `multiaddrs` shows only private IPs | The node has no public IP, or `networking.hostNetwork` was disabled — it must stay `true` |
| Pod stuck `Pending` | No node with capacity; the chart also keeps one gateway per node |

## Getting help

Send us this — it answers most of the first round of questions:

```bash
helm get values gateway -n optimum      # safe: contains no secret
kubectl -n optimum get pods,pvc
kubectl -n optimum logs -l app.kubernetes.io/instance=gateway --tail=100
curl -s localhost:48123/health
curl -s localhost:48123/api/v1/self_info
```

`helm get values` is safe to share — with `apiKey.existingSecret` the key is only
ever a reference, never a value in the release. Every setting is documented inline
in `helm show values`.
