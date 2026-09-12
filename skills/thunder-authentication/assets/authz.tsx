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

// Copied VERBATIM to <app-path>/src/authz.tsx.
//
// The app's ONE authorization surface. It reads the caller's granted scopes
// from the access token's `scope` string, which src/auth.ts already surfaces,
// and answers every "may this user…" question the UI asks. It reads NO groups
// claim, holds no role table of its own, and decodes no JWT. Roles exist here
// only as a label for the header badge and for the copy that tells a user what
// to ask for, and they are DERIVED from the scopes held against ROLE_GRANTS.
//
// Deliberately identical between production and mock mode: mock mode
// substitutes src/auth.ts, and this module only ever talks to src/auth.ts,
// src/authz-core.ts and src/scopes.gen.ts. Do not add an `import.meta.env`
// branch here.
//
// Filename: the design calls this module `src/authz.ts`. It exports React
// components, so on disk it is `.tsx`; every importer still writes
// `from "./authz"` and the module identity is unchanged.
//
// Surface (do not rename — the platform's skills, the wireframe mapping and
// the mock harness all target these names): granted, can, useScopes, Can,
// RequireScope, Forbidden, NoAccess, heldRoles.
//
// Style it with the pinned design system; the markup below is structure, not a
// design. Keep the words.

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";
import { currentUser } from "./auth";
import {
  canReach,
  granted as holdsScope,
  heldRoles as rolesFor,
  parseScopes,
  rolesGranting as rolesGrantingHandle,
  type ScreenRequirement,
} from "./authz-core";
import {
  ROLES,
  ROLE_ASSIGNABLE_BY,
  ROLE_ASSIGN_TO,
  ROLE_GRANTS,
  type Role,
  type Scope,
} from "./scopes.gen";

// --- the session, resolved once ---------------------------------------------

export interface AuthzState {
  /** Every scope the access token carries, the IdP's own included. */
  readonly scopes: ReadonlySet<string>;
  /** Display name for the header and the NoAccess copy. */
  readonly username: string;
  /** false when there is no session at all — the load-time guard signs in. */
  readonly signedIn: boolean;
}

const AuthzContext = createContext<AuthzState | null>(null);

// Published here as well so `heldRoles()` can keep the design's signature: a
// plain synchronous call, not a hook, callable from a header component or a
// plain function. Written before any child renders and replaced only by a new
// session.
let snapshot: ReadonlySet<string> = new Set<string>();

/**
 * Resolves the session's scope set ONCE and publishes it, so `Can`,
 * `RequireScope` and every nav item read it synchronously. Awaiting the session
 * in each component instead would make every route and every nav item its own
 * suspense problem, and two of them could disagree mid-renew.
 *
 * `fallback` renders while the session resolves — nothing below can observe the
 * pre-load state.
 */
export function AuthzProvider({
  children,
  fallback,
}: {
  children: ReactNode;
  fallback: ReactNode;
}): ReactElement {
  const [state, setState] = useState<AuthzState | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      const user = await currentUser();
      const scopes = parseScopes(user?.scope);
      snapshot = scopes;
      if (!live) return;
      setState({ scopes, username: displayName(user), signedIn: user !== null });
    })();
    return () => {
      live = false;
    };
  }, []);

  if (!state) return <>{fallback}</>;
  return <AuthzContext.Provider value={state}>{children}</AuthzContext.Provider>;
}

/**
 * There is NO `username` claim in the ID token — measured. `sub` is a UUID, so
 * "signed in as 5f3c…" is what a naive read renders. Fall back by usefulness.
 */
function displayName(user: { profile?: Record<string, unknown> } | null): string {
  const profile = user?.profile ?? {};
  for (const claim of ["name", "email", "sub"]) {
    const value = profile[claim];
    if (typeof value === "string" && value.length > 0) return value;
  }
  return "";
}

/** The session's authorization state. Throws outside <AuthzProvider>. */
export function useAuthz(): AuthzState {
  const state = useContext(AuthzContext);
  if (!state) throw new Error("useAuthz called outside <AuthzProvider>");
  return state;
}

/** The scopes the caller holds. The one hook the app's own screens need. */
export function useScopes(): ReadonlySet<string> {
  return useAuthz().scopes;
}

// --- the design's async surface ---------------------------------------------
// For callers OUTSIDE React — a route loader, an interceptor, a guard in a
// plain function. Both read the same session, so they cannot disagree with the
// context.

/** Every scope the signed-in caller holds. Empty when signed out. */
export async function granted(): Promise<Set<string>> {
  const user = await currentUser();
  return parseScopes(user?.scope);
}

/** Does the caller hold this handle? Whole-string, never a prefix match. */
export async function can(handle: Scope): Promise<boolean> {
  return holdsScope(await granted(), handle);
}

/**
 * The roles the caller holds — the header badge. Derived from scopes against
 * ROLE_GRANTS; NEVER from a groups claim.
 */
export function heldRoles(): Role[] {
  return rolesFor(snapshot, ROLE_GRANTS);
}

