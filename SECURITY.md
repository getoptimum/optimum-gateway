# Security

## Reporting a vulnerability

Please report security issues to **<security@getoptimum.xyz>** rather than
opening a public GitHub issue. We'll acknowledge receipt within 72 hours.

When reporting, include:

- The affected version (`./bin/optimum-gateway --version` or the commit hash).
- Steps to reproduce.
- Impact assessment if you have one (read primitive, write primitive,
  DoS amplification, etc.).
- Whether the issue affects only the gateway, or also the bootstrap
  server / auth service / fork-digest hub.

## Security research safe harbor

We welcome good-faith security research. The [`LICENSE`](./LICENSE) already
permits you to build, run and modify this software, so no extra permission is
needed for that. What this section does is define what we treat as good-faith
research, so the assurance below is unambiguous.

We will treat security testing as authorized access, and Spice Solutions Inc.
("Optimum") will not pursue legal action for it, provided that you:

- act in good faith and avoid privacy violations, data destruction, and
  degradation of others' services;
- test only against instances you own or operate, or a dedicated test
  deployment you control, and do not target Optimum's production systems or
  other operators' deployments without prior written consent;
- do not access, modify, or exfiltrate data that is not yours, and use the
  minimum access necessary to demonstrate a finding;
- give us a reasonable opportunity to remediate before any public disclosure
  (see below), and do not exploit a finding beyond what is needed to prove it.

This assurance concerns security research only. It is not a waiver of any other
right, and Optimum may withdraw it for conduct outside the scope above. Your
rights in the software itself are governed by the [`LICENSE`](./LICENSE).

### Coordinated disclosure

Report findings to **<security@getoptimum.xyz>** as described above. We ask for
a coordinated disclosure window of 90 days from our acknowledgement, or until a
fix ships, whichever comes first; we are glad to agree a different timeline for
complex issues. We will not pursue legal action for research conducted in good
faith under this safe harbor, and we will credit reporters who wish to be named.

## Threat model and trust anchors

The gateway has three trust anchors, in roughly increasing blast radius:

1. **The local CL client** (the beacon client the gateway peers with via
   libp2p on the port configured by `agent_lib_p2p_port` /
   `OPT_AGENT_LIB_P2P_PORT`, default `33212`). The operator declares the
   CL peer IDs in `direct_cl_peers` (`OPT_DIRECT_CL_PEERS`), which the
   gateway hands to libp2p-pubsub as direct peers
   (`WithDirectPeers`/`WithDirectConnectTicks`). When this list is
   non-empty, `onPeerConnected` also disconnects peers whose ID is not
   in the list (connect-time allowlist, not a `ConnectionGater`). When
   the list is empty, no peer-ID filtering is applied; operators must
   firewall the libp2p port to the intended CL client.
2. **The mump2p fleet** the gateway joins via the embedded mump2p node on
   the port configured by `agent_mump2p_port` / `OPT_AGENT_MUMP2P_PORT`
   (default `33213`). When auth is enabled (`OPT_ENABLE_AUTH=true` with
   `OPT_API_KEY` set), the handshake is authenticated by the operator's
   gateway JWT and verified against the upstream JWKS; when auth is
   disabled, `VerifyToken` is a no-op and only `ClusterID` is enforced.
3. **The bootstrap server + fork-digest hub** that hand the gateway its
   initial mump2p peer set on first boot. See "Bootstrap server trust"
   below — this is the only trust anchor whose compromise can shape the
   peer-set of every gateway in the fleet.

Anything else reachable on the gateway's listen sockets is treated as
hostile by default.

## Listener inventory

Default ports below are the config defaults; the actual port and (for
pprof/telemetry/stream) bind address come from the env vars in parentheses.
The gateway does **not** ship its own per-listener auth or loopback
guard for most ports — operators are expected to firewall every port
except the libp2p (`OPT_AGENT_LIB_P2P_PORT`) and Optimum
(`OPT_AGENT_MUMP2P_PORT`) listeners to peer-eligible networks.

