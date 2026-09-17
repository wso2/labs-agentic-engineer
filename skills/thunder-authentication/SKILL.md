---
name: thunder-authentication
description: "Apply when a component sits on the project's sign-in — a SPA whose users authenticate through the auth platform-resource dependency, or a protected backend those signed-in users call."
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Thunder Authentication

End-user identity is delegated to Thunder, the platform's Identity Provider,
which the API gateway is wired to as its external IDP. **What a caller may do is
the access token's `scope` — one authority, nothing else.**

- **The protected backend** never sees a token: the gateway validates it,
  enforces the scope each operation declares in `openapi.yaml`, and hands the
  service a **gateway-signed assertion** carrying the same `sub` and `scope`.
  See `api-management` for the gateway's side of that contract.
- **The SPA** signs the user in with OIDC Authorization Code + PKCE, asking for
  a **resource indicator** so the token is minted for this project, reads the
  granted permissions from `user.scope`, and gates every screen on the scope of
  the **operation that screen loads**.

**Nothing reads the groups claim to decide anything** — not the SPA, not the
service, not for display. Permissions reach code as scopes. The invariants
behind every rule here are stated once in `authorization-model`; this skill is
the mechanics.

The OAuth client itself is **platform-owned**: you never create, compute, or
hardcode any part of it.

## Where permissions come from

`specs/design/security.json` is the project's permission catalog. Read it before
writing any authorization code:

| Field | What you do with it |
|---|---|
| `permissions[].resource` + `actions[].handle` | joined by `:` these are the **scope handles** — `claims:read`, `reports:export`. This is the closed set. |
| `roles[].grants` | which handles a role holds. Drives the SPA's header badge and the "which role unlocks this" wording; `src/authz/roles.gen.ts` is generated from it. |
| `roles[].assignTo`, `groups[]` | provisioning only. **No application code reads these.** |

