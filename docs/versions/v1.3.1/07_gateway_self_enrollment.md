# Gateway Self-Enrollment

> **Prerequisites:** [Network Requirements](00_network_requirements.md) and [Configuration](02_configuration.md).

Use this path when you run **many gateways** under one operator. Instead of minting and distributing one `ogw_` API key per host, you mint one org-wide **join key** (`ojk_live_...`) and let each gateway register its own asymmetric credential on first boot.

The legacy **API key** path is unchanged. Single-gateway and small deployments should keep using [Quick Start](01_quick_start.md).

## When to use a join key

| | API key (`ogw_`) | Join key (`ojk_`) |
|---|---|---|
| **Best for** | One gateway, or a handful you can map by hand | Fleets — tens or hundreds of hosts |
| **Credential** | One secret per host | One org key, shared across the fleet |
| **Gateway identity** | Baked into the key at mint time | Assigned at enroll (`client_id` in JWT `sub`) |
| **Chain / type / clusters** | From the API key | From the join key — gateway cannot self-assign |

## Mint a join key

1. Sign in to the [Partner Console](https://console.getoptimum.io/).
2. Open **API Keys** in the sidebar and select the **JOIN** tab.
3. Click **GENERATE KEY** and set:
   * **Type** — same gate as gateway API keys (`partner` for standard partner deployments).
   * **Chain** — Hoodi or Mainnet (inherited by every gateway that enrolls with this key).
   * **Cluster IDs** — must match the `gateway_cluster_id` you assign on each host.
   * **Max uses** and **TTL** — optional limits on how many gateways can enroll and how long the key stays valid.
4. Copy the key (`ojk_live_...`). It is **shown only once**. Store it in your secret manager — the same way you would an API key.

Revoking a join key stops **new** enrollments. Gateways already enrolled keep their independent credentials until those are revoked separately in the console.

## Configure a gateway

Set **exactly one** credential mode. `api_key` and `join_key` are **mutually exclusive** — setting both is a startup error.

Create `config/app_conf.yml` (operational fields only):

```yaml
log_level: info
gateway_cluster_id: optimum_ethereum_hoodi_v0_1   # must match the join key's cluster scope

agent_lib_p2p_port: 33212
agent_mump2p_port: 33213
telemetry_enable: true
telemetry_port: 48123
identity_libp2p_dir: /data/libp2p
identity_mump2p_dir: /data/mump2p
```

Pass the join key and a **per-host enrollment label** via the environment — not YAML:

```sh
export OPT_JOIN_KEY=ojk_live_xxx
export OPT_GATEWAY_ID=hoodi-validator-rack-03   # unique per host — enrollment label only
```

> **Join key via environment, not YAML.** Same rule as `OPT_API_KEY`: keep secrets out of config files and image layers.

### Config reference (join-key path)

| Key | Env | Default | Description |
|---|---|---|---|
| `join_key` | `OPT_JOIN_KEY` | *(empty)* | Org-wide join credential (`ojk_live_...`). **Set via env, not YAML.** |
| `enroll_cred_dir` | `OPT_ENROLL_CRED_DIR` | `identity_mump2p_dir` | Directory for `enrollment.json`. Defaults to the mumP2P identity dir. **Must be persistent.** |
| `gateway_id` | `OPT_GATEWAY_ID` | `dev-gateway` | Under join key: used as the **enrollment label** at first boot only. Set a unique value per host. Left at the default, the label is empty — see [Troubleshooting](04_troubleshoot.md#gateway-self-enrollment). After enroll, runtime `gateway_id` in `/health` and metrics comes from the JWT `sub` claim (`client_id`), not this value. |
| `gateway_cluster_id` | `OPT_GATEWAY_CLUSTER_ID` | *(required)* | Must match a cluster ID baked into the join key |
| `identity_mump2p_dir` | `OPT_IDENTITY_MUMP2P_DIR` | `/tmp/mump2p` | mumP2P identity — **persist as a volume**. Holds enrollment credential when `enroll_cred_dir` is unset |
| `identity_libp2p_dir` | `OPT_IDENTITY_LIBP2P_DIR` | `/tmp/libp2p` | libp2p identity — **persist as a volume** |
| `remote_auth_url` | `OPT_REMOTE_AUTH_URL` | `https://auth.getoptimum.io` | Auth service issuer |

All other keys (`agent_*_port`, `telemetry_*`, `direct_cl_peers`, `stream_*`, etc.) are the same as the [API key configuration](02_configuration.md#config-reference).

## What happens on boot

**First boot (no credential on disk):**

1. Gateway generates a P-256 keypair locally. The private key never leaves the host.
2. Gateway calls `POST https://auth.getoptimum.io/api/v1/gateways/enroll` with the join key and a proof-of-possession signature.
3. Auth returns a `client_id` (no secret). Gateway writes `enrollment.json` under `enroll_cred_dir` (mode `0600`).
4. Gateway mints JWTs by signing a client assertion — same bootstrap and mesh behaviour as the API-key path.

**Every restart after that:** gateway loads `enrollment.json` from disk and mints directly. No enroll call, no join-key use consumed.

```sh
docker pull getoptimum/gateway:v1.3.1

docker run -d --name optimum-gateway \
  --network host \
  -e OPT_JOIN_KEY=ojk_live_xxx \
  -e OPT_GATEWAY_ID=hoodi-validator-rack-03 \
  -v $(pwd)/config:/gateway/config \
  -v $(pwd)/data/libp2p:/data/libp2p \
  -v $(pwd)/data/mump2p:/data/mump2p \
  getoptimum/gateway:v1.3.1 \
  -config=/gateway/config/app_conf.yml
```

## Persistent storage

The enrollment credential lives alongside the mumP2P identity:

* Default location: `identity_mump2p_dir` / `enrollment.json`
* Override with `enroll_cred_dir` only if you need a separate mount — both dirs must survive restarts

Losing the credential directory means a new keypair, a new enrollment, and a consumed join-key use. With a stable enrollment label (`OPT_GATEWAY_ID` set per host), re-enrollment under the same label is refused while the old credential is still live — recovery is to revoke the orphan in the console.

## Scope from the join key

A gateway cannot elevate itself:

* **`type`** — e.g. `partner`; privileged types are gated at join-key mint time
* **`chain_id`** — Hoodi vs Mainnet
* **`cluster_ids`** — must include the cluster you set in `gateway_cluster_id`

Mint a join key whose cluster scope matches your deployment. A join key with no `cluster_ids` produces gateways that authenticate but fail mesh handshakes.

## Verify enrollment

```sh
curl -s http://localhost:48123/health | jq '.status, .gateway_id, .checks.auth'
curl -s http://localhost:48123/metrics | grep -E 'auth_enrollment_total|gateway_id'
```

On first boot you should see `auth_enrollment_total{result="success"}` increment once. On later restarts: `result="reused"`. Repeated `success` across a fleet usually means credential directories are not persisting.

The `gateway_id` in `/health` and metrics is the enrolled `client_id` (JWT `sub`), not the enrollment label you set in `OPT_GATEWAY_ID`.

## Migration from API keys

Existing `ogw_` gateways are unaffected. There is no forced migration.

To move a host to join-key enrollment:

1. Mint a join key with matching chain and cluster scope.
2. Remove `OPT_API_KEY`, set `OPT_JOIN_KEY` and a unique `OPT_GATEWAY_ID`.
3. Restart. The gateway enrolls as a **new** credential — revoke the old API key in the console when you decommission it.

## Next steps

* [Configuration](02_configuration.md) — ports, direct CL peers, stream settings
* [Kubernetes (Helm)](05_kubernetes.md) — join-key notes for fleet deployments
* [Troubleshooting](04_troubleshoot.md#gateway-self-enrollment) — enroll failures and credential issues
