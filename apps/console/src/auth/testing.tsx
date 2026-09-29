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

/**
 * Test-only: render a component under a real session, holding exactly the
 * permissions the test names.
 *
 * Imported only by *.test.tsx. It exists because the alternative — the
 * `vi.mock("../../auth/permissions")` block this replaced — stubbed out the
 * very code path the gate under test depends on, and did so in a way that
 * could not fail. Nearly every one of those stubs was written as
 * `useHasPermission: () => canDoThing.current`, ignoring its argument, so a
 * component asking for the WRONG permission key still passed its test: the
 * fake said yes to `ae:design` as readily as to the `ae:build-view` the
 * surface was supposed to want. Two files had also drifted to stubbing a
 * different layer (useSession) than their neighbours, so a change to either
 * layer broke an arbitrary subset of the suite.
 *
 * Feeding the real SessionContext instead means the key is load-bearing, the
 * real useSession/usePermissions/useHasPermission chain runs, and a test that
 * withholds a permission is withholding the same thing production would.
 */

import type { ReactElement, ReactNode } from "react";
import { render, type RenderOptions, type RenderResult } from "@testing-library/react";
import { ALL_PERMISSIONS, type Permission } from "./permissions";
import { SessionContext, type Session } from "./SessionContext";

// Re-exported so a test needs one import for the whole seam, rather than
// reaching past it into the generated file.
export { ALL_PERMISSIONS };

/**
 * Every permission except the named ones.
 *
 * The right way to write a denial case, and the reason is the bug the old
 * mocks could not catch: granting nothing proves only that the surface is
 * gated on SOMETHING, while withholding one key proves it is gated on THAT
 * key — a component that had drifted to checking a neighbouring permission
 * still renders here, and the test fails.
 */
export function allPermissionsExcept(
  ...withheld: readonly Permission[]
): Permission[] {
  return ALL_PERMISSIONS.filter((permission) => !withheld.includes(permission));
}

/** The signed-in identity tests get unless one of them says otherwise. */
export const TEST_USER = { name: "Test User", email: "test@example.com" } as const;

/** The org tests get unless one of them says otherwise. */
export const TEST_ORG = "acme";

/**
 * A session holding `permissions` — every permission when the argument is
 * omitted, so a test that is not about gating does not have to enumerate the
 * vocabulary to get on with what it is about.
 *
 * `permissions` is `Iterable<string>` rather than `Permission[]` on purpose:
 * a test may legitimately want to prove what happens with a key outside the
 * vocabulary, which is exactly what permissionsFromScope now lets through.
 */
export function testSession(
  permissions: Iterable<string> = ALL_PERMISSIONS,
  overrides: Partial<Session> = {},
): Session {
  return {
    user: TEST_USER,
    orgHandle: TEST_ORG,
    permissions: new Set(permissions),
    signOut: () => {},
    ...overrides,
  };
}

/**
 * Wrap `ui` in that session without rendering it — for tests that already
 * have their own render wrapper (a QueryClientProvider, a theme) to nest
 * inside, and for the element passed back to RTL's `rerender`.
 */
export function withPermissions(
  ui: ReactNode,
  permissions?: Iterable<string>,
  overrides?: Partial<Session>,
): ReactElement {
  return (
    <SessionContext.Provider value={testSession(permissions, overrides)}>
      {ui}
    </SessionContext.Provider>
  );
}

/**
 * `render`, under a session holding `permissions`.
 *
 * The returned `rerender` re-wraps, which RTL's own does not: re-rendering a
 * bare element into the same container would drop the provider and leave
 * useSession throwing halfway through a test.
 *
 * A file with many call sites can shadow RTL's render with this —
 * `const render = (ui: ReactElement) => renderWithPermissions(ui, held)` —
 * and leave its cases untouched.
 */
export function renderWithPermissions(
  ui: ReactNode,
  permissions?: Iterable<string>,
  options?: RenderOptions,
): RenderResult {
  const result = render(withPermissions(ui, permissions), options);
  return {
    ...result,
    rerender: (next: ReactNode) =>
      result.rerender(withPermissions(next, permissions)),
  };
}
