# Design

## Context

See `proposal.md` (Why) and `specs/stream-block-publish/spec.md` for the
required behavior. Current code that shapes the approach:

- `GRPCServer.Subscribe` (`pkg/service/stream/grpc.go`) reads the first
  `SubscribeRequest` before authenticating, then authenticates from metadata and
  starts `recvTokens` on its own goroutine. The send loop is the only writer and
  the only place that returns a status. It applies refreshed tokens
  (`connAuth.accept`, which calls `Authenticate` again) and runs periodic
  re-auth (`connAuth.reverify`), which cuts a stream only in `enforce` mode
  (default `observe`). `offerLatest` may drop an older pending token.
- `ConsumerAuthenticator.Authenticate` returns only the subject. WS and gRPC both
  use it. `VerifyStreamToken` already returns full `jwks_verifier.Claims`
  (`GatewayClaims.Scope`, `ExpiresAt`); the verifier allows a 30 s clock skew.
- `processBeaconBlockArrival` (`gossipsub-gateway/beacon_block_measures.go`)
  stamps `lastBlockReceivedAt` and its metric for every source, decodes, calls
  the bootstrapper, emits to the hub (deduplicated per source), and returns
  `(slot, forward bool)`; a decode failure and a stale block both return
  `forward=false`. It is already called concurrently from the CL and mump2p
  ingest goroutines.
- `isDuplicateMessage` is a non-atomic `Get`+`Put` on a shared xxHash cache.
- `publishToCLTopic` returns `nil` when the topic is not subscribed.
  `ShouldForwardMessageToCLP2P` gates CL forwarding by the gateway's own type
  and token; `propagationDisabled` is the dynamic switch.
- Bootstrapper `HandleBeaconBlock` always creates a `LatencyComparator` entry in
  `trackedSlots`, fills it only for libp2p/mump2p, counts first-seen when the
  slot is new, and enqueues the slot for the centralized record.
- `cmd/main.go` builds the stream servers before the gateway service, because
  the gateway takes the hub as an emit sink; `srvGateway.Run()` runs before the
  stream servers start listening.
- Telemetry switches on `entities.Source` with only `libp2p` and `mump2p`
  branches; `IncParseSSZError` sends every non-libp2p source to the mump2p
  bucket; `RecordBlockPathArrival` takes `mumP2P bool`.
- In `stream_only` mode the CL host is not set up; mump2p runs.
- The gRPC server uses the default 4 MiB `MaxRecvMsgSize`;
  `consensus.MaxCompressedGossipSize()` is larger. `forkMgr` exposes the active
  digest but no slot-to-fork mapping.

## Goals / Non-Goals

**Goals:**
- Reuse the existing arrival, dedup and publish path; add no new queue or
  goroutine.
- Keep the read path, the WS transport, and existing clients unchanged.
- Keep `pkg/service/stream` free of an import on the gateway package.
- Keep the gateway's own liveness and latency signals independent of pushes.

**Non-Goals:**
- Proposer signature verification of pushed blocks.
- A push rate limit (clients are trusted; see Risks).
- Pushing attestations or other topics.
- A per-block ack frame (can be added to the `BlockEvent` oneof later).
- Changes to `LatencyComparator` or the centralized latency record.

## Decisions

### D1. Block field on `SubscribeRequest`, not a new RPC
Add `bytes block = 4;` to `SubscribeRequest`. Empty means "no block" (proto3
default). The stream is already bidirectional, so one connection carries reads,
token refreshes and pushes.
*Alternative:* a separate `Publish` RPC with an ack stream — cleaner split, but a
second stream for one client kind and no stated need for acks.

### D2. Authenticator returns an identity
Change `ConsumerAuthenticator.Authenticate(token) (Identity, error)` where
`Identity` holds `Subject`, `CanPublish bool` and `ExpiresAt time.Time`. The
JWKS implementation fills `CanPublish` from the `scope` grants and `ExpiresAt`
from the claims. `allowAllAuthenticator` returns `CanPublish: false`. WS ignores
the new fields.

### D3. One writer of the push identity
The receive goroutine is the **only** writer of `atomic.Pointer[Identity]` that
the push check reads. It is also the only place a refreshed token is verified:
- A message with a token: verify once (same-subject rule as today). On success,
  store the identity, then pass the token and verified identity to the send
  loop over the existing refresh channel, so `accept` no longer re-verifies.
  On failure: if the message also carries a block, deny; otherwise keep the old
  identity and count the auth failure, as `accept` does today.
- Periodic re-auth in the send loop **never stores**. When it fails, in any
  mode, it sets an `atomic.Bool` "push revoked"; the read path still follows
  `stream_reauth_mode`. This removes the race where a re-auth of an older token
  could restore a grant the client just dropped. The flag is sticky for the
  life of the stream: a later valid refresh does not clear it. The next push is
  denied, the stream closes, and the client reconnects with a fresh token.
