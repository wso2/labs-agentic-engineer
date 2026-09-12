/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Copied to <app-path>/src/auth.ts. Change ONLY the `USER_AUTH_` prefix, to
// whatever your component's identity dependency is named in design.json
// (UPPER_SNAKE(depName)); everything else is verbatim.
//
// OIDC Authorization Code + PKCE against the platform's IdP. The OAuth client
// is platform-owned: client id, redirect URIs and the requested scope list all
// arrive as `<DEP>_*` outputs. Nothing here creates or computes any part of it.
//
// `userManager` is NOT exported. Every other module reaches the session through
// the functions below, and that is exactly what lets mock mode substitute this
// whole module (mock/auth.ts) with nothing under src/ changing. A single
// `export const userManager` re-opens the library to the whole app and the
// substitution stops being total.
//
// MODULE-LOAD SIDE EFFECTS, on purpose: ./env throws if the platform's
// /env-config.js did not load, and the UserManager is constructed here. Nothing
// that imports this file can be loaded in a node unit test. The rules that need
// testing therefore live in ./authz-core, which imports nothing.

import { UserManager, WebStorageStateStore, type User } from "oidc-client-ts";
import { env } from "./env";
import { tokenIsValid as expiryIsValid } from "./authz-core";

// RFC 8707 resource indicator: the project's resource-server identifier. The
// IdP mints an access token whose `aud` is this value and whose `scope` is
// narrowed to the handles the signed-in user's roles grant against that RS.
// Without it the token carries the IdP's default audience and the API gateway
// rejects it on `aud` before it ever reads a scope — a 401 that looks exactly
// like every other 401, on a sign-in that looked perfectly healthy.
const RESOURCE = env.USER_AUTH_RESOURCE;

// MEASURED against oidc-client-ts 3.5.0, not assumed. `resource` rides exactly
// ONE of the three legs by itself, so all three are set here:
//
//   1. authorize    `settings.resource` below — appended to the authorize URL.
//   2. code→token   NOT carried. `_processCode` sends client_id, code,
//                   redirect_uri, code_verifier and `...extraTokenParams`, so
//                   the indicator reaches the token endpoint only through
//                   `extraTokenParams` below.
//   3. renew        NOT carried from settings. `signinSilent(args)` forwards
//                   `args.resource` / `args.extraTokenParams` and never falls
//                   back to `this.settings.*`, so a bare `signinSilent()` sends
//                   none — including the one the library's own
//                   SilentRenewService fires. `currentUser()` passes both.
//
// On this IdP leg 3 is survivable: the refresh token is bound to the resource
// server it was issued for, so an argument-less renew keeps the same `aud` (and
// naming a DIFFERENT resource is refused with `invalid_target`). Set all three
// anyway — it is what makes the app portable to an IdP that re-derives the
// audience from the request, where the omission silently downgrades every
// renewed token one access-token lifetime after sign-in.
//
// There is NO `useRefreshToken` setting in 3.5.0. It is a method on
// `OidcClient`, not a member of `UserManagerSettings`, and passing it here is a
// TYPE ERROR. `automaticSilentRenew: true` plus the refresh token in the
// response is what makes renewal use the refresh grant.
const RESOURCE_TOKEN_PARAMS = { resource: RESOURCE } as const;

/**
 * The grace `tokenIsValid()` allows past `expires_at`, in seconds.
 *
 * MEASURED: 3.5.0 has NO `clockSkewInSeconds` setting — like `useRefreshToken`,
 * passing one is a type error; the settings it does carry are
 * `accessTokenExpiringNotificationTimeInSeconds` (default 60, the lead time the
 * silent renew fires at) and `staleStateAgeInSeconds`. So the grace is ours,
 * and 60 is that same renew window: an `expires_at` that has only just passed
 * means a renew is in flight, not that the session is gone. Wider would keep
 * calling a dead session alive for longer; narrower would call a live one dead,
 * and that is the direction that loops (see ./authz-core#tokenIsValid).
 *
 * NOT exported: mock/auth.ts substitutes this whole module, so every name that
 * leaves it is a name the mock has to carry too.
 */
const CLOCK_SKEW_SECONDS = 60;

const userManager = new UserManager({
  authority: env.USER_AUTH_ISSUER,
  client_id: env.USER_AUTH_CLIENT_ID,

  // There is no env key for the redirect URI. The platform registers the SPA's
  // served callback URL, and the SPA is served at its host root.
  redirect_uri: window.location.origin + "/callback",
  post_logout_redirect_uri: window.location.origin,

  response_type: "code",
  scope: env.USER_AUTH_SCOPES,

  resource: RESOURCE,
  extraTokenParams: RESOURCE_TOKEN_PARAMS,

  // The token lives in JS-readable storage — acceptable for a public SPA; keep
  // loadUserInfo:false and lean on the platform CSP. localStorage (not
  // sessionStorage) is also what carries the PKCE verifier across the redirect.
  userStore: new WebStorageStateStore({ store: window.localStorage }),

  automaticSilentRenew: true,
  loadUserInfo: false,
});

export async function signIn(): Promise<void> {
  await userManager.signinRedirect();
}

export async function handleCallback(): Promise<User> {
  return userManager.signinRedirectCallback();
}

// The IdP's discovery document advertises no end_session_endpoint, so
// signoutRedirect() rejects. Drop the LOCAL session instead.
export async function signOut(): Promise<void> {
  try {
    await userManager.signoutRedirect();
  } catch {
    await userManager.removeUser();
    window.location.assign("/");
  }
}

/**
 * The signed-in user, or null when there is no session to renew.
 *
 * An EXPIRED session renews silently — never call signIn() merely because the
 * access token expired, which turns a silent refresh into a full-screen
 * redirect on every visit. The explicit `resource` is leg 3 above.
 */
export async function currentUser(): Promise<User | null> {
  const user = await userManager.getUser();
  if (user && !user.expired) return user;
  try {
    return await userManager.signinSilent({
      resource: RESOURCE,
      extraTokenParams: RESOURCE_TOKEN_PARAMS,
    });
  } catch {
    return null;
  }
}

/** The bearer for an API call, renewing silently first if it has to. */
export async function accessToken(): Promise<string | null> {
  const user = await currentUser();
  return user?.access_token ?? null;
}

/**
 * Is the session's own access token still alive? Reads the STORED user's
 * `expires_at` against the clock — no network, no renew, no sign-in, so
 * src/api-client.ts can call it on every response without recursing into the
 * thing it is deciding about. Deliberately NOT `currentUser()`, which renews.
 *
 * This is the second half of the 401 rule: the gateway answers 401 for a
 * missing scope exactly as it does for a dead token, and this local value is
 * the only thing that tells the two apart. See ./authz-core#classifyApiFailure.
 */
export async function tokenIsValid(): Promise<boolean> {
  const user = await userManager.getUser();
  if (!user?.access_token) return false;
  return expiryIsValid(user.expires_at, Date.now(), CLOCK_SKEW_SECONDS);
}
