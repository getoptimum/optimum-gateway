# Connecting your CL client

The gateway keeps one libp2p session with your beacon node, on TCP `33212`. This page is the setup for that session and the fixes when it drops. Prysm, Lighthouse, Teku, Nimbus, and Lodestar are all supported.

::: info Current stable releases
Peering flags are the same on these releases. For Hoodi and mainnet, run:

* **Prysm v7.2.1**
* **Lighthouse v8.2.3** — `v8.3.0-rc.0` is a Sepolia release candidate
* **Teku 26.9.1** — stay on **26.4.0** or newer; older Teku throws `NoSuchElementException` at `MetadataDasPeerCustodyTracker` while exchanging metadata
* **Nimbus v26.10.0**
* **Lodestar v1.49.0** — `--directPeers` has been in the client since v1.40.0

:::

## Which ID goes where

| Value | Where you read it | Where it goes |
| ----- | ----------------- | ------------- |
| Gateway `peer_id` | `GET /api/v1/self_info` → `.peer_id` | The CL flag below. Lighthouse `--trusted-peers` takes this ID alone. The other clients take it inside the multiaddr. |
| CL peer ID | `GET /eth/v1/node/identity` → `.data.peer_id` | Gateway `direct_cl_peers` only |
| `mump2p.peer_ids` | `self_info.mump2p.peer_ids` | The Optimum mesh. No CL flag uses it. |
| `libp2p.peer_ids` | `self_info.libp2p.peer_ids` | Peers already connected to the gateway, usually your CL after the session is up |

::: warning Two different peer IDs
`--trusted-peers`, `--peer`, `--p2p-direct-peers`, `--direct-peer`, and `--directPeers` take the **gateway** `peer_id`. The beacon node's own `.data.peer_id` is the value you put in `direct_cl_peers`. Pasting the beacon ID into the CL flag leaves `cl_peers` at 0.
:::

## Do you set `direct_cl_peers`

The gateway uses that list for two things, and only when it is non-empty.

* It dials each entry and keeps retrying. That is what brings the session back after a gateway restart.
* It treats the list as an allowlist. Any other libp2p peer is disconnected as soon as it connects.

An empty list means the gateway dials nobody and applies no peer-ID filter. The CL has to dial the gateway, and the firewall on `33212` is the only restriction. `33212` stays private to the CL. `33213` is the port the Optimum mesh uses from the internet.

The field is a list. A second beacon node is fine, and once any entry is set, every beacon node that should stay connected has to be on the list. One that is missing is disconnected.

| Client | Set `direct_cl_peers`? | Why |
| ------ | ---------------------- | --- |
| Prysm | Optional | Prysm re-dials `--peer` on its own. If you do set the list, Prysm's peer ID has to be on it. |
| Lighthouse | Yes | Lighthouse does not re-dial after a gateway restart. The gateway's retry is what brings it back. |
| Teku | Yes | Teku treats `--p2p-direct-peers` as reciprocal. This list is the gateway's half, and the retry. |
| Nimbus | Yes | Same reciprocal direct peer, and Nimbus does not re-dial after a gateway restart. |
| Lodestar | Yes | Lodestar treats `--directPeers` as reciprocal. This list is the gateway's half. |

On Docker you can leave the list empty until the CL identity exists, then add it and restart the gateway. On Helm the chart rejects an empty `gateway.directClPeers`, so the order below is reversed: read the CL identity first, install with that list, then point the CL at the gateway.

## Order

The gateway ID and the CL ID do not both have to exist before either process starts.

1. Mount `identity_libp2p_dir` and `identity_mump2p_dir` as volumes. Without the libp2p volume the gateway `peer_id` changes on every restart and the CL flag goes stale.
2. Start the gateway.
3. Read the gateway ID and build a multiaddr on an address the CL can route:

    ```sh
    curl -s http://localhost:48123/api/v1/self_info | jq -r '.peer_id, .libp2p.multiaddrs[]'
    ```

    ```text
    /ip4/<address-the-cl-can-route>/tcp/33212/p2p/<gateway-peer-id>
    ```

4. Start the CL with the flag for that client.
5. Add the CL's own multiaddr to `direct_cl_peers` and restart the gateway. Prysm can skip this. Lighthouse, Teku, Nimbus, and Lodestar should not. On Helm this step already happened at install.

