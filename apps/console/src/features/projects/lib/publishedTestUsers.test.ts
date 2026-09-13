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

import { describe, expect, it } from "vitest";
import type { ProjectTestUserState } from "../../spec/api/roles";
import { publishedTestUsers } from "./publishedTestUsers";

function user(
  over: Partial<ProjectTestUserState> &
    Pick<ProjectTestUserState, "username" | "owned"> & { role?: string },
): ProjectTestUserState {
  const { role, ...rest } = over;
  return {
    supplied: false,
    exists: true,
    rotatedAt: null,
    referencingProjects: null,
    referencingCount: 1,
    roles: role === undefined ? [] : [role],
    scopes: [],
    ...rest,
  };
}

describe("publishedTestUsers", () => {
  it("includes owned users with their roles and scopes", () => {
    expect(
      publishedTestUsers([
        user({
          username: "test-approver",
          role: "Approver",
          owned: true,
          exists: true,
          roles: ["Approver", "Employee"],
          scopes: ["claims:approve", "claims:read"],
        }),
      ]),
    ).toEqual([
      {
        username: "test-approver",
        roles: ["Approver", "Employee"],
        scopes: ["claims:approve", "claims:read"],
      },
    ]);
  });

  // The wire's order IS the answer: the platform holds the roles in the
  // project's own order and returns the scope union already deduplicated.
  // Re-deriving or re-sorting here would only let the two disagree.
  it("passes roles and scopes through in the order the platform gave them", () => {
    const [row] = publishedTestUsers([
      user({
        username: "test-approver",
        role: "Approver",
        owned: true,
        roles: ["Approver", "Employee"],
        scopes: ["reports:read", "claims:read"],
      }),
    ]);
    expect(row?.roles).toEqual(["Approver", "Employee"]);
    expect(row?.scopes).toEqual(["reports:read", "claims:read"]);
  });

  // A row the platform could not attach any role to is still a login somebody
  // can sign in as, so it is published with an empty Roles column rather than
  // dropped.
  it("reads an absent roles array as empty", () => {
    expect(
      publishedTestUsers([
        user({
          username: "test-viewer",
          role: "Viewer",
          owned: true,
          roles: null,
        }),
      ]),
    ).toEqual([{ username: "test-viewer", roles: [], scopes: [] }]);
  });

  // Two roles can grant the same handle. The union is what the login's token
  // carries, and it carries it once.
  it("de-duplicates the roles and the scope union", () => {
    const [row] = publishedTestUsers([
      user({
        username: "test-approver",
        role: "Approver",
        owned: true,
        roles: ["Approver", "Employee", "Approver"],
        scopes: ["claims:read", "claims:approve", "claims:read"],
      }),
    ]);
    expect(row?.roles).toEqual(["Approver", "Employee"]);
    expect(row?.scopes).toEqual(["claims:read", "claims:approve"]);
  });

  it("reads an absent scopes array as empty", () => {
    const [row] = publishedTestUsers([
      user({
        username: "test-viewer",
        role: "Viewer",
        owned: true,
        scopes: null,
      }),
    ]);
    expect(row?.scopes).toEqual([]);
  });

  it("includes owned: true even when exists is false", () => {
    expect(
      publishedTestUsers([
        user({
          username: "test-viewer",
          role: "Viewer",
          owned: true,
          exists: false,
        }),
      ]),
    ).toEqual([{ username: "test-viewer", roles: ["Viewer"], scopes: [] }]);
  });

  it("omits owned: false (taken username / not ours)", () => {
    expect(
      publishedTestUsers([
        user({
          username: "jsmith",
          role: "Compliance Admin",
          owned: false,
          exists: true,
        }),
      ]),
    ).toEqual([]);
  });

  it("omits owned: false, exists: false (gate has not published)", () => {
    expect(
      publishedTestUsers([
        user({
          username: "test-viewer",
          role: "Viewer",
          owned: false,
          exists: false,
        }),
      ]),
    ).toEqual([]);
  });

  it("keeps only owned rows and preserves order", () => {
    expect(
      publishedTestUsers([
        user({ username: "first-owned", role: "Viewer", owned: true }),
        user({ username: "not-ours", role: "Admin", owned: false }),
        user({
          username: "second-owned",
          role: "Compliance Admin",
          owned: true,
          scopes: ["audit:read"],
        }),
      ]),
    ).toEqual([
      { username: "first-owned", roles: ["Viewer"], scopes: [] },
      {
        username: "second-owned",
        roles: ["Compliance Admin"],
        scopes: ["audit:read"],
      },
    ]);
  });

  it("returns [] for empty input", () => {
    expect(publishedTestUsers([])).toEqual([]);
  });

  it("return value has no password field", () => {
    const [row] = publishedTestUsers([
      user({ username: "test-viewer", role: "Viewer", owned: true }),
    ]);
    expect(row).toBeDefined();
    expect(Object.keys(row!)).not.toContain("password");
    expect(row).not.toHaveProperty("password");
  });
});