/** The hook form of the same thing, for a component that renders the badge. */
export function useHeldRoles(): Role[] {
  const scopes = useScopes();
  return useMemo(() => rolesFor(scopes, ROLE_GRANTS), [scopes]);
}

/** The roles that grant a handle — what Forbidden names to the user. */
export function rolesGranting(handle: Scope): Role[] {
  return rolesGrantingHandle(handle, ROLE_GRANTS);
}

/** Can the caller open a screen declared with this `requires`? */
export function canReachScreen(requires: ScreenRequirement, scopes: ReadonlySet<string>): boolean {
  return canReach(requires, scopes);
}

// --- the gates --------------------------------------------------------------

/**
 * Renders `children` only when the caller holds `handle`. Wrap every nav item
 * and every action button in one of these: the rail is ONE rail whose items are
 * each gated, which is also what makes a user holding two roles see the union.
 */
export function Can({
  handle,
  children,
  fallback = null,
}: {
  handle: Scope;
  children: ReactNode;
  fallback?: ReactNode;
}): ReactElement {
  const scopes = useScopes();
  return <>{holdsScope(scopes, handle) ? children : fallback}</>;
}

/**
 * Route guard for a screen whose security.json `requires` names a handle:
 *
 *   <Route element={<RequireScope handle="claims:approve" screen="Approvals" />}>
 *     <Route path="/approvals" element={<Approvals />} />
 *   </Route>
 *
 * A caller who reaches the URL without the handle gets Forbidden INSIDE the
 * shell — never a redirect to sign-in, which loops, and never a blank page.
 */
export function RequireScope({
  handle,
  screen,
}: {
  handle: Scope;
  screen?: string;
}): ReactElement {
  const scopes = useScopes();
  if (holdsScope(scopes, handle)) return <Outlet />;
  return <Navigate to="/forbidden" replace state={{ handle, screen }} />;
}

type ForbiddenState = { handle?: Scope; screen?: string };

/**
 * "You hold other things, just not this one." Rendered INSIDE the app shell, so
 * the navigation the caller CAN use is still there. Route it at /forbidden.
 *
 * /forbidden and NoAccess are platform-prescribed views. They appear in no
 * wireframe .dsl and they are the carve-out from "no invented screens".
 */
export function Forbidden(): ReactElement {
  const { handle, screen } = (useLocation().state ?? {}) as ForbiddenState;
  const unlockedBy = handle ? rolesGranting(handle) : [];
  const what = screen ?? "That screen";

  return (
    <section>
      <h1>Not available to you</h1>
      <p>
        {unlockedBy.length > 0
          ? `${what} needs the ${listOf(unlockedBy)} role${unlockedBy.length > 1 ? "s" : ""}.`
          : `${what} needs a permission your account does not have.`}
      </p>
      {handle ? (
        <p>
          It is gated on <code>{handle}</code>, which your session does not carry.
        </p>
      ) : null}
      <p>Everything you can reach is still in the navigation.</p>
    </section>
  );
}

/**
 * "This app has nothing for you at all." REPLACES the shell — a rail with no
 * items and an empty page tells the user less than this does. Render it only
 * when NO screen is reachable, and render it ABOVE the shell route, never
 * inside it (see App.example.tsx).
 *
 * Every name in this copy comes from security.json through scopes.gen.ts: the
 * roles that exist, the groups they are assigned to, and who may hand them out.
 * NEVER hardcode a role, a group or an administrator here.
 */
export function NoAccess({
  appName,
  username,
}: {
  appName?: string;
  username?: string;
}): ReactElement {
  // Hooks first, unconditionally: `username ?? useContext(…)` would skip the
  // hook whenever the prop is set, which React forbids.
  const session = useContext(AuthzContext);
  const who = username ?? session?.username ?? "";
  const groups = unique(ROLES.flatMap((role) => [...ROLE_ASSIGN_TO[role]]));
  const askers = unique(ROLES.flatMap((role) => [...ROLE_ASSIGNABLE_BY[role]]));
  const app = appName ?? "This app";

  return (
    <main>
      <h1>No access yet</h1>
      <p>
        {who
          ? `You're signed in as ${who}, but ${app} has nothing for you yet.`
          : `You're signed in, but ${app} has nothing for you yet.`}
      </p>
      <p>
        Ask {askers.length > 0 ? listOf(askers) : "an administrator"} to add you
        {groups.length > 0 ? ` to ${listOf(groups)}` : ""}
        {ROLES.length > 0 ? `, or to give you the ${listOf([...ROLES])} role` : ""}.
      </p>
      <p>
        Your token carries no permission this project declares, so every screen
        and every API call would answer the same way.
      </p>
    </main>
  );
}

function unique(values: readonly string[]): string[] {
  return [...new Set(values)];
}

function listOf(values: readonly string[]): string {
  if (values.length <= 1) return values[0] ?? "";
  return `${values.slice(0, -1).join(", ")} or ${values[values.length - 1]}`;
}
