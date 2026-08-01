# AGENTS.md — Optimum Gateway Developer Guide

## Project Overview

**Optimum Gateway** is a single-process gossipsub relay that bridges Ethereum Consensus Layer (CL) libp2p traffic with the mump2p mesh. It:

- Subscribes to Ethereum gossip topics (via local libp2p)
- Forwards messages to mump2p peers
- Re-publishes inbound mump2p messages back to the local CL

**Core responsibility**: Route and aggregate messages between two p2p networks while enforcing validator-level policies and collecting telemetry.

## Architecture Map

```sh
cmd/main.go
├── auth_token.Service          (JWT lifecycle: mint, verify, refresh)
├── message_router.Service       (Forward/drop decisions by validator & topic)
├── gossipsub_gateway.Service    (Main relay; runs two p2p loops)
│   ├── libp2p host + subscriptions  (CL-side gossipsub)
│   ├── mump2p.Node                  (mump2p-side mesh; pkg/service/mump2p)
│   ├── aggregator.Service           (Batch attestations into containers)
│   └── channels: clMessages, mumP2PMessages
├── telemetry service           (Prometheus, Loki, Mimir pushers)
└── HTTP router (Fiber)          (Metrics, /api/v1/*, /health)
```

**Message flow** (minimal):

1. CL sends → `clMessages` channel → message_router.ShouldForwardMessageToMumP2P
2. If yes: mump2p node publishes; optional aggregation for attestations
3. mump2p peer sends → `mumP2PMessages` channel → message_router.ShouldForwardMessageToCLP2P
4. If yes: Re-encode and publish back to local libp2p

## Critical Developer Workflows

### Build & Run

```bash
make build           # Builds into ./bin/optimum-gateway
make run             # go run cmd/main.go -config config/app_conf.yml
make test            # Go test suite; coverage is reported here, threshold checked by make coverage
make lint            # Installs/runs golangci-lint on ./... (see Makefile)
make proto           # Regenerate Go from .proto files
make vulcheck        # govulncheck with hardcoded exception list
```

### Proto & Code Generation

Proto files define gRPC and aggregation messages:

- `proto/getoptimum/optimum_gateway/service/aggregator/v1/aggregator.proto` — Batched message container

After editing, run `make proto` to regenerate `pkg/service/aggregator/*.pb.go`.

### Testing Strategy

Use `pkg/test_utils/*`:

- `local_bootstrap_server.go` — In-process HTTP bootstrap server (`NewLocalBootstrapServerWithRig`)
- `p2p_node.go` — Spawns libp2p hosts for testing
- `jwt_auth_claims.go` — Fixtures for JWT mint/verify tests

Most automated coverage is in regular Go package tests. The `integration/`
directory is Docker/manual-test support, not a `go test` package.

Example:

```go
import "github.com/getoptimum/optimum-gateway/pkg/test_utils"
rig := test_utils.NewAuthTestRig(t)
mb := test_utils.NewLocalBootstrapServerWithRig(t, rig)
node := test_utils.SpawnLocalCLLibP2PNode(ctx, t, remotePeer, port, identityDir)
```

## Project-Specific Conventions

### Configuration Precedence

YAML (`config/app_conf.yml`) < Environment variables (OPT_* prefix)

Key config fields:

- `chain` / `OPT_DEV_CHAIN` — Overridden by JWT's `chain_id` claim if auth enabled
- `gateway_id` / `OPT_GATEWAY_ID` — Overridden by JWT's `sub` claim if auth enabled
- `enable_auth` (default true) — Disables JWT mint/verify when false; logs an Info-level local-dev warning
- `api_key` / `OPT_API_KEY` — Required for auth-enabled minting; empty value disables auth manager even if `enable_auth=true`
- `propagation_enabled` — Startup seed only; dynamic config may flip the effective runtime value after `InitRuntime()`
- `aggregation_interval_ms` — Batch window for attestation aggregation
- `remote_push_enable` — Requires `telemetry_enable=true` and an enabled auth manager

### Topic Handling

All gateways are connected to same CL topics

- `"beacon_block"`
- `"beacon_attestation_{N}"` (N = 0..63)
All gateways are connected to same MUMP2P topics
- `"mump2p_aggregated_messages"` (aggregated attestations from all gateways)
- `"/eth2/<fork_digest>/beacon_block/ssz_snappy"` (full beacon topic form based on fork digest)
Gateway calls `filterAndBuildEthTopics()` to expand shorts into fulls using fork digest from bootstrap.

### Message Validation Rules

`message_router.Service` enforces:

- **Beacon blocks**: Always forward (any CL client can publish).
- **CL attestations**: Forward only when auth is valid, local gateway type is `partner`, validator is in `knownValidators`, and slot age passes the current max-age gate.
- **Inbound mump2p attestations**: Forward to CL for non-`relay` gateways.
- **Inbound mump2p beacon blocks**: Forward to CL only for `partner` gateways with a valid token.

