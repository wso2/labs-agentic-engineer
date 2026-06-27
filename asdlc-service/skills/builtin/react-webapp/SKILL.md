---
name: react-webapp
description: How to build a React SPA on the platform — Vite project layout, multi-stage Dockerfile → nginx:alpine runtime, an envsubst reverse-proxy that forwards same-origin /api/<dep>/* to each backend dependency, synchronous /env-config.js load before the bundle for THUNDER_* (auth) + feature flags, and the throw-on-missing-key rule. Apply to every web-app component.
metadata:
  asdlc.version: "1"
---

# React Webapp

## What this skill does

The platform deploys React (Vite + TS) SPAs as a `nginx:alpine` image
serving a built static bundle AND acting as a same-origin reverse-proxy
to the component's backend dependencies. This skill tells the architect
what to expect and the coding agent exactly how to wire:

1. the **bundle** so per-browser config (`THUNDER_*` auth + feature
   flags) flows in at request time via `window._env_` — not at build
   time; and
2. **nginx** so the browser calls relative `/api/<dep>/...` (same-origin)
   and nginx proxies each path to the upstream backend address the
   platform injects into the pod env.

The SPA never makes a cross-origin call to a backend. Because every
backend call is same-origin through the proxy, there is no CORS to deal
with, and the backends it consumes may be internal-only.

## Platform facts

- Web-app components have `componentType: web-app`, `entrypoint:
  deployment/web-application`, `buildpack: docker`, default port 9090.
- They do NOT get an OpenAPI spec — `set_openapi` for a web-app is
  rejected.
- They do NOT carry `exposesAPI` — that toggle is for backend API
  enforcement only. Web-apps express auth via `callerIdentity` instead
  (see `thunder-authentication`).
- The built **JS bundle is identical across every environment**. Per-env
  browser values (OIDC config, feature flags) arrive at request time via
  `window._env_`, populated by `/env-config.js`. Per-env backend
  **addresses** arrive as pod env vars and are substituted into the nginx
  config at container start by envsubst.
- The platform mounts `/env-config.js` into `/usr/share/nginx/html/`
  via the SPA's ReleaseBinding (`runtime_config_service.go:
  EmitForComponent`). The agent never generates or commits this file.
- The set of keys the platform emits into `window._env_` is
  **hardcoded in BFF code** (`runtime_config_service.go`). Inventing a
  new key in the SPA produces a runtime error at module load because
  the value is `undefined`. `window._env_` carries ONLY `THUNDER_*`
  (auth) + agent-declared config/flags — **never** a backend URL.
- Each backend the SPA consumes is declared as a
  `dependencies.endpoints[]` entry in `workload.yaml` (see the base
  `asdlc` skill). OpenChoreo resolves that dependency and injects the
  upstream address into the pod as the env var named in
  `envBindings.address`. nginx proxies `/api/<dep>/*` to it.

### Authoritative `window._env_` keys

Use these EXACT spellings — do not invent new keys. **Backend URLs are
NOT here** — the SPA reaches backends via the nginx `/api/<dep>/` proxy.

| Key | Set when | Meaning |
|---|---|---|
| `THUNDER_*` | this web-app has `callerIdentity.mode: end-user` | OIDC config keys (`THUNDER_URL`, `THUNDER_CLIENT_ID`, `THUNDER_REDIRECT_URI`, `THUNDER_SCOPES`, `THUNDER_AFTER_SIGN_IN_URL`) — the IDP the browser redirects to for PKCE (cannot be proxied). Owned by the `thunder-authentication` skill; see it for the per-key meanings and wiring |
| `<NAME>` (any) | the agent declared it in `workload.yaml` `configurations.env` | app-config / feature-flag default (per-env override possible) |

## Recommended practice

### Architect

- One web-app component per user-facing surface; do NOT split a frontend
  into "ui-shell" + "ui-pages" — every SPA is one component, one
  task, one bundle.
- For every backend the web-app depends on, declare a `kind: component`
  (same-project) or `kind: org-service` (cross-project) dependency. The
  SPA reaches it at the relative path `/api/<dep-name>/...` — nginx
  proxies that to the upstream the platform injects. The architect MUST
  include an instruction line in `componentAgentInstructions`:
  `Call upstream <dep-name> at the same-origin path /api/<dep-name>/... (e.g. fetch("/api/<dep-name>/employees")). nginx proxies it to the injected upstream; do NOT read a backend URL from window._env_.`
- Do NOT write anything about `VITE_*`, `REACT_APP_*`,
  `NEXT_PUBLIC_*`, `.env` files, build-time substitution, or
  "Dependency endpoint resolved" comments. Those mechanisms are
  deprecated — runtime config + the nginx proxy are the supported paths.

### Tech-lead — issue body bullets

For every web-app task with a non-empty `dependencies` list, include one
Scope bullet per backend dependency:

- "Wire upstream `<dep-name>`: call it at the same-origin relative path
  `/api/<dep-name>/...` (e.g. `fetch("/api/<dep-name>/employees")`).
  nginx reverse-proxies `/api/<dep-name>/` to the address OpenChoreo
  injects from this component's `workload.yaml`
  `dependencies.endpoints[]` — no backend URL in `window._env_`, no
  hardcoded host."

And one Acceptance criteria bullet:

- "The SPA's API client (`src/api.ts` or equivalent) issues requests to
  the same-origin relative path `/api/<dep-name>/...` and never reads a
  backend URL from `window._env_`. nginx's
  `/etc/nginx/templates/default.conf.template` has a matching
  `location /api/<dep-name>/ { proxy_pass ${<DEP>_URL}; }` block whose
  `${<DEP>_URL}` is the `envBindings.address` env var for that
  dependency."

For every web-app task whose component has `callerIdentity.mode: end-user`,
also add this Scope bullet (covered fully in `thunder-authentication`):

- "Read OIDC config from `window._env_.THUNDER_*` via `src/env.ts`; the
  platform writes per-env values into `/env-config.js` on the SPA's
  ReleaseBinding, loaded synchronously before the bundle."

### Coding agent — implementation

Project layout (Vite + TS):

```
<app-path>/
├── package.json
├── tsconfig.json
├── vite.config.ts
├── index.html
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── env.ts        # typed window._env_ shim (THUNDER_* + flags only)
│   ├── api.ts        # fetch helpers — relative /api/<dep>/... calls
│   ├── auth.ts       # only if callerIdentity.mode: end-user — see thunder-authentication
│   └── pages/
├── nginx/
│   └── default.conf.template   # envsubst'd at container start
└── Dockerfile
```

`index.html` — `<script src="/env-config.js">` is **synchronous**,
BEFORE the bundle. No `async`, no `defer`, no `type="module"` on this
tag. This guarantees `window._env_` is populated before any ES module
evaluates. (`/env-config.js` still carries `THUNDER_*` + flags, so the
synchronous load is still required.)

```html
<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>App</title>
    <script src="/env-config.js"></script>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`src/env.ts` — typed read of `window._env_`. Throws if the file is
missing (which means a config bug, not a missing key default). It carries
ONLY auth + flags now — NO backend URLs:

```ts
type Env = {
  // THUNDER_* OIDC keys when this SPA has callerIdentity.mode: end-user —
  // extend this type with them per the thunder-authentication skill,
  // which owns the auth wiring. Plus any agent-declared feature flags /
  // config defaults. NO backend URL keys — backends are reached via the
  // same-origin /api/<dep>/ nginx proxy.
};

declare global {
  interface Window { _env_: Env }
}

if (!window._env_) {
  throw new Error(
    "window._env_ not set — /env-config.js failed to load. " +
    "The platform mounts this file via ReleaseBinding; if you see " +
    "this locally, host /env-config.js from your dev server.",
  );
}

export const env: Env = window._env_;
```

`src/api.ts` — call each backend at its same-origin relative path
`/api/<dep-name>/...`. There is no base URL to read; the browser hits the
SPA's own origin and nginx proxies the path to the upstream. Do NOT
prefix with `window._env_.<NAME>_URL` (that key no longer exists) and do
NOT hardcode a host. The example below is the unauthenticated client; if
this SPA has `callerIdentity.mode: end-user`, attach `Authorization:
Bearer <token>` to each fetch instead — see the `thunder-authentication`
skill for the auth'd client.

```ts
// Backends are reached same-origin via the nginx /api/<dep>/ proxy.
// For a dependency named "todo-api", call /api/todo-api/...
export async function listTodos(headers: HeadersInit = {}) {
  const res = await fetch("/api/todo-api/todos", { headers });
  return res.json();
}
```

`nginx/default.conf.template` — serve the static bundle AND reverse-proxy
each backend. `nginx:alpine`'s stock docker-entrypoint runs `envsubst` on
every `/etc/nginx/templates/*.template` → `/etc/nginx/conf.d/*.conf` at
container start, substituting `${VAR}` placeholders from the pod env. For
EACH backend dependency the component consumes, add one `location`
block whose `proxy_pass` target is the `${<DEP>_URL}` env var (the
`envBindings.address` from `workload.yaml`):

```nginx
server {
    listen 9090;
    server_name _;
    root /usr/share/nginx/html;
    index index.html;

    location = /health {
        access_log off;
        return 200 'OK';
        add_header Content-Type text/plain;
    }

    # One block PER backend dependency. ${TODO_API_URL} is the
    # envBindings.address env var OpenChoreo injects for the "todo-api"
    # dependency; its value looks like "http://todo-api:9090/".
    location /api/todo-api/ {
        proxy_pass ${TODO_API_URL};
    }

    # Static SPA + the platform-mounted /env-config.js.
    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

**Trailing-slash path mapping (get this exact).** With a trailing-slash
`location /api/todo-api/` AND a `proxy_pass` that includes a URI part
(anything after the host — even just the `/` in
`http://todo-api:9090/`), nginx **strips the matched location prefix**
and appends the rest to the proxy_pass URI. So:

- upstream env value `TODO_API_URL = http://todo-api:9090/`
- browser request `/api/todo-api/employees`
- → nginx forwards to `http://todo-api:9090/employees` ✅

The trailing slash on BOTH the `location` and the `${<DEP>_URL}` value is
what makes the prefix strip cleanly. If `${<DEP>_URL}` had no trailing
slash and no path, `proxy_pass` would be treated as host-only and nginx
would forward the full original URI (`/api/todo-api/employees`) — wrong.
The platform injects the address with a trailing slash; do not strip it.
Each `<DEP>_URL` placeholder is exactly the `envBindings.address` env var
name from this component's `workload.yaml` `dependencies.endpoints[]`
block (see the base `asdlc` skill for that grammar).

`Dockerfile` — multi-stage build + stock `nginx:alpine`, copying the
config as a **template** so envsubst runs at start:

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm i
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
# Put the config under templates/ so nginx:alpine's docker-entrypoint
# envsubst's ${<DEP>_URL} from the pod env into conf.d at start.
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
EXPOSE 9090
CMD ["nginx", "-g", "daemon off;"]
```

The built JS bundle stays env-agnostic; only the nginx config is
templated. Do NOT add a custom `/docker-entrypoint.d/` script —
`nginx:alpine`'s built-in `20-envsubst-on-templates.sh` already runs.

`workload.yaml` for a web-app declares its OWN external endpoint (the
browser must reach the SPA's ingress) PLUS a `dependencies.endpoints[]`
block — one entry per backend — supplied by the platform as a
"Platform-resolved dependencies" issue comment (the base `asdlc` skill
owns that grammar; do NOT invent or rename its fields). Each
`envBindings.address` there is the `${<DEP>_URL}` env var nginx proxies
to:

```yaml
apiVersion: openchoreo.dev/v1alpha1
metadata:
  name: <web-component-name>

endpoints:
  - name: http
    type: HTTP
    port: 9090
    visibility:
      - external          # the browser must reach the SPA's own ingress

# Per backend dependency — copied verbatim from the issue's
# "Platform-resolved dependencies" comment. envBindings.address is the
# env var nginx's proxy_pass ${<DEP>_URL} references. See the asdlc skill.
dependencies:
  endpoints:
    - component: todo-api
      name: http
      visibility: project        # backend may be internal-only
      envBindings:
        address: TODO_API_URL

# Optional: agent-authored defaults that become entries in window._env_.
# Safe browser-side defaults only (flags, support email) — no backend
# URLs, no secrets.
configurations:
  env:
    - name: SUPPORT_EMAIL
      value: support@example.com
```

Build verification (run BEFORE opening the PR):

```bash
cd <app-path>
npm install 2>&1 | tail -30   # regenerates package-lock.json
npx tsc --noEmit              # type-check without emitting JS
npm run build 2>&1 | tail -20 # actually build
```

Commit the resulting `package-lock.json`. Do not commit `node_modules/`.

### Don't

- ❌ Write a `.env` file in the app path.
- ❌ Read `import.meta.env.VITE_*` (or `process.env.REACT_APP_*`,
  `process.env.NEXT_PUBLIC_*`). Build-time mechanisms — the platform
  doesn't use them.
- ❌ Read a backend URL from `window._env_` (`API_BASE_URL`,
  `<NAME>_URL`). Those keys no longer exist — call `/api/<dep-name>/...`
  same-origin and let nginx proxy it.
- ❌ Hardcode an upstream host/port in `src/api.ts` or in the nginx
  config. The upstream comes from the injected `${<DEP>_URL}` env var.
- ❌ Generate or commit your own `env-config.js`. The platform owns it.
- ❌ Use `?? ""` or any silent default when reading a `window._env_`
  key. A missing key must throw at module load.
- ❌ Invent a key name not in the authoritative table above.
- ❌ Add `exposesAPI` to a web-app — the toggle is for backends only.
- ❌ Add a separate `auth` / `login` component — Thunder owns sign-in
  (see `thunder-authentication`).

### Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| SPA throws on load: `window._env_ not set` | `/env-config.js` failed to load (path wrong, served as 404, or `<script>` was `defer`/`async`) | Confirm `<script src="/env-config.js">` is **synchronous** (no `async`, no `defer`, no `type="module"`) and appears in `<head>` BEFORE the bundle's `<script type="module">`. |
| SPA throws on load: `<KEY> not set in window._env_` | The agent invented a key not in the authoritative table | Use the exact spellings; the platform only writes `THUNDER_*` + declared flags. Backend URLs are NOT in `window._env_`. |
| `404` from the SPA host when calling a backend | Frontend used a backend URL from `window._env_` (now removed) or hit a path with no matching `location /api/<dep>/` block | Call `/api/<dep-name>/...` and add the matching `location /api/<dep-name>/ { proxy_pass ${<DEP>_URL>}; }` block. |
| Backend gets `/api/<dep>/employees` instead of `/employees` (404 upstream) | `proxy_pass` had no URI part, or the `location` / upstream value lacked a trailing slash, so nginx forwarded the full original URI | Use a trailing-slash `location /api/<dep>/` and a `${<DEP>_URL}` whose value ends in `/` (the platform injects it that way) so the prefix is stripped. |
| `nginx: [emerg] ... invalid URL prefix in "${TODO_API_URL}"` at start | The `${<DEP>_URL}` env var wasn't injected (missing `dependencies.endpoints[]` entry, or the dep isn't deployed) | Ensure `workload.yaml` has the `dependencies.endpoints[]` entry whose `envBindings.address` matches the placeholder, copied from the issue's "Platform-resolved dependencies" comment. |
| Agent generated a `.env` file with `VITE_*` lines | Stale docs / training data | Delete it. Read `window._env_` (auth/flags only) via `src/env.ts`. |
