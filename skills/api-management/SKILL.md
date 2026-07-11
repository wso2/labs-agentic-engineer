---
name: api-management
description: How the platform's API gateway validates JWTs, injects identity headers (X-User-Id from sub, X-User-Groups for role), attaches CORS, and how to design + write services and consumers that match — including resolving the caller's role from X-User-Groups and returning 403 (not 401) for no role. Apply to any service with exposesAPI.auth set, and to any consumer with a dependency (a `component`-kind sibling OR an `external`-kind upstream API) that calls a protected API.
---


# API Management

## What this skill does

The platform fronts every service with `exposesAPI.auth` set through an
API gateway that validates JWTs, injects user-identity headers, and
attaches CORS. This skill tells the agent how to design and write code
that matches the gateway's contract, and how to call sibling protected
APIs as well as external dependent APIs from a consumer component.

## Platform facts

The following statements describe cluster behaviour. Editing them in
this skill does not change the cluster; it only desyncs your agent's
output from reality.

- The gateway sits in front of every service whose `exposesAPI.auth` is
  `end-user-required` or `service-required`.
- The gateway validates JWTs against the org's IDP. Your service does
  NOT validate JWTs.
- The gateway injects identity headers (lowercase claim → mixed-case
  header):
  - `sub → X-User-Id` (the caller's canonical id — REQUIRED, always present on protected requests)
  - `groups → X-User-Groups` (the caller's role groups — a JSON array; present when the user is in any group; drives RBAC)
  - `username → X-User-Name` (display, optional)
  - `ouHandle → X-User-Ou` (multi-tenant, optional)
- **Identity is not authorization.** `X-User-Id` is an OPAQUE IdP subject:
  stamp it as the owner of THIS service's own rows, but never use it as a
  lookup key into another service's directory (e.g. employee-service) — an
  IdP subject is not an employee id, so the lookup 404s. Resolve the
  caller's ROLE from `X-User-Groups`, the SAME groups claim the SPA reads
  (see `thunder-authentication`). An authenticated caller with no
  recognized role is a **403**, never a 401 — a 401 tells the SPA the
  token expired, so it restarts sign-in and loops forever.
- The gateway attaches an Envoy CORS filter to every `visibility: external`
  HTTPRoute via the `api-configuration` ClusterTrait. Your service does
  NOT add CORS middleware. Doubling produces two `Access-Control-Allow-Origin`
  headers and browsers reject the response.
- The agent does NOT see the gateway's `client_id`, JWT signing keys, or
  the IDP's discovery URL. Those live in BFF code.
- For consumers of a sibling protected API, the BFF injects the upstream
  URL into the consuming workload's runtime config:
  - Web-app consumer: read `window._env_.<NAME>_URL` (e.g.
    `TODO_API_URL`) via `src/env.ts`.
  - Backend consumer: read `os.Getenv("<NAME>_URL")` (Go) or
    `process.env.<NAME>_URL` (Node).
  Never hardcode the URL.
- For consumers of **external** upstream APIs (declared as `external`-kind
  entries in the component's `dependencies`), the BFF pins the URL on the
  consuming workload's ReleaseBinding env as `<NAME>_URL`. Same read
  pattern as sibling URLs.
- API error responses should use `application/problem+json` with a
  top-level `type`, `title`, `status` so the gateway can pass them
  through unchanged.

## Recommended practice

### Architect

- Set `exposesAPI.auth: end-user-required` on a `service` component when
  the spec OR its description implies caller authentication is needed AND
  no sign-in dependency is involved. Use the keyword rubric in the base
  architect prompt to decide.
- Set `exposesAPI.auth: service-required` for machine-to-machine APIs.
- Omit `exposesAPI` entirely for public APIs (landing pages, health,
  status hello-worlds).
- When end-users sign in, do NOT set `exposesAPI.auth` on the backend by
  hand. The SPA and the backend each declare the SAME `thunder-app`
  `platform-resource` dependency, and the platform DERIVES
  `exposesAPI.auth: end-user-required` on the backend from that dependency.
  Setting an explicit `service-required` alongside the dependency is a
  validation error; `service-required` stays manual only for pure
  service-to-service APIs. The `thunder-authentication` skill owns this
  dependency rule (and its rationale) — apply it.
- For external upstreams that already exist outside the project, declare
  them as `external`-kind entries in the consuming component's
  `dependencies`. Use **name-only** declarations (`{ "kind": "external",
  "name": "employee-api", "description": "..." }`) for catalog-known APIs —
  the platform resolves the URL from its in-cluster catalog at design-load
  time. Add `needsSpec: true` (and a `specUrl` hint, or attach the contract
  later) when the agent must call the API by specific endpoints.
- Every component with an `external` dependency MUST also carry an instruction
  line in `componentAgentInstructions` of the form:
  `Upstream external API <name>: env var <NAME_UPPER_SNAKE>_URL (auth: <authentication>). <description>. Read via os.Getenv / process.env / window._env_, call with standard HTTP client.`
- Every component with a sibling backend `dependencies` entry (`kind:
  component`) MUST also carry an instruction line of the form:
  `Upstream <name>: read the URL from <NAME_UPPER_SNAKE>_URL via the runtime-config shim.`
- Protected `service` `componentAgentInstructions` MUST say (verbatim or close):
  `No /auth/* endpoints. The API Platform gateway validates the JWT and the api-configuration trait's jwt-auth policy injects identity headers on every request: X-User-Id (the caller's opaque IdP subject) and X-User-Groups (the caller's role groups, a JSON array). Stamp X-User-Id as the owner of this service's own rows (reject 401 when missing). Resolve the caller's ROLE from X-User-Groups — the same groups the SPA reads — and reject 403 (NOT 401) when the caller has no recognized role. NEVER look the caller up in an org-directory dependency by X-User-Id — an IdP subject is not an employee id. Do NOT validate JWTs yourself; do NOT add CORS middleware (the gateway handles CORS).`
- In the OpenAPI you author for a protected `service`, document the
  injected `X-User-Id` header under `parameters` so consumers know it's
  required-but-injected (the gateway adds it; clients don't set it). The
  generic OpenAPI conventions are in your base design instructions.

### Tech-lead — issue body bullets

For every task targeting a `service` with `exposesAPI.auth: end-user-required`:

- Scope: "Do NOT implement `/auth/login`, `/auth/register`, or any
  token-issuance endpoint. The platform gateway validates the JWT and
  the `api-configuration` trait's `jwt-auth` policy injects `X-User-Id`
  and `X-User-Groups` on every request. Stamp `X-User-Id` as the owner of
  this service's own rows; reject 401 when it is missing."
- Scope (only when the spec has roles): "Resolve the caller's role from
  `X-User-Groups` (a JSON array of the caller's Thunder groups — the SAME
  claim the SPA reads). Reject **403** — never 401 — for an authenticated
  caller with no recognized role. NEVER resolve identity by looking
  `X-User-Id` up in an org-directory dependency; an IdP subject is not an
  employee id."
- Scope: "Do NOT validate JWTs in code; do NOT add CORS middleware. The
  gateway handles both."
- Acceptance criteria: "Requests missing `X-User-Id` get 401; an
  authenticated caller with no recognized role gets 403 (not 401), so the
  SPA does not loop; a caller with a role sees only data their role
  permits. `/health` is exempt and returns 200 without auth."

For every task whose component has one or more `external`-kind
`dependencies` entries, add **one Scope bullet per entry** of the form:

- "External upstream `<name>`: `<METHOD or 'GET'>` `<url>` —
  <description>. Authentication: <authentication>. Read the URL from
  env var `<NAME_UPPER_SNAKE>_URL` (already wired in the component's
  design instructions) and call with a standard HTTP client."

And one Acceptance criteria bullet per entry:

- "Calls to external upstream `<name>` use the URL from env var
  `<NAME_UPPER_SNAKE>_URL` (default `<url>`) and handle non-2xx
  responses without crashing. <auth-specific expectation: `none` → no
  Authorization header; `bearer` → caller's `Authorization` header
  forwarded; `api-key` → static key from env.>"

Use the literal URL, description, and authentication string from the
`external` dependency entry — do not invent values.

For service components (NOT web-apps), always add a Scope bullet: "Do
NOT add CORS middleware. The platform's gateway attaches an Envoy CORS
filter to every `visibility: external` HTTPRoute via the
`api-configuration` ClusterTrait; doubled CORS headers break browsers."

### Coding agent — implementation

Read `X-User-Id` from every protected handler; reject 401 when missing.
Per-user rows MUST be keyed on `X-User-Id`.

```go
func mustUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
    uid := r.Header.Get("X-User-Id")
    if uid == "" {
        http.Error(w, `{"error":"missing X-User-Id"}`, http.StatusUnauthorized)
        return "", false
    }
    return uid, true
}

func listTodos(w http.ResponseWriter, r *http.Request) {
    uid, ok := mustUserID(w, r); if !ok { return }
    rows, err := db.QueryContext(r.Context(),
        `SELECT id, title, done FROM todos WHERE user_id = ? ORDER BY id DESC`, uid)
    /* ... */
}

func updateTodo(w http.ResponseWriter, r *http.Request) {
    uid, ok := mustUserID(w, r); if !ok { return }
    id := r.PathValue("id")
    // AND user_id = ? — both filters are mandatory. A bare `WHERE id = ?`
    // would let a caller toggle any user's row by guessing its id.
    res, _ := db.ExecContext(r.Context(),
        `UPDATE todos SET done = 1 - done WHERE id = ? AND user_id = ?`, id, uid)
    if n, _ := res.RowsAffected(); n == 0 {
        http.NotFound(w, r); return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

When the spec has roles, resolve the caller's role from `X-User-Groups`
(the same groups the SPA reads) — NOT by looking `X-User-Id` up in an
org-directory service. Return **403** (never 401) when no group maps to a
role, so the SPA does not loop:

```go
func callerRole(r *http.Request) string { // "" = no recognized role
    for _, g := range parseGroups(r.Header.Get("X-User-Groups")) {
        lg := strings.ToLower(g)
        switch { // adapt the keywords to the spec's role names
        case strings.Contains(lg, "admin"):   return "admin"
        case strings.Contains(lg, "auditor"): return "auditor"
        }
    }
    return ""
}

// X-User-Groups is a JSON array (e.g. ["Compliance Admin"]); accept a
// comma-separated fallback too.
func parseGroups(h string) []string {
    h = strings.TrimSpace(h)
    if h == "" { return nil }
    var arr []string
    if strings.HasPrefix(h, "[") && json.Unmarshal([]byte(h), &arr) == nil {
        return arr
    }
    return strings.Split(h, ",")
}
```

Gate per-user queries with `AND user_id = ?`. Do NOT validate JWTs in
code. Do NOT add CORS middleware. Errors as `application/problem+json`
with a top-level `type`, `title`, `status`.

`/health` should remain exempt (no `mustUserID` call) so the platform's
readiness probe can reach it without auth.

For Go consumers of sibling APIs OR external dependent APIs, read the
URL from env at startup (NOT per-request); fail fast if missing:

```go
upstreamURL := os.Getenv("EMPLOYEE_API_URL")
if upstreamURL == "" {
    log.Fatal("EMPLOYEE_API_URL not set")
}
```

When forwarding caller auth to an upstream `bearer` API, propagate the
inbound `Authorization` header verbatim — do NOT re-issue a token.

For consumers of dependent APIs from a service component, the URL env
is set on the workload's ReleaseBinding at dispatch time; there is no
build-time URL injection. The agent's Dockerfile must not bake in any
URL.

The generic OpenAPI authoring conventions (3.0.3, `/health` on every
service, cross-component contract agreement) live in the architect's
design instructions — they are not restated here. The auth-specific
addition is in the Architect sub-section above (document the injected
`X-User-Id` header in the spec).

### Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| CORS error in browser when calling upstream | Backend wrongly ships its own CORS middleware (doubled headers), OR upstream's `workload.yaml` lacks `visibility: external` | Remove the middleware; confirm `visibility: external` on upstream's `workload.yaml`. |
| Every protected request 401s in test | Test calls don't carry `X-User-Id`; in production the gateway sets it, in test you set it manually | In integration tests, set `X-User-Id` directly on the request; don't try to mint a JWT. |
| `/health` returns 401 | Handler accidentally went through `mustUserID` middleware | Carve out `/health` (and any other public path) before the auth gate. |
| Signed-in user loops back to the login page forever | A protected handler answers no-role (or a failed org-directory lookup keyed on `X-User-Id`) with **401**; the SPA reads 401 as "token expired" and restarts sign-in | Resolve role from `X-User-Groups`; return **403** for an authenticated caller with no role; never key an org-directory lookup on `X-User-Id`. |
