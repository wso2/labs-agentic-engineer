---
name: api-management
description: How the platform's API gateway validates JWTs and injects X-User-Id from the sub claim, and how to design + write services and consumers that match. Apply to any service with exposesAPI.auth set, and to any consumer (a `kind: component` sibling, a `kind: org-service`, or a `kind: external` dependency) that calls a protected API.
metadata:
  asdlc.version: "1"
---

# API Management

## What this skill does

The platform fronts every service with `exposesAPI.auth` set through an
API gateway that validates JWTs and injects user-identity headers. This
skill tells the agent how to design and write code that matches the
gateway's contract, and how to call sibling protected APIs as well as
org-service and external dependencies from a consumer component.

Browsers reach services **same-origin** through the web-app's nginx
`/api/*` proxy, so there is no cross-origin CORS anywhere — your service
does NOT add CORS middleware, and the platform does NOT attach a gateway
CORS filter.

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
  - `sub → X-User-Id` (canonical caller identifier — REQUIRED, always present on protected requests)
  - `username → X-User-Name` (display, optional)
  - `ouHandle → X-User-Ou` (multi-tenant, optional)
- There is NO cross-origin CORS: browsers call a service same-origin via
  the web-app's nginx `/api/*` proxy. Your service does NOT add CORS
  middleware (it would have no effect and only risks doubled headers).
- The agent does NOT see the gateway's `client_id`, JWT signing keys, or
  the IDP's discovery URL. Those live in BFF code.
- For consumers of a sibling protected API (a `dependencies` entry of
  `kind: component`) OR an `org-service` dependency, the BFF injects the
  upstream URL into the consuming workload's runtime config (`<NAME>` =
  the dependency's `name` in `UPPER_SNAKE_CASE`):
  - Web-app consumer: read `window._env_.<NAME>_URL` (e.g.
    `TODO_API_URL`) via `src/env.ts`.
  - Backend consumer: read `os.Getenv("<NAME>_URL")` (Go) or
    `process.env.<NAME>_URL` (Node).
  Never hardcode the URL.
- For consumers of an **`external`** dependency (`kind: external` on the
  component), the platform collects the user's config values and injects
  them into the consuming workload as the declared `config` env-var keys
  (e.g. the base-URL key `<NAME>_BASE_URL`, plus any `secret: true`
  credential key). Read them the same way (`os.Getenv` / `window._env_`)
  as sibling URLs.
- API error responses should use `application/problem+json` with a
  top-level `type`, `title`, `status` so the gateway can pass them
  through unchanged.

## Recommended practice

### Architect

- Set `exposesAPI.auth: end-user-required` on a `service` component when
  the spec OR its description implies caller authentication is needed.
  Use the keyword rubric in the base architect prompt to decide.
- Set `exposesAPI.auth: service-required` for machine-to-machine APIs.
- Omit `exposesAPI` entirely for public APIs (landing pages, health,
  status hello-worlds).
- When a `service` is `end-user-required` AND a sibling `web-app` signs
  in to it, that web-app MUST also carry `callerIdentity: { mode: end-user }`.
  The `thunder-authentication` skill owns this pairing rule (and its
  rationale) — apply it.
- For upstreams that already exist outside this component, declare them
  as a `dependencies` entry via `add_dependency(name, dependency)`:
  - For a service published by ANOTHER project in the org (catalog-known
    by name), use `kind: org-service`:
    `{ "kind": "org-service", "name": "employee-api", "description": "..." }`
    — the platform resolves the URL from its in-cluster catalog at
    design-load time.
  - For an arbitrary off-platform third-party API, use `kind: external`:
    `{ "kind": "external", "name", "description", "config": [{ "key": "<NAME>_BASE_URL", "secret": false }, ...] }`
    — the base URL is a `config` key (`secret: false`) and any credential
    is a `config` key with `secret: true`. The architect declares only
    the KEY schema; the user supplies the VALUES later.
- Every component with an `org-service` or `external` dependency MUST also
  carry an instruction line in `componentAgentInstructions` of the form:
  `Upstream API <name>: env var <NAME_UPPER_SNAKE>_URL (or its declared config key, e.g. <NAME_UPPER_SNAKE>_BASE_URL for an external dependency). <description>. Read via os.Getenv / process.env / window._env_, call with standard HTTP client.`
- Every component with a sibling `kind: component` dependency on a backend
  service MUST also carry an instruction line of the form:
  `Upstream <name>: read the URL from <NAME_UPPER_SNAKE>_URL via the runtime-config shim.`
