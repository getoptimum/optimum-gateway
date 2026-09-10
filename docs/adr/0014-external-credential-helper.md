# ADR-0014: Mint through an external credential helper

**Status:** Draft  
**Date:** 2026-09-10  

## Context

A gateway authenticates with `OPT_API_KEY`, one shared secret per host, minted centrally
and mapped to a machine by hand. That model does not scale to an operator running a large
fleet: every host needs its own secret created somewhere else and carried to it, and the
mapping is the part that fails. A key deployed twice, a key for the wrong cluster, a
rotated key left live, all look the same from here.

An operator may instead want a gateway's credential held and renewed by a process of
their own, so that hosts are not configured with a secret one at a time. Whether to do
that, and how such a credential is obtained, is out of scope here and not the gateway's
concern. What this ADR settles is the **seam**: what the gateway needs so that something
else can obtain tokens for it, with as little of the gateway's own code involved as
possible. ADR-0013, proposed on an open pull request and not landed, takes the other
path and puts that logic in the gateway.

Two properties of the existing code force the shape of the seam.

**Tokens are requested repeatedly, not once.** `refreshLoop`
(`pkg/service/auth_token/manager.go`) re-mints roughly every three hours. So whatever
holds the credential has to be there for every mint, for the life of the gateway. A tool
that runs once at startup and exits cannot serve this.

**One setting decides both where tokens come from and who is trusted.**
`RemoteAuthURL` supplied the mint URL, the JWKS URL and the verified issuer
(`pkg/service/jwks_verifier/verifier.go`). Repointing it at a local process would move the
trusted issuer too, and a local process cannot hold the signing key the JWKS publishes,
so its tokens would be self-issued and rejected by every peer.

## Decision

**A long-lived helper process serves the gateway's own mint endpoint on loopback, and the
gateway is told where to mint without being told whom to trust.**

1. **One new setting, `auth_token_url` (`OPT_AUTH_TOKEN_URL`).** It overrides only the
   mint endpoint. `remote_auth_url` still fixes the issuer pinned at verification and the
   JWKS fetched to verify against. A helper therefore cannot become an issuer this gateway
   trusts, which is asserted by a test that signs an otherwise valid token with the real
   issuer's key and changes only `iss`.

2. **A helper counts as a credential.** `auth_token.New` previously disabled itself when
   `OPT_API_KEY` was empty. It now enables when either credential is present, and
   `IsEnabled()` reads a field set on that path rather than being derived from the API key.
   Its meaning is unchanged for every configuration that exists today.

3. **The shared secret is never forwarded.** When a helper is configured the mint payload
   carries only `peer_id`, so a misdirected `auth_token_url` cannot leak `OPT_API_KEY`.
   If both are set the helper wins and the unused one is logged once, rather than
   refusing to boot: a fleet mid-migration will still have `api_key` in its
   configuration, and a hard failure there would be an outage.

4. **The gateway keeps supplying its own `peer_id`, and now checks what comes back.**
   Peers require that a token's `cnf.peer_id` matches the connecting libp2p peer
   (`pkg/service/gossipsub-gateway/handshake.go`), so whatever obtains the token must
   know it. Because the gateway sends it on every mint as it always has, the helper
   needs no access to the gateway's identity key. Since the token now arrives from a
   local process rather than the issuer, `mint` also refuses a token whose `cnf.peer_id`
   names a different peer, so a credential directory copied between hosts cannot make
   this gateway adopt another's `sub` and `operator_id` for its own labels. An absent
   `cnf` is not treated as a mismatch, because it is set only when the request carried a
   `peer_id`.

5. **The response is passed through unchanged.** Both credential types return the same
   body, so nothing the gateway parses changes. A helper that relays status and a
   well-formed JSON body preserves the existing classification of a revoked or
   suspended credential; a proxy in front of it that answers 403 with HTML instead
   would surface as a network error and be retried rather than treated as terminal.

## Consequences

* **The helper is a credential holder on loopback.** Anything that can reach its endpoint
  can mint as this gateway, so it binds to `127.0.0.1` and is neither exposed nor
  published as a service port. In the same container that boundary is the container.
* **The gateway gains a startup dependency**, and the requirements that come with it
  fall on the helper, which this repository cannot verify. Because the gateway's first
  mint failure is fatal, the helper must be serving before it starts the gateway. And
  because `Token()` does not check expiry, a gateway whose mint endpoint has gone away
  keeps serving a token until it expires and then fails every handshake in silence, so
  the helper must stop the gateway rather than outlive its own endpoint.
* **`auth_token_url` is unvalidated, and loopback is the only boundary the gateway
  provides.** Nothing here checks that the value is local. A non-local endpoint is
  therefore an operator's deliberate choice, and it moves the boundary onto their
  network: the gateway does not authenticate itself to the endpoint, so anything that
  can reach one holding a credential can obtain tokens for that gateway. Issuer pinning
  keeps this from becoming a trusted-token problem, not an access-control one. Anything
  other than loopback needs network controls of its own.
* **A helper that cannot obtain a credential should fail before the gateway starts**, so
  the error names the credential rather than the gateway.
* **The helper is not in this repository**, so it is not reviewed here and its source is
  not visible to operators reading this code. That is the cost of the decision.
* **A helper's own credential state has to persist** across restarts, which is the
  helper's problem rather than the gateway's, but it constrains deployment: the location
  it uses must be on a volume that survives the container.
* **Behaviour is unchanged for existing deployments.** With no `OPT_AUTH_TOKEN_URL` the
  gateway mints exactly as before, and the new setting defaults to empty. One observable
  detail does change: the startup line for a gateway with no credential now reads
  `neither OPT_API_KEY nor OPT_AUTH_TOKEN_URL set, auth_token disabled`, so a log-based
  alert matching the old text needs updating.

## Alternatives considered

* **Putting the credential client in the gateway** (ADR-0013, proposed and not landed).
  Fewer moving parts at runtime and no new process, and it needs no config seam at all.
  Rejected because it puts credential acquisition and private-key storage into the
  gateway's own source and release cycle, for a model that is still proving out: this
  way the client can change without touching the gateway, which carries about seventy
  lines instead of the whole implementation.
* **A tool that provisions an identity at startup and exits.** Ruled out by the refresh
  loop: there would be nothing left to obtain the next token three hours later.
* **Handing the gateway a pre-minted token from a file or an environment variable.** The
  gateway has no path to consume one, and startup reads several fields out of the mint
  response, so this would mean more gateway code, not less.
* **Repointing `remote_auth_url` at the helper and proxying the JWKS as well.** Zero
  gateway changes, but the verified issuer moves with it, so the helper would have to
  issue tokens itself and no peer would accept them.
* **Intercepting at the network layer** so the helper can proxy the real endpoints
  transparently. Also zero gateway changes, but it needs a certificate the gateway trusts
  for the real issuer's name, and the helper still could not originate a token.

## Non-goals

* Retiring `OPT_API_KEY`. It is unchanged and remains the default.
* Validating that the helper is local, or authenticating the gateway to it.
* Choosing how the helper reaches the image, which is a deployment decision and lands
  separately from this seam.
