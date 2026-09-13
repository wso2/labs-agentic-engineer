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
  enforces the scope each operation declares in `openapi.yaml`, and injects the
  same scope string as `X-User-Scopes`. See `api-management` for the gateway's
  side of that contract.
- **The SPA** signs the user in with OIDC Authorization Code + PKCE, asking for
  a **resource indicator** so the token is minted for this project, reads the
  granted permissions from `user.scope`, and gates every screen on them.

**Nothing reads the groups claim to decide anything** — not the SPA, not the
service, not for display. Permissions reach code as scopes.

The OAuth client itself is **platform-owned**: you never create, compute, or
hardcode any part of it.

## Where permissions come from

`specs/design/security.json` is the project's permission catalog. Read it before
writing any authorization code:

| Field | What you do with it |
|---|---|
| `permissions[].resource` + `actions[].handle` | joined by `:` these are the **scope handles** — `claims:read`, `reports:export`. This is the closed set. |
| `actions[].ownership` | `own` = this action reaches the caller's own rows; `any` = every row. The pair `read`/`read-all` on one resource is the widening idiom. |
| `roles[].grants` | which handles a role holds. Drives the SPA's header badge and the "which role unlocks this" wording; `scopes.gen.ts` is generated from it. |
| `screens[].requires` | a handle, `null` (any signed-in user) or `"public"` (reachable before sign-in) — the SPA's route guard for that screen. |
| `roles[].assignTo`, `groups[]` | provisioning only. **No application code reads these.** |

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
`scripts/gen-scopes.mjs` wired into the build, so `src/scopes.gen.ts` exists →
`src/auth.ts` → `src/authz-core.ts` + `src/authz.tsx` → `src/api-client.ts` →
**`src/api.ts` stripped of every authorization rule of its own** (§4) → a
**`/callback` route** that calls `handleCallback()` once on mount → `src/screens.ts`
and the route guards in `src/App.tsx`. Verify with the `react-webapp` build check.

## What you copy

Seven files ship as assets — five copied verbatim, two patterns you adapt.
**Copy them; do not write your own.** The rules they carry were measured against
this IdP and this gateway, and every one of them has already been got wrong by a
real run.

| Asset | Copy to | How |
|---|---|---|
| `assets/gen-scopes.mjs` | `scripts/gen-scopes.mjs` | verbatim |
| `assets/authz-core.ts` | `src/authz-core.ts` | verbatim |
| `assets/authz.tsx` | `src/authz.tsx` | verbatim — restyle the markup with the pinned design system, keep the words and every export name |
| `assets/auth.ts` | `src/auth.ts` | verbatim except the `USER_AUTH_` prefix, which becomes YOUR dependency's |
| `assets/api-client.ts` | `src/api-client.ts` | verbatim |
| `assets/screens.example.ts` | `src/screens.ts` | **pattern** — replace `COMPONENT` and `ROUTE_BY_KEY` with yours, keep the rest |
| `assets/App.example.tsx` | `src/App.tsx` | **pattern** — replace `PAGE_BY_KEY` *and* `APP_NAME` with yours; the ROUTING STRUCTURE is prescribed |

An eighth file is not copied and is not optional: **`src/api.ts`, the per-service
client you already have.** Whatever it carried about authorization comes OUT —
the bearer it attached, any `WWW-Authenticate` read, and above all
`if (res.status === 401) signIn()` — and it calls `api-client.ts`'s
`authorizationHeader()` and `classifyResponse()` instead (§4 has the
`openapi-fetch` middleware to paste). Two modules that both decide what a 401
means is the same bug as one module deciding it wrong, and on an app that
already exists this is the only edit the copy table cannot make for you.

The two example files are written against the Expense Tracker — `COMPONENT`,
`ROUTE_BY_KEY`, `PAGE_BY_KEY` and `APP_NAME` all name ITS screens. They are a
shape to follow, not a fixture to ship: every one of those four is yours to
replace. If your app already holds its name somewhere (the wireframes' `navbar`
title, usually `src/appName.ts`), import it rather than declaring a second copy.

