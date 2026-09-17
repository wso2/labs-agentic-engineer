---
name: api-management
description: "Apply when a service sits behind the platform's API gateway — its design sets `exposesAPI.auth` — or when a consumer calls a protected API, whether a `component`-kind sibling or an `external`-kind upstream."
metadata:
  aep:
    kind: org
    audience: [design, coding]
---

# API Management

A service whose design sets `exposesAPI.auth` sits behind the platform's API
gateway. The gateway **terminates authentication**: it validates the caller's
token against the org's IDP and passes the verified identity down as headers.
Your code never sees a token.

**The spec is the gateway config.** Deploy reads this component's
`openapi.yaml` and renders one gateway route per operation, carrying that
operation's `security` block: the audience it pins, and the single scope it
requires. Nothing else configures the gateway — there is no separate policy
file to author and no annotation to add. Four consequences you build on:

- A path the contract does not declare **does not exist at the gateway** (404).
  Adding an endpoint means adding it to `openapi.yaml`.
- An operation with `security: []` is public: no token is checked, and no
  identity header is asserted.
- An operation that names **exactly one** scope renders one token policy that
  requires exactly that handle. Two scopes on one operation is not a shape the
  contract allows — `openapi-conventions` owns the rule.
- You never author an `OPTIONS` operation. Deploy synthesises one per path so a
  browser's CORS preflight is answered (an undeclared method+path is a 404
  before any policy runs); the synthesised preflight short-circuits before the
  token check, so it needs no token.

## Constraints

**Never validate a JWT.** The gateway did it already, against keys your service
does not have — the signing keys, the `client_id` and the IDP's discovery URL are
all platform-side. A service that parses or verifies tokens is duplicating the
gateway and will disagree with it.

**Never issue one either.** No `/auth/login`, `/auth/register`, `/auth/logout`,
or any token endpoint on any backend. The IDP owns token issuance — see
`thunder-authentication`.

**Identity arrives as a signed assertion.** On every protected operation the
gateway mints a short-lived JWT of the caller it just authenticated, signs it
with the environment's own key, and puts it on the upstream request:

| | |
|---|---|
| header | `x-jwt-assertion` (the platform tells the service its name) |
| `iss` | the **gateway**, e.g. `aep-gateway-<org>-<env>` — never the IdP |
| `sub` | the caller's canonical, opaque IdP subject — the only key for rows this service creates |
| `scope` | the token's scope string **verbatim**: space-separated, and including the OIDC scopes (`openid profile email group ou`) alongside this project's handles |
| `ouHandle` | the caller's organization (multi-tenant, optional) |
| `exp` | 15 minutes; minted per request |

A service verifies that signature against **one certificate the platform
publishes per environment**, delivered as `GATEWAY_ASSERTION_CERTIFICATE`,
`GATEWAY_ASSERTION_ISSUER` and `GATEWAY_ASSERTION_HEADER` on the container. That
is the whole trust anchor: no IdP JWKS, no discovery URL, no introspection, no
network call. Your stack skill ships the verifier as a copied asset — **you
author no new check**.

**The `x-user-*` headers are still set, and are not an authority.** The gateway
maps `x-user-id`, `x-user-scopes`, `x-user-name`, `x-user-ou` and `x-user-groups`
alongside the assertion. They are unsigned: anything that can open a socket to
your service can set them, and nothing in your code can tell an asserted one
from a supplied one. **Read none of them.** Everything they carry is in the
assertion, signed.

**Public operations carry no assertion at all.** An operation with
`security: []` has no policy attached, and on a policy-free operation the
gateway is a **two-way pass-through**: inbound `x-user-*` reach the handler
exactly as the caller typed them, and so does a real `Authorization`. (On a
protected operation the same headers are **overwritten**, forged values
replaced — including a client-supplied `x-jwt-assertion`.) So a handler behind
`security: []` reads no identity of any kind, and the mock-mode walk checks that
it does not.

**`thunder-authentication` owns what these mean and how to authorize on them** —
verifying the assertion, the `/me/…` filters, and why the subject is not a
directory lookup key. These rules are this skill's, because they are the
gateway's contract:

- **The gateway decides WHETHER a request may happen; the assertion says WHO it
  is from.** Those are different questions and only the second is yours. The
  gateway checked the operation's declared scope before forwarding, so a
  request that arrives has already passed it. A service that re-checks it is
  keeping a second copy of the contract that nothing keeps in sync — and the
  copy is what drifts. **Hold no operation → scope table, in any stack.**

- **An assertion that does not verify → 401, never "anonymous".** Falling back
  to an unauthenticated caller would make forging one strictly better for an
  attacker than sending none. No assertion at all on an operation that needs an
  identity is also a 401: the gateway always mints one, so its absence means the
  request did not come through the gateway.

- **Verification is what makes the header lane safe.** The boundary that keeps
  unrefereed traffic off your service is the `visibility: internal` line and the
  NetworkPolicy behind it. The signature is what makes a breach of that boundary
  survivable: a pod that reaches your service directly can set every `x-user-*`
  header it likes and **cannot produce an assertion**, because it does not hold
  the environment's signing key. That is the property a header check could never
  have, and it is why there is nothing left for a service-side scope table to
  add.

