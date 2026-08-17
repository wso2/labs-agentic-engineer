---
name: thunder-authentication
description: How end-user identity works on the platform — Thunder, the IDP wired into the API gateway, signs users in and components authorize them. Covers the auth platform-resource dependency, the platform-owned OAuth client, the window._env_.<DEP>_* runtime keys, OIDC + PKCE in the SPA, and resolving the caller's role and directory record on the backend. Apply to any SPA whose users sign in, and to every protected backend they call.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Thunder Authentication

End-user identity is delegated to Thunder, the platform's Identity Provider,
which the API gateway is wired to as its external IDP. That gives every project
with sign-in **two halves reading one claim set**:

- **The SPA** signs the user in with OIDC Authorization Code + PKCE and reads
  their claims from the ID token (`user.profile.groups`).
- **The protected backend** never sees a token: the gateway validates it and
  injects the SAME claims as headers (`X-User-Groups`). See `api-management` for
  the gateway's side of that contract.

It is one claim set, so the two sides must resolve a caller's role identically.

The OAuth client itself is **platform-owned**: you never create, compute, or
hardcode any part of it.

---

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
| `USER_AUTH_SCOPES` | `<DEP>_SCOPES` | space-separated scopes (e.g. `openid profile email`) |

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

**There is no sign-out endpoint.** Thunder's discovery document advertises only
issuer, authorize and token — no `end_session_endpoint` — so
`signoutRedirect()` rejects. Sign-out drops the local session (`removeUser()`)
and reloads.

**Roles ride in the ID token.** `oidc-client-ts` surfaces them as
`user.profile.groups`, beside `ouId`/`ouName`/`ouHandle` and standard
`profile`/`email`. The platform requests the `group`/`ou` scopes by default, so
never decode the access token for roles and never hand-parse a JWT.

**Dev clusters** ship a default Thunder admin: `admin` / `admin`, in the
`Administrators` group. Real orgs add users via Thunder's admin console / SCIM.

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

export async function getRoles(): Promise<string[]> {
  const user = await currentUser();
  const groups = user?.profile?.groups;
  return Array.isArray(groups) ? (groups as string[]) : [];
}
```

On app load, gate rendering on `currentUser()`: a user → proceed; `null` →
`signIn()`. Do **not** call `signIn()` merely because the access token expired —
that turns a silent refresh into a full-screen redirect and re-logs the user in
on every visit. `currentUser()` already renews silently.

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

---

# Backend authorization

The gateway hands your service the caller's verified identity as headers
(`api-management` covers the mechanism and the 401-on-missing-`X-User-Id` rule).
What follows is what that identity *means*.

## Identity is not authorization

`X-User-Id` is an **opaque IdP subject** — not a record key in any other service,
so a directory lookup keyed on it 404s. Split the two questions a caller raises:

- **Role** (what may they do) comes from `X-User-Groups` — the SAME groups claim
  the SPA reads from `user.profile.groups`. An authenticated caller with no
  recognized role is a **403, never a 401**: a 401 tells the SPA its token
  expired, so it restarts sign-in and loops forever.
- **Directory attributes** (which unit is theirs, their own id in the directory)
  come from the caller's **directory record**, resolved by `X-User-Name` — the
  username, which the directory keys on. A group name is a role, not an identity,
  so never parse an attribute out of one, and never match `X-User-Id` against a
  stored record id. `X-User-Name` is gateway-set from the validated token so it
  is safe to authorize on, but a username can be renamed — so look the record UP
  by it, and never store it as a row key (`api-management` covers what to key on).

## Implementation

Resolve the role from `X-User-Groups`, never by looking `X-User-Id` up anywhere.
**403**, never 401, when no group maps. One resolver, called by every protected
handler:

- **Parsing `X-User-Groups`.** It arrives as a JSON array (e.g.
  `["Compliance Admin"]`); accept a comma-separated string as a fallback. Match
  each group case-insensitively against the spec's role names — a substring
  match on the keyword (`admin`, `auditor`) survives the org renaming its
  groups, an equality check does not.
- **No recognized group → no role → 403.** Return the empty/absent case
  explicitly rather than defaulting to the least-privileged real role; a
  default role is a silent authorization grant.

When roles scope by the caller's own directory attributes — their unit, their own
id — resolve the caller's **directory record** by `X-User-Name` and filter on
that record's fields:

- Missing `X-User-Name`, or a username that resolves to no record → **403**.
- An admin-equivalent role takes no filter; every other role filters on a field
  of the resolved record — never on `X-User-Id` (opaque) and never on a group
  name.

Express this in your stack's own idiom — where the resolver lives, its
signature, and how a handler returns 403 — following the conventions that skill
already sets. The directory's real endpoint, its username field, and the
dependency wiring are org-specific — `internal-services` owns them; do not
hardcode a roster.

---

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Signed-in user loops back to the login page forever | A protected handler answers no-role (or a failed directory lookup keyed on `X-User-Id`) with **401**; the SPA reads 401 as "token expired" and restarts sign-in | Resolve role from `X-User-Groups`; return **403**; never key a directory lookup on `X-User-Id`. |
| A role-scoped caller signs in but sees no rows | Scope derived the attribute from a group NAME (empty for a generic role group), or matched `X-User-Id` (an opaque subject) against a stored directory id (never equal) | Resolve the caller's directory record via `X-User-Name`, read the attribute from it, filter on that. |
| Every user shows no role / `groups` is empty | Roles read from the access token or a hand-decoded JWT | SPA: `user.profile.groups`. API: `X-User-Groups`. |
| Sign-in loops at the right path, or the user is sent to login on every visit / new tab | No persistent `WebStorageStateStore` (the in-memory default loses the PKCE verifier across the redirect), session in `sessionStorage`, or the load path calls `signIn()` on a merely-expired token | `WebStorageStateStore({ store: localStorage })` + `automaticSilentRenew`; renew via `signinSilent()` and only `signIn()` when there is no session. |
| After login, "invalid redirect URI" | `redirect_uri` doesn't match the `<origin>/callback` the platform registered | Compute `window.location.origin + '/callback'`. |
| Logout button does nothing | `signOut()` calls only `signoutRedirect()`, which rejects (no `end_session_endpoint`), and the handler swallows it | Wrap it in the try/catch fallback to `removeUser()` + reload. |
