---
name: openapi-conventions
description: "Use when creating or editing an openapi.yaml for a service component — designing endpoints, request/response schemas, errors, pagination, or security for a REST API."
metadata:
  aep:
    kind: platform
    audience: [design, coding]
---

# OpenAPI conventions

Every `service` component gets one spec at
`specs/design/components/<name>/openapi.yaml`, authored as **OpenAPI 3.0.3**.

**The spec is validated as it lands.** A write to that path is rejected
(`INVALID_OPENAPI`) unless the document is OpenAPI 3.x with at least one path
and one operation, and a rejected write changes nothing. So a spec that applied
cleanly has already passed that check: do not re-validate it with a separate
tool. Handing your own spec back to a validator as pasted text costs a round
trip and re-emits the entire document — for a check that already ran.

Coverage is checklist-driven, not vibes: walk the PRD (`specs/requirements/prd.md`) against the
component's `design.json` responsibility, and give every capability the
requirements assign to THIS component its resource(s) and every core entity
its schema. A capability with no endpoint is a defect. Commonly dropped when
consolidating services: audit trail/logs, user & role management, notification
preferences, reporting/analytics — check for each explicitly before finishing.

**Keep the spec COMPACT.** Complete coverage, minimal prose: a short `summary`
per operation and a one-line `description` per response — no multi-sentence
descriptions, no `example`/`examples` blocks, no speculative endpoints the
requirements don't imply. Schemas carry the required fields plus the few core
properties that define the entity — not every conceivable attribute.

For resource taxonomy (collection/atomic/controller), URI grammar, HTTP-method
semantics, and a full worked example, read
`references/wso2-rest-api-design-guidelines.md` — the source of truth this
summary condenses.

## Structure

- `servers:` is relative — `- url: /` — never an absolute external host.
- Paths are **kebab-case plural nouns** (`/expense-claims`,
  `/expense-claims/{claimId}/line-items`); verbs only for controller actions
  (`/expense-claims/{claimId}/submit`). Max two nesting levels.
- Every operation has `operationId` in **lowerCamelCase verb+resource**
  (`listExpenseClaims`, `submitExpenseClaim`) and a non-empty `summary`.
- Every response has a non-empty `description`. Bodies are
  `application/json`. Reusable schemas live under `components/schemas`.

## Errors — one shared schema

Define `components/schemas/Error` and reference it from EVERY 4xx/5xx
response:

```yaml
Error:
  type: object
  required: [code, message]
  properties:
    code: { type: integer, description: HTTP or application error code }
    message: { type: string, description: short human-readable label }
    description: { type: string, description: detailed explanation }
    moreInfo: { type: string, description: URI to documentation }
```

Each operation declares at least its failure modes: `'400'`/`'404'` where
applicable, plus `'401'`/`'403'` when the API is authenticated.

## Pagination — every collection GET

Parameters `limit` (integer, default 20, max 100) and `offset` (integer,
default 0). The 200 response is an envelope, not a bare array:

```yaml
type: object
required: [count, data]
properties:
  count: { type: integer, description: total matching items }
  next: { type: string, nullable: true, description: relative URI of the next page }
  previous: { type: string, nullable: true, description: relative URI of the previous page }
  data: { type: array, items: { $ref: '#/components/schemas/ExpenseClaim' } }
```

Filtering and searching are query parameters on the collection GET
(`?status=submitted`, `?employeeId=...`) — never separate endpoints.

## Security

**This block is the gateway's configuration.** Deploy renders one gateway route
per operation from it: the audience it pins and the single scope it requires.
It is also what the service's scope middleware enforces. Get it right here and
there is nothing else to configure anywhere.

A component that depends on the sign-in resource type declares one scheme, one
document-level default, and per-operation overrides:

```yaml
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: /oauth2/authorize
          tokenUrl: /oauth2/token
          scopes:
            claims:read: See own claims
            claims:submit: Create and send a claim
security:
  - oauth2: []        # document default: any signed-in user
```

Every scope key, here and on an operation, is a handle from
`specs/design/security.json` — `<resource>:<action>` — for a resource THIS
component owns. Never invent one.

`openid`, `profile`, `email`, `group` and `ou` are **reserved** OIDC scopes that
ride every access token, so one of them on an operation would admit every
signed-in person in the organisation — silently, and wide open. The gate refuses
all five as an operation scope and as a `flows.*.scopes` key.

**Three states, and only three.** Each operation is exactly one of:

| `security` on the operation | Means | Gateway | Service |
|---|---|---|---|
| absent (inherits the document default) | any signed-in user | token required | 401 without `X-User-Id` |
| `security: []` | public | no token checked | reads no identity header |
| `security: [{oauth2: ["<handle>"]}]` | that permission | scope enforced | 403 `insufficient_scope` |

**Exactly one scope per operation.** No two-element list, no second scheme
object, no `allOf`/`anyOf` question for the gateway and the service to answer
differently. Where an operation's result widens for a more privileged caller
(own rows vs every row), that is a *second handle read inside the handler*, not
a second scope on the operation.

`bearerAuth` is not used on this platform. A component with no sign-in
dependency declares no security scheme at all.

**Declare both injected headers `required: false`, even though the gateway
always sets them.** A generated server binds parameters *before* the scope
middleware runs, and its default error handler answers **400** for a missing
required header — which makes the platform's "no `X-User-Id` on a protected
operation → 401" rule unreachable and turns a gateway bypass into a confusing
400. `api-management` owns that rule; this is the spelling that lets a service
obey it.

The gateway sets `X-User-Id` and `X-User-Scopes` from the validated token and a
client never sends them. Define each once under `components/parameters`, then
`$ref` them from every **protected** path item's `parameters` — path level, not
per operation, so one reference covers every method on that path — and from no
public one. A definition nothing references is not in the spec:

```yaml
components:
  parameters:
    UserId:
      name: X-User-Id
      in: header
      required: false      # see below - required:true makes the 401 rule unreachable
      description: caller identity injected by the gateway from the validated token; clients never set it
      schema: { type: string }
    UserScopes:
      name: X-User-Scopes
      in: header
      required: false
      description: space-separated granted scopes, injected by the gateway; clients never set it
      schema: { type: string }
paths:
  /expense-claims/{claimId}:
    parameters:
      - $ref: '#/components/parameters/ClaimId'
      - $ref: '#/components/parameters/UserId'
      - $ref: '#/components/parameters/UserScopes'
    post:
      security:
        - oauth2: [claims:submit]
  /health:
    get:
      security: []
```

**Never spec an auth endpoint.** No `/auth/login`, `/auth/register`,
`/auth/logout`, or any other token-issuance path on any service: the IDP issues
tokens and the gateway validates them (see `thunder-authentication`). Specifying
one puts the coding agent's issue in direct conflict with its skills.

## YAML hygiene

2-space indentation throughout; quote status-code keys (`'200'`, `'404'`).
The file is edited with anchored string edits later, so consistent
indentation is load-bearing.