- **Only the gateway may assert identity.** A proxy in front of your service that
  forwards untrusted traffic (a SPA's nginx) must clear inbound `X-User-*` and
  `x-jwt-assertion`, and must itself proxy THROUGH the gateway — `react-webapp`
  ships an asset that does both.

- **Never invent a second authority.** Permissions reach a service only as
  scopes in the assertion, so one resolved from the service's own table sees
  none the platform granted. Your own records hold per-user DATA, never a role
  and never a permission. A `client_credentials` client belongs on its own
  scopes and off a route that assumes an end user.

**Verify the assertion; never verify the caller's own token.** The assertion is
signed by the gateway with a key the platform hands you. Reaching for the IdP's
JWKS instead — to check the `Authorization` or `X-Forwarded-Authorization`
token — couples every service to the identity provider and reverses this skill
wholesale.

**Own your rows by the assertion's `sub`.** It is the only stable per-caller key
you get: stamp it on every row this service creates, and gate every per-user
query on it.

**CORS.** The `api-configuration` ClusterTrait attaches an Envoy CORS filter per
`visibility: external` HTTPRoute.

**Do not document the injected headers.** Earlier revisions of this skill had
you list `X-User-Id` and `X-User-Scopes` under `components.parameters`. Stop:
nothing reads them now, and a contract that declares them invites a handler to
bind one. The assertion is not a contract parameter either — it is a property of
the deployment, not of the API, and a client never sets it.
`openapi-conventions` owns the `security` block, which is the only place the
contract says anything about auth.

## Status matrix

What a caller sees. There is no longer a separate "service, called directly"
column: a request that does not come through the gateway carries no assertion
and is answered 401 by every handler that needs an identity.

| Case | What the caller sees |
|---|---|
| public operation, no token | 200; the handler reads no identity |
| protected operation, no token | 401 at the gateway — nothing reaches the service |
| token for another project (wrong `aud`) | 401 at the gateway |
| valid token, operation's scope held | 200 |
| valid token, operation's scope NOT held | **401** at the gateway |
| a row that exists but is not the caller's | 404 from the service |
| path the contract does not declare | 404 at the gateway |

One column, because there is one answer. **The gateway answers 401 for every
authentication-policy failure** — no token, bad signature, wrong issuer, wrong
audience, missing scope — with a byte-identical body, and it **never sends
`WWW-Authenticate`**. There is no per-operation or per-API knob for it on the
pinned gateway, and nothing downstream can learn "insufficient scope" from it by
any signal. The SPA is written so that either status reaches the Forbidden
screen rather than a sign-in loop.

A **403 has no place in a generated service**: the only authorization question a
service still answers is "is this row yours", and the answer to that is 404 (see
below), never 403.

## Implementation

Three rules, all mandatory in every protected handler:

1. **Verify the assertion once, at the edge of the service.** Copy the verifier
   from your stack skill and wire it as that skill says. A missing
   `GATEWAY_ASSERTION_CERTIFICATE` must stop the service from starting: one that
   runs without it cannot tell a real caller from a forged one.
2. **Resolve the caller from the verified assertion, in one helper**, rather
   than re-reading a header at each call site — and 401 when a handler that
   needs an identity has none.
3. **Gate every per-user query on the caller's `sub` — both filters, always.** A
   bare `WHERE id = ?` lets a caller reach any user's row by guessing its id; it
   must be `WHERE id = ? AND user_id = ?`. The same pairing applies to updates
   and deletes, and a query that matches nothing is a `404`, not a `500`. Which
   queries are per-user is the operation's PATH: everything under `/me/…`
   resolves through the caller's `sub`; an operation outside `/me/` reaches
   every row and filters on nothing. No handler widens a result on a scope
   (`openapi-conventions`, ADR-0031).

   **A row that exists but is not the caller's is a `404`, not a `403`.** Using
   403 tells the caller that the id exists, which is the enumeration the owner
   filter is there to prevent.

Express all three in your stack's own idiom — its routing style, where a shared
helper lives, and how a handler returns a status — following the conventions
that skill already sets rather than inventing a second one here.

Assertion verification builds on this — see `thunder-authentication`.

## Calling a protected upstream

**The gateway strips `Authorization` before your service sees it.** The
validated token is re-presented as `X-Forwarded-Authorization: Bearer <jwt>`,
so a handler that reads `Authorization` finds nothing on a request that came
through the gateway. **Never read `Authorization`**: the caller is the
assertion's, as above. When you must forward the caller's own token to an
upstream `bearer` API, read `X-Forwarded-Authorization` and send it verbatim —
never re-issue or mint a token, and never verify it yourself. The one request
that still carries a real `Authorization` is a call to a **public** operation,
where the caller supplied it and it proves nothing.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| CORS error in the browser when calling this API | This service ships its own CORS middleware (doubled headers) | Remove the middleware. |
| Every protected request 401s in tests | Test calls carry no assertion | Mint one with a throwaway RSA key and point `GATEWAY_ASSERTION_CERTIFICATE` at its certificate; never try to reach a real gateway or IdP from a test. |
| Every request 401s right after a deploy | The environment's gateway was re-provisioned and this component still holds the old certificate | Redeploy the component; the certificate rides its ReleaseBinding. |
| A forged request is served as an anonymous caller | An unverifiable assertion was treated as "no caller" instead of 401 | A present-but-invalid assertion is always a 401. |
| Every caller looks anonymous | The service read `x-user-id` instead of the assertion | Those headers are unsigned and prove nothing; read the verified caller. |
| A new endpoint 404s on the deployed app but works locally | It is not in `openapi.yaml`, so the gateway has no route for it | Add the operation to the contract; the spec IS the gateway config. |
