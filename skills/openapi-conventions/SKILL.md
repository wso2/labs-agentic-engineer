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

**This block is the gateway's configuration, and the ONLY place the rule
lives.** Deploy renders one gateway route per operation from it: the audience it
pins and the single scope it requires. The service holds no copy of it — a
request that fails this check never reaches the service at all. Get it right
here and there is nothing else to configure anywhere.

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
| absent (inherits the document default) | any signed-in user | token required; signed assertion forwarded | 401 when the assertion is missing or does not verify |
| `security: []` | public | no token checked; no assertion minted | reads no identity at all |
| `security: [{oauth2: ["<handle>"]}]` | that permission | scope enforced, whole-string; 401 otherwise | no scope check — a request that arrives has passed it |

The gateway answers **401** for every refusal (no token, expired, wrong
audience, missing scope) with the same body and no `WWW-Authenticate`. No
generated service answers 403: the only authorization question left to it is
"is this row the caller's", and that answer is 404 (`api-management`).

**Exactly one scope per operation.** No two-element list, no second scheme
object, no `allOf`/`anyOf` question for the gateway and the service to answer
differently.

## Reach is the path

Which **rows** an operation reaches is its path, and nothing else says it:

| Path | Reaches | Guarded by |
|---|---|---|
| `/me/<resource>` | the caller's own rows | the resource's own-rows action — `claims:read`, `claims:submit` |
| `/me/<relation>/<resource>` | rows of a relation of the caller's — `/me/team/claims`, `/me/company/orders` | its own action — `claims:review-manager` |
| anything else | **every row** | the every-row action — `claims:read-all`, `claims:approve` |

The `<relation>` is a noun the domain model has (`team` because `Employee` has a
`managerId`), never a role or a group name — `/me/team/claims`, not
`/manager/claims`. The handler behind a `/me/…` path resolves the rows through
the gateway assertion's `sub` and nothing the client sends; a row that is not
the caller's does not exist there, so the answer is 404, never 403.

Two consequences, both mechanical:

- **One reach per handle.** A handle guards operations under `/me/` or outside
  it, never both — the same grant cannot mean "your rows" on one operation and
  "every row" on another. The gate refuses the document that mixes them, naming
  both operations. A capability with two reaches is two operations with two
  handles: `GET /me/claims` on `claims:read`, `GET /claims` on `claims:read-all`.
- **Nothing widens.** There is no handler-side check that turns one list into a
  bigger one. A caller who may see every claim calls `GET /claims`; the SPA
  picks the operation for the screen, and the gateway is the whole of the
  decision. A role that needs both views holds both handles.

Say the reach in the operation's `summary` too — "The caller's claims", "Every
claim" — so a reader of the contract does not have to parse the path for it.

`bearerAuth` is not used on this platform. A component with no sign-in
dependency declares no security scheme at all.

**Declare no identity header.** The gateway hands the service the caller as a
signed `x-jwt-assertion`; the unsigned `X-User-*` headers it also sets are not
an authority and nothing reads them (`api-management`). Neither belongs in the
contract: the assertion is a property of the deployment, not of the API, and a
declared `X-User-Id` parameter invites a handler to bind one. The write gate
refuses a `required: true` identity header and any identity header on a public
operation; the correct document simply has none.

```yaml
paths:
  /me/claims:
    get:
      summary: The caller's claims
      security:
        - oauth2: [claims:read]
  /claims:
    get:
      summary: Every claim
      security:
        - oauth2: [claims:read-all]
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
