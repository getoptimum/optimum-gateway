# Proposal

## Why

The consumer block stream (ADR-0011) is read-only: a client can observe beacon
blocks but cannot hand one to the gateway. A trusted client may obtain a beacon
block elsewhere, and the gateway should relay that block to its consensus
client (CL) and to mump2p the same way it relays blocks from either network
today.

## What Changes

- The gRPC `Subscribe` stream accepts a beacon block from the client: the gossip
  payload `snappy(SSZ(SignedBeaconBlock))`, bytes only. WebSocket stays
  read-only.
- New block source `stream`. A pushed block runs through the existing arrival
  path (decode, stale check, stream emit, dedup) and is published to mump2p and,
  through the existing CL forwarding gates, to the CL.
- Push is allowed only when all hold: the new config flag
  `stream_publish_enable` is on, stream auth is on, the current token carries
  the new `scope` grant `stream:publish`, the token has not expired, and no
  re-verification of it has failed. Any other push closes the stream with
  `PermissionDenied`.
- The stream verifier starts reading the `scope` claim of `aud=stream` tokens.
  Reading the stream still needs no grant.
- No per-block acknowledgement. A new metric counts each push outcome, and the
  publish result per target network (CL, mump2p).
- Every telemetry function that switches on the block source gains a `stream`
  branch. Pushed blocks do not feed the gateway's own liveness signal (health
  check, last-block timestamp) or the centralized latency record.
- The gRPC server raises its receive limit above the largest gossip block.
- **BREAKING** (internal API only): `stream.ConsumerAuthenticator.Authenticate`
  returns the verified claims needed for the push check, not just the subject.

## Capabilities

### New Capabilities
- `stream-block-publish`: an authorized gRPC stream client pushes beacon blocks
  that the gateway publishes to the CL and mump2p under the source `stream`.

### Modified Capabilities

None. No main specs exist yet under `openspec/specs/`; the stream's read path
is unchanged.

## Impact

- Code: `pkg/service/stream` (gRPC server, auth), `pkg/service/gossipsub-gateway`
  (push entry point, arrival path), `pkg/service/bootstrapper`, `pkg/entities`
  (`SourceStream`), `pkg/service/telemetry`, `pkg/config`, `cmd/main.go`
  (wiring order).
- API: `proto/.../stream/v1/stream.proto` gains a block field on
  `SubscribeRequest`; regenerated `stream.pb.go`. Existing clients are
  unaffected.
- Dependency: `optimum-common` gains `GrantStreamPublish = "stream:publish"`
  (can start as a local constant until released).
- External: the auth service must mint `aud=stream` tokens with
  `scope: "stream:publish"` for clients allowed to push.
- Docs: ADR-0011 is amended — the gRPC stream gains a write path, and
  subscribers may receive client-pushed blocks.
