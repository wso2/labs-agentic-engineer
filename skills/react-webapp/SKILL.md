---
name: react-webapp
description: How to build a React SPA on the platform — project layout, the build-verify command, and this stack's constraints and pitfalls. Apply when a component's `type` is `web-application`.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# React Webapp

A web-app on this platform: a Vite + TS SPA built to static files, served by
stock `nginx:alpine`. The image is **byte-identical across every environment**.
Per-env values the **browser** needs (OIDC config, flags) arrive at request time
in `window._env_`, never at build time. Sibling API addresses are **not**
browser config — they are pod env for nginx.

## Development flow

1. **Scaffold** per Layout, including the nginx drop-in copy in step 1 of Layout.
2. **Implement** — `src/env.ts` first (every other module reads config through
   it), then generate `src/generated/` from each dependency's OpenAPI contract,
   then `src/api.ts` with **same-origin** `baseUrl`, then pages. Every rule under
   Constraints is a runtime failure if broken, not a style preference.
3. **Mock mode** — read `references/mock-mode.md` and follow it. Every web-app
   on this platform ships a `mock/` directory that stands the same app up with
   no cluster, no sibling service and no IDP behind it; step 5 is what opens it.
   Production is untouched: the build eliminates the whole branch as dead code
   and ships no `msw`.
4. **Verify** — from the app path:
   ```bash
   npm install                   # regenerates package-lock.json
   # ← the design system's check goes here (see below)
   npx tsc --noEmit              # type-check without emitting
   npm run build                 # actually build
   ! grep -rqE "mock/|msw" dist/ # the bundle carries no mock — step 3
   git status --porcelain --ignored=matching -- . \
     | grep '^!!' | grep -vE 'node_modules|dist'   # ← output MUST be empty
   ```
   Commit the `package-lock.json` this produces. Never commit `node_modules/`.

   **The last line is not a formality.** Every step above it reads your working
   tree; the cluster builds the *committed* tree of this folder alone. A build
   input that git ignores is present for all four checks and absent from the
   image, and git will not tell you: `git add <app-path>` skips an ignored file
   **silently, exit 0**, and leaves `git status` clean. `src/generated/` is the
   one that bites, because the repo-root `.gitignore` is shared with backend
   components that legitimately ignore a `generated/` directory — an unanchored
   pattern there reaches down into this app. `--ignored=matching` is what makes
   those paths visible; `node_modules` and `dist` are the only two the builder
   stage makes for itself, which is why they are the only two filtered out.
   A `!!` line naming anything else means the image will not carry that file.
   Fix the pattern (anchor it in the repo-root `.gitignore`), never `git add -f`.

   **The design-system skill contributes one step to this sequence**, and it is
   mandatory: run the command its own Verify section names, after `npm install`
   and before the type-check, and treat a non-zero exit exactly like a failing
   build. That slot exists because a design system's own wiring — a missing
   build plugin, an unimported theme, a peer-dependency mismatch — is the one
   class of fault `tsc` and `vite build` cannot see: it type-checks and builds
   perfectly clean, then renders an unstyled page in the cluster. If the pinned
   design-system skill names no such command, the sequence is just the five
   above.

   The `build` script is `tsc --noEmit && vite build` — **not** `tsc -b`, which
   needs a composite project: a `tsconfig.json` that `references` a
   `tsconfig.node.json` setting `noEmit` fails with `TS6310: Referenced project
   may not disable emit`, and unwinding that costs more than it buys.

   Verification ends at exit 0. **Never run `npm audit` or `npm audit fix`** —
   the advisories land on Vite's dev-only transitive dependencies, which never
   reach a static bundle served by nginx, and `audit fix` bumps pinned
   dependencies behind your back.
5. **Walk** — a build exiting 0 says the code is well-formed and nothing about
   what the screens do, so the app is not finished until somebody opens every
   screen in a real browser and repairs what does not work. That walk is
   `mock-verification`, and it is a **separate** agent's job: leave `mock/` and
   the `dev:mock` script ready for it, and hand your build off clean.
6. **PR** — only once the walk leaves no failure open.

## Constraints

**Runtime config, not build-time.** The platform mounts `/env-config.js` into
the served root and it populates `window._env_`. You never generate or commit
that file. `import.meta.env.VITE_*`, `process.env.REACT_APP_*`,
`NEXT_PUBLIC_*` and `.env` files are all build-time mechanisms the platform does
not use — reading one gets you `undefined` in production.

