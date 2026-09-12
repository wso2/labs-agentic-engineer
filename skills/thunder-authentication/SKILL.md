---
name: thunder-authentication
description: "How end-user identity and permission work on the platform — Thunder, the IDP wired into the API gateway, signs users in and the access token's scopes say what they may do. Covers the auth platform-resource dependency, the platform-owned OAuth client, the window._env_.<DEP>_* runtime keys, OIDC + PKCE in the SPA, and the scope middleware every protected backend copies. Apply to any SPA whose users sign in, and to every protected backend they call."
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
- **The SPA** signs the user in with OIDC Authorization Code + PKCE and calls
  the API with the resulting access token.

**Nothing reads the groups claim to decide anything** — not the service, not for
display. Permissions reach code as scopes.

The OAuth client itself is **platform-owned**: you never create, compute, or
hardcode any part of it.

## Where permissions come from

`specs/design/security.json` is the project's permission catalog. Read it before
writing any authorization code:

| Field | What you do with it |
|---|---|
| `permissions[].resource` + `actions[].handle` | joined by `:` these are the **scope handles** — `claims:read`, `reports:export`. This is the closed set. |
| `actions[].ownership` | `own` = this action reaches the caller's own rows; `any` = every row. The pair `read`/`read-all` on one resource is the widening idiom. |
| `roles[].grants` | which handles a role holds. |
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

<!-- phase 4 rewrites the SPA half -->
<!--
     The SPA sections below are the previous revision and are SUPERSEDED by the
     backend half of this skill where the two disagree. Do not copy their role
     model into new code: `user.profile.groups` / `getRoles()` is NOT the
     authority — the access token's `scope` is (`user.scope`), the SPA needs the
     `resource` indicator on sign-in, and a 403 means "signed in, not allowed"
     and must never restart sign-in. The replacement text, the generated scope
     table and the `authz` surface land here in a later change.
-->

# SPA sign-in

## Order of work

`src/env.ts` (add the `<DEP>_*` keys to the `react-webapp` shim) → `src/auth.ts`
→ a **`/callback` route** that calls `handleCallback()` once on mount → the
bearer header in `src/api.ts`. Verify with the `react-webapp` build check.

## Constraints

**Keys are derived from YOUR dependency name.** The platform emits every
platform-resource dependency's outputs into `window._env_` as
`<UPPER_SNAKE(depName)>_<UPPER_SNAKE(outputName)>`. There is **no fixed prefix** —
it is the UPPER_SNAKE of the dependency `name` the architect chose. The auth
resource type outputs `client_id`, `issuer`, `jwks_url` and `scopes`, so a
dependency named `user-auth` yields:

| Key (dep `user-auth`) | Generic form | Meaning |
|---|---|---|
| `USER_AUTH_CLIENT_ID` | `<DEP>_CLIENT_ID` | this app's platform-owned OAuth client id |
| `USER_AUTH_ISSUER` | `<DEP>_ISSUER` | OIDC issuer / authority for `oidc-client-ts` |
| `USER_AUTH_JWKS_URL` | `<DEP>_JWKS_URL` | JWKS endpoint (token validation reference) |
| `USER_AUTH_SCOPES` | `<DEP>_SCOPES` | space-separated scopes (e.g. `openid profile email group ou`) |

Hardcoding a fixed prefix — or any prefix other than YOUR dependency's name —
gives `undefined` at module load and a redirect to `undefined/oauth2/authorize`.

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
which is why `react-webapp` does not proxy `/oidc/` — nginx only reverse-proxies sibling APIs under `/api`.

**Persist the session and renew silently.** The OAuth client is provisioned with
the `refresh_token` grant alongside `authorization_code` + PKCE, so an expiring
access token is renewed by posting the refresh token — no hidden iframe, no
third-party-cookie dependency. Store the session in `localStorage` (a
`WebStorageStateStore`) and set `automaticSilentRenew: true`. `sessionStorage` is
per-tab and wiped on close, which forces a re-login on every visit; and without
persistent web storage the PKCE verifier does not survive the redirect at all.

**A refresh narrows, never widens.** Permission scopes are re-evaluated on
renewal, so a grant removed from a role disappears at the next refresh — but a
grant ADDED to a role never appears on a refresh at all (RFC 6749 §6: a refresh
may not exceed the original grant, and the narrowing sticks to the refresh
token). A new permission needs a full re-sign-in.