### Telemetry & Logging

- Logger: Structured fields are added with `logger.With`* helpers.
- Metrics: Prometheus namespace/subsystem come from config; defaults are `mump2p` / `gateway`.
- Loki push: Active only when `remote_push_enable=true`, `telemetry_enable=true`, and auth is enabled (`enable_auth=true` with `OPT_API_KEY` present).
- Mimir push: Prometheus remote write over HTTP (`/api/v1/push`) using protobuf+snappy; endpoint comes from config.

### Error Handling in Loops

- **libp2p subscriptions** (`subscribe_nodes.go:handleSubscription`): Treats `ErrSubscriptionCancelled`, `context.Canceled`, and `context.DeadlineExceeded` as normal shutdown/cancel paths and returns without error spam.
- **Bootstrap retries**: Uses `utils.RetryPostRequest` / `utils.RetryGetRequest` (3 attempts, fixed 5s sleep, no jitter); falls back to GitHub raw content if bootstrap is unavailable or returns no peers.

## Integration Points & Dependencies


| Dependency                           | Purpose                                              | Notes                                           |
| ------------------------------------ | ---------------------------------------------------- | ----------------------------------------------- |
| github.com/libp2p/go-libp2p          | libp2p host & gossipsub                              | Local CL peering                                |
| github.com/getoptimum/mump2p-protocol | mump2p gossipsub (RLNC)                              | Used by `pkg/service/mump2p`                    |
| github.com/getoptimum/optimum-common | Shared types, logger, config                         | Utilities, auth claims                          |
| github.com/libp2p/go-libp2p-kad-dht  | mump2p peer discovery                                | Used by `pkg/service/mump2p/dhtdiscovery`       |
| github.com/prometheus/client_golang  | Metrics export                                       | Local Prometheus scrape                         |
| github.com/gofiber/fiber/v3          | HTTP API                                             | `/`, `/api/v1/self_info`, `/metrics`, `/health` |


## Security Trust Model

Three trust anchors (see SECURITY.md for details):

1. **Local CL** — `direct_cl_peers` configures pubsub direct peering and auto-reconnect. When non-empty, the gateway also enforces a connect-time peer ID allowlist (`onPeerConnected` disconnects non-listed peers). When empty, no peer-ID filtering applies; operators must firewall the libp2p port to the intended CL client
2. **mump2p fleet** — `ClusterID` is always enforced; JWT verification happens when auth is enabled
3. **Bootstrap server** — Returns initial peer set; both direct HTTP and fallback to GitHub `forkdigest-hub` repo

Auth disabled (`OPT_ENABLE_AUTH=false` or empty `OPT_API_KEY`) is development-only; the auth manager logs an Info-level startup message when disabled.

## Common Patterns & Pitfalls

### Forward/Drop Decisions

Always route through `message_router.Service`:

- Use `ShouldForwardMessageToMumP2P()` before sending CL → mump2p
- Use `ShouldForwardMessageToCLP2P()` before republishing mump2p → CL
- Both methods inspect `authMgr.OwnClaims()` to read gateway type

### Slot & Validator Tracking

- Current slot: `chainstate.CurrentSlot(time.Now())`
- Known validators: Synced via `bgSync()` in message_router; stored in `knownValidators` RWMap
- Skip stale attestations: Check slot age against `cfg.GetAttestationMaxSlotAge()` (current default 0 = same-slot only)

### Runtime Config Rotation

- `propagation_enabled` and `skip_messages_from_self` are atomics seeded from YAML/env, then updated by the dynamic-config rotator created in `InitRuntime()`
- Use `cfg.PropagationEnabled()` and `cfg.GetSkipMessagesFromSelf()` for current runtime values instead of reading raw config fields

### TTL Cache for Deduplication

libp2p messages and mump2p messages both cached via `messagesMap` (TTL map, 30s). **Never relies on hash alone** — also checks if already published to avoid loops.

### Graceful Shutdown

- Main loop blocks on signal channel; receives interrupt/SIGTERM
- Calls `appRouter.Stop()`, `srvGateway.Stop()` in order
- Waits for `lokiDone` and `mimirDone` channels to flush before exit

## File Structure Essentials

