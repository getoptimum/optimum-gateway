# Tasks

## 1. API and config

- [ ] 1.1 Add `bytes block = 4;` to `SubscribeRequest` in `proto/getoptimum/optimum_gateway/service/stream/v1/stream.proto`, with comments: empty means no block, a block on the first message is an error, token applies before block (D1); run `make proto` and verify `buf lint` passes and `go build ./...` succeeds
- [ ] 1.2 Add `StreamPublishEnable` (`stream_publish_enable` / `OPT_STREAM_PUBLISH_ENABLE`, default false) and its three `Validate` rules (needs `stream_enable`, needs `stream_require_auth`, not with `stream_only`) (D7); verify three failing and one passing case in `pkg/config/config_test.go`
- [ ] 1.3 Add `entities.SourceStream = "stream"`; verify `SourceStream.String() == "stream"` in `pkg/entities/handshake_test.go`

## 2. Authorization

- [ ] 2.1 Add the `stream:publish` grant constant and a scope check that sets `CanPublish` from verified stream claims (D10); verify unit tests for: no scope, `stream:publish`, `p2p:publish` only, `stream:publish` among other grants, NBSP-separated value
- [ ] 2.2 Change `ConsumerAuthenticator.Authenticate` to return `Identity{Subject, CanPublish, ExpiresAt}`; `allowAllAuthenticator` returns `CanPublish: false` (D2); update WS and gRPC callers and verify `go test ./pkg/service/stream/...` passes with no test expectation changed

## 3. Gateway push entry point

- [ ] 3.1 Change `processBeaconBlockArrival` to return an explicit arrival status (`fresh`, `stale`, `invalid`) and to skip `lastBlockReceivedAt` and its metric for `SourceStream`; update the CL and mump2p callers (D5); verify existing tests in `pkg/service/gossipsub-gateway` pass and a new test shows a stream arrival leaves `lastBlockReceivedAt` unchanged
- [ ] 3.2 Implement `(*Service).PublishStreamBlock(data) PushResult` per D5; verify tests for: accepted with both targets `published`; `duplicate` (block already seen from CL, still emitted once with `source = "stream"`); `stale`; `invalid`; propagation disabled → CL `skipped`; CL forwarding refused by gateway type → CL `skipped`; mump2p down → mump2p `skipped`
- [ ] 3.3 Verify in a test that a health check with only pushed blocks arriving reports the last block as stale

## 4. gRPC stream push path

- [ ] 4.1 Add `BlockPublisher`, `PushResult` and outcome types to `pkg/service/stream`; `NewGRPCServer` takes the publisher (nil = publish off) (D4); verify `go build ./...`
- [ ] 4.2 Reject a block on the first `SubscribeRequest` with `InvalidArgument`, uncounted; verify a grpc test
- [ ] 4.3 Replace `recvTokens` with a receive loop that is the single writer of the identity pointer and the single verifier of refreshed tokens, passes verified tokens to the send loop, checks push rights (publisher, `CanPublish`, `ExpiresAt` + 30 s, not revoked), calls the publisher, and on denial counts `denied` once, signals the send loop, and returns (D3); verify grpc tests: flag off (nil publisher), no grant, expired token in `observe` mode, token+block in one message, invalid token+block, token-only message, undecodable push keeps the stream open, read-only client unchanged
- [ ] 4.4 Make periodic re-auth set "push revoked" on failure in every mode and never store an identity (D3); verify tests: re-auth failure in `observe` denies the next push while reads continue; downgrade to a grant-less token followed by re-auth of the stream still denies the next push; a valid refresh after a failed re-auth does not restore push rights
- [ ] 4.5 Set `grpc.MaxRecvMsgSize` to `consensus.MaxCompressedGossipSize()` + 64 KiB (D9); verify a grpc test pushing a block larger than 4 MiB together with a token is received and processed
- [ ] 4.6 Run `go test -race ./pkg/service/stream/... ./pkg/service/gossipsub-gateway/...` and verify it passes with the tests from 4.3 and 4.4 included

## 5. Telemetry

- [ ] 5.1 Add `stream` branches to `IncBlocksFirstSeen`, `IncreaseBadMessages`, and `IncParseSSZError` (stop counting non-libp2p as mump2p) (D8); verify unit tests per source
- [ ] 5.2 Change `RecordBlockPathArrival` to take `entities.Source` and update its bootstrapper callers; verify `go test ./pkg/service/bootstrapper/... ./pkg/service/telemetry/...`
- [ ] 5.3 In bootstrapper `HandleBeaconBlock`, handle `stream` with no `LatencyComparator` entry or enqueue and a `streamFirstSlots` TTL set for first-seen (D8); verify tests: a stream-first slot counts stream first-seen once; later libp2p and mump2p arrivals fill the record as without the push and do not count their own first-seen
- [ ] 5.4 Add `stream_publish_total{outcome}` and `stream_publish_target_total{target,result}`, counted once per push in the receive loop (D8); verify tests read both counters for an accepted push and a denied push

## 6. Wiring and docs

- [ ] 6.1 In `cmd/main.go`, build the hub, then run the gateway, then build and start the gRPC server with the gateway as publisher when `StreamPublishEnable` is on (D4); verify `go build ./cmd/...` and that the gRPC server is constructed with a nil publisher when the flag is off (unit test or code review note)
- [ ] 6.2 Document `stream_publish_enable` and its three constraints next to the other `stream_*` settings in `config/sample.app_conf.yml`, and the push path in the docs version being prepared (`docs/versions/<next>/02_configuration.md` and `06_block_stream.md`); verify the setting name and env var appear in each file
- [ ] 6.3 Amend ADR-0011: the gRPC stream gains a write path, subscribers may receive client-pushed blocks flagged `source = "stream"`, and push rights end on any failed re-verification; verify the ADR-0011 status line and `docs/adr/README.md` row mention the amendment

## 7. Verification

- [ ] 7.1 Run `gofmt -l .`, `go vet ./...`, the project linter target, and `go test -race ./...`; verify all pass and report any unavailable check by name