| Port (default)                   | Service                 | Bind                                                                        | Auth                                                                                                                                                           | Notes                                                                                                                           |
| -------------------------------- | ----------------------- | --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 33212 (`OPT_AGENT_LIB_P2P_PORT`) | libp2p host (CL gossip) | all interfaces (`/ip4/0.0.0.0/tcp/...`, `/ip6/::/tcp/...`); Noise transport | libp2p-pubsub direct peering with `OPT_DIRECT_CL_PEERS`; connect-time peer ID allowlist when that list is non-empty (not a `ConnectionGater`) | Firewall to the local CL client when `OPT_DIRECT_CL_PEERS` is empty. |
| 33213 (`OPT_AGENT_MUMP2P_PORT`)  | Optimum (mump2p)        | all interfaces                                                              | `ClusterID` always enforced in handshake; JWT verified by `authMgr.VerifyToken` (no-op when `OPT_ENABLE_AUTH=false` or `OPT_API_KEY` empty)                    | No mainnet/hoodi refusal if auth is disabled — operators are responsible for setting `OPT_ENABLE_AUTH=true` in production.      |
| 48123 (`OPT_TELEMETRY_PORT`)     | HTTP telemetry (Fiber)  | all interfaces                                                              | **none** — `/`, `/health`, `/api/v1/self_info` are unauthenticated; `/metrics` is registered only when `OPT_ENABLE_TELEMETRY=true` and is also unauthenticated | Put behind a reverse proxy / firewall if exposed.                                                                               |
| 6060 (`OPT_PPROF_ADDR`)          | pprof HTTP              | `OPT_PPROF_ADDR` (default `127.0.0.1:6060`)                                 | none                                                                                                                                                           | Only started when `OPT_ENABLE_PPROF=true`. Remote exposure is controlled by setting `OPT_PPROF_ADDR` to a non-loopback address. |
| 9600 (`OPT_STREAM_ADDR`)         | Consumer block stream (WebSocket) | `OPT_STREAM_ADDR` (default `127.0.0.1:9600`)                        | Consumer JWT (`aud=stream`) when `OPT_STREAM_REQUIRE_AUTH=true`; auth-off rejected on non-loopback                                                              | Only started when `OPT_STREAM_ENABLE=true`. No native TLS; a public bind must sit behind a trusted terminating proxy.            |
| 9601 (`OPT_STREAM_GRPC_ADDR`)    | Consumer block stream (gRPC) | `OPT_STREAM_GRPC_ADDR` (default `127.0.0.1:9601`)                      | same as WebSocket                                                                                                                                              | Same exposure rules as `OPT_STREAM_ADDR`. Must differ from `stream_addr`.                                                        |

## Bootstrap server trust

On first boot the gateway builds a bootstrap URL via
`utils.BootstrapExposeNodesURL(...)` and calls
`GET <bootstrap>/api/v1/expose-nodes?chain_id=<chain>&cluster_id=<cluster>&expose_amount=<n>`
(see `pkg/service/gossipsub-gateway/setup_mump2p_host.go`) for its
initial mump2p peer set. If that response is non-`200` or the returned
list is empty, the gateway falls back to a raw GitHub URL on the
`forkdigest-hub` repo
(`https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/<chain>/peers.json`).

A compromised bootstrap server or `forkdigest-hub` repo can hand every
gateway in the fleet an attacker-controlled peer set. When auth is
enabled, the mump2p handshake gates final membership: `handshakeHandler`
checks `h.ClusterID == cfg.GatewayClusterID` and calls
`authMgr.VerifyToken(h.JWTToken)`, which in turn requires the JWT's
`chain_id` claim to match the gateway's normalized chain. The impact is
therefore bounded by JWT issuance hygiene — but a holder of a valid JWT
for the same cluster + chain could insert themselves as the bootstrap of
every new gateway. When auth is disabled (`OPT_ENABLE_AUTH=false` or
`OPT_API_KEY` empty), `VerifyToken` is a no-op and only `ClusterID` is
enforced.

