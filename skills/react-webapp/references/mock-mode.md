# Mock mode

A second way to run this same app: `npm run dev:mock` stands it up on a laptop
or in a build sandbox with **no cluster, no sibling service and no IDP** behind
it, so its screens can be opened and clicked before anything is deployed. What
you owe the walk (`mock-verification`) is the harness below, working.

**Production is the default and mock is the opt-in.** `npm run build` eliminates
the whole mock branch as dead code, ships no `msw`, and produces an image
byte-identical to one built from an app that has no `mock/` at all. That
property is what makes mock mode safe to commit, so the check that proves it is
part of Verify.

## What it substitutes

The platform gives a running pod three things a bare dev server does not. Mock
mode supplies exactly those three and nothing else:

| Missing in a dev server | Production | Mock mode |
|---|---|---|
| the API gateway | enforces each operation's declared scope; 401 | `mock/authz/gateway.ts`, from the contract |
| the sibling service at `/api` | nginx proxies to it | `mock/handlers.ts`, through **MSW** |
| `window._env_` | the platform mounts `/env-config.js` | `mock/plugin.ts` serves it from `mockEnv` |
| sign-in | Thunder, OIDC + PKCE | `mock/authz/session.ts` — `?role=` and `?auth=out` on the URL |

Everything else is the real app: the real router, the real pages, the real
design-system components, the real generated client.

**The gateway and the service are two layers here, as they are in a cell.** They
answer different questions — *may this caller call this operation at all* (the
gateway, 401) and *which of these rows are theirs* (the service, 404) — and
mock mode keeps them in separate files so an app cannot quietly merge them. You
author only the second. The first is read out of `openapi.yaml` at dev-server
start, so it cannot drift from the contract the deployed gateway is rendered
from.

