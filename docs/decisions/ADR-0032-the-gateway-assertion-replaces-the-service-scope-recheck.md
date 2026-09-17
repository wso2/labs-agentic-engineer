# ADR-0032 — The gateway's signed assertion replaces the service-side scope re-check

**Status:** Accepted · 2026-09-16
**Supersedes point 5 of:** [ADR-0030](ADR-0030-scopes-are-the-authorization-authority-for-a-generated-app.md)
("the service enforces the same scope the gateway does"), and the sentence in
[ADR-0031](ADR-0031-reach-is-the-path.md)'s consequences that left it standing.
Every other point of both ADRs stands.

## Context

ADR-0030 point 5 made the generated service a second enforcement point: a
middleware re-read the operation's handle out of the contract and compared it
against the unsigned `x-user-scopes` header the gateway sets, so that "a trait
that failed to render, a route reached by another path inside the cell, or a
policy somebody relaxed" would still be refused. The alternative it rejected,
gateway-only enforcement, was rejected because a decision that lives only in a
rendered trait is invisible in the component's code and unprovable by its tests,
and because a service is reachable from inside its cell without crossing the
gateway.

Two facts changed under it, both measured on the pinned gateway
(`gateway-1.2.2`, `jwt-auth v1.3.0`, `backend-jwt v1.0.1`) on 2026-09-15:

* The `backend-jwt` policy mints an RS256-signed assertion into
  `x-jwt-assertion` carrying the gateway as `iss`, the caller's `sub`, the
  token's `scope` verbatim, `aud`, `ouHandle` and a fifteen-minute `exp`. A
  payload edited after signing fails verification; a client-supplied
  `x-jwt-assertion` is overwritten on every protected operation. A service can
  verify it against one certificate the platform publishes per environment,
  with no IdP JWKS, no discovery and no network call.
* The middleware's own inputs were unsigned. `x-user-scopes` is set by the
  gateway and by anything else that can open a socket to the pod, and nothing in
  the service can tell one from the other. So the re-check defended against a
  relaxed policy only for a request that had in fact come through the gateway,
  and defended against nothing for the in-cell bypass it was written for: a
  bypassing caller sets the header the middleware trusts.

## Decision

**A generated service holds no operation → scope table, in any stack. It
verifies the gateway's signed assertion once at its edge and reads the caller
from that, and nothing else.**

1. **The gateway is the whole of the "may this operation be called" decision.**
   It is rendered per operation from the same `security` block the write gate
   and the build gate read through one shared reader, so the two cannot drift,
   and there is no second copy in the service to keep in sync.
2. **The service verifies the assertion, and that is its authentication.** Each
   stack skill ships one verifier asset (`go/assets/gateway_assertion.go`,
   `ballerina/assets/gateway_assertion.bal`) wired once. It checks the
   signature against `GATEWAY_ASSERTION_CERTIFICATE`, pins `iss` to
   `GATEWAY_ASSERTION_ISSUER`, checks `exp`, and puts the caller on the request
   context. A missing variable stops the service from starting.
3. **Three outcomes, and the middle one is the point.** No assertion continues
   with no caller (a public operation carries none); an assertion that is present
   but does not verify is a 401, never an anonymous caller; a verified one
   continues with the caller. Downgrading a bad assertion to anonymous would make
   forging one strictly better than sending none.
4. **`sub` is the service's key, `scope` is not its gate.** Every `/me/…`
   operation resolves rows through the assertion's `sub` (ADR-0031). The
   assertion's `scope` claim is available for a badge or an audit line and
   decides nothing; a handler that reaches for it to choose rows has found a
   design with one operation where it needs two.
5. **The `x-user-*` headers are not an authority and nothing reads them.** They
   are still set beside the assertion, and a contract does not declare them.
   `openapi-conventions` and `api-management` say so in the same words.
6. **No generated service answers 403.** The only authorization question left
   to it is "is this row the caller's", and that answer is 404, so the caller
   cannot learn the row exists.
7. **The in-cell bypass is answered by the boundary, and survivable because of
   the signature.** `visibility: internal` and the NetworkPolicy behind it keep
   unrefereed traffic off the service. A pod that breaches that boundary can set
   every header it likes and cannot produce an assertion, because it does not
   hold the environment's signing key. That is the property the header re-check
   never had.

## Alternatives considered

**Keep the middleware and feed it the assertion's `scope` instead of the
header.** It would be a correct check, and it would still be a second copy of
the contract in every service, one per stack, kept in sync by review. The
trait render is the one place the operation → scope mapping needs to exist, and
the shared reader in `aep-api` is what keeps that one copy honest.

**Verify the caller's own token at the service.** Rejected in ADR-0030 and
still rejected: it couples every service to the identity provider's keys and
discovery, and the gateway strips `Authorization` before the service sees it.

**Trust the NetworkPolicy alone and verify nothing.** Rejected: a service that
believes an unsigned header cannot distinguish a real caller from a forged one
the moment the boundary is misconfigured, and nothing in its own tests can prove
the boundary is there.

## Consequences

**The service-side scope assets are gone.** `go/assets/scopes_middleware.go`,
`ballerina/assets/scopes.bal` and the drift test beside it are deleted; their
place is taken by the assertion verifier in each stack. A stack skill's tests
now cover four cases against a throwaway RSA keypair: accepted, wrong key is
401, tampered payload is 401, public operation is 200 with no assertion and
with forged `X-User-*` headers that the handler never reads.

**A test never reaches a gateway.** Minting an assertion with a throwaway key
and pointing `GATEWAY_ASSERTION_CERTIFICATE` at its certificate is the whole
fixture.

**A re-provisioned gateway needs a redeploy of every protected component.** The
certificate rides the component's ReleaseBinding, so a new keypair 401s every
request until the component is redeployed with the new one.

**`backend-jwt` must be emitted per operation.** An API-level `backend-jwt`
cannot see a per-operation `jwt-auth`'s authenticated context and mints an
assertion with no `sub` and no `scope`. The trait emits it inside each
operation's `policies`, after that operation's `jwt-auth`.

**The mock-mode walk checks the public case.** A handler behind `security: []`
reads no identity of any kind, because on a policy-free operation the gateway is
a two-way pass-through and inbound headers arrive as the caller typed them.
