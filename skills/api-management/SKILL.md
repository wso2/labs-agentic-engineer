---
name: api-management
description: "How the platform's API gateway fronts a service — it validates the caller's token, enforces the scope each operation declares in openapi.yaml, injects identity headers, and attaches CORS — plus how a consumer calls a protected API. Apply to any service with exposesAPI.auth set, and to any consumer with a dependency (a `component`-kind sibling OR an `external`-kind upstream API) that calls a protected API. What the injected identity MEANS, and how to authorize on it, is owned by `thunder-authentication`."
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

**Identity arrives in headers**, set by the gateway from the validated token:

| Header | Claim | What it is for |
|---|---|---|
| `X-User-Scopes` | `scope` | **The authorization authority.** The token's scope string **verbatim** — space-separated, and it **includes the five OIDC scopes** (`openid profile email group ou`) alongside this project's handles. Tokenise on space and compare whole strings; "the caller holds some scope" is true of everybody signed in and is never an authorization answer, and `claims:read-all` does not imply `claims:read`. The middleware compares the operation's declared scope against it; handlers read it only for the finer rule an operation-level check cannot express. |
| `X-User-Id` | `sub` | The caller's canonical, opaque IdP subject — the only key for rows this service creates. |
| `X-User-Name` | `username` | The caller's username — a directory lookup key for attribute-scoped rules. |
| `X-User-Ou` | `ouHandle` | The caller's organization (multi-tenant, optional). |
| `X-User-Groups` | `groups` | **Not read for authorization.** The gateway still maps it; no generated code consumes it, for a decision or for display. Permissions reach you as scopes. If you ever render it, its value is **JSON** — `["Finance"]`, brackets and quotes included — so `json.Unmarshal` it; splitting it on a comma is always wrong. |

**Every mapped header is always SET on a protected operation — test for EMPTY,
never for MISSING.** A claim the token does not carry still yields its header,
set to `""` (`x-user-name: ""`, `x-user-groups: ""`). A check written as "header
present" therefore treats a token with no subject as identified. Write every
rule against the empty string.

**Public operations read no identity header at all.** An operation with
`security: []` has no policy attached, and on a policy-free operation the
gateway is a **two-way pass-through**: inbound `x-user-*` reach the handler
exactly as the caller typed them, and so does a real `Authorization`. (On a
protected operation the same headers are **overwritten**, forged values
replaced.) So a handler behind `security: []` reads none of them, and the
mock-mode walk checks that it does not.

**`thunder-authentication` owns what these mean and how to authorize on them** —
the scope middleware, ownership filters, and why `X-User-Id` is not a directory
lookup key. These rules are this skill's, because they are the gateway's
contract:

- **`X-User-Id` empty on a protected request → 401.** The gateway always sets it
  from a validated token, so an empty value means the request did not come
  through the gateway — a deployment fault, not an anonymous caller. Declare the
  header OPTIONAL in your framework and resolve it in one helper, so your
  service picks that status: a framework-level "required header" rejection
  answers 400 before your resolver runs, which makes this rule unreachable.

- **A required scope absent from `X-User-Scopes` → 403, never 401.** A 401 tells
  the SPA its token expired, so it restarts sign-in and loops forever; a 403
  tells it the user is signed in and lacks a permission, which is what the
  Forbidden screen is for. Answer it with
  `WWW-Authenticate: Bearer error="insufficient_scope", scope="<handle>"`.

- **The gateway is the first line; your service enforces the same rule as the
  second.** The boundary that keeps unrefereed traffic off your service is the
  one `visibility: internal` line and the NetworkPolicy behind it — not the
  code. The middleware does not close that boundary and does not try to: a
  hostile pod inside the same namespace can set `X-User-Scopes` itself and will
  be believed, and no service-side check can tell an asserted header from a
  supplied one. What the middleware catches is **misconfiguration** — an
  endpoint accidentally marked public, an older trait in another environment, a
  sibling calling the cluster DNS name directly, a port-forward, a local run
  with no gateway at all — where header trust would otherwise turn into a full
  authorization bypass. It is cheap: the service already holds the spec the
  gateway was rendered from, one shared middleware compares the operation's
  declared scope with `X-User-Scopes`, and a configuration error becomes a 403
  instead of a breach. `thunder-authentication` prescribes that middleware, per
  stack, as a copied asset — **you author no new check**.