Operator-side mitigations:

- **JWKS signing key rotation.** Rotate at least every 90 days. The
  gateway's JWKS cache refreshes every `JWKSRefreshIntervalSec` (default
  `3600` seconds / 1h; used as the `Refresh` interval of the
  `keyfunc.Keyfunc`), so revocations take up to that long to propagate.
  Shorten this in incident response.
- **Branch protection on `forkdigest-hub`.** `main` should require code
  review from at least one ops engineer outside the contributor list,
  and force-push must be disabled.
- **Bootstrap server access control.** The publishing path for
  `/api/v1/expose-nodes` runs under a service account with no shell
  access; SSH to the bootstrap host is restricted to the on-call
  rotation.

## What the gateway does *not* protect against

- **A malicious local CL client.** The `direct_cl_peers` direct-peering
  configuration trusts the operator's beacon client. If that client is
  compromised, the gateway will faithfully forward anything it publishes
  onto the mump2p mesh.
- **A leaked or stolen gateway JWT.** The JWT binds chain ID but not
  source IP; a JWT that escapes the operator's infrastructure can be
  used to peer with the mump2p mesh from anywhere. Revoke via the
  upstream JWKS rotation rather than waiting for the JWT to expire.
- **Compromise of the upstream auth service.** A compromised
  `auth.getoptimum.io` can mint JWTs for arbitrary operators. JWKS
  rotation only fences this if the key material itself is rotated.

## Verifying published images (signatures & SBOMs)

Every image published to Docker Hub carries a BuildKit SBOM and SLSA
provenance attestation, and is signed with [cosign](https://github.com/sigstore/cosign)
keyless (Sigstore/Fulcio, GitHub Actions OIDC identity). This applies to all
three published images:

- `getoptimum/gateway`
- `getoptimum/gateway-bench`
- `getoptimum/hermes-gateway-sidecar`

Tagged `getoptimum/gateway` releases additionally carry the repo's CycloneDX
SBOM (`docs/sbom.json`, the shipped-binary `./cmd` dependency footprint) as a
signed attestation.

Signing is by image-index digest, so the checks below work against any tag
pointing at that digest. Set the image once:

```bash
IMG=getoptimum/gateway:latest          # or :dev-latest, or a specific tag
ISSUER=https://token.actions.githubusercontent.com
IDENTITY='^https://github.com/getoptimum/optimum-gateway/'
```

**Verify the signature** (fails if the image was not signed by this repo's
release workflow):

```bash
cosign verify \
  --certificate-oidc-issuer "$ISSUER" \
  --certificate-identity-regexp "$IDENTITY" \
  "$IMG"
```

**Inspect the attached SBOM and provenance:**

```bash
docker buildx imagetools inspect "$IMG" --format '{{json .SBOM}}'
docker buildx imagetools inspect "$IMG" --format '{{json .Provenance}}'
```

**Verify the CycloneDX attestation** (tagged `getoptimum/gateway` releases
only):

```bash
cosign verify-attestation --type cyclonedx \
  --certificate-oidc-issuer "$ISSUER" \
  --certificate-identity-regexp "$IDENTITY" \
  "$IMG"
```

The committed `docs/sbom.json` (shipped-binary deps) and `docs/sbom-full.json`
(all deps) are the source-level SBOMs used for license/dependency governance;
they are regenerated with `make sbom` and kept current in CI.

## Hardening references

Operator-applied mitigations that don't require a rebuild — firewall
rules, reverse proxies in front of the telemetry port, JWKS rotation
cadence, and pprof exposure — are described inline in the sections
above. Release-specific hardening notes, when published, will be linked
from this section.
