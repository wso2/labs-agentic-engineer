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

// PATTERN, not a verbatim copy. Copy this to <app-path>/src/App.tsx and replace
// PAGE_BY_KEY with YOUR pages. The ROUTING STRUCTURE below is the part that is
// prescribed, and it is a structure rule, not styling:
//
//   NoAccess sits ABOVE the shell route and REPLACES it.
//     A caller who unlocks nothing gets no navbar, no empty sidebar and no
//     branded chrome around "you have no access". Rendering NoAccess inside
//     AppShell's outlet — the natural thing to write, and what a real run
//     did — wraps an empty rail around the message and tells the user less
//     than the message alone does.
//
//   Forbidden sits INSIDE the shell.
//     The opposite case: the caller holds other scopes and has somewhere to go,
//     so the rail stays. /forbidden is a route inside the shell branch.
//
//   Every gated route is wrapped in <RequireScope>, with the handle taken from
//     SCREEN_ROUTES — never a handle typed here.
//
//   /callback is routed OUTSIDE the provider: there is no session to read until
//     the redirect has been processed.

import { useEffect, type ReactElement } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthzProvider, Forbidden, NoAccess, RequireScope, useScopes, useAuthz } from "./authz";
import { SCREEN_ROUTES, reachableScreens } from "./screens";
import { signIn } from "./auth";
import { AppShell } from "./shell/AppShell";
import { CallbackPage } from "./pages/Callback";
import { MyClaimsPage } from "./pages/MyClaims";
import { SubmitClaimPage } from "./pages/SubmitClaim";
import { ApprovalsPage } from "./pages/Approvals";
import { ReportsPage } from "./pages/Reports";

const APP_NAME = "Expense Tracker";

/** YOUR pages, keyed by the normalized screen name src/screens.ts produces. */
const PAGE_BY_KEY: Record<string, ReactElement> = {
  myclaims: <MyClaimsPage />,
  submitclaim: <SubmitClaimPage />,
  approvals: <ApprovalsPage />,
  reports: <ReportsPage />,
};

export function App(): ReactElement {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/callback" element={<CallbackPage />} />
        <Route
          path="*"
          element={
            <AuthzProvider fallback={<Splash />}>
              <SignedIn />
            </AuthzProvider>
          }
        />
      </Routes>
    </BrowserRouter>
  );
}

function Splash(): ReactElement {
  return (
    <main>
      <h1>{APP_NAME}</h1>
      <p>Checking your session…</p>
    </main>
  );
}

function SignedIn(): ReactElement {
  const { signedIn } = useAuthz();
  const scopes = useScopes();

  // The load-time guard. Only a MISSING session starts a sign-in: currentUser()
  // has already tried a silent renew, and signing in on a merely expired token
  // re-logs the user in on every visit.
  useEffect(() => {
    if (!signedIn) void signIn();
  }, [signedIn]);

  if (!signedIn) return <Splash />;

  const reachable = reachableScreens(scopes);

  // ── G9: NoAccess REPLACES the shell. It is returned here, above the <Routes>
  //    that carry AppShell, so there is no rail to wrap it.
  if (reachable.length === 0) return <NoAccess appName={APP_NAME} />;

  const landing = reachable[0].path;

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Navigate to={landing} replace />} />
        {SCREEN_ROUTES.map((screen) => {
          const page = PAGE_BY_KEY[screen.key];
          // "public" and null need no handle — anyone with a session is in.
          if (screen.requires === null || screen.requires === "public") {
            return <Route key={screen.key} path={screen.path} element={page} />;
          }
          // The handle comes from the generated table, never from this file.
          return (
            <Route
              key={screen.key}
              element={<RequireScope handle={screen.requires} screen={screen.label} />}
            >
              <Route path={screen.path} element={page} />
            </Route>
          );
        })}
        {/* ── Forbidden is INSIDE the shell: the rail the caller can use stays. */}
        <Route path="/forbidden" element={<Forbidden />} />
        <Route path="*" element={<Navigate to={landing} replace />} />
      </Route>
    </Routes>
  );
}