```sh
pkg/
├── config/          AppConfig{}, YAML+env parsing, InitRuntime()
├── routes/          Fiber HTTP server (`/`, `/health`, `/api/v1/self_info`, optional `/metrics`)
├── service/
│   ├── gossipsub-gateway/  Main relay loop (setup_*, subscribe_*, handle_*)
│   ├── message_router/     Forward/drop policies, validator sync
│   ├── auth_token/         JWT minting & verification
│   ├── aggregator/         Batch + emit attestation containers
│   ├── bootstrapper/       Remote bootstrap client, heartbeats, block latency
│   ├── mump2p/             mump2p libp2p node, handshake, topics
│   ├── telemetry/          Prometheus, Loki, Mimir remote write
│   └── jwks_verifier/      JWT JWKS caching & verification
├── protocol/        chain_state, consensus, forks, topics, fastssz_codegen
├── entities/        TopicKind, TopicMeta, CLMessage types
├── utils/           Topic helpers, bootstrap URL builders, retry helpers
└── test_utils/      Local bootstrap stub, p2p spawner, JWT fixtures
docs/versions/       Operator docs (VitePress; `latest/` tracks main)
cmd/main.go          Bootstrap flow, service initialization, HTTP server startup
```

## Debugging Tips

1. **Trace selectively**: `OPT_TRACE_MESH=true`, `OPT_TRACE_SHARD=true` in config (don't enable `OPT_TRACE_RPC` unless debugging handshake).
2. **Bootstrap fallback**: If bootstrap unreachable, check `remote_bootstrap_url` and GitHub `forkdigest-hub` repo.
3. **Validator sync failures**: `bgSync()` itself is silent; check auth mint responses / JWT-derived validator indexes and the `known_validators_total` metric.
4. **Propagation disabled**: The YAML/env flag only seeds startup state. Runtime behavior comes from the dynamic-config rotator (`cfg.GetDCRotator()`), which can flip `PropagationEnabled()` and `GetSkipMessagesFromSelf()`.

---

## Operating rules

### 1. Stay inside scope

Before editing, identify the requested behavior, the likely touched packages, and the non-goals.

Change only what is required to satisfy the task.
Do not add side refactors.
Do not rename unrelated things.
Do not move files because it feels cleaner.
Do not “improve” architecture unless the task explicitly includes it.

If a correct fix needs broader changes, say that before editing.

### 2. Respect existing conventions

Look at the nearest production code and tests in the same package first.

Match the repository’s established patterns for:

- naming
- package layout
- error handling
- logging via `logger.With`*
- context plumbing
- tests
- configuration access through `AppConfig` getters/runtime helpers
- dependency injection
- concurrency control

Do not introduce a new style when an existing one already solves the problem.

### 3. Prefer minimal change

Choose the smallest change that solves the problem.

Prefer:

- modifying existing flow
- reusing existing helpers
- extending existing types carefully
- deleting complexity when possible

Avoid:

- speculative abstractions
- new layers
- generic helpers added “for future use”
- broad rewrites

### 4. Make code readable

Write code that another engineer can follow quickly.

Prefer:

- clear names
- short control flow
- explicit error handling
- narrow responsibilities
- small helpers only when they remove duplication or clarify a single concept
- comments only where intent is not obvious

Avoid:

- clever compactness
- hidden side effects
- unnecessary indirection
- dense nested logic

### 5. Preserve invariants

Before changing behavior, identify invariants in the touched path.

Do not silently break:

- protocol assumptions
- lifecycle guarantees
- cleanup behavior
- locking discipline
- context cancellation semantics
- persistence assumptions
- existing API behavior

If an invariant must change, state the old behavior, the new behavior, and why the change is required.

### 6. Distinguish required change from optional improvement

You may notice other problems nearby.
That does not make them part of this task.

If they matter, report them in risks or follow-up notes.
Do not expand scope on your own.

### 7. Be honest about uncertainty

If code behavior is unclear or conflicts with research:

- stop
- report the conflict

Do not patch blindly.

### 8. Avoid unnecessary surface growth

Do not add new Go module dependencies, new top-level packages, or new public APIs unless the task requires them.
Prefer existing packages, config fields, and interfaces.

## Code quality rules

Your implementation should be:

- simple
- explicit
- locally understandable
- behavior-safe
- easy to review
- easy to test

Prefer:

- existing interfaces, config getters, and logger helpers over new ones
- explicit branching over magic helpers
- small helper extraction only when it reduces local complexity
- stable behavior over elegant rewrite
- focused tests close to the changed behavior

Avoid:

- generic wrappers with single use
- helper explosion
- comments that restate code
- hidden global state
- unnecessary concurrency changes
- new dependencies without task pressure
- premature optimization

---

## Failure conditions

Your work is considered poor if you:

- change unrelated files
- introduce new abstraction without pressure
- fail to explain changed behavior
- violate non-goals
- silently alter existing semantics
- produce vague report instead of exact changes
- hide uncertainty
- make code more complex than task requires

---

## Response style

- Be exact.
- Be concise.
- Be technical.
- Do not pad.
- Do not sell the code.
- Do not claim elegance.
- Do not hide weak points.

Use plain engineering language.

---

## Final instruction

Your implementation must make the codebase better in the narrowest way needed to solve the task.

Write less.
Change less.
Break less.
Explain exactly what changed.