- **Only the gateway may assert identity.** A proxy in front of your service that
  forwards untrusted traffic (a SPA's nginx) must clear inbound `X-User-*`, and
  must itself proxy THROUGH the gateway — `react-webapp` ships an asset that does
  both. A caller reaching your service on a lane with no gateway on it can set
  those headers freely.

- **Never invent a second authority.** Permissions reach a service only as
  scopes, so one resolved from the service's own table sees none the platform
  granted. Your own records hold per-user DATA, never a role and never a
  permission. A `client_credentials` client belongs on its own scopes and off a
  route that assumes an end user.

**Do not verify a JWT in the service to harden any of this.** That couples every
service to the IdP's JWKS and reverses this skill wholesale. The middleware plus
the network boundary is the design.

**Own your rows by `X-User-Id`.** It is the only stable per-caller key the
gateway gives you: stamp it on every row this service creates, and gate every
per-user query on it.

**CORS.** The `api-configuration` ClusterTrait attaches an Envoy CORS filter per
`visibility: external` HTTPRoute.

**Document the injected headers.** In the OpenAPI you author for a protected
service, list `X-User-Id` and `X-User-Scopes` under `components.parameters` and
reference them from every protected operation, so consumers know they are
required-but-injected: the gateway adds them, clients never set them. Declare
them `required: false` — a framework that binds a required header answers 400
before your resolver runs. Reference them from no public operation — a public
handler reads neither. `openapi-conventions` owns the `security` block itself.

## Status matrix

What a caller sees, per layer. The service's column is what your tests assert;
the gateway's is what the deployed app answers.

| Case | Gateway | Service, called directly |
|---|---|---|
| public operation, no token | 200 | 200, reads no identity header |
| protected operation, no token | 401 | 401 (`X-User-Id` empty) |
| token for another project (wrong `aud`) | 401 | 401 (no `X-User-Id` was injected) |
| valid token, operation's scope held | 200 | 200 |
| valid token, operation's scope NOT held | **401** | **403** + `WWW-Authenticate: Bearer error="insufficient_scope", scope="<handle>"` |
| path the contract does not declare | 404 | your router's own 404 |

The one row that differs is deliberate, and it is not a setting anybody can
change. **The gateway answers 401 for every authentication-policy failure** — no
token, bad signature, wrong issuer, wrong audience, missing scope — with a
byte-identical body, and it **never sends `WWW-Authenticate`**. There is no
per-operation or per-API knob for it on the pinned gateway. So nothing
downstream can learn "insufficient scope" from the gateway by any signal:
**your service always answers 403**, and the SPA is written so that either
status reaches the Forbidden screen rather than a sign-in loop.

## Implementation

Three rules, all mandatory in every protected handler:

1. **Read `X-User-Id`; 401 when it is empty.** Resolve it once, in one helper,
   rather than re-reading the header at each call site.
2. **Let the scope middleware answer the operation's own permission.** Copy it
   from your stack skill; wire it once. A handler re-checking the operation's
   scope is a second authority that drifts.
3. **Gate every per-user query on `X-User-Id` — both filters, always.** A bare
   `WHERE id = ?` lets a caller reach any user's row by guessing its id; it must
   be `WHERE id = ? AND user_id = ?`. The same pairing applies to updates and
   deletes, and a query that matches nothing is a `404`, not a `500`. Where the
   catalog declares an `any`-ownership action that widens the same operation
   (`claims:read` own rows, `claims:read-all` every row), drop the owner filter
   only when `hasScope` says the caller holds the wider handle.

   **A row that exists but is not the caller's is a `404`, not a `403`.** 403
   means "this operation needs a permission you lack", which the middleware has
   already answered; using it for an ownership miss tells the caller that the
   id exists, which is the enumeration the owner filter is there to prevent.

Express all three in your stack's own idiom — its routing style, where a shared
helper lives, and how a handler returns a status — following the conventions
that skill already sets rather than inventing a second one here.

Scope enforcement and ownership widening build on this — see
`thunder-authentication`.

## Calling a protected upstream

**The gateway strips `Authorization` before your service sees it.** The
validated token is re-presented as `X-Forwarded-Authorization: Bearer <jwt>`,
so a handler that reads `Authorization` finds nothing on a request that came
through the gateway. **Never read `Authorization`**: authorize on
`X-User-Scopes`, as above. When you must forward the caller's own token to an
upstream `bearer` API, read `X-Forwarded-Authorization` and send it verbatim —
never re-issue or mint a token, and never verify it yourself. The one request
that still carries a real `Authorization` is a call to a **public** operation,
where the caller supplied it and it proves nothing.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| CORS error in the browser when calling this API | This service ships its own CORS middleware (doubled headers) | Remove the middleware. |
| Every protected request 401s in tests | Test calls carry no `X-User-Id` — in production the gateway sets it | Set `X-User-Id` and `X-User-Scopes` directly on the request in tests; don't try to mint a JWT. |
| A signed-in user loops back to sign-in forever | A handler answered a missing permission with 401; the SPA reads 401 as "token expired" | 403 with `insufficient_scope`. |
| Every operation answers 200 for anyone, including in tests | The scope middleware was wired where the framework runs it BEFORE the per-operation scope is known | Wire it exactly where your stack skill says; a router-level `Use` is silently fail-open. |
| A token with no subject is treated as an identified caller | The check tested "`X-User-Id` present"; the gateway sets every mapped header even when its claim is absent | Test for the EMPTY string, never for a missing header. |
| A new endpoint 404s on the deployed app but works locally | It is not in `openapi.yaml`, so the gateway has no route for it | Add the operation to the contract; the spec IS the gateway config. |
