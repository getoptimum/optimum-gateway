# v1.0.2

> **Mandatory upgrade.** v1.0.2 is a **required** upgrade for all partners. It **replaces** all earlier releases — it does not supersede them side by side. Previous release candidates (rc11, rc12) are **no longer supported**. Move to v1.0.2 as soon as you have your API key.

## Highlights

* **API key authentication & onboarding.** Each gateway authenticates with an API key (`ogw_live_...`) generated in the [Optimum Partner Console](https://console.getoptimum.io/). Access is invite-only — the Optimum team onboards you first, then you sign in and generate a key. The key drives your `gateway_id`, `chain`, and validator scope, so there is no per-network YAML. **Set the key via the `OPT_API_KEY` environment variable, not in YAML.** Use a **separate key per gateway**. See [Quick Start -> Generate your API key](01_quick_start.md#generate-your-api-key).
* **Nimbus and Lodestar support.** Both consensus clients are now supported alongside Prysm, Lighthouse, and Teku.
* **Better Lighthouse / PeerDAS compatibility.** The gateway advertises a **custody group count of 8** (Fulu `VALIDATOR_CUSTODY_REQUIREMENT`) in its libp2p metadata so PeerDAS-aware clients keep the gateway as a useful peer. See the [Fulu p2p metadata spec](https://github.com/ethereum/consensus-specs/blob/master/specs/fulu/p2p-interface.md#metadata).
* **Improved health endpoints.** `GET /health` returns structured checks — `cl_peers`, `mump2p_peers`, `subscribed_topics`, `last_block_age_sec`, `cl_health`, `mump2p_health` — with 200/503 status. A new lightweight liveness endpoint `GET /` returns `{"status":"ok"}` for simple probes.
* **Attestation subnet carry + performance metrics.** The gateway subscribes to all 64 attestation subnets and forwards your partner validators' attestations across mump2p, with metrics for inclusion rate, packing, inclusion delay, and end-to-end propagation latency.
* **New metric namespace.** Gateway metrics are now prefixed **`mump2p_gateway_`** (previously `optp2p_gateway_optimum_gateway_`). Update any saved Prometheus queries or Grafana dashboards. Metrics also carry `org_id` and your API key's metadata labels (e.g. `gateway_label`). See [Metrics](metrics.md).
* **Simpler configuration.** Removed `enable_aggregation`, the `eth_topics_subscribe` list (topics are baked in), the sidecar port (`33211`), and the separate remote-push client ID/secret. Remote push now reuses the API-key JWT as its Bearer token.
* **Security hardening.** Verified JWT lifetime is bounded, JWKS refresh is more frequent, and tokens are routed by audience.

## Upgrade

1. Generate an API key in the [Partner Console](https://console.getoptimum.io/) (one per gateway).
2. Update your `app_conf.yml` to the v1.0.2 shape: drop `chain`, `gateway_id`, `partner_id`, `enable_aggregation`, `eth_topics_subscribe`, the `33211` port, and the remote-push client ID/secret. See [Configuration](02_configuration.md).
3. Set the key via the environment and pull/restart:

   ```bash
   export OPT_API_KEY=ogw_live_xxx
   docker pull getoptimum/gateway:v1.0.2
   docker restart optimum-gateway
   ```

4. Update Prometheus/Grafana queries to the `mump2p_gateway_` prefix.

## Version Status

| Version     | Status                 |
| ----------- | ---------------------- |
| v1.0.2      | **CURRENT — required** |
| v0.0.1-rc12 | REMOVED / unsupported  |
| v0.0.1-rc11 | REMOVED / unsupported  |
