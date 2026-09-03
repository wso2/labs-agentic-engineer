# Mock mode

A second way to run this same app: `npm run dev:mock` stands it up on a laptop
or in a build sandbox with **no cluster, no sibling service and no IDP** behind
it, so its screens can be opened and clicked before anything is deployed. The
clicking is a separate agent's job once your build is clean — `mock-verification`
is its procedure, and step 5 of this skill's Development flow. What you owe it is
the harness below, working.

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
| the sibling API at `/api` | nginx proxies to the service | `mock/handlers.ts`, through **MSW** |
| `window._env_` | the platform mounts `/env-config.js` | `mock/plugin.ts` serves it from `mockEnv` |
| sign-in | Thunder, OIDC + PKCE | `mock/auth.ts` — `?role=` and `?auth=out` on the URL |

Everything else is the real app: the real router, the real pages, the real
design-system components, the real generated client.

**The API half is [Mock Service Worker](https://mswjs.io).** A service worker
intercepts the app's own `fetch` calls, so the handlers you write are ordinary
request handlers rather than a hand-rolled router. `apps/console` in the
platform's own repository is built the same way. The other two halves are a
dev-server plugin because a request interceptor cannot reach them: `window._env_`
has to exist before the bundle runs, and the worker starts inside it; and
swapping `src/auth.ts` is a module substitution, not a request.

## Layout

```
<app-path>/
├── mock/
│   ├── plugin.ts     copied verbatim — env-config, the auth swap, the worker script
│   ├── browser.ts    copied verbatim — starts MSW with your handlers
│   ├── handlers.ts   YOURS — the seed data and the request handlers
│   ├── env.ts        YOURS — what window._env_ holds
│   ├── roles.ts      YOURS — the roster        ┐ only with an auth
│   └── auth.ts       copied verbatim           ┘ dependency
├── src/main.tsx      + the dev-only guard that starts the worker
├── vite.config.ts    + the plugin, under `mode === "mock"`
├── tsconfig.json     + `mock` in `include`, + `vite/client` in `types`
└── package.json      + the `dev:mock` script, + `msw` and `@types/node`
```

## 1 · Copy the verbatim files

From the App Path:

```bash
mkdir -p mock
cp "$AEP_SKILLS_DIR/react-webapp/assets/mock-plugin.ts"  mock/plugin.ts
cp "$AEP_SKILLS_DIR/react-webapp/assets/mock-browser.ts" mock/browser.ts
# only if this component declares an auth platform-resource dependency:
cp "$AEP_SKILLS_DIR/react-webapp/assets/mock-auth.ts"    mock/auth.ts
```

**Copy again even when `mock/` already exists.** These files carry every fix the
platform has made to the harness since this component last saw them, and a
component that keeps its first copy quietly loses them — leaving the walk in
step 5 reaching for a lever this app has never had. For `auth.ts` that
means taking the current asset and re-applying your app's own exports, below.

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` beside this skill's
`SKILL.md`. `plugin.ts` and `browser.ts` are complete as they stand — a change
to either is a change to how every app on the platform mocks, so leave them
exactly as copied and put what this app needs in the files you author.

`auth.ts` is the one exception: it is a **module substitution**, so it must
export what YOUR `src/auth.ts` exports. The copy carries the standard surface;
where your app's auth module adds to it — a `resolveRole`, a `Role` type, a
`getGroups` — add a mock of that here too, or the swap fails to compile. Removing
an export it does not have is fine as well.

**Never run `npx msw init`.** It copies the worker script into `public/`, where
it would be committed and then shipped inside every production image;
`mock/plugin.ts` serves that script from the installed package instead.

## 2 · Wire the four project files

`vite.config.ts` — the plugin is added under `mock` mode only:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { mockMode } from "./mock/plugin";

export default defineConfig(({ mode }) => ({
  plugins: [react(), ...(mode === "mock" ? [mockMode()] : [])],
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

`package.json` — one script, and `msw` + `@types/node` in `devDependencies`:

```json
"scripts": { "dev:mock": "vite --mode mock" }
```

`tsconfig.json` — two edits:

```jsonc
"types": ["vite/client", "node"],          // in compilerOptions
"include": ["src", "mock", "vite.config.ts"]
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

```ts
import { http, HttpResponse } from "msw";
import type { components } from "../src/generated/todo-api";
import { rolesFromToken } from "./auth";

type Todo = components["schemas"]["Todo"];

let todos: Todo[] = [
  { id: "1", title: "Buy milk", done: false, owner: "mock-owner" },
  { id: "2", title: "Ship the thing", done: true, owner: "mock-owner" },
];

export const handlers = [
  http.get("/api/todos", ({ request }) => {
    // Role scoping is part of the story, so the mock enforces it too.
    const roles = rolesFromToken(request.headers.get("authorization"));
    return HttpResponse.json(
      roles.includes("Manager") ? todos : todos.filter((t) => t.owner === "mock-owner"),
    );
  }),

  http.post("/api/todos", async ({ request }) => {
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

`mock/browser.ts` adds one handler of its own after yours: a catch-all that
answers any other `/api` call `501` naming the method and path. That is not
decoration — MSW passes an *unhandled* request through to the network, so
without it a call you forgot would reach the dev server and come back as
index.html with status 200, which reads as a working screen. Seeing a 501 means
a handler is missing, never that the app is wrong.

## 4 · Author `mock/env.ts`

`mockEnv` carries the keys **the platform actually emits** for this component,
and only those — the key table under Constraints in `SKILL.md`: this app's own
`<DEP>_*` OIDC keys, anything it declared under `configurations.env`, and
`<NAME>_URL` for an `external`-kind dependency.

```ts
export const mockEnv = {
  USER_AUTH_CLIENT_ID: "mock-client",
  USER_AUTH_ISSUER: "https://mock-idp.test",
  USER_AUTH_JWKS_URL: "https://mock-idp.test/.well-known/jwks.json",
  USER_AUTH_SCOPES: "openid profile email groups",
};
```

For a correct app that is exactly the set `src/env.ts` declares, and the app just
runs. Where the two differ, **mock mode reproduces production**: `src/env.ts`
throws on the missing key here for the same reason it would throw in a pod, and
the app failing to start IS the finding.

Do not add a key to make the screen appear. A sibling service's address is never
a browser key — it is same-origin `/api` (Constraints) — so supplying one here
turns the one defect this arrangement exists to catch into a green screen.

## 5 · Author `mock/roles.ts` — only with an auth dependency

The roster comes from `specs/design/security.json`, in the order that file
declares it: the first is who a visitor is with no `?role=` on the URL.

```ts
// specs/design/security.json → roles[].name
export const mockRoles = ["Manager", "Owner"];
```

## Verify

Added to this stack's ordinary sequence, after `npm run build`:

```bash
npm run build && ! grep -rqE "mock/|msw" dist/
```

**Done when:** the build exits 0 and the grep finds nothing. A hit means the
guard in `src/main.tsx` was written so the bundler could not prove the branch
dead — production would then ship the mock, which is the one failure this whole
arrangement exists to prevent.

To drive it by hand:

```bash
npm run dev:mock -- --port 5173 --strictPort
```

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Blank screen, console says `window._env_ not set` | `/env-config.js` did not load — the plugin is not in the mode you started | Start with `--mode mock`; the plugin serves that file. |
| Throws `<DEP>_URL not set in window._env_` | The app reads a sibling's address from `window._env_`, which the platform does not emit | A real production failure, reproduced. Fix the app — `baseUrl: "/api"` — never `mockEnv`. |
| A call answers `501 {"error":"mock: no handler for …"}` | No handler matches — often the `/api` prefix is missing from the handler's path | Handler paths are what the app calls: `/api/todos`, not `/todos`. |
| A parameterised route swallows a literal one | Registration order | Most specific first — `/api/todos/archived` before `/api/todos/:id`. |
| The first call of the page escapes the mock | The app rendered before the worker started | `enableMocking()` is awaited before `createRoot`; keep that order. |
| The app renders as the wrong role | `mock/roles.ts` is ordered differently from `security.json` | The first entry is the default; keep the file's order. |
| `dist/` contains `msw` or `mock/` | The dev-only guard is not statically decidable | Keep the `import.meta.env.DEV` test first and the import dynamic. |
| `tsc --noEmit` passes but `dev:mock` fails on a type | `mock` is missing from `tsconfig.json`'s `include` | Add it, and re-run the type-check. |
| `TS2339: Property 'env' does not exist on type 'ImportMeta'` | `vite/client` is not in `tsconfig.json`'s `types` | Add `"types": ["vite/client", "node"]`. |
| `mockServiceWorker.js` appears in `public/` | `npx msw init` was run | Delete it. The plugin serves the script from the installed package. |
