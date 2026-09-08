# ADR-0013: Gateway self-enrollment with an org join key

**Status:** Draft
**Date:** 2026-09-08
**Related:** [ADR-0011](./0011-gateway-consumer-block-stream.md)

---

## Context

A gateway authenticates with an `ogw_` API key: one secret per host, minted in the
Partner Console and exchanged at `/api/v1/auth/token` for the token pair.

That does not scale to a fleet. An operator running N gateways mints N keys,
downloads a CSV, and maps key to host by hand. The failures follow from the
mapping being manual: one key deployed twice, so the second gateway collides on
registration; a Hoodi key against a Mainnet cluster, which authenticates and then
matches no topics; a key regenerated and the old one left live.

The secret also has to travel. Every key is minted somewhere else and carried to
the host that will use it.

## Decision

An operator mints one reusable, org-scoped **join key** (`ojk_`) and configures it
on every gateway. Each gateway then enrolls itself once, on first boot:

1. Generate a P-256 keypair. The private half never leaves the host.
2. `POST {issuer}/api/v1/gateways/enroll` with the public JWK, the join key, and an
   `enroll_assertion` proving possession of the private half. The response carries
   a `client_id` and **no secret**.
3. Persist the keypair and `client_id`, and from then on mint tokens by signing an
   RFC 7523 `private_key_jwt` client assertion.

`type`, `chain_id` and `cluster_ids` all come from the join key, so a gateway
cannot self-assign a privileged type or admit itself to a cluster.

### Config

```yaml
join_key: ojk_live_...      # OPT_JOIN_KEY
# enroll_cred_dir: /tmp/mump2p   # OPT_ENROLL_CRED_DIR, defaults to identity_mump2p_dir
```

`api_key` and `join_key` are mutually exclusive, and setting both is a startup
error rather than a silent precedence rule, so a half-migrated host fails loudly.
A gateway with neither behaves exactly as it does today.

`enroll_cred_dir` defaults to the mumP2P identity directory because that is
already a persistent mount and already holds the peer identity the credential is
bound to.

### The credential on disk

`enrollment.json`, mode `0600`, written through the same atomic CRC64-framed
writer as the peer identity. It holds the PKCS#8 private key, the `client_id`, the
JWK thumbprint, and the `peer_id` enrolled with.

The thumbprint is re-derived on load and compared. A file pairing a `client_id`
with the wrong key would otherwise look healthy at boot and fail three hours later
as an opaque 401.

The keypair is written to `enrollment.key` **before** the enroll POST and removed
once the credential is saved. If a response is lost after the server committed,
the retry presents the same public key, so the server's idempotency returns the
credential it already issued instead of enrolling a second one. Without this a
stable `gateway_id` makes the retry collide with its own orphaned label, which is
terminal, and the gateway never boots again.

## Why asymmetric

The alternative was a join key that mints a per-host `ogw_` secret. Rejected: it
puts a replayable secret on the wire and at rest, and gains nothing, because the
join key is already a bearer credential of equal power.

The proof of possession does not authenticate the caller. It binds the submitted
public key to its private half, so nobody can enroll a key they do not control.

## Protocol details that are load-bearing

**Coordinates are fixed-width.** The JWK's `x` and `y` are sliced from the SEC 1
uncompressed point, not from `big.Int.Bytes()`. The latter drops a leading zero
byte, which happens for roughly 1 key in 256, producing a 42-character coordinate
that the server's 43-character validation rejects. A naive implementation fails
*intermittently*. `TestCoordinatesAreFixedWidth` pins it.

**JWK member order is the canonical order.** The RFC 7638 thumbprint is the SHA-256
of the JSON with members in lexicographic order, and the struct's field order is
what produces it. Reordering the fields silently changes every thumbprint we
compute. `TestThumbprintMatchesJose` pins the result against a vector from the
server's own library.

**Assertion audiences derive from the issuer, never the request URL.** The server
builds the expected audience from its signer issuer, so a gateway posting to any
other host would sign the wrong audience. The enroll and token audiences differ,
which is what stops an enrollment proof being replayed to mint tokens.

**`client_assertion_type` is required.** Omitting the URN is a 400, not a 401.

**`peer_id` travels inside the signature** on the mint, where the server prefers
the signed claim and rejects a mismatch against the body.

## Failure modes

| What happened | What the gateway does |
| ------------- | --------------------- |
| No credential file | Enrolls, consuming one join-key use |
| Credential present | Reuses it. No network call, no use consumed |
| Enroll response lost after the server committed | Retries with the pending keypair, so the server returns the credential it already issued. No second use |
| Pending keypair unreadable | Generates a new one. Costs a use rather than the ability to boot |
| Credential corrupt or unreadable | Fails to start. Re-enrolling would burn a use and orphan the old credential |
| mumP2P identity changed under an existing credential | Fails to start, naming both peer IDs. Every mint would otherwise 401 with nothing pointing at the cause |
| Join key unknown, expired, exhausted, revoked | `401`, terminal. Not retried |
| Host clock more than ~2 min slow | The assertion is already expired, so also `401`. Indistinguishable from a bad join key, because upstream collapses both. A fast clock is not bounded server-side |
| Label already live in the org, or org at its key cap | `409`, terminal. Needs an operator, not a retry |
| Credential directory not writable | Fails before contacting the server, so no credential is orphaned upstream |
| Transient `401` on a later mint | Retried with backoff. On the assertion path a `401` is also every verification failure, including an assertion that expired in flight |
| `403` revoked or suspended | Terminal on both grants |

The `409` codes need optimum-auth to distinguish them; until then a duplicate
label arrives as a `401` and points at the wrong thing.

## Consequences

* One credential is distributed to a fleet instead of one per host, and no gateway
  secret is transmitted or stored server-side.
* Enrollment is idempotent on the thumbprint upstream, and persisting the keypair
  before the POST is what makes that reachable across a restart. A lost response
  therefore costs nothing: the retry presents the same key and gets the same
  credential back. The cost is a second file in the credential directory.
* Losing the credential directory behaves differently depending on `gateway_id`,
  and neither outcome is good. The shipped sample leaves it unset, so the label is
  empty and exempt from the unique index: the gateway silently enrolls a fresh
  credential on every boot, spending a join-key use and orphaning the last one,
  until the org key cap turns it into a terminal `gateway_key_limit`. With
  `gateway_id` set the label is stable, so the first re-enrollment collides with
  the still-live credential and is refused as a conflict, which is terminal
  immediately. Recovery is to revoke the orphan in the console. The directory must
  be persistent, which is also why the chart retains its PVCs.
* Revoking a join key stops future enrollments. It does **not** revoke gateways
  already enrolled through it, which keep independent credentials.
* `cluster_ids` is validated for shape only, so an operator can mint a join key
  naming a cluster they do not own. Cluster admission constrains the gateway
  process, not the human minting the key. `type` is checked server-side.
* A join key minted without `cluster_ids` produces gateways that authenticate and
  then fail every mesh handshake.
* Two processes sharing a credential directory would both enroll and orphan one
  credential. Unlocked, because they cannot share the mumP2P identity either.
* The legacy `api_key` path is unchanged.
