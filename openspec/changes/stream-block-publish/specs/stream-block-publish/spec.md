# Spec Delta

## Purpose

Lets a trusted client of the gRPC consumer block stream push beacon blocks into
the gateway, which relays them to its consensus client and to mump2p under the
block source `stream`.

## ADDED Requirements

### Requirement: Push a block over the gRPC stream
The gRPC `Subscribe` stream SHALL accept, on any client message after the first,
a beacon block as the gossip payload `snappy(SSZ(SignedBeaconBlock))`, bytes
only. An empty block field SHALL mean the message carries no block. The first
message SHALL keep selecting mode and topics, and a block on it SHALL end the
stream with `InvalidArgument`. When one message carries both a token and a
block, the gateway SHALL apply the token first and check the push against it.
The WebSocket transport SHALL stay read-only.

#### Scenario: Block on a later message
- **WHEN** an authorized client sends a block on its second `SubscribeRequest`
- **THEN** the gateway processes the block and the subscription keeps streaming

#### Scenario: Block on the first message
- **WHEN** a client's first `SubscribeRequest` carries a block
- **THEN** the gateway ends the stream with `InvalidArgument`

#### Scenario: Token and block in one message
- **WHEN** a client sends a refreshed token with `stream:publish` and a block in one message, and its previous token lacked the grant
- **THEN** the gateway accepts the push

#### Scenario: Invalid token and block in one message
- **WHEN** a client sends a token that fails verification, or names another subject, together with a block
- **THEN** the gateway keeps the previous identity, publishes nothing, and ends the stream with `PermissionDenied`

#### Scenario: Token-only message
- **WHEN** a client sends a message with a token and an empty block field
- **THEN** the gateway treats it as a token refresh only

#### Scenario: Existing read-only client
- **WHEN** a client that never sends a block subscribes as today
- **THEN** it receives the same frames as before this change

### Requirement: Push authorization
The gateway SHALL accept a push only when all hold: `stream_publish_enable` is
on, stream auth is on, the current token carries the `scope` grant
`stream:publish`, the current token has not expired (with the same clock-skew
tolerance the token verifier applies), and no periodic re-verification of the
current token has failed on this stream. A failed re-verification SHALL end push
rights for the rest of the stream; a later refresh SHALL NOT restore them. These
checks SHALL apply whatever
`stream_reauth_mode` is; the mode keeps governing the read path only. Any failed
condition SHALL end the stream with `PermissionDenied`. Reading the stream SHALL
need no grant.

#### Scenario: Flag off
- **WHEN** `stream_publish_enable` is off and a client with `stream:publish` pushes a block
- **THEN** the gateway ends the stream with `PermissionDenied` and publishes nothing

#### Scenario: Token without the grant
- **WHEN** a client whose token has no `stream:publish` grant pushes a block
- **THEN** the gateway ends the stream with `PermissionDenied`

#### Scenario: Expired token in observe mode
- **WHEN** `stream_reauth_mode` is `observe`, the client's token expired more than the skew tolerance ago, and the client pushes a block
- **THEN** the gateway ends the stream with `PermissionDenied`

#### Scenario: Re-verification failed in observe mode
- **WHEN** `stream_reauth_mode` is `observe`, a periodic re-verification of the current token fails, and the client then pushes a block
- **THEN** the gateway ends the stream with `PermissionDenied`, while a client that does not push keeps reading

#### Scenario: Refresh after a failed re-verification
- **WHEN** a periodic re-verification has failed, the client then sends a valid refreshed token with `stream:publish`, and pushes a block
- **THEN** the gateway ends the stream with `PermissionDenied`, and the client regains push rights only on a new connection

#### Scenario: Token downgrade
- **WHEN** a client with `stream:publish` refreshes to a token without the grant, a periodic re-verification runs, and the client pushes a block
- **THEN** the gateway ends the stream with `PermissionDenied`

#### Scenario: Read without grant
- **WHEN** a client with a valid `aud=stream` token and no `scope` subscribes
- **THEN** the subscription works as before

### Requirement: Publish-enable configuration
The gateway SHALL provide `stream_publish_enable` (`OPT_STREAM_PUBLISH_ENABLE`),
default `false`. Startup SHALL fail when it is on together with any of:
`stream_enable` off, `stream_require_auth` off, or `stream_only` on.

#### Scenario: Publish without stream
- **WHEN** the gateway starts with `stream_publish_enable: true` and `stream_enable: false`
- **THEN** config validation fails with an error naming both settings

