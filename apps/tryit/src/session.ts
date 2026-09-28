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

import { UserManager, WebStorageStateStore, type UserManagerSettings } from "oidc-client-ts";
import type { Launch } from "./launch";

/**
 * OIDC Authorization Code + PKCE against the PROJECT's identity provider, as
 * the project's public client, requesting a token for the project's resource
 * server. Every value comes off the launch URL; the app owns none of them.
 *
 * `resource` is set on all three legs (settings, extraTokenParams, and every
 * signinSilent call) — oidc-client-ts carries it on only one by itself. Why,
 * measured: skills/thunder-authentication/assets/app/src/authz/session.ts.
 */
export function managerSettings(launch: Launch, origin: string): UserManagerSettings {
  return {
    authority: launch.issuer,
    client_id: launch.clientId,
    // The platform registers exactly this URL on every project's sign-in
    // client (aep-api's TRY_IT_CALLBACK_URL), so the origin is load-bearing.
    redirect_uri: origin + "/callback",
    post_logout_redirect_uri: origin,
    response_type: "code",
    scope: launch.scopes.join(" "),
    resource: launch.resource,
    extraTokenParams: { resource: launch.resource },
    // Per tab, like the launch itself: a token for one project must not leak
    // into a tab testing another. sessionStorage also carries the PKCE
    // verifier across the redirect, which is all the redirect needs.
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
    automaticSilentRenew: false,
    // The ID token carries no username — `sub` is a UUID (measured in the
    // thunder-authentication reference app). userinfo fills the profile claims.
    loadUserInfo: true,
  };
}

/**
 * The domain the platform gives a test user's email — `<username>@` this
 * (aep-api identity/ensure.go, testUserEmail). Thunder's userinfo carries the
 * email and no username claim, so the username is read back off it.
 */
const TEST_USER_EMAIL_DOMAIN = "@test-users.invalid";

/** The most useful name a profile carries; `sub` (a UUID) only as the last resort. */
export function displayName(profile: Record<string, unknown>): string | null {
  for (const claim of ["preferred_username", "username", "name"]) {
    const value = profile[claim];
    if (typeof value === "string" && value.length > 0) return value;
  }
  const email = profile.email;
  if (typeof email === "string" && email.length > 0) {
    return email.endsWith(TEST_USER_EMAIL_DOMAIN) ? email.slice(0, -TEST_USER_EMAIL_DOMAIN.length) : email;
  }
  const sub = profile.sub;
  return typeof sub === "string" && sub.length > 0 ? sub : null;
}

export interface Session {
  /** Leaves the page for the identity provider's sign-in. */
  signIn(): Promise<void>;
  /** Finishes the redirect that landed on /callback. */
  handleCallback(): Promise<void>;
  /** The bearer for the agent, renewed silently if it has to be; null without a session. */
  accessToken(): Promise<string | null>;
  /** The signed-in username, for the header; null without a session. */
  username(): Promise<string | null>;
  /** Drops the local session. The provider advertises no end-session endpoint. */
  signOut(): Promise<void>;
}

export function createSession(launch: Launch): Session {
  const manager = new UserManager(managerSettings(launch, window.location.origin));
  const renewArgs = { resource: launch.resource, extraTokenParams: { resource: launch.resource } };
  const current = async () => {
    const stored = await manager.getUser();
    if (stored && !stored.expired) return stored;
    if (!stored?.refresh_token) return null;
    try {
      return await manager.signinSilent(renewArgs);
    } catch {
      return null;
    }
  };
  return {
    signIn: () => manager.signinRedirect(),
    // signinCallback(), not signinRedirectCallback(): it reads the request
    // type off the stored state, so the one registered redirect URI serves a
    // silent leg too should one ever land here.
    handleCallback: async () => {
      await manager.signinCallback();
    },
    accessToken: async () => (await current())?.access_token ?? null,
    username: async () => {
      const user = await current();
      return user ? displayName(user.profile) : null;
    },
    signOut: () => manager.removeUser(),
  };
}
