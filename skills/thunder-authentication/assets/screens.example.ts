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

// PATTERN, not a verbatim copy. Copy this to <app-path>/src/authz/screens.ts
// and replace SCREEN_ROUTES with YOUR screens — the table below is the Expense
// Tracker fixture's, not a default. Everything else stays as it is.
//
// THIS IS THE ONLY FILE THAT KNOWS ABOUT SCREENS, and all it says about each
// one is which API operation it LOADS. The gate follows: a screen is reachable
// when the caller may call that operation, and what the operation needs is in
// the contract, projected into ./operations.gen.ts. Nothing here names a scope,
// a role or a handle, and security.json carries no screen table at all.
//
// WHY `loads` AND NOT A HANDLE. `scopes.has("claims:read")` written by hand into
// App.tsx is a stale handle the moment the design moves, and nothing — not tsc,
// not the build gate, not the mock walk — can see that it went stale. An
// OperationKey is a generated literal union, so a contract change that renames
// or drops the operation fails `npm run gen && tsc` on the next build.
//
// THE ORDER OF THIS TABLE IS THE RAIL'S ORDER, and its first reachable row is
// the screen the app lands on. Write the screens in the order the prototype's
// navigation lists them.

import { canCall } from "./core";
import { OPERATIONS, isOperationKey, type OperationKey } from "./operations.gen";

export interface ScreenRoute {
  /** A stable id the App maps to a page component. */
  readonly key: string;
  /** The prototype screen's name, for the rail and the Forbidden copy. */
  readonly label: string;
  readonly path: string;
  /**
   * The operation this screen exists to perform: the call it renders on load
   * for a screen that reads, or the call its submit makes for a form that only
   * writes. `null` is for a screen that needs NO operation at all — static
   * content behind sign-in — and is rare.
   */
  readonly loads: OperationKey | null;
  /**
   * A screen the PRD gives to a signed-out visitor: reachable before sign-in,
   * routed ABOVE the sign-in guard. Its load operation, if it has one, is `security: []` in the
   * contract.
   */
  readonly public?: boolean;
}

/**
 * YOUR screens, in RAIL ORDER. One row per prototype screen.
 *
 * `loads` is the operation the screen exists to perform, AT THE REACH THE
 * SCREEN SHOWS: an every-row queue loads `GET /claims`, a "mine" page loads
 * `GET /me/claims`.
 *
 * A FORM THAT ONLY WRITES NAMES THE OPERATION ITS SUBMIT MAKES. It has no load
 * call, but it still has exactly one operation that makes opening it worth
 * anything, and naming it here is what keeps the three answers in step: the
 * rail shows the form, the route guards it, and `<Can>` enables the button,
 * all from one fact. Leaving it `null` splits them — the caller reaches a form
 * whose only control they can never use — and three separate mock walks have
 * now called that a defect and patched it, in two different places.
 *
 * `null` is only for a screen that needs no operation at all, which is rare. A
 * screen the PRD gives to a signed-out visitor is `public: true`.
 */
export const SCREEN_ROUTES: readonly ScreenRoute[] = [
  { key: "myclaims", label: "My Claims", path: "/claims", loads: "GET /me/claims" },
  { key: "submitclaim", label: "Submit Claim", path: "/submit", loads: "POST /me/claims" },
  { key: "approvals", label: "Approvals", path: "/approvals", loads: "GET /claims" },
  { key: "reports", label: "Reports", path: "/reports", loads: "GET /reports" },
  // The public row. Here because `public` is the one field of this table whose
  // shape an author has nothing to copy without it — and because it is the row
  // that makes `hasScopedReach` a different question from "is the rail empty":
  // it is reachable by everyone, so it keeps `reachableScreens` non-empty for a
  // caller who has earned nothing, which is precisely the case NoAccess exists
  // to name.
  { key: "policy", label: "Expense Policy", path: "/policy", loads: null, public: true },
];

// FAIL LOUDLY, at module load — the first render, every time, in dev, in the
// mock walk and in the deployed pod. `loads` is typed as an OperationKey, so a
// name the contract does not declare is already a type error; this catches the
// case tsc cannot, a COMMITTED operations.gen.ts that went stale against a
// contract nobody regenerated from. Do not soften it to a console.warn: the
// alternative is a screen that silently reads as "no such operation" and gates
// on nothing.
for (const screen of SCREEN_ROUTES) {
  if (screen.loads !== null && !isOperationKey(screen.loads)) {
    throw new Error(
      `src/authz/screens.ts: screen "${screen.label}" loads "${screen.loads}", which ` +
        `no contract declares. Re-run \`npm run gen\`, or name the operation the ` +
        `way openapi.yaml spells it.`,
    );
  }
}

/**
 * The screens a caller can actually open, in rail order. The first one is the
 * landing screen; an EMPTY list is the NoAccess case.
 *
 * One rule, and it is the gate the API itself applies: may this caller call the
 * operation the screen loads?
 */
export function reachableScreens(
  scopes: ReadonlySet<string>,
  signedIn: boolean,
): readonly ScreenRoute[] {
  return SCREEN_ROUTES.filter((screen) => {
    if (screen.public) return true;
    if (screen.loads === null) return signedIn;
    return canCall(OPERATIONS[screen.loads], scopes, signedIn);
  });
}

/**
 * Does this caller reach anything their scopes actually earned them?
 *
 * This is the NoAccess question, and it is NOT "is `reachableScreens` empty".
 * A `public` screen is reachable by everyone, and a `loads: null` screen by any
 * signed-in caller, so an app holding either makes `reachableScreens`
 * non-empty for a caller with NO scopes at all — and the NoAccess screen, whose
 * whole job is to explain that situation, can never render.
 *
 * Naming a form's submit operation (see `SCREEN_ROUTES`) removed the common
 * case of this: a form used to be `loads: null` and so counted as reach for a
 * caller who could not submit it. What is left are genuinely operation-free
 * screens and public ones, which is why this question still has to be asked
 * separately from "is the rail empty".
 */
export function hasScopedReach(scopes: ReadonlySet<string>, signedIn: boolean): boolean {
  return reachableScreens(scopes, signedIn).some((screen) => !screen.public && screen.loads !== null);
}