Two things are **not** in this file, on purpose. Which **rows** an operation
reaches is the operation's path in `openapi.yaml`: under `/me/` the caller's
(or a relation's — `/me/team/claims`), anywhere else every row.
`openapi-conventions` owns that rule; here it means a `/me/…` handler resolves
rows through the assertion's `sub`, and nothing else ever filters. And which
**screens** a caller may open is the scope of the operation each screen
**loads** — the one handle that operation already names in `openapi.yaml`. No
screen table exists anywhere; the generator projects every contract into
`src/authz/operations.gen.ts` and you bind each screen to its load operation
once, in `src/authz/screens.ts` (§5).

The platform creates the resource server, the roles and the test users when the
user clicks Build. Never write user-, group- or role-provisioning code, and
never seed a roster: an account you create is not one the platform can hand to
the validation agent.

**Dev clusters** ship a default Thunder admin: `admin` / `admin`, in the
`Administrators` group. That group administers the PLATFORM — it is not one of
your app's roles, and the platform will not grant it your app's scopes.

---

# SPA sign-in and screen gating

## Order of work

`src/env.ts` (add the four `<DEP>_*` keys the SPA reads to the `react-webapp` shim) →
copy the `assets/app/` tree (below) → `scripts/gen-authz.mjs` wired into the
build and run once, so the three generated files exist → the `USER_AUTH_`
prefix in `src/authz/session.ts` → **`src/api.ts` stripped of every
authorization rule of its own** (§4) → a **`/callback` route** that calls
`handleCallback()` once on mount → `src/authz/screens.ts` and the route guards
in `src/App.tsx`. Verify with the `react-webapp` build check.

## What you copy

The assets are laid out as the app is, so the install is **one command** and
nothing is renamed on the way in. **Copy them; do not write your own.** The
rules they carry were measured against this IdP and this gateway, and every one
of them has already been got wrong by a real run.

```bash
cp -r "$AEP_SKILLS_DIR/thunder-authentication/assets/app/." .
```

That puts eight files in place, all verbatim:

| Path in the app | What it is |
|---|---|
| `scripts/gen-authz.mjs` | the generator — run by `npm run gen`, emits the three `*.gen.ts` files |
| `src/authz/session.ts` | sign-in, the session, `tokenIsValid()` — the ONE edit: the `USER_AUTH_` prefix becomes YOUR dependency's |
| `src/authz/core.ts` | the pure rules, no imports — testable in plain node |
| `src/authz/gates.tsx` | `AuthzProvider`, `Can`, `RequireOperation`, `Forbidden`, `NoAccess` — restyle the markup with the pinned design system, keep the words and every export name |
| `src/authz/client.ts` | the 401 rule and the `openapi-fetch` middleware |
| `mock/authz/session.ts` | mock mode's substitute for `src/authz/session.ts` (`?role=`, `?auth=out`) |
| `mock/authz/gateway.ts`, `mock/authz/contract.ts` | mock mode's API gateway: the contract projected into a table, and the 401s |

Two more ship as **patterns** beside `app/`, and are adapted rather than copied:

| Asset | Becomes | How |
|---|---|---|
| `assets/screens.example.ts` | `src/authz/screens.ts` | replace `SCREEN_ROUTES` with YOUR screens — key, label, path and the operation each **loads** — in rail order; keep the rest |
| `assets/App.example.tsx` | `src/App.tsx` | replace `PAGE_BY_KEY` *and* `APP_NAME` with yours; the ROUTING STRUCTURE is prescribed |

One file is not copied and is not optional: **`src/api.ts`, the per-service
client you already have.** Whatever it carried about authorization comes OUT —
the bearer it attached, any `WWW-Authenticate` read, and above all
`if (res.status === 401) signIn()` — and it calls `src/authz/client.ts`'s
`authorizationHeader()` and `classifyResponse()` instead (§4 has the
`openapi-fetch` middleware to paste). Two modules that both decide what a 401
means is the same bug as one module deciding it wrong, and on an app that
already exists this is the only edit the copy cannot make for you.

The two example files are written against the Expense Tracker — its
`SCREEN_ROUTES`, `PAGE_BY_KEY` and `APP_NAME` all name ITS screens and ITS
operations. They are a shape to follow, not a fixture to ship. If your app
already holds its name somewhere (the wireframes' `navbar` title, usually
`src/appName.ts`), import it rather than declaring a second copy.

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's
`SKILL.md`. The two `*.example.*` files and everything under `assets/__tests__/`
are **not** copied into the app: the examples are patterns you adapt, and the
tests are this skill's own regression suite. `react-webapp`'s mock harness
(`mock/plugin.ts`, `mock/browser.ts`) is copied separately and imports the
`mock/authz/` files above by those paths.

## Constraints

**Keys are derived from YOUR dependency name.** The platform emits every
platform-resource dependency's outputs into `window._env_` as
`<UPPER_SNAKE(depName)>_<UPPER_SNAKE(outputName)>`. There is **no fixed prefix** —
it is the UPPER_SNAKE of the dependency `name` the architect chose. The auth
resource type outputs `client_id`, `issuer`, `jwks_url`, `scopes` and
`resource`, so a dependency named `user-auth` yields:

| Key (dep `user-auth`) | Generic form | Meaning |
|---|---|---|
| `USER_AUTH_CLIENT_ID` | `<DEP>_CLIENT_ID` | this app's platform-owned OAuth client id |
| `USER_AUTH_ISSUER` | `<DEP>_ISSUER` | OIDC issuer / authority for `oidc-client-ts` |
| `USER_AUTH_JWKS_URL` | `<DEP>_JWKS_URL` | JWKS endpoint. Emitted, and **not** in the SPA's `Env`: the browser never validates a token — the API gateway does — so no asset reads it. Leave it out of `src/env.ts` and out of `mock/env.ts` |
| `USER_AUTH_SCOPES` | `<DEP>_SCOPES` | space-separated scopes to request: the OIDC ones (`openid profile email group ou`) plus the project's catalog handles |
| `USER_AUTH_RESOURCE` | `<DEP>_RESOURCE` | the project's **resource-server identifier** — the RFC 8707 `resource` indicator, and the `aud` the gateway pins |

Hardcoding a fixed prefix — or any prefix other than YOUR dependency's name —
gives `undefined` at module load and a redirect to `undefined/oauth2/authorize`.

**Ask for the resource indicator on all three legs, or every API call 401s.**
The indicator is what makes the IdP mint an access token whose `aud` is this
project's resource server and whose `scope` is narrowed to what this user's
roles grant there. Without it the token carries the IdP's default audience and
the gateway rejects it **before it reads a scope** — while sign-in itself looks
perfectly healthy.

`oidc-client-ts` 3.5.0 carries `resource` on exactly ONE of the three legs by
itself. Measured against the library, not assumed:

| Leg | Does `settings.resource` reach it? | What you set |
|---|---|---|
| `/authorize` redirect | **yes** — appended to the authorize URL | `resource: env.<DEP>_RESOURCE` in the `UserManager` settings |
| code → token exchange | **no** — `_processCode` sends only its own fields plus `...extraTokenParams` | `extraTokenParams: { resource: env.<DEP>_RESOURCE }` |
| refresh / silent renew | **no** — `signinSilent(args)` forwards `args` and never falls back to `this.settings` | pass `{ resource, extraTokenParams }` to **every** `signinSilent()` call |

`src/authz/session.ts` sets all three. A run that copied a settings-only
snippet got a green build, a healthy-looking sign-in, and a wrong `aud` on every
token.

On **this** IdP leg 3 is survivable: Thunder binds the refresh token to the
resource server it was issued for, so an argument-less renew keeps the same
`aud` (and asking for a *different* resource is refused with `invalid_target`).
Set all three anyway — it is what makes the app portable to an IdP that
re-derives the audience per request, where the omission silently downgrades
every renewed token one access-token lifetime after sign-in.

**Two settings that do not exist in 3.5.0.** Both are type errors, and both look
plausible enough that they get written:

- **`useRefreshToken`** is a method on `OidcClient`, not a member of
  `UserManagerSettings`. `automaticSilentRenew: true` plus a refresh token in
  the response is what makes renewal use the refresh grant.
- **`clockSkewInSeconds`** is not a member either. The settings that do exist
  are `accessTokenExpiringNotificationTimeInSeconds` (default 60 — the lead time
  the silent renew fires at) and `staleStateAgeInSeconds`. So the grace period
  `tokenIsValid()` allows past `expires_at` is **ours**: `src/authz/session.ts`
  holds a private `CLOCK_SKEW_SECONDS = 60`, the same width as that renew window.

**`client_id` is platform-owned.** It is a platform-derived opaque identifier,
**not** the dependency's `name`; the platform delivers it in
`window._env_.<DEP>_CLIENT_ID`. Same for the client secret and the registered
redirect URIs. Never add Thunder client-provisioning code anywhere — the
platform's Thunder Application operator does it when the dependency provisions —
and never write a `/login` form that POSTs credentials to your own API.

**Compute the redirect URI; there is no key for it.** The platform registers the
SPA's served callback URL once its public URL resolves, and the SPA is served at
its host root (see `react-webapp`), so that URL is `<origin>/callback`.
Reconstruct exactly that in the browser: `window.location.origin + '/callback'`,
and serve the route at `/callback`. Post-sign-in landing is
`window.location.origin`. Neither is an env key.

**Token endpoint is cross-origin.** The browser posts straight to
`<DEP>_ISSUER/oauth2/token`; discovery is
`<DEP>_ISSUER/.well-known/openid-configuration`. Nothing is proxied same-origin,
which is why `react-webapp` does not proxy `/oidc/` — nginx only reverse-proxies
sibling APIs under `/api`.

**Persist the session and renew silently.** The OAuth client is provisioned with
the `refresh_token` grant alongside `authorization_code` + PKCE, so an expiring
access token is renewed by posting the refresh token — no hidden iframe, no
third-party-cookie dependency. The session lives in `localStorage` (a
`WebStorageStateStore`) with `automaticSilentRenew: true`. `sessionStorage` is
per-tab and wiped on close, which forces a re-login on every visit; and without
persistent web storage the PKCE verifier does not survive the redirect at all.

**A refresh narrows, never widens.** Permission scopes are re-evaluated on
renewal, so a grant REMOVED from a role disappears at the next silent renew —
but a grant ADDED never appears on a refresh at all (RFC 6749 §6: a refresh may
not exceed the original grant, and the narrowing sticks to the refresh token).
A new permission needs a full sign-out and sign-in. Say that in the UI where a
user could plausibly wait for a grant to arrive.

**There is no sign-out endpoint.** Thunder's discovery document advertises only
issuer, authorize and token — no `end_session_endpoint` — so `signoutRedirect()`
rejects. Sign-out drops the local session (`removeUser()`) and reloads;
`src/authz/session.ts` already wraps it in that fallback.

**Permissions ride in the access token's `scope`.** `oidc-client-ts` surfaces the
token response's scope string as `user.scope`, and `src/authz/gates.tsx` is the
only module that reads it. Never decode the access token, never hand-parse a JWT, and
**never read `user.profile.groups`** — the groups claim is still issued and
nothing in your app may consume it. There is no `getRoles()`, no role table of
your own, and no default role for a user who matches nothing: a caller whose
token grants nothing sees `NoAccess`.

**`userManager` is never exported.** Every other module reaches the session
through `src/authz/session.ts`'s functions — `signIn`, `handleCallback`,
`signOut`, `currentUser`, `accessToken`, `tokenIsValid`. That list is the
module's whole surface, and it is what lets mock mode substitute the module
wholesale with `mock/authz/session.ts` (`react-webapp`'s
`references/mock-mode.md` owns the wiring). `userManager` is
`oidc-client-ts`'s own object and has no mock substitute, so a single
`export const userManager` compiles in production and breaks the app the moment
anybody opens it without a cluster.

## Implementation

### 1 · The three generated files

`scripts/gen-authz.mjs` reads `specs/design/security.json` **and every contract
under `specs/design/components/`** (`*/openapi.yaml` and
`*/dependencies/*.openapi.yaml`) and emits the files that carry the design into
the bundle. Wire it into `package.json`:

```json
"scripts": {
  "gen":   "node scripts/gen-authz.mjs",
  "build": "npm run gen && tsc --noEmit && vite build"
}
```

**`gen` before `tsc` is not optional.** The whole point of a generated string
literal union is that a handle the design dropped, or an operation the contract
renamed, fails the type check — and a **stale** generated file type-checks
perfectly green.

Run `npm run gen` once by hand and **commit all three outputs**. The generator
walks UP from the app folder to find `specs/design/security.json` (and reads
the contracts under that same `specs/`), because how many levels up the
project's spec tree sits is a layout detail nothing may hardcode. Inside a
per-component image build there is no such ancestor at all — the build context
is the app folder alone — so when the specs are out of reach and all three
committed outputs exist the generator **keeps them, says so on stdout and exits
0**. Exiting 1 there would fail every image build the platform runs. With any
output missing and no specs it exits 1 and names which, which is the
uncommitted-file case. The contract half imports `yaml` (a devDependency every
web app already carries for the mock plugin; `npm i` runs before `npm run
build` in the image) — when `yaml` cannot be resolved the same fallback applies.

That fallback is the generator's ONLY quiet path. Everything else fails loudly,
because each of these otherwise emits an empty table — an app where every
caller lands on `NoAccess`, type-checked green:

| Situation | What it does |
|---|---|
| `--spec` / `AEP_SECURITY_JSON` names a file that is not there | **exit 1** — a path the caller typed and misspelled is never a build context |
| the document is not version 3 (a v1 or v2 file, `null`, an array) | **exit 1** naming the version; **only v3 is accepted** |
| a version-3 document that still carries `screens[]` | **exit 1** naming the key — screens are not authored anywhere |
| a version-3 document missing `permissions[]` or `roles[]` | **exit 1** naming the field |
| an operation whose `security` names two scopes, two requirements or a reserved OIDC scope | **exit 1** naming the file, method and path |
| an operation whose scope is not in the catalog | **exit 1** naming the handle |
| an unknown flag, or a flag with no value | **exit 2** with the usage, and nothing written |

`--help` prints the usage and exits 0 without writing. `--out-dir` (rarely
needed; the default is the app root) sets where the three files land. Two
contracts declaring the same `METHOD path`: the first wins, with a warning. A
contract that declares no `oauth2` scheme contributes every operation as
`public`.

What it emits, and what each export is for:

| File | Export | Use |
|---|---|---|
| `src/authz/roles.gen.ts` | `Scope`, `SCOPES`, `isScope` | the catalog handles as a string-literal union |
| | `Role`, `ROLES` | the user-kind roles (a service-kind role has no login and is not here) |
| | `ROLE_GRANTS` | role → grants; `heldRoles()` projects the caller's scopes through it |
| | `ROLE_ASSIGN_TO`, `ROLE_ASSIGNABLE_BY` | the group and role names `NoAccess` tells the user to ask for, and whom to ask |
| `src/authz/operations.gen.ts` | `OperationKey` | `"<METHOD> <path template>"` as the contract spells it — `"GET /me/claims"` — a string-literal union of every operation in every contract |
| | `OPERATIONS`, `OperationRequirement`, `isOperationKey` | operation → `{ kind: "public" }` \| `{ kind: "signedIn" }` \| `{ kind: "scope", scope }`, the projection the gateway trait and the mock gateway make of the same block |
| `mock/authz/roles.gen.ts` | `mockRoles` | the user-kind roles with their grants, in DECLARED order — the first is mock mode's default persona |

### 2 · `src/authz/session.ts`

Add the four `<DEP>_*` keys the SPA reads — `CLIENT_ID`, `ISSUER`, `SCOPES`,
`RESOURCE`; not `JWKS_URL` — to the `Env` type in the `react-webapp` shim, and
change only the `USER_AUTH_` prefix in the copied file to YOUR dependency's. It
is `env.ts` and this module that make the import graph browser-only: `env.ts`
throws at module load when `/env-config.js` did not run, and the `UserManager`
is constructed at module load.

Its surface: `signIn`, `handleCallback`, `signOut`, `currentUser` (renews
silently; `null` ONLY when there is no session to renew), `accessToken`, and
`tokenIsValid`.

**Gate the app's first render on `currentUser()`**: a user → proceed; `null` →
`signIn()`. Do **not** call `signIn()` merely because the access token expired —
that turns a silent refresh into a full-screen redirect on every visit.

**There is no `username` claim in the ID token.** Measured: Thunder issues `sub`,
`email`, `given_name`, `family_name`, `groups` and `ouId`/`ouName`/`ouHandle`,
and no `username`, even when the application is registered with one in its
attribute list. So "signed in as …" falls back `name` → `email` → `sub`, which
is what `gates.tsx`'s `displayName()` does. Reading `profile.username` renders
`undefined`; reading `sub` alone renders a UUID.

### 3 · `src/authz/core.ts` and `src/authz/gates.tsx` — the one authorization surface

The rules are **split** from the wiring, and the split is what makes them
testable. `src/env.ts` throws at module load and `src/authz/session.ts`
constructs a `UserManager` at module load, so anything that imports
`session.ts` drags a browser into the import graph and cannot be loaded by a
unit test in a plain node environment. `core.ts` imports **nothing** — not even
the generated tables, which is why every function that needs them takes them as
an argument — and `gates.tsx` re-exports what callers need, so the module
surface is unchanged.

`core.ts`: `parseScopes`, `granted`, `canCall`, `heldRoles`, `rolesGranting`,
`tokenIsValid`, `classifyApiFailure`, `createUnauthorizedHandler`.

`gates.tsx` — the surface every screen decision goes through:

| Export | Use |
|---|---|
| `<AuthzProvider fallback={…}>` | resolves the session's scopes ONCE, above everything that gates |
| `useScopes()`, `useAuthz()` | the caller's scopes / the whole session state |
| `granted()`, `can(op)` | the async form, for a route loader or a plain function outside React. `granted()` resolves to a `Set<string>` — the token's scopes verbatim, the five OIDC ones included — so ask it with `can()`, never "is the set non-empty" |
| `<Can op={…}>` | show a nav item, a button, a column — hide it otherwise |
| `<RequireOperation op={…} screen={…} />` | route guard; a caller who may not call the operation lands on `/forbidden` |
| `<Forbidden />` | route it at `/forbidden`, **inside** the app shell |
| `<NoAccess />` | **replaces** the shell when nothing at all is reachable |
| `heldRoles()`, `useHeldRoles()` | the header badge |
| `rolesGranting(scope)` | which roles unlock a scope — the Forbidden copy |

**The prop is `op`, an `OperationKey`**: `<RequireOperation op="GET /claims" />`,
`<Can op="POST /me/claims">`. Every gate answers
`canCall(OPERATIONS[op], scopes, signedIn)`: a `public` operation is always
callable, a `signedIn` one by any session, a `scope` one when the token holds
that handle. No component takes a handle; a handle typed into JSX is the stale
gate nothing can see (§5).

**Scope comparison is a whole-string match, everywhere.** A token carrying
`claims:read-all` does NOT satisfy `claims:read`: the gateway does not treat one
as a superset of the other, so neither may the SPA. A role that needs both holds
both.

**`heldRoles()` is derived from scopes against `ROLE_GRANTS`** — a role whose
EVERY grant is held. It is a label for the badge and for the copy that tells a
user what to ask for; it is never an input to a gate, it never comes from a
groups claim, and there is no default role. The `NoAccess` copy names the groups
and the granting roles from `ROLE_ASSIGN_TO` / `ROLE_ASSIGNABLE_BY`: no role
name, group name or administrator is ever hardcoded in that page.

### 4 · `src/authz/client.ts` — the 401 rule

**The gateway answers 401 for every failure, missing scope included, and sends
no `WWW-Authenticate`** — `api-management`'s status matrix owns that and why no
setting changes it. What follows for the SPA: nothing on the wire tells an
expired token from a refused permission, so **do not add a `WWW-Authenticate`
read — there is nothing to read.**

So the SPA absorbs it, in three places, and all three are already in the assets:

1. **`gates.tsx` gates before it calls.** A screen whose load operation the
   token does not unlock is never rendered, so that operation is never invoked.
   That removes every routine case — and it is not sufficient alone: a typed
   URL, a stale bundle or a race past a narrowed renew still reach the gateway.
2. **`session.ts` exposes `tokenIsValid()`** — a pure, side-effect-free read of
   the stored user's `expires_at` against the clock, with the 60 s grace. No
   network, no renew, no sign-in, so the API client can call it on every
   response without recursing into the thing it is deciding about. `expires_at`
   is local and authoritative, and it is the only signal that tells the two
   401s apart.
3. **`client.ts` maps the answer**, through `classifyApiFailure`:

| Answer | Outcome | Why |
|---|---|---|
| **401 + `tokenIsValid()`** | route to **Forbidden** | the session is fine, so the refusal is about scope — and the gateway cannot say so |
| **401 + `!tokenIsValid()`** | **`signIn()`**, at most once per page load | the ordinary expired/absent case |
| **403** | **Forbidden, always** | no generated service answers 403 today, but an org API behind the same gateway may; signing in again cannot add a scope the caller's roles do not grant |

`if (res.status === 401) signIn()` — what the previous revision of this skill
taught — is **DELETED**, and deleting it is the point of the file. It threw a
correctly provisioned user who touched one operation their role does not grant
into an endless sign-in loop: sign in, succeed, call, 401, sign in.

`signIn` is guarded to one call per page load: a screen firing several requests
at once answers 401 several times over, and without the guard each answer starts
its own redirect.

**Wire the Forbidden route once, at the router root.** `client.ts` holds no
router import; it takes the navigator. One component, rendered inside the router
and above every route — `assets/App.example.tsx`'s `ForbiddenWiring`:

```tsx
const navigate = useNavigate();
useEffect(() => setForbiddenNavigator(() => navigate("/forbidden", { replace: true })), [navigate]);
```

`replace: true` keeps the refused URL out of the history, so Back does not walk
the user straight back into the same 403. Until that call lands, a refusal logs a
named error rather than silently doing nothing — **this wiring is not optional**:
without it every refusal the screen gate did not catch routes nowhere, which
looks exactly like a screen that renders nothing. Your per-service client — `src/api.ts`, generated types and all — calls
`apiFetch`/`apiJson`, or, with an `openapi-fetch` client, `authorizationHeader()`
and `classifyResponse()` from its middleware. It adds **nothing** of its own
about authorization.

### 5 · `src/authz/screens.ts` — each screen names the operation it loads

**A screen is gated on the operation it loads, and that operation is named
once, here.** The screen table is yours to write from `wireframes.dsl`, in
**rail order** — the first reachable row is the landing screen:

```ts
export const SCREEN_ROUTES: readonly ScreenRoute[] = [
  { key: "my-claims", label: "My Claims",    path: "/claims/mine", loads: "GET /me/claims" },
  { key: "submit",    label: "Submit Claim", path: "/claims/new",  loads: null },
  { key: "approvals", label: "Approvals",    path: "/approvals",   loads: "GET /claims" },
];
```

`loads` is the operation whose answer the screen renders when it opens — the
list or the detail call, at the reach the screen shows: an every-row queue
loads `GET /claims`, a "mine" page loads `GET /me/claims`. `loads: null` is a
screen with no load call (a form), reachable by any signed-in caller. A screen
in a flow with **no `role` line** is `public: true` and is routed above the
sign-in guard (§6). `reachableScreens(scopes, signedIn)` filters the table
through `canCall`, and the rail, the landing redirect and `NoAccess` all read
it.

**Never retype a handle in JSX.** `scopes.has("claims:read")` hand-written into
`App.tsx` is a stale handle the moment the design moves, and nothing — not
`tsc`, not the build gate, not the mock walk — can see that it went stale.
`loads` is typed as `OperationKey`, the union the generator emits from the
contracts, so an operation the design renamed or dropped fails the type check
on the next `npm run gen`, which `build` runs first.

**Fail loudly on a stale table.** The pattern checks every `loads` against
`OPERATIONS` at module load and throws — in dev, in the walk and in the
deployed pod — so a committed table that outlived its contract cannot become a
screen nobody can reach and nobody notices. Do not soften that to a
`console.warn` or a filter.

### 6 · `src/App.tsx` — where each view sits is a routing rule

| View | Where it goes | Why |
|---|---|---|
| `NoAccess` | **ABOVE** the shell route — returned *instead of* the shell | a caller who unlocks nothing gets no navbar and no empty sidebar. A rail with no items wrapped around "you have no access" tells the user less than the message alone |
| `Forbidden` | **INSIDE** the shell, routed at `/forbidden` | the opposite case: the caller holds other scopes and has somewhere to go, so the rail stays |

This is a **routing-structure** rule, not styling. The natural thing to write —
and what a real run wrote — is `NoAccess` inside `AppShell`'s `<Outlet />`; its
own walk caught it. The test sits above the `<Routes>` that carry the shell:

```tsx
const reachable = reachableScreens(scopes, true);
if (reachable.length === 0) return <NoAccess appName={APP_NAME} />;   // no shell around it
return <Routes><Route element={<AppShell />}>…</Route></Routes>;
```

The rest of the structure, from `assets/App.example.tsx`: `/callback` is routed
**outside** the provider (there is no session to read until the redirect has been
processed); every route whose screen `loads` an operation is wrapped in
`<RequireOperation op={screen.loads} screen={screen.label}>`; the landing route
redirects to the first **reachable** screen in `SCREEN_ROUTES` order.

- `loads: "<operation>"` → `<RequireOperation op={…}>` around the route and
  `<Can op={…}>` around its nav item.
- `loads: null` → any signed-in caller; no guard.
- `public: true` (a screen in a flow with no `role` line) → reachable before
  sign-in; keep it outside the sign-in gate entirely. `App.example.tsx` routes
  those screens **above** `SignedIn`, still inside `AuthzProvider` (so `<Can>`
  and `useScopes()` work on them) and outside `AppShell` — a visitor with no
  session has no signed-in chrome to draw. A public screen routed inside the
  guard is unreachable by the visitor it exists for, because the guard
  redirects them to the IdP first.

**The rail is ONE rail whose items are each wrapped in `Can`.** The DSL draws a
different sidebar per role because it draws one role at a time; a single gated
rail reproduces every one of those pictures and also covers the case the DSL
cannot draw — somebody holding two roles, who sees the union.

**A reachable screen with nothing in it shows its empty state**, not Forbidden,
and a region whose operation the viewing role cannot call renders its own
forbidden state rather than vanishing. The UI never turns away a user the API
would serve.

**`/forbidden` and `NoAccess` are platform-prescribed views.** They appear in no
`wireframes.dsl` and they are the carve-out from "no invented screens"
(`wireframes`' `references/implementing.md` says the same); their absence is a
defect even though no wireframe names them.

---

# Backend authorization

**The operation's scope is enforced at the gateway, and nowhere else.** Every
protected operation in `openapi.yaml` declares exactly one scope, or none; the
gateway is rendered from that same contract and refuses a caller who lacks the
handle before your service is reached. A generated service therefore holds **no
operation → scope table, in any stack**. `api-management` owns the argument.

What a gateway cannot do is prove to your code that a request came through it.
So it mints a short-lived JWT of the caller it authenticated, signs it with the
environment's own key, and puts it on the upstream request — and your service
verifies that signature. That is the whole of backend authentication.

## Verify the assertion: one copied asset, wired once

| Stack | Asset | Wiring |
|---|---|---|
| Go | `$AEP_SKILLS_DIR/go/assets/gateway_assertion.go` | `auth.NewVerifierFromEnv()` in `main` (fatal on error) + `r.Use(verifier.Middleware)` |
| Ballerina | `$AEP_SKILLS_DIR/ballerina/assets/gateway_assertion.bal` | `http:InterceptableService` + `createInterceptors() returns AssertionInterceptor` |

Both read the same three variables, which the platform sets on the container
when the environment's gateway publishes a keypair:
`GATEWAY_ASSERTION_CERTIFICATE`, `GATEWAY_ASSERTION_ISSUER`,
`GATEWAY_ASSERTION_HEADER`. **A missing one stops the service from starting**,
in both stacks, on purpose: a service that runs without them cannot tell a real
caller from a forged one.

**You author no new check.** The asset verifies the signature, pins the issuer
to the gateway (never the IdP), checks the expiry and puts the caller on the
request context. A handler reads the caller from there and nothing else.

Three outcomes, and the middle one is the point:

```
no assertion                 → continue with no caller    # a public operation has none
present but not verifiable   → 401, immediately           # never "anonymous"
verified                     → continue with the caller
```

Downgrading an unverifiable assertion to an anonymous request would make forging
one strictly better for an attacker than sending none.

**The SPA sees only 401s** — the gateway answers 401 for a missing scope exactly
as for a dead token, so §4's `classifyResponse()` sends a 401 with a still-valid
token to Forbidden. Nothing in the service answers 403.

## Reach is the path, not a check in the handler

A handler holds **no authorization logic**. Whether the operation may be called
was settled at the gateway from the contract; which rows it returns was settled
by its path. So there are exactly two shapes of handler, and neither branches
on a scope:

```
GET /me/claims        →  rows := claims.where(owner_id = caller.sub)
GET /me/team/claims   →  rows := claims.where(owner_id in reports_of(caller.sub))
GET /claims           →  rows := claims.all()
POST /me/claims       →  claim.owner_id = caller.sub   // stamped, never read from the body
```

`hasScope` (Go: `auth.HasScope`; Ballerina: `hasScope(caller, …)`) exists for
the rare handler that needs a second fact about the caller for something other
than authorization — a header badge, an audit line. If you are reaching for it
to decide which rows to return, the design has one operation where it needs
two: report that, do not code around it.

**A row that exists but is not the caller's is a 404, not a 403** — a caller who
may not see a row may not learn it exists (`api-management` owns that rule).
Under `/me/…` that is not a special case: the row is simply not in the caller's
collection.

## Identity is not authorization

- **What may they do** was answered at the gateway, from the contract, and
  **which rows** by the operation's path. Inside the service the assertion's
  `scope` claim decides nothing. There is no group matching, no substring match
  on a role name, no role column in your own table, and no cold-start default.
- **Which rows are theirs** is the assertion's `sub` — an opaque IdP subject,
  and the one key for rows this service creates. Stamp it on every row a
  `/me/…` operation creates, and resolve every `/me/…` query through it.
- **Directory attributes** (their unit, their own id in some other system) come
  from the caller's directory record, resolved by their username. A username can
  be renamed, so look the record UP by it and never store it as a row key. Never
  match `sub` against a record id another system minted, and never parse an
  attribute out of a group name. A username that resolves to no record is a
  refusal. The directory's real endpoint, its username field, and the dependency
  wiring are org-specific — `internal-services` owns them; do not hardcode a
  roster.

## The service's own per-caller rows

Rows this service creates for a caller — their claim, their draft, their
preferences — are keyed on the assertion's `sub`, the one case where that is
right, because the service stored the subject itself rather than matching it
against ids another system minted. **Store no role column and no permission
column**: the token already answered that, and a second copy is a second
authority that drifts.

Express this in your stack's own idiom — where the verifier is wired, where the
shared helper lives, and how a handler returns 401 — following the conventions
that skill already sets.

---

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| The service will not start: `GATEWAY_ASSERTION_CERTIFICATE is not set` | Running outside a deployed cell, or in an environment whose gateway has no keypair | Locally, set the three variables from a throwaway keypair. In a cell this is fail-closed on purpose: the environment's gateway needs provisioning. |
| Every request 401s right after a deploy | The environment's gateway was re-provisioned; this component still holds the previous certificate | Redeploy the component — the certificate rides its ReleaseBinding. |
| A forged request is served as an anonymous caller | An unverifiable assertion was treated as "no caller" | A present-but-invalid assertion is always a 401. |
| Signed-in user loops back to the login page forever, ~160 ms per cycle | `if (res.status === 401) signIn()` in the API client: the gateway answers 401 for a missing scope exactly as for a dead token | The 401 rule — 401 + `tokenIsValid()` ⇒ Forbidden; only an absent/expired token signs in. Copy `src/authz/client.ts` from the assets. |
| Sign-in succeeds, looks perfectly healthy, and EVERY `/api` call 401s | The token's `aud` is not this project's resource server — `resource` missing from one of the three legs | Set all three: `settings.resource`, `extraTokenParams`, and the `signinSilent({ resource, … })` argument. |
| `tsc` is green but a screen is gated on an operation the contract dropped, or a new handle reaches nothing | `operations.gen.ts` is stale, or a handle was retyped as a string literal in JSX | `build` is `npm run gen && tsc --noEmit && vite build`; gate on `loads` through `src/authz/screens.ts`, never from a literal. |
| A role opens its own screen and the screen's list call answers 401, or the screen is hidden for the role it exists for | The role's `grants` lack the handle of the operation the screen **loads** — whole-string match, so `x:read-all` (`GET /x`) is not `x:read` (`GET /me/x`); or `loads` names the wrong reach for the screen | A design finding: report the role, the screen and the handle. Never widen `loads`, a mock or a guard to hide it. |
| `TS2353: 'useRefreshToken' / 'clockSkewInSeconds' does not exist in type 'UserManagerSettings'` | Neither is a member in oidc-client-ts 3.5.0 | Delete both. `automaticSilentRenew: true` drives the refresh grant; the expiry grace is the asset's own `CLOCK_SKEW_SECONDS`. |
| The header badge is empty for a user who clearly has a role | Code read `user.profile.groups` | `heldRoles()` — the roles whose EVERY grant is in the token's `scope`. |
| "You have no access" is drawn with a navbar and an empty sidebar around it | `NoAccess` was rendered through `AppShell`'s `<Outlet />` | Return it ABOVE the shell route; `Forbidden` is the one that stays inside. |
| A role-scoped caller signs in but sees no rows | The SPA called `/me/…` for a role whose screen lists every row, or the handler matched `sub` against a directory id | The screen calls the operation whose path is its reach (`/claims` for an every-row queue); resolve directory records by username. |
| A caller holding `claims:read-all` is refused `GET /me/claims` | Scope comparison is an exact string match; `claims:read-all` guards `GET /claims`, a different operation | The SPA calls the operation the role's handle guards. If the role genuinely needs both views, the design grants both handles — report it; do not special-case it in code. |
| A public endpoint trusts an identity | A public operation has no policy, so every inbound header is the caller's own and no assertion is minted | A handler behind `security: []` reads no identity at all. |
| A newly granted permission does not show up for a user who is already signed in | A refresh narrows but never widens — RFC 6749 §6 | Sign out and in again; a removed grant, by contrast, disappears at the next renew. |
| Sign-in loops at the right path, or the user is sent to login on every visit / new tab | No persistent `WebStorageStateStore` (the in-memory default loses the PKCE verifier across the redirect), session in `sessionStorage`, or the load path calls `signIn()` on a merely-expired token | `WebStorageStateStore({ store: localStorage })` + `automaticSilentRenew`; renew via `signinSilent()` and only `signIn()` when there is no session. |
| After login, "invalid redirect URI" | `redirect_uri` doesn't match the `<origin>/callback` the platform registered | Compute `window.location.origin + '/callback'`. |
| Logout button does nothing | `signOut()` calls only `signoutRedirect()`, which rejects (no `end_session_endpoint`), and the handler swallows it | Wrap it in the try/catch fallback to `removeUser()` + reload. |
