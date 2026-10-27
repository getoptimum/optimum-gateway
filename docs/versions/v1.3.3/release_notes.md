# v1.3.3

> **Current release.** v1.3.3 is the current release. v1.3.2 remains supported, but upgrading is recommended because it is affected by [GHSA-rcrq-vcj2-m787](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-rcrq-vcj2-m787). Networking and CL peering are unchanged — same ports and firewall rules.

## Highlights

* **Certificate-trust fix (medium severity).** The gateway is built with Go 1.27.3, which stops trusting a set of compromised certificate-authority intermediates (CVE-2026-111943). HTTPS calls from the gateway reject those chains. No partner configuration change.
* **Traffic stays on the current fork until your CL uses the next one** (new in v1.3.3). Consensus clients subscribe to the next fork a few minutes early. The gateway used to follow that subscription and publish attestations on the next-fork topic before the fork. It now stays on the current fork until a message arrives on the next one, then follows that. No partner configuration change.
* **Turning propagation off stops attestations too.** With propagation disabled, attestations from mump2p are no longer forwarded to your CL, the same as blocks. Traffic from your CL into mump2p is unchanged.
* **Health works with telemetry off.** `GET /health` answers when the metrics exporter is disabled. `mump2p_gateway_last_block_received_timestamp` is now set when a block arrives, not when `/health` is called.
* **Large beacon blocks are accepted.** The CL connection accepts a block up to the compressed size the gateway can decode, so a large block is not dropped before it is read.
* **Memory-exhaustion fix for aggregated attestations (medium severity, [GHSA-rcrq-vcj2-m787](https://github.com/getoptimum/optimum-gateway/security/advisories/GHSA-rcrq-vcj2-m787)).** A mump2p peer with publish scope could send an aggregated attestation pack with out-of-range subnet indexes and make every subscribed gateway allocate far more memory than the message size. The gateway now rejects the whole pack if any subnet is outside 0–63. v1.3.2 and earlier are affected.
* **The access token refreshes about every 15 minutes.** The gateway mints a new token on a 10–20 minute spread, so a change to gateway type or validator set shows up well before the six-hour token expires. Connected peers still learn the new handshake after a restart.

### Telemetry changes

* `mump2p_gateway_auth_token_mint_total`, `mump2p_gateway_auth_enrollment_total` and `mump2p_gateway_p2p_handshake_cluster_claim_total` export every `result` at 0 from startup, so alerts can target failure series directly. Rules built on `absent()` need review.
* `mump2p_gateway_app_build_info` moves to the new `paired_with` value when the gateway type changes; the old series is removed.
* Block latency: a gateway's own publish counts as its mump2p first-seen, and only the first copy of a block sets first-seen times.
* An accelerate-slots poll that returns no horizon no longer refreshes the freshness gauge.
* Auth mint counters advance about 12× faster with the shorter refresh interval. Revisit rate-based alerts.

Beacon blocks on every slot, join-key enrollment, and the consumer block stream carry forward from v1.3.2 unchanged.

## Upgrade from an earlier release

`docker restart` alone keeps the old image. Recreate the container:

```bash
export OPT_API_KEY=ogw_live_xxx   # unchanged if you use API keys
docker pull getoptimum/gateway:v1.3.3
docker rm -f optimum-gateway
docker run --name optimum-gateway --rm \
  -p 33212:33212/tcp \
  -p 33213:33213/tcp \
  -p 127.0.0.1:48123:48123/tcp \
  -e OPT_API_KEY=$OPT_API_KEY \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/data/libp2p:/tmp/libp2p \
  -v $(pwd)/data/mump2p:/tmp/mump2p \
  getoptimum/gateway:v1.3.3 \
  -config=/app/config/app_conf.yml
```

Confirm health and mesh:

```bash
curl -s http://localhost:48123/health | jq '.status, .checks'
curl -s http://localhost:48123/api/v1/self_info | jq '.mump2p.total_peers, .chain'
```

No firewall or CL peering changes are required. Join-key enrollment is **opt-in** — you only change credentials if you adopt the new fleet path.

If you run consumer streams, treat [Holding a stream open for weeks](06_block_stream.md#holding-a-stream-open-for-weeks) as the complete checklist for production always-on feeds. It covers client keepalives and in-band token refresh, and also reconnecting after `GOAWAY`, retrying `Internal` as well as `Unavailable`, and accounting for permanent gaps.

## Version Status

| Version     | Status                          |
| ----------- | ------------------------------- |
| v1.3.3      | **CURRENT**                     |
| v1.3.2      | supported / upgrade recommended |
| v1.3.1      | DEPRECATED / unsupported        |
| v1.3.0      | DEPRECATED / unsupported        |
| v1.2.0      | DEPRECATED / unsupported        |
| v1.1.1      | DEPRECATED / unsupported        |
| v1.0.2      | DEPRECATED / unsupported        |
| v0.0.1-rc12 | REMOVED / unsupported           |
| v0.0.1-rc11 | REMOVED / unsupported           |