**There is no sign-out endpoint.** Thunder's discovery document advertises only
issuer, authorize and token — no `end_session_endpoint` — so
`signoutRedirect()` rejects. Sign-out drops the local session (`removeUser()`)
and reloads.

<!-- phase 4 rewrites the SPA half: the paragraph below is superseded — the
     authority is the access token's `scope` (`user.scope`), never `groups`. -->
**Roles ride in the ID token.** `oidc-client-ts` surfaces them as
`user.profile.groups`, beside `ouId`/`ouName`/`ouHandle` and standard
`profile`/`email`. The platform requests the `group`/`ou` scopes by default, so
never decode the access token for roles and never hand-parse a JWT.

**Roles and test users are platform-provisioned.** The roles your app matches on
are the ones `specs/design/security.json` declares; the platform creates them, and a
test user per role, when the user clicks Build. Match on those names. Never write
user- or group-provisioning code, and never seed a roster: an account you create
is not one the platform can hand to the validation agent.

## Implementation

Add the four `<DEP>_*` keys to the `Env` type in the `react-webapp` shim.

`src/auth.ts` — `oidc-client-ts` wired to `env.<DEP>_*`, with `redirect_uri`
computed from the origin (shown for a dependency named `user-auth` — use YOURS):

```ts
import { UserManager, WebStorageStateStore } from "oidc-client-ts";
import { env } from "./env";

export const userManager = new UserManager({
  authority: env.USER_AUTH_ISSUER,
  client_id: env.USER_AUTH_CLIENT_ID,
  redirect_uri: window.location.origin + "/callback",
  post_logout_redirect_uri: window.location.origin,
  response_type: "code",
  scope: env.USER_AUTH_SCOPES,
  // The token lives in JS-readable storage — acceptable for a public SPA; keep
  // loadUserInfo:false and lean on the platform CSP.
  userStore: new WebStorageStateStore({ store: window.localStorage }),
  automaticSilentRenew: true,
  loadUserInfo: false,
});

export async function signIn()         { await userManager.signinRedirect(); }
export async function handleCallback() { return userManager.signinRedirectCallback(); }

// No end_session_endpoint → signoutRedirect() rejects; drop the LOCAL session
// instead and let the load-time guard start a fresh sign-in.
export async function signOut() {
  try {
    await userManager.signoutRedirect();
  } catch {
    await userManager.removeUser();
    window.location.assign("/");
  }
}

// null ONLY when there is no session to renew — an expired one renews silently.
export async function currentUser() {
  const user = await userManager.getUser();
  if (user && !user.expired) return user;
  try { return await userManager.signinSilent(); } catch { return null; }
}

export async function getAccessToken(): Promise<string | null> {
  const user = await currentUser();
  return user?.access_token ?? null;
}
```

On app load, gate rendering on `currentUser()`: a user → proceed; `null` →
`signIn()`. Do **not** call `signIn()` merely because the access token expired —
that turns a silent refresh into a full-screen redirect and re-logs the user in
on every visit. `currentUser()` already renews silently.

<!-- phase 4 rewrites the SPA half: the sample below is superseded — it teaches
     the sign-in loop. 403 means "signed in, not allowed" and must render
     Forbidden; on this gateway a 401 that arrives while a session still exists
     means the same thing and must NOT restart sign-in. -->
`src/api.ts` — attach the bearer token; on 401 fall back to a full sign-in:

```ts
export async function listTodos() {
  const token = await getAccessToken();
  const res = await fetch(`/api/todos`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (res.status === 401) { await signIn(); return []; }
  return res.json();
}
```

## Keep `userManager` inside `src/auth.ts`

