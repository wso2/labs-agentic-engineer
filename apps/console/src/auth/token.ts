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

import { env } from "../config/env";
import { mockAccessToken } from "./mockSession";
import type { User } from "oidc-client-ts";
import { getUserManager } from "./userManager";

// Token access for non-React code: the API client attaches it. Mode-aware so
// callers never are.

export async function getAccessToken(): Promise<string | null> {
  if (env.authMode === "mock") return mockAccessToken();
  const user = await getUserManager().getUser();
  return user?.access_token ?? null;
}

// Refresh-token renewal, deduplicated: parallel 401s share one renew.
let renewInFlight: Promise<string | null> | null = null;

export function renewAccessToken(): Promise<string | null> {
  if (env.authMode === "mock") return Promise.resolve(mockAccessToken());
  renewInFlight ??= getUserManager()
    .signinSilent()
    .then((user) => user?.access_token ?? null)
    .catch(() => null)
    .finally(() => {
      renewInFlight = null;
    });
  return renewInFlight;
}

/**
 * Called with each silently renewed access token, for a long-lived connection
 * (the collab room) that must keep presenting a valid one. Returns the
 * unsubscribe.
 */
export function subscribeAccessTokenRefresh(cb: (token: string) => void): () => void {
  if (env.authMode === "mock") return () => {};
  const um = getUserManager();
  const handler = (user: User) => {
    if (user.access_token) cb(user.access_token);
  };
  um.events.addUserLoaded(handler);
  return () => um.events.removeUserLoaded(handler);
}

// Full re-auth, preserving where the user was (restored by the provider's
// onSigninCallback after the round-trip).
export function redirectToSignIn(): void {
  if (env.authMode === "mock") return;
  const returnTo = window.location.pathname + window.location.search;
  void getUserManager().signinRedirect({ state: { returnTo } });
}
