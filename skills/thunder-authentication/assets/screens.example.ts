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

// PATTERN, not a verbatim copy. Copy this to <app-path>/src/screens.ts and
// replace COMPONENT and ROUTE_BY_KEY with YOUR component name and YOUR routes —
// both below are the Expense Tracker fixture's, not defaults.
// Everything else stays as it is.
//
// THE RULE THIS FILE EXISTS FOR: a screen's required handle is READ FROM THE
// GENERATED TABLE, never retyped in JSX. `scopes.has("claims:read") ||
// scopes.has("claims:submit")` written by hand into App.tsx is a stale handle
// the moment the design moves, and nothing — not tsc, not the build gate, not
// the mock walk — can see that it went stale. Bind the route to the row and a
// design change reaches the app on the next `npm run gen` and nowhere else.
//
// security.json spells a screen the way a person reads it ("My Claims"); the
// wireframe DSL cannot carry a space and spells the same screen "MyClaims".
// Normalizing both to lowercase alphanumerics is what binds one to the other.

import { canReach } from "./authz-core";
import { SCREENS, type ScreenGate } from "./scopes.gen";

/** YOUR component's name, exactly as design.json and security.json spell it. */
export const COMPONENT = "expense-webapp";

/** "My Claims" and "MyClaims" both normalize to "myclaims". */
export function screenKey(name: string): string {
  return name.replace(/[^a-zA-Z0-9]/g, "").toLowerCase();
}

/** YOUR routes, keyed by the normalized screen name. One row per wireframe screen. */
const ROUTE_BY_KEY: Record<string, string> = {
  myclaims: "/claims",
  submitclaim: "/submit",
  approvals: "/approvals",
  reports: "/reports",
};

export interface ScreenRoute {
  /** The normalized name — the key App.tsx maps to a page component. */
  readonly key: string;
  /** The label security.json gives it, for the nav rail and Forbidden copy. */
  readonly label: string;
  readonly path: string;
  /** null = any signed-in caller; "public" = before sign-in; else a handle. */
  readonly requires: ScreenGate["requires"];
}

/**
 * The screen table for THIS component, in declared order.
 *
 * FAIL LOUDLY. A screen security.json declares for this component and this app
 * has no page for is a design/implementation mismatch, and it is caught here at
 * module load — the first render, every time, in dev, in the mock walk and in
 * the deployed pod — rather than becoming a screen nobody can reach and nobody
 * notices. Do not soften this to a console.warn or a filter.
 */
export const SCREEN_ROUTES: readonly ScreenRoute[] = SCREENS.filter(
  (row) => row.component === COMPONENT,
).map((row) => {
  const key = screenKey(row.screen);
  const path = ROUTE_BY_KEY[key];
  if (!path) {
    throw new Error(
      `security.json declares screen "${row.screen}" for ${COMPONENT}, ` +
        `which this app has no route for. Add it to ROUTE_BY_KEY in src/screens.ts ` +
        `(or remove the screen from specs/design/security.json).`,
    );
  }
  return { key, label: row.screen, path, requires: row.requires };
});

/** The screens a caller holding `scopes` can actually open, in declared order. */
export function reachableScreens(scopes: ReadonlySet<string>): readonly ScreenRoute[] {
  return SCREEN_ROUTES.filter((screen) => canReach(screen.requires, scopes));
}
