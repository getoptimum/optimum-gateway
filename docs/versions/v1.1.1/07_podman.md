# Podman

Run the Optimum Gateway with rootless Podman. Same binary and config as
[Quick Start (Docker)](01_quick_start.md) — this page only covers where
Podman's defaults differ from Docker's and will silently break the gateway
if you don't account for them.

> **Prerequisites:** [Requirements](index.md#requirements) and
> [Network Requirements](00_network_requirements.md). You also need an
> **API key** — see [Generate your API key](01_quick_start.md#generate-your-api-key).

> **Running on Kubernetes instead?** See [Kubernetes (Helm)](05_kubernetes.md).

## Why this page exists: rootless networking

Docker's default bridge network and Podman's default rootless network are
**not the same thing**. Rootless Podman without `--network=host` runs the
container behind `slirp4netns`/`pasta` — a user-mode NAT. The gateway inside
that namespace detects a private, non-routable address, advertises *that* to
the Optimum bootstrap registry, registers as "reachable," and then silently
receives **no inbound mump2p peers** — publishing port `33213` on the Podman
CLI does not fix this, because the gateway never advertises the address that
port mapping makes reachable.

This is the same underlying problem the [Kubernetes guide](05_kubernetes.md#networking-why-hostnetwork-is-required)
documents for `hostNetwork` — the gateway has no announce-address override.
The fix here is the same shape: run the container in the host's network
namespace.

```sh
--network=host
```

With `--network=host` there is nothing to publish — the container simply
listens on the host's ports directly. Do not combine `-p`/`--publish` with
`--network=host`; Podman will ignore or reject it.

## Requirements

* **Podman 4.4+** (rootless)
* A host with a **public IP**
* Inbound **TCP 33213** open to that host from the internet — the Optimum
  mump2p port; the gateway is unusable without it
* The CL client able to reach the gateway on **TCP 33212**
* Outbound HTTPS to `bootstrap.getoptimum.io` and `auth.getoptimum.io`
* SELinux-enforcing hosts (Fedora, RHEL, CentOS Stream): see the volume note below

## Run

```sh
mkdir -p config data/libp2p data/mump2p
# fill in config/app_conf.yml — see Quick Start (Docker) for the minimal example
```

```sh
podman run --name optimum-gateway -d \
  --network=host \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config:Z \
  -v $(pwd)/data/libp2p:/tmp/libp2p:Z \
  -v $(pwd)/data/mump2p:/tmp/mump2p:Z \
  getoptimum/gateway:v1.1.1 \
  -config=/app/config/app_conf.yml
```

### The `:Z` suffix

On SELinux-enforcing hosts, a bind mount without a label suffix fails with a
generic `permission denied` — nothing in the gateway's own logs points at
SELinux. `:Z` applies a private, container-specific label to the directory
so **only this container** can access it. Use `:z` (lowercase) instead only
if the same host directory must be shared with another container running at
the same time. On non-SELinux hosts (Debian, Ubuntu without SELinux) the
suffix is accepted and ignored — safe to leave in either way.

If you see `permission denied` on the identity or config directories despite
the `:Z` suffix, confirm the host directory is owned by your user, not root:

```sh
podman unshare chown -R $(id -u):$(id -g) data/libp2p data/mump2p
```

> Persist `data/libp2p` and `data/mump2p` across restarts — as with Docker,
> without them the gateway's peer ID changes every run and your CL client's
> configured multiaddr goes stale.

## Verify

Same checks as Docker, since `--network=host` means the ports are just the
host's ports:

```sh
curl -s http://localhost:48123/health | jq
curl -s http://localhost:48123/api/v1/self_info | jq '{peer_id, multiaddrs: .libp2p.multiaddrs}'
```

`multiaddrs` must show a **public** IP, not `10.x`/`172.x`/`192.168.x`. A
private-only address here means `--network=host` was dropped somewhere, or
the host itself has no public interface.

```sh
podman logs optimum-gateway
```

## Connect CL Client

Identical to [Quick Start — Connect CL Client](01_quick_start.md#connect-cl-client):
get `peer_id` and a reachable multiaddr from `self_info`, then use the
Prysm/Teku/Lighthouse/Nimbus/Lodestar flags documented there. Nothing about
peering changes with Podman — only the container networking above does.

## Production: systemd Quadlet

For a restart-on-boot, restart-on-crash deployment, use a
[Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit instead of a bare `podman run`. Quadlet generates a systemd service from
a declarative `.container` file — the Podman equivalent of Compose's
`restart: unless-stopped`, but managed by systemd (`systemctl status`,
`journalctl`, boot-time start).

`~/.config/containers/systemd/optimum-gateway.container`:

```ini
[Unit]
Description=Optimum Gateway
After=network-online.target
Wants=network-online.target

[Container]
Image=getoptimum/gateway:v1.1.1
ContainerName=optimum-gateway
Network=host
Exec=-config=/app/config/app_conf.yml
Secret=optimum-gateway-api-key,type=env,target=OPT_API_KEY
Volume=%h/optimum-gateway/config:/app/config:Z
Volume=%h/optimum-gateway/data/libp2p:/tmp/libp2p:Z
Volume=%h/optimum-gateway/data/mump2p:/tmp/mump2p:Z
HealthCmd=curl -f http://localhost:48123/api/v1/self_info || exit 1
HealthInterval=30s

[Service]
Restart=always

[Install]
WantedBy=default.target
```

The API key is passed as a **Podman secret**, not a plaintext env line in
the unit file — mirrors the Kubernetes guide's use of a
[Secret](05_kubernetes.md#install) rather than a value in `values.yaml`:

```sh
mkdir -p ~/.config/containers/systemd
echo -n 'ogw_live_...' | podman secret create optimum-gateway-api-key -
```

Then, for a user session that should keep running after logout:

```sh
loginctl enable-linger $(whoami)
systemctl --user daemon-reload
systemctl --user start optimum-gateway.service
systemctl --user status optimum-gateway.service
journalctl --user -u optimum-gateway.service -f
```

Editing the `.container` file requires `daemon-reload` before it takes
effect, same as any systemd unit change.

## When it doesn't work

Podman-specific symptoms below. For gateway behaviour that isn't
Podman-specific (CL peering, PeerDAS, identity, log noise), see
[Troubleshooting](04_troubleshoot.md).

| Symptom | Cause |
|---|---|
| `permission denied` on `/app/config` or `/tmp/libp2p` at startup | Missing `:Z` on the volume mount, or the host directory isn't owned by your user — see [The `:Z` suffix](#the-z-suffix) |
| `mump2p_peers: 0` after a few minutes, but `cl_peers` is fine | Container is not on `--network=host` — check `podman inspect optimum-gateway --format '{{.HostConfig.NetworkMode}}'`, expect `host` |
| `self_info` shows only a private IP in `multiaddrs` | Same as above, or the host itself has no public interface |
| `Error: address already in use` on `podman run` | Another process (or a previous gateway container) already holds `33212`/`33213`/`48123` on the host — with `--network=host` there is no port remapping to fall back on |
| Peer ID changes every restart | `data/libp2p` / `data/mump2p` not persisted, or pointed at the wrong host path — confirm the bind mount source matches across restarts |
| Quadlet service won't start; `systemctl --user status` shows nothing | Run `systemctl --user daemon-reload` after creating/editing the `.container` file |
| Quadlet service stops when you log out | `loginctl enable-linger $(whoami)` was not run |

## Getting help

Send us this — it answers most of the first round of questions:

```sh
podman inspect optimum-gateway --format '{{.HostConfig.NetworkMode}}'
podman logs optimum-gateway --tail 100
curl -s http://localhost:48123/health
curl -s http://localhost:48123/api/v1/self_info
```