Every other module reaches auth through the **functions** — `currentUser`,
`getAccessToken`, `signIn`, `handleCallback`, `signOut`. That list is the
module's whole surface, and it is what lets the app also run with no IDP behind
it: mock mode substitutes the module wholesale (`react-webapp`'s
`references/mock-mode.md` owns that). `userManager` is `oidc-client-ts`'s own
object and has no substitute, so a page that reaches for it directly compiles in
production and breaks the moment anybody tries to open the app without a cluster.

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
if required and required ∉ X-User-Scopes  → 403 + WWW-Authenticate: Bearer error="insufficient_scope", scope="<handle>"
next()
```

Copy it from your stack skill and wire it once:

| Stack | Asset | Wiring |
|---|---|---|
| Go | `$AEP_SKILLS_DIR/go/assets/scopes_middleware.go` | `gen.ChiServerOptions{Middlewares: …}` — **never** `r.Use` |
| Ballerina | `$AEP_SKILLS_DIR/ballerina/assets/scopes.bal` + `scope_table_drift_test.bal` | `http:InterceptableService` + `createInterceptors`; verify with `bal build && bal test` |

**You author no new check.** A handler that re-tests the operation's own scope
is a second authority that drifts from the contract.

**Test for EMPTY, never for MISSING.** The gateway sets every mapped header even
when the claim is absent, so `X-User-Id: ""` is what an identity-less request
looks like on the wire; "the header is present" is not a signal.

**The service answers 403 even though the gateway answers 401.** On the pinned
gateway every policy failure — no token, bad token, wrong issuer, wrong
audience, missing scope — is a 401 with a byte-identical body and no
`WWW-Authenticate`, and there is no knob that changes it. Your 403 is what a
bypass, a test, or a direct in-cluster call sees, and it is the status the SPA's
Forbidden path is written against. Never answer 401 for a permission failure:
the SPA reads 401 as "token expired" and restarts sign-in, which loops forever.

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
whole-string match, at the gateway and in the middleware: `claims:read-all` does
not imply `claims:read` to anything but a human reader. A role meant to call
`GET /claims` must be granted the operation's OWN handle (`claims:read`) as well
as the widening one. If the design grants a role only the widening handle, say
so in your report — the app cannot fix it, and the role will be refused at the
gateway before your code runs.

**A row that exists but is not the caller's is a 404, not a 403** — a caller who
may not see a row may not learn it exists (`api-management` owns that rule).

**`X-User-Scopes` also carries the OIDC scopes** (`openid profile email group
ou`), verbatim from the token. Tokenise on space and compare whole strings;
"the caller has some scope" is true of everybody signed in and is never an
authorization answer.

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
| Signed-in user loops back to the login page forever | A handler answered "no permission" with **401**; the SPA reads 401 as "token expired" and restarts sign-in | Return **403** with `insufficient_scope`. |
| A caller with no subject is treated as signed in | The check tested "`X-User-Id` present"; the gateway sets every mapped header even when the claim is absent | Test for the EMPTY string. |
| A role-scoped caller signs in but sees no rows | The handler filtered on `X-User-Id` and never widened, or matched `X-User-Id` against a directory id | Widen with `hasScope("<resource>:<any-action>")`; resolve directory records by `X-User-Name`. |
| A caller holding `claims:read-all` is refused `GET /claims` | Scope comparison is an exact string match; the widening handle does not imply the operation's own handle | A design finding: the role must be granted `claims:read` too. Report it; do not special-case it in code. |
| A public endpoint trusts `X-User-Id` | A public operation has no policy to overwrite an inbound header, so the caller set it | A handler behind `security: []` reads no identity header at all. |
| `bal test` never runs, so the Ballerina scope table silently drifts | `bal build` does not run tests | Verify is `bal build && bal test`. |
| A newly granted permission does not show up for a user who is already signed in | A refresh narrows but never widens — RFC 6749 §6 | Sign out and in again; a removed grant, by contrast, disappears at the next renew. |
| Sign-in loops at the right path, or the user is sent to login on every visit / new tab | No persistent `WebStorageStateStore` (the in-memory default loses the PKCE verifier across the redirect), session in `sessionStorage`, or the load path calls `signIn()` on a merely-expired token | `WebStorageStateStore({ store: localStorage })` + `automaticSilentRenew`; renew via `signinSilent()` and only `signIn()` when there is no session. |
| After login, "invalid redirect URI" | `redirect_uri` doesn't match the `<origin>/callback` the platform registered | Compute `window.location.origin + '/callback'`. |
| Logout button does nothing | `signOut()` calls only `signoutRedirect()`, which rejects (no `end_session_endpoint`), and the handler swallows it | Wrap it in the try/catch fallback to `removeUser()` + reload. |