<svg viewBox="0 0 980 168" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Gateway starts, the CL dials it with the gateway peer ID, then the gateway can dial the CL" style="width:100%;height:auto;max-width:980px;display:block;margin:1.25rem auto;">
  <defs>
    <marker id="cl-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse" markerUnits="userSpaceOnUse">
      <path d="M0,0 L10,5 L0,10 L2.2,5 Z" fill="currentColor" fill-opacity="0.55"></path>
    </marker>
  </defs>
  <line x1="250" y1="78" x2="338" y2="78" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#cl-arrow)"></line>
  <line x1="630" y1="78" x2="718" y2="78" stroke="currentColor" stroke-opacity="0.55" stroke-width="1.25" marker-end="url(#cl-arrow)"></line>
  <rect x="24" y="36" width="226" height="84" rx="16" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32"></rect>
  <text x="137" y="68" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="13" font-weight="600" fill="currentColor">1. Gateway up</text>
  <text x="137" y="90" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="12" fill="currentColor" fill-opacity="0.72">persisted peer_id</text>
  <rect x="348" y="28" width="282" height="100" rx="16" fill="#B87CFF" fill-opacity="0.08" stroke="#B87CFF" stroke-width="1.75"></rect>
  <text x="489" y="64" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="13" font-weight="600" fill="currentColor">2. CL dials the gateway</text>
  <text x="489" y="86" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="12" fill="currentColor" fill-opacity="0.72">gateway peer_id in the CL flag</text>
  <text x="489" y="106" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="12" fill="currentColor" fill-opacity="0.72">TCP 33212</text>
  <rect x="728" y="36" width="228" height="84" rx="16" fill="currentColor" fill-opacity="0.035" stroke="currentColor" stroke-opacity="0.32"></rect>
  <text x="842" y="68" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="13" font-weight="600" fill="currentColor">3. Gateway retries</text>
  <text x="842" y="90" text-anchor="middle" font-family="Geist, ui-sans-serif, system-ui, sans-serif" font-size="12" fill="currentColor" fill-opacity="0.72">direct_cl_peers</text>
</svg>

