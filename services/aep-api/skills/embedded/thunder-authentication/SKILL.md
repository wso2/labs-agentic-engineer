---
name: thunder-authentication
description: How the platform's Thunder IDP is wired into SPAs that sign users in. Covers the end-user-auth platform-resource dependency that triggers auth, the per-dependency Thunder OAuth client (platform-owned — agent never sees client_id), the generic window._env_.<DEP>_* runtime keys derived from YOUR dependency name, and OIDC client wiring with oidc-client-ts. Pairs with react-webapp when the SPA wiring patterns apply. Apply on any project whose spec implies users sign in.
---


# Thunder Authentication

## What this skill does

The platform delegates end-user authentication to Thunder (the WSO2
Identity Provider running on the cluster). This skill tells the
architect when to mark a web-app for sign-in, what the platform's Thunder
Application operator provisions behind the scenes via the provisioned
dependency, and how the SPA code reads OIDC config at runtime to
sign users in via Authorization Code + PKCE.

## Platform facts

- One Thunder application is provisioned per auth platform-resource
  dependency, created by the platform's Thunder Application operator once the
  dependency is provisioned. Its `client_id` is a platform-derived opaque
  identifier — it is NOT the dependency's design.json `name`, and the agent
  never computes, sees, or hardcodes it; the platform delivers it via
  `window._env_.<DEP>_CLIENT_ID`. The same goes for the `client_secret`
  and redirect URIs — the platform owns them.