**The API half is [Mock Service Worker](https://mswjs.io).** A service worker
intercepts the app's own `fetch` calls, so the handlers you write are ordinary
request handlers rather than a hand-rolled router. `apps/console` in the
platform's own repository is built the same way. The other two halves are a
dev-server plugin because a request interceptor cannot reach them: `window._env_`
has to exist before the bundle runs, and the worker starts inside it; and
swapping `src/authz/session.ts` is a module substitution, not a request.

## Layout

```
<app-path>/
├── mock/
│   ├── plugin.ts         copied verbatim — env-config, the session swap, the worker
│   │                     script, and the operation table read off openapi.yaml
│   ├── browser.ts        copied verbatim — starts MSW, gateway ahead of handlers
│   ├── handlers.ts       YOURS — the seed data and the request handlers
│   ├── env.ts            YOURS — what window._env_ holds
│   └── authz/            thunder-authentication's — arrives with its `assets/app/` tree
│       ├── contract.ts   verbatim — projects each contract into the operation table
│       ├── gateway.ts    verbatim — the API gateway: the 401s
│       ├── session.ts    verbatim — the substitute for src/authz/session.ts  ┐ only with an
│       └── roles.gen.ts  GENERATED — role -> grants, by `npm run gen`       ┘ auth dependency
├── src/main.tsx      + the dev-only guard that starts the worker
├── vite.config.ts    + the plugin, under `mode === "mock"`
├── tsconfig.json     + `mock` in `include`, + `vite/client` in `types`
└── package.json      + the `dev:mock` script, + `msw`, `yaml` and `@types/node`
```

## 1 · Copy the verbatim files

From the App Path:

```bash
mkdir -p mock
cp "$AEP_SKILLS_DIR/react-webapp/assets/mock-plugin.ts"   mock/plugin.ts
cp "$AEP_SKILLS_DIR/react-webapp/assets/mock-browser.ts"  mock/browser.ts
# the gateway layer, ALWAYS — auth dependency or not:
mkdir -p mock/authz
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/app/mock/authz/contract.ts" mock/authz/contract.ts
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/app/mock/authz/gateway.ts"  mock/authz/gateway.ts
```

With an auth dependency the two `mock/authz/` files above are already in place:
`thunder-authentication`'s one `cp -r` of its `assets/app/` tree laid them down
beside `mock/authz/session.ts`, and `npm run gen` writes
`mock/authz/roles.gen.ts` next to them. Copy them again anyway; a second copy of
an identical file changes nothing.

`contract.ts` and `gateway.ts` are copied **always**, auth dependency or not:
`plugin.ts` and `browser.ts` import them by those paths. Together they are the
gateway layer, and they turn themselves off: a contract that declares no
`oauth2` scheme has no sign-in to enforce, so the table comes back empty and no
handler is registered. The dev server says which it did.

**Copy again even when `mock/` already exists.** These files carry every fix the
platform has made to the harness since this component last saw them, and a
component that keeps its first copy quietly loses them — leaving the walk
reaching for a lever this app has never had. For `mock/authz/session.ts` that
means taking the current asset and re-applying your app's own exports, below.

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` beside each skill's
`SKILL.md`. `plugin.ts` and `browser.ts` are complete as they stand — a change
to either is a change to how every app on the platform mocks, so leave them
exactly as copied and put what this app needs in the files you author.

`mock/authz/session.ts` is the one exception: it is a **module substitution**,
so it must export what YOUR `src/authz/session.ts` exports — `signIn`,
`signOut`, `handleCallback`, `currentUser`, `accessToken`, `tokenIsValid`, plus
`scopesFromToken` for the header badge. Where your app's session module adds to
it, add a mock of that here too, or the swap fails to compile. Removing an
export it does not have is fine as well.

**`src/authz/gates.tsx`, `src/authz/core.ts` and `src/authz/screens.ts` are NOT
substituted.** They read `user.scope` off whatever session module is in play,
so the app's real authorization code — the route guards, the hidden nav items,
`Forbidden`, `NoAccess`, the role badge, the 401 rule — runs unchanged in mock
mode. That is the whole point of the mock emitting a scope string rather than a
role name: what you walk is what deploys.

**Never run `npx msw init`.** It copies the worker script into `public/`, where
it would be committed and then shipped inside every production image;
`mock/plugin.ts` serves that script from the installed package instead.

## 2 · Wire the four project files

`vite.config.ts` — the plugin is added under `mock` mode only. **These four
snippets are ADDITIONS to the files you already have, not replacements for
them**; each is written as the whole file so the shape is unambiguous, and
pasting one over a file that already carried something else is how a `test`
block or a `types` entry silently disappears:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { mockMode } from "./mock/plugin";

export default defineConfig(({ mode }) => ({
  plugins: [react(), ...(mode === "mock" ? [mockMode()] : [])],
  // KEEP whatever else this file carried — a `test: { … }` block for vitest,
  // `server`, `resolve`, anything. Only `plugins` and the arrow-function form
  // are what mock mode needs.
}));
```

`src/main.tsx` — the one place `src/` mentions mock mode, and the reason it is
allowed to:

```tsx
// Dev-only, dynamic-import-guarded. `import.meta.env.DEV` is statically false
// in a production build, so this branch and the msw chunk are both eliminated.
async function enableMocking(): Promise<void> {
  if (!import.meta.env.DEV || import.meta.env.MODE !== "mock") return;
  const { startMockWorker } = await import("../mock/browser");
  await startMockWorker();
}

void enableMocking().then(() => {
  createRoot(document.getElementById("root")!).render(<App />);
});
```

**This is the single exception to this skill's `import.meta.env` rule**, and it
is a narrow one. The ban is on reading *configuration* from the build
(`import.meta.env.VITE_*`), which arrives `undefined` in production because the
platform delivers config through `window._env_` at request time. `DEV` and
`MODE` are not configuration: they are literals Vite substitutes at build time,
which is exactly what lets the bundler prove the branch is dead and drop it.
Read no other key, and read these two nowhere else.

`package.json` — one script, and `msw`, `yaml` and `@types/node` in
`devDependencies`:

```json
"scripts": { "dev:mock": "vite --mode mock" }
```

`yaml` is for reading the contracts — the plugin's, and `scripts/gen-authz.mjs`
shares it for the same job. Both import it **dynamically**: the plugin inside
`mockMode()`, so a production `vite build` — which loads `vite.config.ts` and
never calls it — does not resolve it at all; the generator at the point it
reads a contract, so an image build where the specs are out of reach keeps the
committed tables whether or not `yaml` resolved.

The table then rides `/env-config.js`, beside `window._env_`, because both have
the same requirement: in place before the bundle runs. Vite's `define` is the
obvious reach and is the wrong one — it is skipped for client modules in dev
(`vite:define` returns early when `consumer === "client" && !isBuild`), so the
table would be correct in a production build nobody runs and MISSING from
`dev:mock`, silently, with the gateway disabling itself.

`tsconfig.json` — two edits, both **additive**: add the entries that are
missing and keep the ones that are there. An app with a vitest suite already
carries `"vitest/globals"` in `types`, and dropping it turns every `describe` in
the suite into `TS2304: Cannot find name`.

```jsonc
"types": ["vite/client", "node", /* …and whatever was already here */],
"include": ["src", "mock", "vite.config.ts", /* …and whatever was already here */]
```

The `include` puts the whole directory in the ordinary Verify run, rather than
letting it fail the first time somebody starts it. The `types` entry is what
gives `import.meta.env` a type at all — without `vite/client` the guard in
`main.tsx` is `TS2339: Property 'env' does not exist on type 'ImportMeta'`, and
`node` is what types the plugin's `node:` imports.

## 3 · Author `mock/handlers.ts`

One handler per operation in the sibling's `openapi.yaml`, exported as
`handlers`. Paths are same-origin and carry the `/api` prefix, exactly as the
app calls them.

**The contract is `openapi.yaml`, the same document `src/generated/` came from.**
Reading shapes off your own page code instead is what makes a mock agree with a
bug: both halves would then be wrong in the same direction and the screen would
look right. Response bodies match the schemas; status codes match the responses
the document declares, 4xx included.

**Hold state in module scope so the app behaves like an app.** A create shows up
in the next list, a delete removes it, an edit persists. That state lives in the
PAGE, not in the server: `setupWorker` resolves every request in the page's own
JS context, so any full page load — a reload, a typed or opened URL, a link that
leaves the SPA — re-runs this module and puts the seed data back. Only in-app
navigation carries a change forward. Say that in the comment you write here,
because the reset cuts both ways: it is what makes a verification run
repeatable, and it is also why a row created a moment ago can vanish if the run
leaves the app mid-scenario. Seed enough rows that a table, its empty state and
its pagination are all reachable.

**Write NO scope check here.** Whether an operation may be called at all is
`mock/authz/gateway.ts`'s answer, read from the contract, exactly as it is the API
gateway's answer in a cell. A handler that re-checks the operation's handle is a
second copy of the contract that nothing keeps in sync — the same defect the
real services no longer carry.

**What a handler DOES owe** is what a real service owes: its path's reach. A
`/api/me/…` handler answers the caller's rows and nothing else — resolved from
the mock identity, never from a query parameter — and a handler outside `/me/`
answers every row. Nothing widens on a scope; a caller who may see every row
calls the every-row operation. A row that exists and is not theirs is a **404**
under `/me/…`, never 403. `scopesFromToken` is exported for the header badge
and for nothing a handler decides with.

If a role the design means to serve cannot reach an operation with the grants
`security.json` gives it, **that is a defect in the design**: report the role,
the screen and the handle, in the walk's progress lines and in your own report.
Never widen a handler or a route guard to make it pass — and there is now
nothing in a handler to widen, which is the point.

```ts
import { http, HttpResponse } from "msw";
import type { components } from "../src/generated/todo-api";
import { scopesFromToken } from "./authz/session";

type Todo = components["schemas"]["Todo"];

// The caller this mock speaks for, shaped like the gateway assertion: a
// directory id AND a login name, because which one a row stores is the model's
// choice. Seed one row owned by somebody else, or `/me/` and every-row look
// identical.
export const mockCaller = {
  userId: "01a0ab00-0000-7000-8000-000000000001",
  username: "mock-owner",
};

let todos: Todo[] = [
  { id: "1", title: "Buy milk", done: false, owner: mockCaller.userId },
  { id: "2", title: "Ship the thing", done: true, owner: mockCaller.userId },
  { id: "3", title: "Someone else's", done: false, owner: "not-the-caller" },
];

export const handlers = [
  // The caller's todos — the path says so. No `todos:read` check: a caller who
  // does not hold it was refused by mock/authz/gateway.ts and never reached here.
  // Resolve the caller from `mockCaller`, never a literal: filtering on a
  // constant the seed rows also hardcode makes reach pass by construction.
  http.get("/api/me/todos", () =>
    HttpResponse.json(todos.filter((t) => t.owner === mockCaller.userId)),
  ),

  // Every todo — a different operation, guarded by todos:read-all. Nothing to
  // decide here either: whoever reached it may see it all.
  http.get("/api/todos", () => HttpResponse.json(todos)),

  http.post("/api/me/todos", async ({ request }) => {
    const input = (await request.json()) as { title?: string };
    if (!input?.title) {
      return HttpResponse.json({ error: "title is required" }, { status: 400 });
    }
    const created: Todo = {
      id: String(todos.length + 1),
      title: input.title,
      done: false,
      owner: "mock-owner",
    };
    todos = [...todos, created];
    return HttpResponse.json(created, { status: 201 });
  }),

  http.delete("/api/todos/:id", ({ params }) => {
    const before = todos.length;
    todos = todos.filter((t) => t.id !== params.id);
    return before === todos.length
      ? HttpResponse.json({ error: "not found" }, { status: 404 })
      : new HttpResponse(null, { status: 204 });
  }),
];
```

**Order handlers most-specific first.** MSW takes the first match, so
`/api/todos/archived` has to be registered before `/api/todos/:id` or the
literal path is swallowed by the parameter.

`mock/browser.ts` registers three layers, in the order a request meets them in a
cell: `mock/authz/gateway.ts` first, your handlers next, and last a catch-all that
answers any other `/api` call `501` naming the method and path. The 501 is not
decoration — MSW passes an *unhandled* request through to the network, so
without it a call you forgot would reach the dev server and come back as
index.html with status 200, which reads as a working screen. Seeing a 501 means
a handler is missing, never that the app is wrong.

The layering works because an MSW resolver that returns nothing falls through to
the next matching handler: the gateway answers only the requests it refuses, and
everything it lets past reaches your handler unchanged.

**What a refusal looks like.** A bare **401**, with no body and no
`WWW-Authenticate` — byte for byte what the deployed gateway answers, which
cannot distinguish a missing scope from a dead token. The reason goes to the
browser CONSOLE instead:

```
[mock gateway] 401 GET /todos — the caller does not hold todos:read.
```

Never 403. `src/authz/client.ts` decides between Forbidden and sign-in from the
app's own `expires_at`, and 401-while-my-token-is-valid is the branch the
deployed app actually takes; answering 403 here would walk the other one and
leave that branch — the one whose regression is an endless sign-in loop —
untested.

## 4 · Author `mock/env.ts`

`mockEnv` carries the keys **the platform actually emits** for this component,
and only those — the key table under Constraints in `SKILL.md`: this app's own
`<DEP>_*` OIDC keys, anything it declared under `configurations.env`, and
`<NAME>_URL` for an `external`-kind dependency.

```ts
export const mockEnv = {
  USER_AUTH_CLIENT_ID: "mock-client",
  USER_AUTH_ISSUER: "https://mock-idp.test",
  // No <DEP>_JWKS_URL: the platform emits it, src/env.ts does not declare it
  // (the browser never validates a token), so mock mode does not carry it.
  // The OIDC scopes are `group` and `ou`, SINGULAR — not `groups` — and the
  // project's own catalog handles follow them, exactly as the platform requests
  // them. `<DEP>_RESOURCE` is emitted too; src/env.ts throws without it.
  USER_AUTH_SCOPES: "openid profile email group ou todos:read todos:read-all todos:submit",
  USER_AUTH_RESOURCE: "https://mock-idp.test/resources/mock-project",
};
```

For a correct app that is exactly the set `src/env.ts` declares, and the app just
runs. Where the two differ, **mock mode reproduces production**: `src/env.ts`
throws on the missing key here for the same reason it would throw in a pod, and
the app failing to start IS the finding.

Do not add a key to make the screen appear. A sibling service's address is never
a browser key — it is same-origin `/api` (Constraints) — so supplying one here
turns the one defect this arrangement exists to catch into a green screen.

## 5 · The personas — `mock/authz/roles.gen.ts`, generated

**Role → grants** is not authored. `npm run gen` writes
`mock/authz/roles.gen.ts` from `specs/design/security.json`'s `roles[]`, in the
order that file declares them — the first is who a visitor is with no `?role=`
on the URL — alongside the two `src/authz/*.gen.ts` tables
(`thunder-authentication` §1). Commit it with them; mock mode restates nothing.

`mock/authz/session.ts` turns the `?role=` on the URL into the space-separated
scope string a real token would carry — the OIDC scopes plus that role's grants
— so `src/authz/gates.tsx` gates exactly as it does in production. Three URLs
the walk depends on:

| URL | What it produces |
|---|---|
| no `?role=` | the first role's grants |
| `?role=Manager` | that role's grants — what that role, and only that role, reaches |
| `?role=` (empty) | signed in, holding **no project scope**: the `NoAccess` case |

A name absent from the list grants nothing, which is how a screen is checked
against a role that must not reach it. Roles compose: `?role=Owner,Manager`
holds the union.

**The role survives internal navigation.** The asset persists the last explicit
`?role=` in `sessionStorage`, because the app's own links and redirects carry
bare paths — a mock that re-read the URL on every mount reverted every non-default
role to the first one on the first click. That fix lives in the asset, and this
is why you re-copy the asset rather than patch your project's copy.

**Do not restate the grants anywhere.** `src/authz/roles.gen.ts` is generated
from the same file for production; the mock table exists as a separate output
only because `mock/` may not import from `src/` in a way that survives the
substitution. A hand-edited entry in either is overwritten on the next `gen`.

## Verify

Added to this stack's ordinary sequence, after `npm run build`:

```bash
npm run build && ! grep -rq mockServiceWorker dist/
```

`build` is `npm run gen && tsc --noEmit && vite build`, so the generator runs
first and there is nothing to run by hand. On an app with an auth dependency
that ordering is not optional: a stale generated table type-checks green while
the app gates on operations the design no longer has, which is why `gen` is
wired as the first half of `build` rather than left to a habit.

**Done when:** the build exits 0 and the grep finds nothing. A hit means the
guard in `src/main.tsx` was written so the bundler could not prove the branch
dead — production would then ship the mock, which is the one failure this whole
arrangement exists to prevent. `mockServiceWorker` is the marker because msw's
browser build carries that string literally and nothing else in a bundle does;
a bare `msw` matches three characters of the base64 font data a design system
inlines, and fails a clean build.

You never start it. The walk (`mock-verification`) starts and stops the dev
server through its own script, and a server you start here has nothing to reap
it: the runner image has no `pkill`, and a stray `vite` stays in the pod's
memory for the rest of the run.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Blank screen, console says `window._env_ not set` | `/env-config.js` did not load — the plugin is not in the mode you started | Start with `--mode mock`; the plugin serves that file. The gateway's table rides the same script, so this also silently disables it. |
| Throws `<DEP>_URL not set in window._env_` | The app reads a sibling's address from `window._env_`, which the platform does not emit | A real production failure, reproduced. Fix the app — `baseUrl: "/api"` — never `mockEnv`. |
| A call answers `501 {"error":"mock: no handler for …"}` | No handler matches — often the `/api` prefix is missing from the handler's path | Handler paths are what the app calls: `/api/todos`, not `/todos`. |
| A parameterised route swallows a literal one | Registration order in `mock/handlers.ts` | Most specific first — `/api/me/todos` before `/api/todos/:id`. (The gateway layer sorts its own table; this is about yours.) |
| The first call of the page escapes the mock | The app rendered before the worker started | `enableMocking()` is awaited before `createRoot`; keep that order. |
| The app renders as the wrong role | `mock/authz/roles.gen.ts` is stale — `gen` did not run after `security.json` changed | `npm run gen`; the first entry is the default, and the order is the design's. |
| Every screen is reachable under every `?role=` | A hand-written role table with names and no grants, so the scope string carries no handle and the gate was written to fall open | Delete it; `npm run gen` writes the table with its grants, and `authz` hides what is not held. |
| Every role lands on `NoAccess` | `mockEnv`'s `USER_AUTH_SCOPES` is v1 (`groups`, no project handles), or the mock user carries no `scope` field | `openid profile email group ou <the project's handles>`; the mock user's `scope` is what `authz` reads. |
| The first internal click reverts to the default role | A project-local `mock/authz/session.ts` that re-reads `?role=` on every mount | Re-copy the asset; it persists the last explicit `?role=` in `sessionStorage`. |
| `Forbidden` is unreachable in mock mode | The gateway layer is off — no contract was found, or none declares an `oauth2` scheme | Read what the dev server printed at startup: it says how many operations it enforces and from which files, or why it enforces none. |
| Every `/api` call 401s under every role | The handlers were reached from a page that calls before sign-in resolves, or the generated role table is stale and grants nothing | Check the `[mock gateway]` console line — it names the operation and the handle it wanted. |
| A screen the role's flow walks is hidden from the rail under `?role=<that role>` | The role's `grants` lack the handle of the operation the screen `loads` | A design finding, not a mock one: report the role, the screen, the operation and the handle. Never widen `loads` or the table. |
| A call the contract does declare answers 501 | The gateway let it through and no handler matched | Add the handler; the 501 is the mock's own gap, not a refusal. |
| `dist/` contains `msw` or `mock/` | The dev-only guard is not statically decidable | Keep the `import.meta.env.DEV` test first and the import dynamic. |
| `tsc --noEmit` passes but `dev:mock` fails on a type | `mock` is missing from `tsconfig.json`'s `include` | Add it, and re-run the type-check. |
| `TS2339: Property 'env' does not exist on type 'ImportMeta'` | `vite/client` is not in `tsconfig.json`'s `types` | Add `"types": ["vite/client", "node"]`. |
| `mockServiceWorker.js` appears in `public/` | `npx msw init` was run | Delete it. The plugin serves the script from the installed package. |
