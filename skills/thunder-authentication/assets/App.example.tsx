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
// PAGE_BY_KEY and APP_NAME with YOUR pages and YOUR app's name — both below are
// the Expense Tracker fixture's. If the app already holds its name (the
// prototype's `name`, usually src/appName.ts), import it from there
// rather than declaring a second copy that can drift.
// The ROUTING STRUCTURE below is the part that is
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
//   /forbidden is WIRED INTO authz/client once, from the router.
//     client.ts imports no router, so it cannot navigate by itself: it calls
//     the navigator this file hands it. Without that call, a refusal the screen
//     gate did not catch — a typed URL, a stale bundle, a race past a narrowed
//     renew — logs an error and routes nowhere, which looks exactly like a
//     screen that renders nothing.
//
//   Every gated route is wrapped in <RequireOperation>, with the OPERATION
//     taken from SCREEN_ROUTES — never a handle or an operation typed here.
//
//   A `public` screen is routed ABOVE the sign-in guard.
//     `public: true` is a screen the PRD gives to a signed-out visitor, reachable
//     BEFORE sign-in. SignedIn() below redirects anyone without a session to
//     the IdP, so a public screen routed inside it can never be seen by the
//     visitor it exists for. It keeps the session provider (so <Can> and
//     useScopes work on it) and loses the app shell, which is the honest shape:
//     there is no signed-in chrome to draw.
//
//   /callback is routed OUTSIDE the provider: there is no session to read until
//     the redirect has been processed.

import { useEffect, type ReactElement } from "react";
import { BrowserRouter, Navigate, Route, Routes, useNavigate } from "react-router-dom";
import {
  AuthzProvider,
  Forbidden,
  NoAccess,
  RequireOperation,
  useAuthz,
  useScopes,
} from "./authz/gates";
import { SCREEN_ROUTES, reachableScreens, hasScopedReach } from "./authz/screens";
import { setForbiddenNavigator } from "./authz/client";
import { signIn } from "./authz/session";
import { AppShell } from "./shell/AppShell";
import { CallbackPage } from "./pages/Callback";
import { MyClaimsPage } from "./pages/MyClaims";
import { SubmitClaimPage } from "./pages/SubmitClaim";
import { ApprovalsPage } from "./pages/Approvals";
import { ReportsPage } from "./pages/Reports";
import { ExpensePolicyPage } from "./pages/ExpensePolicy";

const APP_NAME = "Expense Tracker";

/** YOUR pages, keyed by the screen keys src/authz/screens.ts declares. */
const PAGE_BY_KEY: Record<string, ReactElement> = {
  myclaims: <MyClaimsPage />,
  submitclaim: <SubmitClaimPage />,
  approvals: <ApprovalsPage />,
  reports: <ReportsPage />,
  policy: <ExpensePolicyPage />,
};

/** The screens reachable before sign-in — routed above the guard, below. */
const PUBLIC_SCREENS = SCREEN_ROUTES.filter((screen) => screen.public);

export function App(): ReactElement {
  return (
    <BrowserRouter>
      <ForbiddenWiring />
      <Routes>
        <Route path="/callback" element={<CallbackPage />} />
        {PUBLIC_SCREENS.map((screen) => (
          <Route
            key={screen.key}
            path={screen.path}
            element={
              <AuthzProvider fallback={<Splash />}>{PAGE_BY_KEY[screen.key]}</AuthzProvider>
            }
          />
        ))}
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

/**
 * Hands src/authz/client.ts the route a refusal goes to. ONCE, from inside the
 * router and above every route, so it is wired before the first request can be
 * answered. `replace` keeps the refused URL out of the history, so Back does
 * not walk the user straight into the same 403.
 */
function ForbiddenWiring(): null {
  const navigate = useNavigate();
  useEffect(() => {
    setForbiddenNavigator(() => navigate("/forbidden", { replace: true }));
  }, [navigate]);
  return null;
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

  const reachable = reachableScreens(scopes, signedIn);

  // NoAccess REPLACES the shell. It is returned here, above the <Routes> that
  // carry AppShell, so there is no rail to wrap it.
  //
  // The question is `hasScopedReach`, not `reachable.length === 0`: a
  // `loads: null` form is reachable by any signed-in caller and a `public`
  // screen by everyone, so an app holding either would never show NoAccess to
  // a caller with no scopes — it would drop them on a form with an empty rail
  // instead. See the note on `hasScopedReach`.
  if (!hasScopedReach(scopes, signedIn)) return <NoAccess appName={APP_NAME} />;

  // Safe: hasScopedReach just proved at least one scope-gated screen is here.
  const landing = (reachable.find((s) => !s.public && s.loads !== null) ?? reachable[0]).path;

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Navigate to={landing} replace />} />
        {SCREEN_ROUTES.map((screen) => {
          // `public` screens are routed above this guard, in App(), and their
          // path never reaches here.
          if (screen.public) return null;
          const page = PAGE_BY_KEY[screen.key];
          // No load call: anyone with a session is in.
          if (screen.loads === null) {
            return <Route key={screen.key} path={screen.path} element={page} />;
          }
          // The requirement comes from the contract, through the generated
          // table — never from this file.
          return (
            <Route
              key={screen.key}
              element={<RequireOperation op={screen.loads} screen={screen.label} />}
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