- The redirect URI is platform-registered: the platform patches the SPA's
  served callback URL onto the OAuth app once its public URL resolves (driven
  by the resource type's `consumer-url-env-config` marker). The SPA is served
  at its host root (see `react-webapp`), so that URL is `<origin>/callback`.
  The SPA is NOT handed a redirect-URI key; it reconstructs the SAME value in
  the browser, `window.location.origin + '/callback'`, and serves the callback
  route at `/callback`.

### Runtime keys — derived from YOUR dependency name

For a web-app, the platform emits EVERY platform-resource dependency's
binding outputs into `window._env_` under generic keys
`<UPPER_SNAKE(depName)>_<UPPER_SNAKE(outputName)>`. There is NO fixed
key prefix — the prefix is the UPPER_SNAKE of the dependency `name` the
ARCHITECT chose. The auth resource type outputs `client_id`,
`issuer`, `jwks_url`, and `scopes`, so for a dependency named `user-auth`
the keys are:

| Key (dep `user-auth`) | Generic form | Meaning |
|---|---|---|
| `USER_AUTH_CLIENT_ID` | `<DEP>_CLIENT_ID` | this app's platform-owned OAuth client id |
| `USER_AUTH_ISSUER` | `<DEP>_ISSUER` | OIDC issuer / authority for `oidc-client-ts` |
| `USER_AUTH_JWKS_URL` | `<DEP>_JWKS_URL` | JWKS endpoint (token validation reference) |
| `USER_AUTH_SCOPES` | `<DEP>_SCOPES` | space-separated OIDC scopes (e.g. `openid profile email`) |

Substitute YOUR dependency name for `<DEP>`. Read these keys with their
EXACT derived spellings — inventing one (or hardcoding a fixed prefix
instead of deriving it from YOUR dependency name) produces a
`ReferenceError` at module load because the value is `undefined`.

- The redirect URI and post-sign-in URL are NOT platform-emitted keys —
  compute them in the browser (redirect URI as above; post-sign-in landing =
  `window.location.origin`).
- The Thunder OIDC discovery endpoint is `<DEP>_ISSUER/.well-known/openid-configuration`.
- Token endpoint: `<DEP>_ISSUER/oauth2/token`. The SPA posts to it
  cross-origin — there is NO same-origin `/oidc/` proxy in nginx.
- The signed-in user's identity claims ride in the ID TOKEN, surfaced by
  `oidc-client-ts` as `user.profile`: `groups` (their role/group memberships)
  and `ouId`/`ouName`/`ouHandle` (their organization), beside standard
  `profile`/`email`. The platform requests the `group`/`ou` scopes by default,
  so a role-aware SPA reads roles from `user.profile.groups` — it never decodes
  the access token for them. The protected backend reads the SAME `groups`
  from the gateway-injected `X-User-Groups` header (see `api-management`), so
  the SPA and API resolve the caller's role identically.
- Default Thunder admin user (dev clusters): `admin` / `admin` in the
  `Administrators` group. Real orgs add their own users via Thunder's
  admin console / SCIM.
- Switching IDPs (Asgardeo, custom) is a settings-page action against
  the org's `OrganizationIDPProfile` record — NOT a skill edit. The
  `<DEP>_*` keys are emitted for every SPA that declares an auth
  platform-resource dependency; a future PR honours the profile flavour.
  Until then, attaching an `asgardeo-authentication` custom skill produces
  code that *talks Asgardeo client semantics against a Thunder backend*
  — the OIDC handshake completes but Asgardeo-specific extensions
  don't apply.

## Recommended practice

### Architect

**The sign-in trigger is an explicit auth platform-resource dependency —
nothing else provisions auth.** When the spec implies users sign in
(keywords: `login`, `sign in`, `user account`, `personal`, ...), the SPA
**and** every backend it calls each declare the SAME `platform-resource`
dependency of the auth resource type:

```json
{ "kind": "platform-resource", "name": "user-auth", "resourceType": "thunder-app", "description": "sign-in for shoppers" }
```

- Call `list_platform_resource_types` FIRST — never guess the type name.
  The auth resource type outputs `client_id` / `issuer` / `jwks_url` /
  `scopes`; those become the `window._env_.<DEP>_*` runtime keys.
- Choose a clear dependency `name` — it becomes the runtime key prefix
  (`user-auth` → `USER_AUTH_*`). The SAME name on the SPA and its backends
  is what ties sign-in to token-carrying API calls.
- You MAY propose the `scopes` parameter value derived from the spec — this
  is the one explicit exception to the never-invent-parameters rule (default
  `openid profile email`).
- NEVER set `redirectUris` — they are platform-managed (the platform
  registers the SPA's `<origin>/callback` URL once its public URL resolves).
- Do NOT emit `exposesAPI.auth: end-user-required` on the backend yourself —
  the platform DERIVES it from the shared auth dependency (keyed on the
  resource type's `end-user-auth` role marker). Setting an explicit
  `service-required` alongside the dependency is a validation error.
- Declare the dependency on the SPA and on each protected backend with the
  SAME dependency `name`, so the SPA signs in and its API calls carry a
  token the backend's gateway accepts. Without the dependency, NO Thunder
  application is provisioned, NO `<DEP>_*` keys land in `window._env_`,
  and the SPA deploys unable to sign in.

- The web-app's `componentAgentInstructions` MUST say (verbatim or close):
  `OIDC Authorization Code + PKCE against the platform IDP using oidc-client-ts. Read OIDC config from window._env_.<DEP>_* (<DEP> = UPPER_SNAKE of the auth dependency name) and upstream URLs from window._env_.<UPSTREAM>_URL — typed via src/env.ts. Compute redirect_uri = window.location.origin + '/callback' and serve the callback route at '/callback'. Attach Authorization: Bearer <access_token> to every API call. DO NOT write a .env file. Runtime config comes from window._env_, never import.meta.env.VITE_*. DO NOT use envsubst, /etc/nginx/templates/, or any custom nginx entrypoint — stock nginx:alpine serves the static bundle + env-config.js.`
- Do NOT create a separate `auth` / `identity` / `login` /
  `session` / `user-service` component. Thunder owns token issuance;
  the API just reads `X-User-Id` (covered by `api-management`).
- Do NOT add `/auth/login`, `/auth/register`, `/auth/logout` endpoints
  to ANY backend service. Thunder issues tokens; the SPA initiates the
  redirect.

### Tech-lead — issue body bullets

For every web-app task whose component declares an auth platform-resource
dependency:

- Scope: "Implement OIDC Authorization Code + PKCE using
  `oidc-client-ts`, configured from `window._env_.<DEP>_*` (where `<DEP>`
  is the UPPER_SNAKE of the auth dependency name — e.g. dep `user-auth` →
  `USER_AUTH_ISSUER`, `USER_AUTH_CLIENT_ID`, `USER_AUTH_SCOPES`). The
  platform writes these keys into `env-config.js` via the SPA's
  ReleaseBinding; the agent's `index.html` loads it synchronously
  before the bundle. Read values via the typed `src/env.ts` shim and
  throw at module top-level on missing keys — no `?? ''` fallback. Do
  NOT write a `.env` file. Do NOT use `import.meta.env.VITE_*`."
- Scope: "Compute `redirect_uri = window.location.origin + '/callback'` and
  land the user on `window.location.origin` after sign-in — these are NOT env
  keys. Serve the callback route at `/callback` — the platform registered
  exactly that URL."
- Scope: "Attach `Authorization: Bearer <access_token>` to every
  `window._env_.API_BASE_URL` fetch. On 401, restart the login flow
  via `signIn()`. Do NOT write a `/login` form that POSTs credentials
  anywhere."
- Acceptance criteria: "Loading the webapp unauthenticated redirects to
  the OIDC authorize endpoint; after sign-in, the user lands back on
  the app with a token in sessionStorage; subsequent API calls carry
  `Authorization: Bearer <token>` and return per-user data; reloading
  the page keeps the user signed in."

### Coding agent — implementation

`src/env.ts` — the base shim (the `window._env_` presence guard,
`API_BASE_URL`, any `<UPSTREAM>_URL` keys, and the `export const env`)
is owned by the `react-webapp` skill — don't duplicate it. When the
component declares an auth platform-resource dependency, the platform
also populates the `<DEP>_*` keys; extend the `Env` type with them
(this example uses a dependency named `user-auth` → `USER_AUTH_*` — use
YOUR dependency name):

```ts
type Env = {
  API_BASE_URL: string;
  // ...plus any <UPSTREAM>_URL keys (see react-webapp).
  USER_AUTH_CLIENT_ID: string;
  USER_AUTH_ISSUER: string;
  USER_AUTH_JWKS_URL: string;
  USER_AUTH_SCOPES: string;
};
```

`src/auth.ts` — `oidc-client-ts` wired to `env.<DEP>_*`; `redirect_uri`
and the post-sign-in URL are computed from `window.location.origin`, NOT
read from env (again shown for a dependency named `user-auth`):

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
  userStore: new WebStorageStateStore({ store: window.sessionStorage }),
  loadUserInfo: false,
});

export async function signIn()         { await userManager.signinRedirect(); }
export async function signOut()        { await userManager.signoutRedirect(); }
export async function handleCallback() { return userManager.signinRedirectCallback(); }

export async function getAccessToken(): Promise<string | null> {
  const user = await userManager.getUser();
  return user?.access_token ?? null;
}
```

When the spec calls for role-based UI, read the user's roles from
`user.profile.groups` — the id_token claim `oidc-client-ts` surfaces (see
Platform facts); the platform already requests the `group`/`ou` scopes:

```ts
export async function getRoles(): Promise<string[]> {
  const user = await userManager.getUser();
  const groups = user?.profile?.groups;
  return Array.isArray(groups) ? (groups as string[]) : [];
}
```

Add a callback route at `/callback` in your router that calls
`handleCallback()` once on mount, then navigates to `/`. If you instead gate
rendering on `window.location.pathname`, compare it against `/callback`.

`src/api.ts` — attach `Authorization: Bearer <token>`; redirect on 401:

```ts
import { env } from "./env";
import { getAccessToken, signIn } from "./auth";

async function authHeaders(): Promise<HeadersInit> {
  const token = await getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export async function listTodos() {
  const res = await fetch(`${env.API_BASE_URL}/todos`, {
    headers: await authHeaders(),
  });
  if (res.status === 401) { await signIn(); return []; }
  return res.json();
}
```

### Don't

- ❌ Write a `/login` form that POSTs credentials to your API. Thunder
  owns token issuance.
- ❌ Hardcode a fixed key prefix, or any prefix other than the
  UPPER_SNAKE of YOUR auth dependency name. The keys are
  `<DEP>_CLIENT_ID` / `<DEP>_ISSUER` / `<DEP>_JWKS_URL` / `<DEP>_SCOPES`.
- ❌ Read a `redirect_uri` from `window._env_` — there is no such key.
  Compute `window.location.origin + '/callback'`.
- ❌ Add a same-origin `/oidc/` proxy in nginx. The browser posts to
  `${env.<DEP>_ISSUER}/oauth2/token` cross-origin.
- ❌ Hardcode the `client_id`. It is per-dependency and platform-derived;
  the platform puts it in `window._env_.<DEP>_CLIENT_ID`.
- ❌ Add Thunder client provisioning code anywhere — the platform's Thunder
  Application operator does it when the auth dependency is provisioned.

### Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| SPA throws `<KEY> not set` / redirects to `undefined/oauth2/authorize` | Agent hardcoded a fixed key prefix instead of deriving keys from YOUR dependency name | Use `env.<DEP>_ISSUER` etc., where `<DEP>` = UPPER_SNAKE of the auth dependency name (dep `user-auth` → `USER_AUTH_ISSUER`). |
| Every user shows no role / `groups` is empty | Roles read from the wrong place — the access token, or a hand-decoded JWT | Read `user.profile.groups` (the id_token claim `oidc-client-ts` surfaces). The platform already requests the `group`/`ou` scopes, so the claim is present. |
| After login, "invalid redirect URI" | `redirect_uri` doesn't match the `<origin>/callback` URL the platform registered (e.g. an invented redirect-URI key) | Compute `window.location.origin + '/callback'`. |
| Sign-in loops endlessly even at the right path | `oidc-client-ts` written without `WebStorageStateStore({ store: sessionStorage })` | Use the constructor shown above; without it, state and PKCE verifier don't survive the redirect. |