::: info Address the CL can actually route
`libp2p.multiaddrs[0]` is often an address the CL cannot dial: a Docker bridge address in `172.16.0.0/12`, a Podman address in `10.89.0.0/24`, or a Kubernetes pod IP. Put an `/ip4/` address the CL can route into the CL flag. A `/dns4/<name>/...` multiaddr works in that flag when the CL itself resolves the name, and it works in `direct_cl_peers` because the gateway parses it as a libp2p multiaddr. See [Kubernetes](05_kubernetes.md#point-your-cl-client-at-the-gateway).
:::

Open TCP `33212` only toward the CL. Leave it off the public internet. `33213` is the mesh port.

## Same host

Gateway and CL on one machine can both use `127.0.0.1` when the CL is on the host network and the gateway publishes `33212` to the host, as in the [Quick Start](01_quick_start.md#run).

The CL flag takes:

```text
/ip4/127.0.0.1/tcp/33212/p2p/<gateway-peer-id>
```

After the CL is up, `direct_cl_peers` takes the CL's own P2P port:

```text
/ip4/127.0.0.1/tcp/<cl-p2p-port>/p2p/<cl-peer-id>
```

Restart the gateway after adding that line. On Helm, skip this and follow [Kubernetes](05_kubernetes.md): the CL peer ID goes into `gateway.directClPeers` before install.

## Prysm

```sh
./beacon-chain \
  --peer=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID
```

`--peer` may be repeated. Each value is a trusted peer. It accepts a multiaddr, an ENR, or an enode. Prysm re-dials trusted peers on its own, so `direct_cl_peers` is optional. If you set the list, Prysm's peer ID has to be on it or the gateway disconnects Prysm. Prysm's own P2P port in that multiaddr is `13000`, and its identity HTTP port is `3500`.

::: info Prysm v7.2
Partial data columns are on by default. That does not change the `--peer` flag. `--disable-partial-data-columns` turns them off.
:::

## Lighthouse

```sh
lighthouse beacon_node \
  --boot-nodes=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --trusted-peers=YOUR_GATEWAY_PEER_ID
```

`--boot-nodes` takes the multiaddr (or an ENR), comma-separated if you have more than one. `--trusted-peers` takes the gateway peer ID alone, also comma-separated. `--libp2p-addresses` is the old name for `--boot-nodes` and is deprecated.

`--trusted-peers` keeps that peer at the top of Lighthouse's score, so the peer-table prune leaves it alone. One trusted gateway against the default target is the normal setup.

::: info PeerDAS, separate from the peering flags
`--semi-supernode` makes the beacon node custody half of the data columns so it can reconstruct blobs. It does not create the gateway session.

`--target-peers` is the size of the peer table. Lighthouse prunes down toward it. Raise it (for example `--target-peers=500`) only when the gateway is being dropped because the table is full. If the number of trusted peers reaches the target, Lighthouse warns that peer selection gets worse.
:::

::: warning After a gateway restart
Lighthouse does not re-dial. Once Lighthouse has a stable identity, add it to `direct_cl_peers` so the gateway keeps retrying. Lighthouse's P2P port is `9000` unless you changed it. Its identity HTTP port is `5052`.
:::

## Teku

```sh
teku \
  --p2p-direct-peers=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID
```

`--p2p-direct-peers` is a static peer that keeps exchanging full messages. Teku's own docs treat it as reciprocal: the gateway side of that pair is `direct_cl_peers`, added once you have Teku's peer ID. The first session can come up with only the flag above.

::: warning Static peers are pruned
`--p2p-static-peers` without `--p2p-direct-peers` can be dropped when Teku's peer table is full. Use `--p2p-direct-peers`.
:::

Teku's P2P port is `9000`. Its identity HTTP port is `5051`.

## Nimbus

```sh
nimbus_beacon_node \
  --direct-peer=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID \
  --netkey-file=/data/netkey
```

::: warning A random netkey drops the direct peer
`--direct-peer` is ignored when `--netkey-file` is `random`, which is the default. Point it at a file Nimbus can reuse. The peer ID in `direct_cl_peers` is then stable. Nimbus treats the direct peer as reciprocal, so add Nimbus to `direct_cl_peers` as well. Open `33212` on the gateway to Nimbus, and open Nimbus's P2P port to the gateway.
:::

```sh
curl -s http://localhost:5052/eth/v1/node/peers/YOUR_GATEWAY_PEER_ID | jq '.data | {state, agent}'
```

That curl needs `--rest` on Nimbus. The REST server is off by default, and when it is on it listens on `5052`.

A connected gateway shows `"state": "connected"` and an agent containing `optimum-gateway`.

::: info Warmup
Nimbus can take a few hours to settle. It drops a peer when the connection closes or the score falls, then reconnects. A short flap in `cl_peers` during that window is expected. A peer that stays `disconnected` is the netkey or the firewall, not the warmup.
:::

## Lodestar

```sh
lodestar beacon \
  --directPeers=/ip4/YOUR_GATEWAY_IP/tcp/33212/p2p/YOUR_GATEWAY_PEER_ID
```

`--directPeers` keeps a GossipSub direct peer, as a multiaddr or an ENR. Lodestar documents it as reciprocal, so add Lodestar's peer ID to `direct_cl_peers` once you have it. The flag has been in the client since v1.40.0. Lodestar's P2P port is `9000`. Its REST port is `9596`. Nimbus is not on that port.

## `direct_cl_peers`

This is the CL multiaddr the gateway dials, and keeps dialing after a gateway restart. Lighthouse and Nimbus need it for that reconnect. Teku, Nimbus, and Lodestar also use it as the reciprocal half of a direct peer.

```yaml
direct_cl_peers:
  - /ip4/YOUR_CL_IP/tcp/YOUR_CL_P2P_PORT/p2p/YOUR_CL_PEER_ID
```

Read the CL identity from the beacon REST API, then use the **P2P** port in the multiaddr, not the HTTP port you queried. Teku's REST API is off until `--rest-api-enabled`. Nimbus's is off until `--rest`. A refused curl is the beacon API, not the gateway.

```sh
curl -s http://localhost:5052/eth/v1/node/identity | jq '.data.peer_id, .data.p2p_addresses[0]'  # Lighthouse, or Nimbus with --rest
curl -s http://localhost:3500/eth/v1/node/identity | jq '.data.peer_id, .data.p2p_addresses[0]'  # Prysm
curl -s http://localhost:5051/eth/v1/node/identity | jq '.data.peer_id, .data.p2p_addresses[0]'  # Teku with --rest-api-enabled
curl -s http://localhost:9596/eth/v1/node/identity | jq '.data.peer_id, .data.p2p_addresses[0]'  # Lodestar
```

| Client | P2P port in `direct_cl_peers` | Identity HTTP port |
| ------ | ----------------------------- | ------------------ |
| Prysm | `13000` | `3500` |
| Lighthouse | `9000` | `5052` |
| Teku | `9000` | `5051`, after `--rest-api-enabled` |
| Nimbus | `9000` | `5052`, after `--rest` |
| Lodestar | `9000` | `9596` |

::: warning Allowlist
When `direct_cl_peers` is set, the gateway disconnects any libp2p peer whose ID is missing from that list, including a second beacon node you forgot to add. A wrong peer ID means `cl_peers` stays 0 even though the CL is dialing `33212`. An empty list applies no peer-ID filter. Either way, open `33212` only to the CL.
:::

The gateway advertises a custody group count of 8 in its libp2p metadata so PeerDAS clients keep it as a useful peer. That advertisement is in the binary. There is no custody flag to set on the gateway. See the [Fulu p2p metadata spec](https://github.com/ethereum/consensus-specs/blob/master/specs/fulu/p2p-interface.md#metadata).

## Verify

```sh
curl -s http://localhost:48123/health | jq '{status, cl_peers: .checks.cl_peers, cl_health: .checks.cl_health}'
curl -s http://localhost:48123/api/v1/self_info | jq '{peer_id, total: .libp2p.total_peers, peers: .libp2p.peer_ids}'
```

A peered CL is `cl_peers.value` of at least 1 and `libp2p.total_peers` of at least 1. Expect that within a couple of minutes of the CL flag being loaded. On a `stream_only` gateway both CL checks are `skipped`.

::: warning `cl_health` is not the peer count
`cl_health: 1` means some CL-path gossip arrived in the last 30 seconds. Publishes from the mesh can tick it while `libp2p.total_peers` is 0. Alert on `mump2p_gateway_cl_peers == 0`, not on `cl_health`. The rules are in [Telemetry](03_telemetry.md#alerts-to-set).
:::

`cl_peers` can be ok while `last_block_age_sec` fails. The session is up and the beacon node is not delivering blocks, usually because the execution client is still syncing. Prysm can sit in optimistic sync in that state. Wait for the execution client, then confirm the beacon node is serving. Teku logs `Payload marked as invalid by Execution Client` for the same wait.

## If the link fails

| What you see | What it is | What to change |
| ------------ | ---------- | -------------- |
| `cl_peers` stays 0 | CL is not dialing a reachable address, or the allowlist rejects it | Rebuild the multiaddr. Open TCP `33212` to the CL only. If `direct_cl_peers` is set, every CL peer ID in it has to match `.data.peer_id`. |
| CL flag was copied from `.data.peer_id` or from `mump2p.peer_ids` | Wrong ID | Replace it with `self_info.peer_id`. |
| Worked, then died after a gateway restart | Lighthouse or Nimbus did not re-dial, or the peer ID changed | Persist the identity volume. Add `direct_cl_peers`. |
| Worked, then the peer ID in `self_info` changed | Identity directory was not a volume | Mount `identity_libp2p_dir` and update the CL flag. |
| Lighthouse says goodbye, or drops the gateway | Peer table prune, or a custody metadata mismatch on an old gateway build | `--boot-nodes` plus `--trusted-peers` as above, on gateway v1.3.2. `--semi-supernode` does not fix the session. |
| Teku `NoSuchElementException` at `MetadataDasPeerCustodyTracker` | Teku older than v26.4.0 | Upgrade. Current stable is 26.9.1. |
| Teku connects, then the gateway disappears | `--p2p-static-peers` only | Switch to `--p2p-direct-peers`. |
| Nimbus logs `Adding privileged direct peer` and never connects | Firewall, or the netkey is `random` | Stable `--netkey-file`, both ports open, public or routable IP in `direct_cl_peers`. |
| Nimbus REST shows the gateway `disconnected` | Same netkey or allowlist problem | `--netkey-file` plus `direct_cl_peers`. |
| `cl_peers` ok, no blocks | Execution client still syncing | Wait. This is not a peering flag. |