**The key set is fixed.** It is hardcoded in platform code, so a key you invent
is `undefined` at module load. Use these exact spellings:

| Key | Set when | Meaning |
|---|---|---|
| `<NAME>_URL` | `dependencies` include an `external`-kind entry `<name>` | URL of that **external** upstream (browser may call it). Not used for a sibling `component`-kind service. |
| `<DEP>_*` | this web-app declares an auth `platform-resource` dependency named `<dep>` | OIDC config (`<DEP>_CLIENT_ID`, `<DEP>_ISSUER`, `<DEP>_JWKS_URL`, `<DEP>_SCOPES`), `<DEP>` = UPPER_SNAKE of the dependency name (`user-auth` → `USER_AUTH_*`) — owned by `thunder-authentication` |
| `<NAME>` (any) | you declared it in `workload.yaml` `configurations.env` | app-config default, per-env override possible |

There is **no** `API_BASE_URL` and **no** `<UPSTREAM>_URL` in `window._env_` for
a sibling service. The sibling lives at **same-origin** `/api` (extra siblings:
`/api/<component-name>/`). Its addresses — `<DEP>_GATEWAY_URL` and `<DEP>_URL` —
are **pod** env vars, never browser keys; only the nginx drop-in reads them (see
**Same-origin API proxy** below).

**Throw on a missing key, never default it.** No `?? ""`, no `|| ''`, for keys
this table says are set. A silent fallback hides a missing OIDC issuer. Do not
declare sibling API URL keys on `Env` just to throw — they are not emitted.

**Served at host root.** Each web-app gets its **own** gateway hostname, so the
stock Vite default is correct: **do NOT set `base`**. Asset URLs, any react-router
`basename`, and any OAuth `redirect_uri` are plain root paths (`/assets/…`,
`/callback`). Services ARE path-routed, under `/<project>-<component>-http` on a
shared gateway — copying that prefix into `base` 404s every asset.

**Same-origin API proxy, through the gateway.** Nginx reverse-proxies
`location /api/` to the primary sibling. Two pod env vars address that sibling and
they are NOT interchangeable:

| Pod env var | Reaches | Auth |
|---|---|---|
| `<DEP>_GATEWAY_URL` | the API gateway. Set by the platform for a sibling whose design declares `exposesAPI.auth`. Carries a **context path prefix**. | validates the bearer token, injects `X-User-*` from its claims |
| `<DEP>_URL` | the project Service, directly | none — nothing validates a token, nothing injects identity |

The asset **prefers `<DEP>_GATEWAY_URL`** and falls back to `<DEP>_URL`. That
order is the whole point: browser traffic is untrusted, and this proxy is the one
hop that would otherwise carry it into the project's trusted lane with no
authentication in between. Two rules follow, and the asset already obeys both —
which is why you copy it rather than write it:

- **Preserve the context prefix.** The gateway routes on it; a rewrite that
  strips it 404s every call.
- **Clear inbound `X-User-*`.** Identity is the gateway's to assert. A browser
  that sets those headers itself must not be believed.

Copy the assets in Layout; do not hand-write a different `proxy_pass`, do not add
`/oidc/` (token endpoint stays cross-origin; `thunder-authentication`), do not
copy `apps/console/docker-entrypoint.sh`. Keep the official `nginx:alpine`
`ENTRYPOINT`. The only extra file is `/docker-entrypoint.d/15-aep-api-proxy.sh`.

**Auth.** If the component declares an auth `platform-resource` dependency, add
`src/auth.ts` and attach `Authorization: Bearer <token>` to every API call —
`thunder-authentication` owns that wiring.

**Never `exposesAPI`.** That toggle is for backends only; a web-app expresses
auth through its auth dependency instead.

**The UI comes from the organization's design system.** Every component, layout
primitive and style under `src/` comes from the design-system skill pinned on
this component — no raw HTML styling, no second component or styling library.
That skill owns everything inside `src/`; this skill owns the app around it.
Where the two appear to disagree — `base`, the `index.html` script tags, nginx,
`window._env_` — **this skill wins**, because those are deployment facts, not
style preferences. The data layer is untouched either way: `openapi-fetch` and
the committed `src/generated/` client stay exactly as specified above.

**Contract-first client, never hand-rolled shapes.** Every dependency has a
committed OpenAPI contract: `specs/design/components/<component-name>/openapi.yaml`
for a `component`-kind dependency, or
`specs/design/components/<this-app>/dependencies/<dep-name>.openapi.yaml` for an
`external`-kind one — project-root paths, sibling to this app's own folder.
Generate types from it and call through `openapi-fetch`'s typed client (Layout);
don't hand-write request/response shapes. Commit `src/generated/` — the
per-component Docker build's context is this app's own folder alone.