#### Scenario: Publish without auth
- **WHEN** the gateway starts with `stream_publish_enable: true` and `stream_require_auth: false`
- **THEN** config validation fails with an error naming both settings

#### Scenario: Publish with stream-only
- **WHEN** the gateway starts with `stream_publish_enable: true` and `stream_only: true`
- **THEN** config validation fails with an error naming both settings

### Requirement: Processing of a pushed block
The gateway SHALL handle a pushed block as an arrival with source `stream`, in
this order: decode the header; record arrival telemetry and emit the block to
stream subscribers with `source = "stream"`; drop a block more than 3 slots
behind the current slot; drop a block it has already handled from any source.
Emit and telemetry SHALL happen before the duplicate check, as for every other
source. Otherwise the gateway SHALL publish the block to mump2p with no further
gate, and to the CL subject to the same propagation switch and forwarding rules
as a block from mump2p. The topic SHALL be the beacon-block topic of the active
fork digest. A pushed block SHALL NOT update the gateway's last-block-received
time or the health check that reads it.

#### Scenario: Fresh block
- **WHEN** an authorized client pushes a fresh block the gateway has not seen
- **THEN** the gateway publishes it to mump2p and to the CL, and subscribers receive it with `source = "stream"`

#### Scenario: Duplicate block
- **WHEN** a client pushes a block the gateway already received from the CL
- **THEN** subscribers receive it once with `source = "stream"`, the gateway publishes nothing, and counts the push as `duplicate`

#### Scenario: Stale block
- **WHEN** a client pushes a block more than 3 slots behind the current slot
- **THEN** the gateway publishes nothing and counts the push as `stale`

#### Scenario: Undecodable payload
- **WHEN** a client pushes bytes that are not a valid snappy-compressed beacon block
- **THEN** the gateway publishes nothing, counts the push as `invalid`, and keeps the stream open

#### Scenario: Propagation disabled
- **WHEN** CL propagation is disabled by dynamic config and a client pushes a fresh block
- **THEN** the gateway publishes it to mump2p only and counts the CL target as `skipped`

#### Scenario: Gateway type does not forward to the CL
- **WHEN** a gateway whose type does not forward this block to the CL receives a fresh push
- **THEN** it publishes the block to mump2p only and counts the CL target as `skipped`

#### Scenario: Health is not masked
- **WHEN** the CL and mump2p deliver no blocks while a client keeps pushing
- **THEN** the health check reports the last block as stale

### Requirement: Push outcome metrics
The gateway SHALL send no per-push reply. It SHALL count each push exactly once
by outcome: `accepted`, `duplicate`, `stale`, `invalid`, or `denied`. For each
accepted push it SHALL count the result per target (`cl`, `mump2p`) as
`published`, `skipped` (gated off or no subscribed topic), or `error`.

#### Scenario: Outcome counted
- **WHEN** an authorized client pushes a fresh block that both targets publish
- **THEN** the outcome `accepted` and the target results `cl/published` and `mump2p/published` each increase by one, and no extra frame is sent on the stream

#### Scenario: Denied push counted once
- **WHEN** a client without the grant pushes a block
- **THEN** the outcome `denied` increases by one

### Requirement: Source `stream` in telemetry
Every metric that splits beacon-block events by source SHALL report blocks with
source `stream` under their own label, not drop them or count them as another
source. A `stream` arrival SHALL NOT create, fill, or enqueue an entry of the
latency record sent to the centralized service; that record SHALL not change.

#### Scenario: First-seen by stream
- **WHEN** the first copy of a slot's block arrives by push
- **THEN** the first-seen counter for source `stream` increases by one

#### Scenario: Later network copies still recorded
- **WHEN** a slot's block arrives first by push and then from libp2p and mump2p
- **THEN** the latency record for that slot holds the libp2p and mump2p times as it would without the push, and the first-seen counters for libp2p and mump2p do not increase for that slot

#### Scenario: Parse error by stream
- **WHEN** a pushed block fails to decode
- **THEN** the parse-error and bad-message metrics count it under `stream`, not under mump2p

### Requirement: Largest gossip block fits
The gRPC stream server SHALL accept a client message as large as the largest
compressed gossip block plus protobuf framing and a token.

#### Scenario: Large block
- **WHEN** an authorized client pushes a valid block whose compressed size exceeds 4 MiB
- **THEN** the gateway receives and processes it without ending the stream