- Push check: publisher set, identity `CanPublish`, `time.Now()` before
  `ExpiresAt + 30 s` (the verifier's skew), and push not revoked.
- On denial the receive goroutine counts `denied` once, sends the error on a
  1-buffered channel, and returns. The send loop selects on that channel and
  returns `PermissionDenied`.
- A block on the first message returns `InvalidArgument` before the receive
  goroutine starts. It is not a push and is not counted.
*Alternative:* route pushes through the send loop — it would serialize publishes
behind `stream.Send` and heartbeat writes to a slow client.

### D4. Publisher interface in the stream package
`stream` declares
`type BlockPublisher interface { PublishStreamBlock(data []byte) PushResult }`.
The gateway service implements it; `cmd/main.go` passes it to `NewGRPCServer`.
`main.go` builds the hub, then the gateway (`NewService` + `Run`), then the
gRPC server, so no push arrives before the gateway runs. The WS server keeps its
position. A nil publisher means publish is off; every push is denied.

### D5. One entry point in the gateway
`(*Service).PublishStreamBlock(data) PushResult`:
1. `processBeaconBlockArrival(..., SourceStream, "", "")`. Its return value
   becomes an explicit arrival status (`fresh`, `stale`, `invalid`) instead of
   `forward bool`; the two existing callers map `fresh` to today's `true`. For
   `SourceStream` it does not stamp `lastBlockReceivedAt` or its metric.
2. `isDuplicateMessage(data)` → `duplicate`.
3. Topic: the beacon-block topic of `forkMgr.ActiveDigest()`.
4. mump2p: `nodeMumP2P.PublishMessage` → `published` / `error`; `skipped` when
   mump2p is not up. No type gate, as for blocks from the CL.
5. CL: `skipped` if `propagationDisabled` or `ShouldForwardMessageToCLP2P` is
   false or the topic is not subscribed; else `publishToCLTopic` →
   `published` / `error`.
6. Return `PushResult{Outcome: accepted, CL: ..., MumP2P: ...}`.
The receive loop counts the result in one place.
*Alternative:* reuse `processMumP2PBeaconBlock` — it carries mump2p message
metadata and self-message rules that do not apply.

### D6. Topic from the active digest
The client sends bytes only. `forkMgr` has no slot-to-fork mapping, so the
gateway cannot cheaply reject a block of the other fork. Accepted risk below.

### D7. Config
`StreamPublishEnable bool yaml:"stream_publish_enable" env:"OPT_STREAM_PUBLISH_ENABLE" default:"false"`.
`Validate` rejects it with `stream_enable` off, `stream_require_auth` off, or
`stream_only` on.

### D8. Telemetry
- `entities.SourceStream = "stream"`.
- `IncBlocksFirstSeen`, `IncreaseBadMessages`, `IncParseSSZError`: explicit
  `stream` branch with its own counter or label.
- `RecordBlockPathArrival(source entities.Source, ...)` replaces `mumP2P bool`;
  the bootstrapper does not call it for `stream`.
- Bootstrapper `HandleBeaconBlock` for `stream`: no `LatencyComparator` entry,
  no enqueue. If the slot is in neither `trackedSlots` nor a small
  `streamFirstSlots` TTL set (same `syncx.TTLMap` and TTL as `trackedSlots`),
  count stream first-seen and add the slot to that set. A later libp2p/mump2p
  arrival creates the entry as today but skips its first-seen count when the
  slot is in `streamFirstSlots`.
- New counters: `stream_publish_total{outcome}` (`accepted`, `duplicate`,
  `stale`, `invalid`, `denied`) and
  `stream_publish_target_total{target,result}` (`cl`|`mump2p` ×
  `published`|`skipped`|`error`).

### D9. Receive size
`grpc.MaxRecvMsgSize(consensus.MaxCompressedGossipSize() + 64 KiB)` on the stream
server; 64 KiB covers protobuf framing and a JWT in the same message.

### D10. Grant constant
`GrantStreamPublish = "stream:publish"` belongs in `optimum-common` next to the
p2p grants. Until that release, keep a local constant in `auth_token` and parse
`scope` with the same SP-only split. The `aud=stream` check keeps this grant off
p2p tokens, so the p2p role fallback in `CanPublish` is unaffected.

## Risks / Trade-offs

- [No signature check and no rate limit: a client with the grant can push any
  bytes with a decodable header; dedup drops only byte-identical copies, and
  invalid blocks lower the gateway's gossipsub score at its CL peers] → push
  needs the flag (off by default), auth, and a `stream:publish` token; clients
  are trusted.
- [Fork boundary: a block of the other fork goes to the active digest's topic,
  CL peers REJECT it] → rare; clients can skip blocks whose stream `fork_digest`
  differs; a topic field can be added later.
- [xxHash dedup over compressed bytes misses re-encodings, and its `Get`+`Put`
  is not atomic, so two concurrent arrivals may both publish] → same limits the
  gateway already has between its CL and mump2p paths; a third caller adds no
  new class of failure.
- [A pushed block is emitted to all stream subscribers, including the pusher,
  before the duplicate check] → by design, flagged `source = "stream"`; clients
  that push must drop their own echoes; ADR-0011's trust note is amended.
- [Push revocation is sticky: a transient re-auth failure (for example a JWKS
  fetch error), or a re-auth that checks a token the send loop has not yet
  replaced, ends push rights for that stream] → the next push closes the stream
  and the client reconnects; reconnecting is cheap and keeps the rule simple.
- [The larger receive limit also applies to the first, unauthenticated
  message] → bounded by the existing connection caps and `maxConcurrentStreams`;
  the listener defaults to loopback.
- [Stream listener exposed beyond loopback] → `stream_grpc_addr` defaults to
  loopback; publish-enabled deployments keep it there.

## Migration Plan

1. Ship with `stream_publish_enable: false`; nothing changes for existing
   deployments or clients.
2. Mint `aud=stream` tokens with `scope: "stream:publish"` for allowed clients.
3. Enable the flag per host. Rollback: turn the flag off; pushes are then denied
   and reads continue.