## Layout

```
<app-path>/
├── package.json
├── tsconfig.json         # ONE file — no project references, no tsconfig.node.json
├── vite.config.ts        # no `base` — served at host root
├── index.html
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── env.ts            # typed window._env_ shim
│   ├── generated/        # openapi-typescript output, one file per dependency — commit, never hand-edit
│   ├── api.ts            # openapi-fetch client(s), typed against generated/
│   ├── auth.ts           # only with an auth dependency — see thunder-authentication
│   └── pages/            # design-system components only, never raw HTML
├── mock/                 # mock mode — references/mock-mode.md
├── nginx/
│   ├── default.conf      # copied from the skill assets, then /api locations kept
│   └── 15-aep-api-proxy.sh
├── Dockerfile
└── .dockerignore         # what `COPY . .` leaves behind
```

**Copy the nginx assets first.** From the App Path:

```bash
mkdir -p nginx
cp "$AEP_SKILLS_DIR/react-webapp/assets/nginx-default.conf" nginx/default.conf
cp "$AEP_SKILLS_DIR/react-webapp/assets/15-aep-api-proxy.sh" nginx/15-aep-api-proxy.sh
```

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's `SKILL.md`
(the BFF mirrors that directory to `.claude/skills/react-webapp/`).

Then in `nginx/15-aep-api-proxy.sh` only: rename **both** variables so they name
the **primary** component-kind dependency in UPPER_SNAKE —
`API_URL="${TODO_API_GATEWAY_URL:-}"` and the fallback `"${TODO_API_URL:-}"`
(`todo-api` → `TODO_API_GATEWAY_URL` / `TODO_API_URL`). Rename both or the
fallback silently wins and the app runs unauthenticated. Do not invent a second
name, and do not delete the fallback — an unprotected sibling has no gateway
address.

**Done when:** `nginx/default.conf` contains `location /api/`,
`proxy_pass http://$api_backend` and the `__API_CONTEXT__` rewrite; the drop-in
script's two `API_URL=` lines use that
primary `<DEP>_URL`; there is no `/oidc/` location.

Extra component-kind siblings: add one `location /api/<component-name>/` block
each (same `proxy_pass` pattern, rewrite stripping that prefix) and a matching
`sed` of `__<NAME>_BACKEND__` from that sibling's `<DEP>_URL`. Primary stays `/api`.

`index.html` — the `env-config.js` tag is **synchronous** and comes BEFORE the
bundle. No `async`, no `defer`, no `type="module"` on it.

```html
<head>
  <script src="./env-config.js"></script>          <!-- 1. synchronous -->
</head>
<body>
  <div id="root"></div>
  <script type="module" src="/src/main.tsx"></script>  <!-- 2. the bundle -->
</body>
```

`src/env.ts` — typed read, throwing if the file never loaded. Declare only keys
from the table above that this app actually has (OIDC / `configurations.env` /
external-kind URLs). Example with no browser API URL:

```ts
type Env = {
  // USER_AUTH_* only if this SPA declares that auth dependency
};

declare global {
  interface Window { _env_: Env }
}

if (!window._env_) {
  throw new Error(
    "window._env_ not set — /env-config.js failed to load. " +
    "The platform mounts this file; if you see this locally, host " +
    "/env-config.js from your dev server.",
  );
}

export const env: Env = window._env_;
```

`src/generated/<component-name>.ts` — one run per dependency, before writing
`api.ts`:

```bash
npx openapi-typescript ../specs/design/components/<component-name>/openapi.yaml \
  -o src/generated/<component-name>.ts
```

(`external`-kind dependency: point at
`../specs/design/components/<this-app>/dependencies/<dep-name>.openapi.yaml`
instead.) Re-run and commit the diff whenever the upstream spec changes.

`src/api.ts` — **same-origin** `baseUrl`. OpenAPI paths stay as designed
(`/hello`, `/todos`); nginx strips `/api` before proxying.

```ts
import createClient from "openapi-fetch";
import type { paths } from "./generated/todo-api";

export const todoApi = createClient<paths>({ baseUrl: "/api" });

// extra sibling:
// export const otherApi = createClient<paths>({ baseUrl: "/api/other-api/" });
```