- Protected `service` `componentAgentInstructions` MUST say (verbatim or close):
  `No /auth/* endpoints. The API Platform gateway validates the JWT and the api-configuration trait's jwt-auth policy injects X-User-Id (from JWT sub claim) on every request. Read X-User-Id to identify the caller; reject (401) when missing. Per-user records MUST be keyed on X-User-Id. Do NOT validate JWTs yourself; do NOT add CORS middleware (browsers reach you same-origin via the web-app proxy).`
- In the OpenAPI you author for a protected `service`, document the
  injected `X-User-Id` header under `parameters` so consumers know it's
  required-but-injected (the gateway adds it; clients don't set it). The
  generic OpenAPI conventions are in your base design instructions.

### Tech-lead — issue body bullets

For every task targeting a `service` with `exposesAPI.auth: end-user-required`:

- Scope: "Do NOT implement `/auth/login`, `/auth/register`, or any
  token-issuance endpoint. The platform gateway validates the JWT and
  the `api-configuration` trait's `jwt-auth` policy injects `X-User-Id`
  (from JWT `sub` claim) on every request. Read `X-User-Id`; reject
  (401) when missing. Per-user records MUST be keyed on `X-User-Id`."
- Scope: "Do NOT validate JWTs in code (the gateway validates them); do
  NOT add CORS middleware (browsers reach you same-origin via the web-app
  `/api/*` proxy)."
- Acceptance criteria: "Every protected endpoint rejects requests
  missing `X-User-Id` with 401; with a valid `X-User-Id`, returns only
  data owned by that subject. `/health` is exempt and returns 200
  without auth."

For every `external` or `org-service` dependency on the task's component,
add **one Scope bullet per entry** of the form:

- "Upstream `<name>`: `<METHOD or 'GET'>` — <description>. Read the URL
  from env var `<NAME_UPPER_SNAKE>_URL` (for an `org-service`) or its
  declared `config` URL key (e.g. `<NAME_UPPER_SNAKE>_BASE_URL`, for an
  `external` dependency) — already wired in the component's design
  instructions — and call with a standard HTTP client. For an `external`
  dependency, also read any declared `secret: true` config key for the
  credential."

And one Acceptance criteria bullet per entry:

- "Calls to upstream `<name>` use the URL from its env var
  (`<NAME_UPPER_SNAKE>_URL` or the declared `config` URL key) and handle
  non-2xx responses without crashing. Credentials, if any, come from the
  declared `secret: true` config key — no Authorization header when none
  is declared; forward the caller's `Authorization` header for a passed-
  through bearer; use the static key from its config env otherwise."

Use the literal name, description, and declared `config` keys from the
`dependencies` entry — do not invent values.

For service components (NOT web-apps), always add a Scope bullet: "Do
NOT add CORS middleware. Browsers reach this service same-origin through
the consuming web-app's nginx `/api/*` proxy, so there is no cross-origin
request to allow."

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

Gate per-user queries with `AND user_id = ?`. Do NOT validate JWTs in
code. Do NOT add CORS middleware (browsers reach you same-origin via the
web-app `/api/*` proxy). Errors as `application/problem+json` with a
top-level `type`, `title`, `status`.

`/health` should remain exempt (no `mustUserID` call) so the platform's
readiness probe can reach it without auth.

For Go consumers of sibling APIs OR `org-service`/`external`
dependencies, read the URL from env at startup (NOT per-request); fail
fast if missing:

```go
upstreamURL := os.Getenv("EMPLOYEE_API_URL")
if upstreamURL == "" {
    log.Fatal("EMPLOYEE_API_URL not set")
}
```

When forwarding caller auth to an upstream `bearer` API, propagate the
inbound `Authorization` header verbatim — do NOT re-issue a token.

For consumers of `org-service`/`external` dependencies from a service
component, the URL env (and, for an `external` dependency, its config
values) is set on the workload's ReleaseBinding at dispatch time; there
is no build-time URL injection. The agent's Dockerfile must not bake in
any URL.

The generic OpenAPI authoring conventions (3.0.3, `/health` on every
service, cross-component contract agreement) live in the architect's
design instructions — they are not restated here. The auth-specific
addition is in the Architect sub-section above (document the injected
`X-User-Id` header in the spec).

### Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| CORS error in browser when calling upstream | Browser is calling the backend's URL directly instead of the web-app's same-origin `/api/*` proxy, OR the backend wrongly ships its own CORS middleware | Call `/api/<dep>` (relative, same-origin) from the frontend; remove any backend CORS middleware. |
| Every protected request 401s in test | Test calls don't carry `X-User-Id`; in production the gateway sets it, in test you set it manually | In integration tests, set `X-User-Id` directly on the request; don't try to mint a JWT. |
| `/health` returns 401 | Handler accidentally went through `mustUserID` middleware | Carve out `/health` (and any other public path) before the auth gate. |
