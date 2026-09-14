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

import { useEffect, useMemo, type PropsWithChildren } from "react";
import { hasAuthParams, useAuth } from "react-oidc-context";
import { env } from "../config/env";
import { getUserManager } from "./userManager";
import { decodeJwtClaims, identityFromClaims, type TokenClaims } from "./claims";
import { permissionsFromScope } from "./permissions";
import { MOCK_ORG, MOCK_USER, mockPermissions } from "./mockSession";
import { AuthScreen } from "./AuthScreen";
import { BillingActivation } from "./BillingActivation";
import { SessionContext, type Session } from "./SessionContext";

const MOCK_SESSION: Session = {
  user: MOCK_USER,
  orgHandle: MOCK_ORG,
  permissions: mockPermissions(),
  signOut: () => {
    console.info("[auth] mock mode — sign-out is a no-op");
  },
};

// Gates the whole app (rendered at __root): children only ever see a
// signed-in state, with the session in context.
export function AuthGuard({ children }: PropsWithChildren) {
  if (env.authMode === "mock") {
    return (
      <SessionContext.Provider value={MOCK_SESSION}>
        <BillingActivation />
        {children}
      </SessionContext.Provider>
    );
  }
  return <OidcGuard>{children}</OidcGuard>;
}

// RP-initiated logout when the IdP advertises end_session_endpoint; some
// Thunder versions don't (dev-thunder-setup's v0.47.0), so fall back to
// best-effort token revocation + dropping the local session, then a fresh
// page load — the guard restarts the login flow, which prompts again.
// Uses the raw UserManager: the react-oidc-context wrapper converts the
// signoutRedirect rejection into auth.error instead of rethrowing, which
// would strand the user on the error screen.
async function signOut(): Promise<void> {
  const userManager = getUserManager();
  try {
    await userManager.signoutRedirect();
  } catch {
    try {
      await userManager.revokeTokens();
    } catch {
      // Best effort — revocation failing must not block sign-out.
    }
    await userManager.removeUser();
    window.location.assign("/");
  }
}

function OidcGuard({ children }: PropsWithChildren) {
  const auth = useAuth();

  // Kick off the redirect only when settled-unauthenticated: not while the
  // provider is loading, not mid-navigation, and not while ?code&state from
  // Thunder is still being exchanged.
  const shouldSignIn =
    !auth.isLoading &&
    !auth.isAuthenticated &&
    !auth.activeNavigator &&
    !auth.error &&
    !hasAuthParams();

  useEffect(() => {
    if (shouldSignIn) {
      const returnTo = window.location.pathname + window.location.search;
      void auth.signinRedirect({ state: { returnTo } });
    }
  }, [shouldSignIn, auth]);

  const session = useMemo<Session>(() => {
    const idClaims = (auth.user?.profile ?? {}) as TokenClaims;
    const accessClaims = auth.user
      ? (decodeJwtClaims(auth.user.access_token) ?? {})
      : {};
    const identity = identityFromClaims(idClaims, accessClaims);
    return {
      user: { name: identity.name, email: identity.email },
      orgHandle: identity.orgHandle,
      // TEMP (testing only — revert before merging): hardcoded to every AE
      // permission regardless of the token's actual scope claim. aep-api still
      // enforces the real scope server-side, so this only unlocks console UI
      // gating, not backend authorization.
      permissions: new Set([
        // "ae:skill-config",
        // "ae:skill-view",
        // "ae:model-config",
        // "ae:github-config",
        // "ae:requirement-update",
        "ae:requirement-view",
        "ae:design-view",
        "ae:build",
        "ae:build-view",
        // "ae:usage-view",
        // "ae:observability-view",
        "ae:ai-chat",
        // "ae:resource-view",
        // "ae:resource-config",
      ]),
      // Real permissions, derived from the access token's own scope claim —
      // the same source aep-api's permission gate checks (auth.Claims.Permissions()),
      // so the console can never grant something the backend would refuse.
      // Requires the token to actually carry ae:* scope entries, which needs
      // both a scope request that lists them (VITE_THUNDER_SCOPES) and a
      // matching OAuth `resource` indicator (VITE_THUNDER_RESOURCE) — see
      // deployments/single-cluster/thunder-resources/92-ae-roles.yaml's
      // resource_server header for why the resource indicator is required.
      // permissions: permissionsFromScope(accessClaims["scope"]),
      signOut: () => void signOut(),
    };
  }, [auth]);

  if (auth.error) {
    return (
      <AuthScreen
        label=""
        error={auth.error.message}
        onRetry={() => void auth.signinRedirect()}
      />
    );
  }
  if (!auth.isAuthenticated) {
    return (
      <AuthScreen
        label={
          hasAuthParams() || auth.isLoading
            ? "Completing sign-in…"
            : "Redirecting to sign-in…"
        }
      />
    );
  }
  return (
    <SessionContext.Provider value={session}>
      <BillingActivation />
      {children}
    </SessionContext.Provider>
  );
}