**Done when:** no `env.API_BASE_URL`, no `env.TODO_API_URL`, no
`window._env_` key used as an API host.

`Dockerfile` — multi-stage onto stock `nginx:alpine`. **Do not set `ENTRYPOINT`**
— the image already runs `/docker-entrypoint.sh`, which runs
`/docker-entrypoint.d/*.sh` then execs `CMD`.

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm i
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx/default.conf /etc/nginx/conf.d/default.conf
COPY nginx/15-aep-api-proxy.sh /docker-entrypoint.d/15-aep-api-proxy.sh
RUN chmod +x /docker-entrypoint.d/15-aep-api-proxy.sh
EXPOSE 9090
CMD ["nginx", "-g", "daemon off;"]
```

`.dockerignore` — beside it, so `COPY . .` uploads this app's sources rather than
a local `node_modules` and a stale `dist`, both of which the builder stage makes
for itself:

```text
node_modules
dist
```

`mock/` stays in the context: `vite.config.ts` imports `mock/plugin`, so the
production build needs the directory on disk even though it ships none of it.

**Done when:** Dockerfile COPYs the drop-in to `/docker-entrypoint.d/` and has
no `ENTRYPOINT` line, and `.dockerignore` sits beside it.

`workload.yaml` follows your prompt — as given when it carries one, else per the
component contract. Consumer connection to the sibling: `visibility: project`,
`envBindings.address: <DEP_NAME>_URL` (pod, for nginx). Any default under
`configurations.env` arrives as a `window._env_` entry.

**Done when:** this app's dependency on the sibling is `visibility: project`
(never `external`). The sibling *service's* own endpoint lists all three of
`project`, `internal` and `external` — `internal` is what admits the gateway to
the service's NetworkPolicy, and without it every `/api` call answers `503`.
That file is the Go (or other backend) skill's to write: leave all three in
place rather than stripping `external` because this SPA uses `/api`
(`workload-and-wiring` covers what each item earns).

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| SPA throws on load: `window._env_ not set` | `/env-config.js` failed to load — path wrong, 404, or the `<script>` was `defer`/`async` | Make the tag synchronous in `<head>`, BEFORE the bundle's `<script type="module">`. |
| `nginx: [emerg] host not found in upstream "…"` at pod start | Literal `proxy_pass http://hostname` (startup DNS) or leftover `/oidc/` block | Use the asset conf (`proxy_pass http://$api_backend`) and the drop-in; delete `/oidc/`. |
| Browser CORS error calling the sibling API | `baseUrl` is the public gateway URL or `window._env_.API_BASE_URL` | `baseUrl: "/api"`. |
| `/api` 502, SPA otherwise fine | API pod down, or drop-in left `TODO_API_URL` when the dep is named something else | Align both `API_URL="${…}"` lines with the dependency name; 502 while the API is down is expected. |
| `/api` 400 `no header value found for 'x-user-id'` | The proxy took the direct-Service lane, so nothing injected identity | Check the pod log line `aep-api-proxy: /api -> … [lane]`. `direct Service` means `<DEP>_GATEWAY_URL` was unset: the provider's design has no `exposesAPI.auth`, or the drop-in names the wrong variable. |
| `/api` 404 from the gateway | The rewrite dropped the context prefix | `nginx/default.conf` must rewrite to `__API_CONTEXT__/$1`, not `/$1`. |
| `/api` 503 through the gateway | The gateway authenticated but cannot reach the service | The provider endpoint needs `internal` in its `workload.yaml` visibility (`workload-and-wiring`). |
| Types in `src/generated/*` don't match the live service | Upstream `openapi.yaml` changed since last generation | Re-run the `openapi-typescript` command and commit the diff. |
| Docker build succeeds but ships stale/hand-written shapes, or fails `ENOENT ../specs/...` | `src/generated/` wasn't committed — the per-component build context is this app's folder alone | Generate and commit `src/generated/` before PR. |
| Build red on `TS2307: Cannot find module './generated/…'` (plus a burst of `TS7006` implicit-`any`) while `tsc --noEmit` is clean locally | `src/generated/` is **git-ignored**, usually by an unanchored `generated/` in the repo-root `.gitignore` written for a backend component. `git add` skipped it at exit 0 and `git status` stayed clean | `git check-ignore -v src/generated/*` names the offending line. Anchor that pattern (`/onboarding-api/generated/`), then re-add. The `TS7006` rows are downstream of the missing types and vanish with them. Never `git add -f`. |