```bash
mkdir -p scripts
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/gen-scopes.mjs"  scripts/gen-scopes.mjs
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/authz-core.ts"   src/authz-core.ts
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/authz.tsx"       src/authz.tsx
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/auth.ts"         src/auth.ts
cp "$AEP_SKILLS_DIR/thunder-authentication/assets/api-client.ts"   src/api-client.ts
```

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's
`SKILL.md`. The two `*.example.*` files and everything under `assets/__tests__/`
are **not** copied into the app: the examples are patterns you adapt, and the
tests are this skill's own regression suite.

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

`assets/auth.ts` sets all three. A run that copied a settings-only snippet got a
green build, a healthy-looking sign-in, and a wrong `aud` on every token.

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
  `tokenIsValid()` allows past `expires_at` is **ours**: `assets/auth.ts` holds a
  private `CLOCK_SKEW_SECONDS = 60`, the same width as that renew window.

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
`assets/auth.ts` already wraps it in that fallback.

**Permissions ride in the access token's `scope`.** `oidc-client-ts` surfaces the
token response's scope string as `user.scope`, and `src/authz.tsx` is the only
module that reads it. Never decode the access token, never hand-parse a JWT, and
**never read `user.profile.groups`** — the groups claim is still issued and
nothing in your app may consume it. There is no `getRoles()`, no role table of
your own, and no default role for a user who matches nothing: a caller whose
token grants nothing sees `NoAccess`.

