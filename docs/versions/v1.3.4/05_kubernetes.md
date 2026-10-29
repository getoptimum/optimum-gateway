# Kubernetes (Helm)

Run the Optimum Gateway on Kubernetes with the official Helm chart. The gateway
sits beside your Ethereum consensus-layer (CL) client and bridges it into the
Optimum network.

**One Helm release = one gateway = one CL client.**

> **Running on bare metal or Docker instead?** See [Quick Start (Docker)](./01_quick_start.md).

## What you need from Optimum

Three things. Only the join key (or a legacy API key) is secret: keep it out of
any file you commit.

| | example |
|---|---|
| join key | `ojk_live_…` |
| cluster ID | `optimum_ethereum_mainnet_v0_1` |
| image digest | `sha256:943f6bd4df92…` |

The join key is one org-wide enrollment key for your whole fleet. You generate
it yourself in the Partner Console, with a cluster scope that includes your
cluster ID: see [Mint a join key](./07_gateway_self_enrollment.md#mint-a-join-key).
Join keys need gateway v1.3.2 or later; with chart 0.5.0 or later run v1.3.4 or
later (see [Requirements](#requirements)). Have a per-gateway API key
(`ogw_live_…`) instead? See [Legacy API key](#legacy-api-key). Ask Optimum for a
**digest**, not a tag: a tag can be repointed, a digest cannot.

## Requirements

* **Kubernetes 1.25+**, **Helm 3.8+**
* Chart **0.5.0+** with gateway **v1.3.4+**: older images ignore the chart's
  loopback telemetry bind and serve 48123 on every node IP. Join keys alone
  work from v1.3.2
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
ports. Schedule it on a node with a public IP and open inbound **33213**. Do not
open **48123**: Optimum never connects to it, and with chart 0.5.0 or later the
gateway keeps it on the node's loopback.

## Install

```bash
export CHART=oci://registry-1.docker.io/getoptimum/optimum-gateway
export VER=$(helm show chart $CHART | awk '/^version:/{print $2}')

kubectl create namespace optimum
kubectl label namespace optimum pod-security.kubernetes.io/enforce=privileged

# the join key, out of band (never in your values file)
kubectl -n optimum create secret generic optimum-gateway-join-key \
  --from-literal=join-key='<your join key>'

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
  # gatewayId: <cluster>-<release>  # set if the release name repeats in your org

joinKey:
  existingSecret: optimum-gateway-join-key
```

`directClPeers` is an **allowlist** — the gateway closes any connection from a
peer not on it. A wrong peer ID or port means it talks to nothing.

Get your client's peer ID:

```bash
curl -s localhost:5052/eth/v1/node/identity | jq -r .data.peer_id   # lighthouse, or nimbus with --rest
curl -s localhost:3500/eth/v1/node/identity | jq -r .data.peer_id   # prysm
curl -s localhost:5051/eth/v1/node/identity | jq -r .data.peer_id   # teku with --rest-api-enabled
curl -s localhost:9596/eth/v1/node/identity | jq -r .data.peer_id   # lodestar
```

Nimbus does not answer on `9596`, and its REST server is off until `--rest`. Teku's REST server is off until `--rest-api-enabled`. A refused curl is the beacon API.

Use the client's **P2P** port in the multiaddr — prysm `13000`, others `9000` —
not the HTTP port you just queried.

> `joinKey.existingSecret` is the one under the top-level `joinKey:` block.
> The legacy `apiKey:` block has its own `existingSecret`; set only one of them.

The enrollment label defaults to the release's full name
(`gateway-optimum-gateway` above, printed in the install notes). Set
`gateway.gatewayId` if that name repeats across your clusters or namespaces, or
matches the console label of a legacy API key still in use: enrolling under
that label revokes that API key or fails with `409`, depending on its type,
network and clusters. See
[Fleet enrollment](#fleet-enrollment-join-key).

## Point your CL client at the gateway

On Helm the order is the reverse of Docker. The chart rejects an empty `gateway.directClPeers`, so the CL peer ID has to exist before install. After the pod is ready, point the CL at the gateway. Flags are in [Connecting your CL client](08_cl_clients.md).

```bash
kubectl -n optimum port-forward deploy/gateway-optimum-gateway 48123:48123
curl -s localhost:48123/api/v1/self_info | jq -r '.peer_id, .libp2p.multiaddrs[]'
```

Build the multiaddr from the address the CL can route, and port `33212`:

```text
/ip4/<gateway-node-public-ip>/tcp/33212/p2p/<gateway-peer-id>
```

A pod address in `libp2p.multiaddrs` is not that address. The peer ID stays across restarts. The IP does not: if the pod moves to another node, update the multiaddr on the CL. Nimbus skips `--direct-peer` when its netkey is `random`. See [Nimbus](08_cl_clients.md#nimbus).

## Check it works

A `Running` pod proves nothing on its own — the gateway can start, register, and
peer with no one.

```bash
kubectl -n optimum port-forward deploy/gateway-optimum-gateway 48123:48123

curl -s localhost:48123/health | jq '{status, cl: .checks.cl_health.status, cl_peers: .checks.cl_peers.value, mump2p: .checks.mump2p_health.status, mump2p_peers: .checks.mump2p_peers.value}'
curl -s localhost:48123/api/v1/self_info | jq '{peer_id, multiaddrs: .libp2p.multiaddrs}'
```

Healthy looks like: `status: "healthy"`, `cl: "ok"`, `cl_peers: 1`, and
`mump2p_peers` in the tens. Expect this **within a couple of minutes** of the pod
going ready. On a `stream_only` deployment `cl` reads `skipped` and `cl_peers`
prints `null` (the check carries no value), while `status` is still `"healthy"`.

In `self_info`, `multiaddrs` must contain a **public IP**. If it only shows a
private or pod address (`10.x`), nothing outside your cluster can dial you and
`mump2p_peers` will stay at 0.

For the full meaning of each `/health` and `self_info` field, see
[Telemetry & Monitoring](./03_telemetry.md).

## Upgrade, rollback, uninstall

When Optimum sends a new image digest, put it in `image.digest` in your values
file (gateway v1.3.4 or later with chart 0.5.0 or later), then:

```bash
helm upgrade  gateway $CHART --version $VER -n optimum -f my-values.yaml
helm history  gateway -n optimum
helm rollback gateway <REVISION> -n optimum
helm uninstall gateway -n optimum
```

The deployment strategy is `Recreate`, not `RollingUpdate` — two pods must never
hold the same identity volume. Expect a brief gap on every upgrade.

Uninstall **keeps** the identity volumes (unless `persistence.retainOnDelete` is
false), so reinstalling keeps the same peer IDs and the same enrolled
credential, and your CL client's configured multiaddr still matches. Reinstall
under the same release name and namespace with the volumes kept: new volumes
mean a new enrollment, and the old credential stays live until you revoke it. If
the label is unchanged (an explicit `gateway.gatewayId`, or the same release
full name), the new enrollment fails with `409` until you do. To discard the
volumes:

```bash
kubectl -n optimum delete pvc -l app.kubernetes.io/instance=gateway
```

That is permanent. The gateway returns as a new peer and you must update your CL
client. With a join key it also re-enrolls: revoke the old credential in the
console first, or the new enrollment fails with `409 label_conflict`.

## Fleet enrollment (join key)

With `joinKey`, the chart passes the key as `OPT_JOIN_KEY`. On first boot the
gateway generates its own keypair, enrolls it with Optimum, and writes
`enrollment.json` (plus a transient `enrollment.key` mid-enrollment) to the
mump2p identity volume. Later boots reuse it without spending another join-key
use. Keep the join-key Secret for the gateway's lifetime: the gateway only uses
its enrollment while `OPT_JOIN_KEY` is set, and the pod does not start without
the Secret. Needs chart 0.5.0 or later, and gateway v1.3.4 or later as in
[Requirements](#requirements). See
[Gateway Self-Enrollment](07_gateway_self_enrollment.md).

* **The identity volume must persist.** The chart refuses `joinKey` with
  `persistence.enabled: false`: on an `emptyDir` each new pod re-enrolls under
  the same label and fails with `409 label_conflict` while the old credential is
  live.
* **`gateway.gatewayId` is the enrollment label.** It defaults to the release
  full name, which stays the same across rollouts, and is read only at first
  enrollment. It must be unique among your org's live credentials, so set it per
  gateway when release names repeat across clusters (for example
  `<cluster>-<release>`). At most 63 letters, digits, `.`, `_` or `-`, starting
  with a letter or digit; quote numeric labels. `dev-gateway` is refused.
* Exactly one of `joinKey` and `apiKey`; the gateway and the chart both refuse
  both. `joinKey` also needs `gateway.auth.enabled` (the default).

### Legacy API key

A per-gateway API key (`ogw_live_…`, see
[Generate your API key](./01_quick_start.md#generate-your-api-key)) still works:
create the Secret with key `api-key` and set `apiKey.existingSecret` instead of
`joinKey`. `gateway.gatewayId` is ignored there; the key carries the identity.

To move an installed gateway to a join key, create the join-key Secret, make
sure `image.digest` is gateway v1.3.4 or later, then swap `apiKey` for `joinKey`
in your values and `helm upgrade` to chart 0.5.0 or later. The gateway keeps its
peer ID but enrolls as a new credential. If the old API key carries the same
label in the console, Optimum either revokes it at enrollment (same type and
network, and clusters that overlap or are unscoped; rolling back to `apiKey`
then fails with `401`) or refuses the enrollment with `409 label_conflict` (set
a different `gateway.gatewayId`). With a different label, revoke the old API key
yourself once the gateway is up.

## Optional

**Send telemetry to Optimum** — lets us help you debug. Authenticated with a
token derived from your credential, so it needs no extra push credentials:

```yaml
gateway:
  remotePush:
    enabled: true
```

**Scrape it yourself.** With chart 0.5.0 or later, telemetry (`/health`,
`/metrics`, `/api/v1/self_info`) binds the node's loopback
(`gateway.telemetryAddr: 127.0.0.1`), so it is not reachable from other hosts
and `kubectl port-forward deploy/...` is the way in. A PodMonitor cannot scrape
loopback: use remote push above, or, if you run the Prometheus Operator, set
`gateway.telemetryAddr: 0.0.0.0` (a single node IP would not bind on other
nodes), firewall `48123` on every node so only Prometheus reaches it, and
enable:

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
| `CrashLoopBackOff` immediately (API key) | Usually a rejected API key: `kubectl logs` will show `api key not recognized (401)` |
| Logs show `enroll gateway` and `401` | The join key is unknown, expired, exhausted, or revoked, or the node clock is off. Check its badge under **Enrollment keys** in the console |
| Logs show `409` `label_conflict`, another gateway uses the label | A second live gateway (or a legacy API key Optimum did not revoke at enrollment) has the same `gateway.gatewayId` (or release full name). Give this one a unique `gateway.gatewayId`; do not revoke the other credential |
| Logs show `409` `label_conflict`, this gateway lost its volume | Its own earlier enrollment is still live. Revoke it under **Manage Gateways** → **Gateway** (find it by the label); the pod enrolls on its next restart, spending a join-key use, so the key in the Secret must still be valid |
| Logs show `409` `gateway_key_limit` | Your org is at its live-credential cap. Revoke unused credentials |
| Join-key pod crash-loops with `initial JWT mint failed` and `api key not recognized (401)` | The node clock is off (sync NTP first), or the enrolled credential was revoked. Only once the console shows it revoked: set `replicaCount: 0`, delete `enrollment.json` from the root of the `<release full name>-mump2p-identity` PVC (or your `persistence.mump2p.existingClaim`) with a one-off pod that mounts it, then restore `replicaCount: 1` to re-enroll with the same peer ID. That spends a join-key use, so the key in the Secret must still be valid |
| `cl_peers: 0` | `directClPeers` wrong, **or** your CL client was never pointed at the gateway (see above) |
| `mump2p_peers: 0` after a few minutes | Inbound **33213** is not reachable from the internet |
| `mump2p_peers: 0` after a join-key enrollment, port open | The join key's cluster scope does not include `gateway.clusterId`. The enrolled credential keeps that scope: put a correctly scoped join key in the Secret, revoke the credential, and remove `enrollment.json` as above |
| `multiaddrs` shows only private IPs | The node has no public IP, or `networking.hostNetwork` was disabled — it must stay `true` |
| Pod stuck `Pending` | No node with capacity; the chart also keeps one gateway per node |

## Getting help

Send us this — it answers most of the first round of questions:

```bash
helm get values gateway -n optimum      # safe unless you set a .value key
kubectl -n optimum get pods,pvc
kubectl -n optimum logs -l app.kubernetes.io/instance=gateway --tail=100
# with the port-forward from "Check it works" running
curl -s localhost:48123/health
curl -s localhost:48123/api/v1/self_info
```

`helm get values` is safe to share: with `existingSecret` or `externalSecret`
(under `joinKey` or `apiKey`) the key is only ever a reference, never a value in
the release. Do not share it if you set `joinKey.value` or `apiKey.value`. Every
setting is documented inline in `helm show values`.
