---
name: api-management
description: How the platform's API gateway fronts a service — it validates the caller's token, injects identity headers, and attaches CORS — plus how a consumer calls a protected API. Apply to any service with exposesAPI.auth set, and to any consumer with a dependency (a `component`-kind sibling OR an `external`-kind upstream API) that calls a protected API. What the injected identity MEANS, and how to authorize on it, is owned by `thunder-authentication`.
metadata:
  aep:
    kind: org
    audience: [design, coding]
---

# API Management

A service whose design sets `exposesAPI.auth` sits behind the platform's API
gateway. The gateway **terminates authentication**: it validates the caller's
token against the org's IDP and passes the verified identity down as headers.
Your code trusts those headers and never sees a token.

## Constraints

**Never validate a JWT.** The gateway did it already, against keys your service
does not have — the signing keys, the `client_id` and the IDP's discovery URL are
all platform-side. A service that parses or verifies tokens is duplicating the
gateway and will disagree with it.

**Never issue one either.** No `/auth/login`, `/auth/register`, `/auth/logout`,
or any token endpoint on any backend. The IDP owns token issuance — see
`thunder-authentication`.

**Identity arrives in headers**, set by the gateway from the validated token, so
a client cannot spoof them:

| Header | Claim | Presence |
|---|---|---|
| `X-User-Id` | `sub` | the caller's canonical, opaque IdP subject — always present on a protected request |
| `X-User-Groups` | `groups` | the caller's role groups, a JSON array — present when the user is in any group |
| `X-User-Name` | `username` | the caller's username — **may be absent** |
| `X-User-Ou` | `ouHandle` | the caller's organization (multi-tenant, optional) |

**`thunder-authentication` owns what these mean and how to authorize on them** —
role resolution, the directory join, and why `X-User-Id` is not a lookup key.
Two rules are this skill's, because they are the gateway's contract:

- **`X-User-Id` missing on a protected request → 401.** The gateway always sets
  it when it lets a request through, so its absence means the request did not
  come through the gateway.
- **An authenticated caller who has no role → 403, never 401.** A 401 tells the
  SPA its token expired, so it restarts sign-in and loops forever. The role
  resolution itself is in `thunder-authentication`.

**Own your rows by `X-User-Id`.** It is the only stable per-caller key the
gateway gives you: stamp it on every row this service creates, and gate every
per-user query on it.

**CORS.** The `api-configuration` ClusterTrait attaches an Envoy CORS filter per
`visibility: external` HTTPRoute.

**Document the injected header.** In the OpenAPI you author for a protected
service, list `X-User-Id` under `parameters` so consumers know it is
required-but-injected: the gateway adds it, clients never set it.

## Implementation

Two rules, and both are mandatory in every protected handler:

1. **Read `X-User-Id`; 401 when it is missing.** Resolve it once, in one helper,
   rather than re-reading the header at each call site.
2. **Gate every per-user query on it — both filters, always.** A bare
   `WHERE id = ?` lets a caller reach any user's row by guessing its id; it must
   be `WHERE id = ? AND user_id = ?`. The same pairing applies to updates and
   deletes, and a query that matches nothing is a `404`, not a `500`.

Express both in your stack's own idiom — its routing style, where a shared
helper lives, and how a handler returns a status — following the conventions
that skill already sets rather than inventing a second one here.

Role-based and directory-scoped handlers build on this — see
`thunder-authentication`.

## Calling a protected upstream

When forwarding the caller's auth to an upstream `bearer` API, propagate the
inbound `Authorization` header verbatim — never re-issue or mint a token.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| CORS error in the browser when calling this API | This service ships its own CORS middleware (doubled headers) | Remove the middleware. |
| Every protected request 401s in tests | Test calls carry no `X-User-Id` — in production the gateway sets it | Set `X-User-Id` directly on the request in tests; don't try to mint a JWT. |