**`userManager` is never exported.** Every other module reaches the session
through `src/auth.ts`'s functions — `signIn`, `handleCallback`, `signOut`,
`currentUser`, `accessToken`, `tokenIsValid`. That list is the module's whole
surface, and it is what lets mock mode substitute the module wholesale
(`react-webapp`'s `references/mock-mode.md` owns that). `userManager` is
`oidc-client-ts`'s own object and has no mock substitute, so a single
`export const userManager` compiles in production and breaks the app the moment
anybody opens it without a cluster.

## Implementation

### 1 · `src/scopes.gen.ts`, generated from the catalog

`scripts/gen-scopes.mjs` reads `specs/design/security.json` and emits the one
file that carries the design into the bundle. Wire it into `package.json`:

```json
"scripts": {
  "gen":   "node scripts/gen-scopes.mjs --component <this component's name>",
  "build": "npm run gen && tsc --noEmit && vite build"
}
```

**`gen` before `tsc` is not optional.** The whole point of a generated string
literal union is that a handle the design dropped fails the type check — and a
**stale** `scopes.gen.ts` type-checks perfectly green. `--component` filters
`SCREENS` to this component's rows (`AEP_APP_COMPONENT` does the same).

Run `npm run gen` once by hand and **commit `src/scopes.gen.ts`**. The generator
walks UP from the app folder to find `specs/design/security.json`, because how
many levels up the project's spec tree sits is a layout detail nothing may
hardcode. Inside a per-component image build there is no such ancestor at all —
the build context is the app folder alone — so when the catalog is out of reach
and a committed output exists the generator **keeps it, says so on stdout and
exits 0**. Exiting 1 there would fail every image build the platform runs. With
no committed output and no catalog it exits 1 and names why, which is the
uncommitted-file case.

That fallback is for the walk-up only, and it is the generator's ONLY quiet
path. Everything else fails loudly, because each of these otherwise emits
`Scope = never` / `SCREENS = []` — an app where every caller lands on
`NoAccess`, type-checked green:

| Situation | What it does |
|---|---|
| `--spec` / `AEP_SECURITY_JSON` names a file that is not there | **exit 1** — a path the caller typed and misspelled is never a build context |
| the document is not version 2 (a v1 file, `null`, an array) | **exit 1** naming the version; **v1 is not accepted** |
| a version-2 document missing `permissions[]`, `roles[]` or `screens[]` | **exit 1** naming the field |
| `--component` matches no screen in the document | **exit 1** listing the components the document does declare |
| an unknown flag, or a flag with no value | **exit 2** with the usage, and nothing written |

`--help` prints the usage and exits 0 without writing. `--out` (rarely needed;
the default is the app's own `src/scopes.gen.ts`) resolves against the current
directory.

What it emits, and what each export is for:

| Export | Use |
|---|---|
| `Scope`, `SCOPES`, `isScope` | the catalog handles as a string-literal union |
| `Role`, `ROLES` | the user-kind roles (a service-kind role has no login and is not here) |
| `ROLE_GRANTS` | role → grants; `heldRoles()` projects the caller's scopes through it |
| `ROLE_ASSIGN_TO`, `ROLE_ASSIGNABLE_BY` | the group and role names `NoAccess` tells the user to ask for, and whom to ask |
| `ScreenGate`, `SCREENS` | the screen table, in DECLARED order — the nav order, whose first reachable row is the landing screen |

### 2 · `src/auth.ts`

Add the four `<DEP>_*` keys the SPA reads — `CLIENT_ID`, `ISSUER`, `SCOPES`,
`RESOURCE`; not `JWKS_URL` — to the `Env` type in the `react-webapp` shim, copy
the asset, and change only the `USER_AUTH_` prefix to YOUR dependency's. It is
`env.ts` and this module that make the import graph browser-only: `env.ts`
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
is what `authz.tsx`'s `displayName()` does. Reading `profile.username` renders
`undefined`; reading `sub` alone renders a UUID.

### 3 · `src/authz-core.ts` and `src/authz.tsx` — the one authorization surface

The rules are **split** from the wiring, and the split is what makes them
testable. `src/env.ts` throws at module load and `src/auth.ts` constructs a
`UserManager` at module load, so anything that imports `auth.ts` drags a browser
into the import graph and cannot be loaded by a unit test in a plain node
environment. `authz-core.ts` imports **nothing** — not even `./scopes.gen`,
which is why every function that needs the generated tables takes them as an
argument — and `authz.tsx` re-exports what callers need, so the module surface
is unchanged.

`authz-core.ts`: `parseScopes`, `granted`, `canReach`, `heldRoles`,
`rolesGranting`, `tokenIsValid`, `classifyApiFailure`, `createUnauthorizedHandler`.

`authz.tsx` — the surface every screen decision goes through:

| Export | Use |
|---|---|
| `<AuthzProvider fallback={…}>` | resolves the session's scopes ONCE, above everything that gates |
| `useScopes()`, `useAuthz()` | the caller's scopes / the whole session state |
| `granted()`, `can(scope)` | the async form, for a route loader or a plain function outside React. `granted()` resolves to a `Set<string>` — the token's scopes verbatim, the five OIDC ones included — so ask it with `can()`, never "is the set non-empty" |
| `<Can scope={…}>` | show a nav item, a button, a column — hide it otherwise |
| `<RequireScope scope={…} screen={…} />` | route guard; a caller without the scope lands on `/forbidden` |
| `<Forbidden />` | route it at `/forbidden`, **inside** the app shell |
| `<NoAccess />` | **replaces** the shell when nothing at all is reachable |
| `heldRoles()`, `useHeldRoles()` | the header badge |
| `rolesGranting(scope)` | which roles unlock a scope — the Forbidden copy |

**The prop is `scope`**: `<RequireScope scope="claims:approve" />`,
`<Can scope="claims:submit">`.

**Scope comparison is a whole-string match, everywhere.** A token carrying
`claims:read-all` does NOT satisfy `claims:read`: the gateway does not treat one
as a superset of the other, the service middleware does not, so neither may the
SPA. A role that needs both holds both.

**`heldRoles()` is derived from scopes against `ROLE_GRANTS`** — a role whose
EVERY grant is held. It is a label for the badge and for the copy that tells a
user what to ask for; it is never an input to a gate, it never comes from a
groups claim, and there is no default role. The `NoAccess` copy names the groups
and the granting roles from `ROLE_ASSIGN_TO` / `ROLE_ASSIGNABLE_BY`: no role
name, group name or administrator is ever hardcoded in that page.

### 4 · `src/api-client.ts` — the 401 rule

**The gateway answers 401 for every failure, missing scope included, and sends
no `WWW-Authenticate`** — `api-management`'s status matrix owns that and why no
setting changes it. What follows for the SPA: nothing on the wire tells an
expired token from a refused permission, so **do not add a `WWW-Authenticate`
read — there is nothing to read.**

So the SPA absorbs it, in three places, and all three are already in the assets:

1. **`authz.tsx` gates before it calls.** A screen the token does not unlock is
   never rendered, so its operations are never invoked. That removes every
   routine case — and it is not sufficient alone: a typed URL, a stale bundle
   or a race past a narrowed renew still reach the gateway.
2. **`auth.ts` exposes `tokenIsValid()`** — a pure, side-effect-free read of the
   stored user's `expires_at` against the clock, with the 60 s grace. No
   network, no renew, no sign-in, so the API client can call it on every
   response without recursing into the thing it is deciding about. `expires_at`
   is local and authoritative, and it is the only signal that tells the two
   401s apart.
3. **`api-client.ts` maps the answer**, through `classifyApiFailure`:

| Answer | Outcome | Why |
|---|---|---|
| **401 + `tokenIsValid()`** | route to **Forbidden** | the session is fine, so the refusal is about scope — and the gateway cannot say so |
| **401 + `!tokenIsValid()`** | **`signIn()`**, at most once per page load | the ordinary expired/absent case |
| **403** | **Forbidden, always** | the service, reached past the gateway, saying `insufficient_scope`. Signing in again cannot add a scope the caller's roles do not grant |

`if (res.status === 401) signIn()` — what the previous revision of this skill
taught — is **DELETED**, and deleting it is the point of the file. It threw a
correctly provisioned user who touched one operation their role does not grant
into an endless sign-in loop: sign in, succeed, call, 401, sign in.

`signIn` is guarded to one call per page load: a screen firing several requests
at once answers 401 several times over, and without the guard each answer starts
its own redirect.

**Wire the Forbidden route once, at the router root.** `api-client.ts` holds no
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

### 5 · `src/screens.ts` — screens come from `SCREENS`, never from JSX

**A screen's required handle is read from the generated table and never retyped
in JSX.** `scopes.has("claims:read") || scopes.has("claims:submit")` hand-written
into `App.tsx` is a stale handle the moment the design moves, and nothing — not
`tsc`, not the build gate, not the mock walk — can see that it went stale. Bind
each row to a route instead, and a design change reaches the app on the next
`npm run gen` and nowhere else.

`security.json` spells a screen the way a person reads it (`"My Claims"`); the
wireframe DSL cannot carry a space and spells the same screen `MyClaims`.
**Normalize both to lowercase alphanumerics** — that is what binds one to the
other, and it is the rule both sides of the platform use.

**Fail loudly on an unknown screen.** A screen `security.json` declares for this
component that the app has no route for throws at module load — in dev, in the
walk and in the deployed pod — rather than becoming a screen nobody can reach
and nobody notices. Do not soften that to a `console.warn` or a filter.

### 6 · `src/App.tsx` — where each view sits is a routing rule

| View | Where it goes | Why |
|---|---|---|
| `NoAccess` | **ABOVE** the shell route — returned *instead of* the shell | a caller who unlocks nothing gets no navbar and no empty sidebar. A rail with no items wrapped around "you have no access" tells the user less than the message alone |
| `Forbidden` | **INSIDE** the shell, routed at `/forbidden` | the opposite case: the caller holds other scopes and has somewhere to go, so the rail stays |

This is a **routing-structure** rule, not styling. The natural thing to write —
and what a real run wrote — is `NoAccess` inside `AppShell`'s `<Outlet />`; its
own walk caught it. The test sits above the `<Routes>` that carry the shell:

```tsx
const reachable = reachableScreens(scopes);
if (reachable.length === 0) return <NoAccess appName={APP_NAME} />;   // no shell around it
return <Routes><Route element={<AppShell />}>…</Route></Routes>;
```

The rest of the structure, from `assets/App.example.tsx`: `/callback` is routed
**outside** the provider (there is no session to read until the redirect has been
processed); every gated route is wrapped in `<RequireScope>` with the scope taken
from `SCREEN_ROUTES`; the landing route redirects to the first **reachable**
screen.

- `requires: "<handle>"` → `<RequireScope scope={…}>` around the route and
  `<Can scope={…}>` around its nav item.
- `requires: null` → any signed-in caller; no guard.
- `requires: "public"` → reachable before sign-in; keep it outside the sign-in
  gate entirely. `App.example.tsx` routes those screens **above** `SignedIn`,
  still inside `AuthzProvider` (so `<Can>` and `useScopes()` work on them) and
  outside `AppShell` — a visitor with no session has no signed-in chrome to
  draw. A public screen routed inside the guard is unreachable by the visitor it
  exists for, because the guard redirects them to the IdP first.

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

The gateway hands your service the caller's verified identity as headers
(`api-management` covers the mechanism, the header table, the never-read-
`Authorization` rule and the 401-on-empty-`X-User-Id` rule). What follows is how
you enforce on it.

## The operation's scope is enforced by a middleware you copy

Every protected operation in `openapi.yaml` declares **exactly one** scope, or
none. The middleware reads that declaration — from the generated server, or from
a table a drift test pins to the contract — and answers:

```
required = the operation's declared scope        (exactly one, or none)
if the operation is public                → next()   # reads no identity header
if X-User-Id is EMPTY                     → 401      # the request bypassed the gateway
if required and required ∉ X-User-Scopes  → 403 + the insufficient_scope challenge (api-management)
next()
```

Copy it from your stack skill and wire it once:

| Stack | Asset | Wiring |
|---|---|---|
| Go | `$AEP_SKILLS_DIR/go/assets/scopes_middleware.go` | `gen.ChiServerOptions{Middlewares: …}` — **never** `r.Use` |
| Ballerina | `$AEP_SKILLS_DIR/ballerina/assets/scopes.bal` + `scope_table_drift_test.bal` | `http:InterceptableService` + `createInterceptors`; verify with `bal build && bal test` |

**You author no new check.** A handler that re-tests the operation's own scope
is a second authority that drifts from the contract.

**Test for EMPTY, never for MISSING** — `X-User-Id: ""` is what an
identity-less request looks like on the wire (`api-management`).

**The service answers 403 even though the gateway answers 401** —
`api-management`'s status matrix owns the two columns and why they differ. What
it means here is that the SPA has to treat **both** statuses as a refusal: §4's
`classifyResponse()` sends a 401 with a still-valid token to Forbidden for
exactly this reason. Never answer 401 for a permission failure — the SPA reads
that as "token expired" and restarts sign-in, which loops forever.

## Ownership: the finer rule an operation-level check cannot express

The catalog marks every action `own` or `any`. A pair on one resource
(`claims:read` own, `claims:read-all` any) is one operation whose result set
widens:

```
rows := claims.where(owner_id = X-User-Id)        // ownership: own
if hasScope("claims:read-all") { rows = claims.all() }
```

`hasScope` (Go: `auth.HasScope(ctx, handle)`; Ballerina:
`hasScope(xUserScopes, handle)`) is for exactly this, never as the only check on
an operation. Where the catalog gives an action a prose-named scope rather than
an ownership pair, map it the same way: read the catalog's
`actions[].description`, not your own guess.

**The widening handle is extra, not a substitute.** Scope comparison is an exact
whole-string match (`api-management`), so `claims:read-all` does not imply
`claims:read` to anything but a human reader. A role meant to call
`GET /claims` must be granted the operation's OWN handle (`claims:read`) as well
as the widening one. If the design grants a role only the widening handle, say
so in your report — the app cannot fix it, and the role will be refused at the
gateway before your code runs.

**A row that exists but is not the caller's is a 404, not a 403** — a caller who
may not see a row may not learn it exists (`api-management` owns that rule).

## Identity is not authorization

- **What may they do** is `X-User-Scopes`, always. There is no group matching, no
  substring match on a role name, no role column in your own table, and no
  cold-start default. A caller whose token grants no scope for an operation is a
  **403**, full stop.
- **Which rows are theirs** is `X-User-Id` — an opaque IdP subject, and the one
  key for rows this service creates. Stamp it on every row, gate every
  per-caller query on it.
- **Directory attributes** (their unit, their own id in some other system) come
  from the caller's directory record, resolved by `X-User-Name` — the username,
  which the directory keys on. A username can be renamed, so look the record UP
  by it and never store it as a row key. Never match `X-User-Id` against a
  record id another system minted, and never parse an attribute out of a group
  name. Empty `X-User-Name`, or a username that resolves to no record → **403**.
  The directory's real endpoint, its username field, and the dependency wiring
  are org-specific — `internal-services` owns them; do not hardcode a roster.

## The service's own per-caller rows

Rows this service creates for a caller — their claim, their draft, their
preferences — are keyed on `X-User-Id`, the one case where that is right,
because the service stored the subject itself rather than matching it against
ids another system minted. Fill display fields from `X-User-Name`. **Store no
role column and no permission column**: the token already answered that, and a
second copy is a second authority that drifts.

Express this in your stack's own idiom — where the middleware is wired, where
the shared helper lives, and how a handler returns 403 — following the
conventions that skill already sets.

---

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Every operation answers 200 for anyone, tests included | The scope middleware is wired where the framework runs it before the operation's scope is known (Go: `r.Use`) | Wire it in the generated server's `Middlewares` / as the service's interceptor — the asset carries the proof test. |
| Signed-in user loops back to the login page forever, ~160 ms per cycle | `if (res.status === 401) signIn()` in the API client: the gateway answers 401 for a missing scope exactly as for a dead token | The 401 rule — 401 + `tokenIsValid()` ⇒ Forbidden; only an absent/expired token signs in. Copy `assets/api-client.ts`; a handler answers **403** + `insufficient_scope`. |
| Sign-in succeeds, looks perfectly healthy, and EVERY `/api` call 401s | The token's `aud` is not this project's resource server — `resource` missing from one of the three legs | Set all three: `settings.resource`, `extraTokenParams`, and the `signinSilent({ resource, … })` argument. |
| `tsc` is green but a screen is gated on a handle the design dropped, or a new handle reaches nothing | `scopes.gen.ts` is stale, or the handle was retyped as a string literal in JSX | `build` is `npm run gen && tsc --noEmit && vite build`; gate from `SCREENS` through `src/screens.ts`, never from a literal. |
| `TS2353: 'useRefreshToken' / 'clockSkewInSeconds' does not exist in type 'UserManagerSettings'` | Neither is a member in oidc-client-ts 3.5.0 | Delete both. `automaticSilentRenew: true` drives the refresh grant; the expiry grace is the asset's own `CLOCK_SKEW_SECONDS`. |
| The header badge is empty for a user who clearly has a role | Code read `user.profile.groups` | `heldRoles()` — the roles whose EVERY grant is in the token's `scope`. |
| "You have no access" is drawn with a navbar and an empty sidebar around it | `NoAccess` was rendered through `AppShell`'s `<Outlet />` | Return it ABOVE the shell route; `Forbidden` is the one that stays inside. |
| A caller with no subject is treated as signed in | The check tested "`X-User-Id` present"; the gateway sets every mapped header even when the claim is absent | Test for the EMPTY string. |
| A role-scoped caller signs in but sees no rows | The handler filtered on `X-User-Id` and never widened, or matched `X-User-Id` against a directory id | Widen with `hasScope("<resource>:<any-action>")`; resolve directory records by `X-User-Name`. |
| A caller holding `claims:read-all` is refused `GET /claims` | Scope comparison is an exact string match; the widening handle does not imply the operation's own handle | A design finding: the role must be granted `claims:read` too. Report it; do not special-case it in code. |
| A public endpoint trusts `X-User-Id` | A public operation has no policy to overwrite an inbound header, so the caller set it | A handler behind `security: []` reads no identity header at all. |
| `bal test` never runs, so the Ballerina scope table silently drifts | `bal build` does not run tests | Verify is `bal build && bal test`. |
| A newly granted permission does not show up for a user who is already signed in | A refresh narrows but never widens — RFC 6749 §6 | Sign out and in again; a removed grant, by contrast, disappears at the next renew. |
| Sign-in loops at the right path, or the user is sent to login on every visit / new tab | No persistent `WebStorageStateStore` (the in-memory default loses the PKCE verifier across the redirect), session in `sessionStorage`, or the load path calls `signIn()` on a merely-expired token | `WebStorageStateStore({ store: localStorage })` + `automaticSilentRenew`; renew via `signinSilent()` and only `signIn()` when there is no session. |
| After login, "invalid redirect URI" | `redirect_uri` doesn't match the `<origin>/callback` the platform registered | Compute `window.location.origin + '/callback'`. |
| Logout button does nothing | `signOut()` calls only `signoutRedirect()`, which rejects (no `end_session_endpoint`), and the handler swallows it | Wrap it in the try/catch fallback to `removeUser()` + reload. |
