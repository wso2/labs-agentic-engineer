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

import {
  grantsOf,
  parseSecurityDesign,
  plannedUsersFor,
  roleKind,
  securityMatrix,
  type SecurityDesign,
} from "../../features/spec/api/securityDesign";
import { projectSpecFiles } from "./project";
import { projectRolesView, projectRolesViewOffline } from "./roles";

/**
 * The Security page reads TWO halves that are authored in two files, and a mock
 * is only worth having while they cannot contradict each other: the design half
 * is `security.json` in `fixtures/project.ts`, the live half is
 * `fixtures/roles.ts`, and the whole point of the page is showing where the two
 * agree and where they do not. A fixture whose `assignTo` named a group the
 * live catalog spelled differently would render every badge "New at Build" and
 * quietly hide the states these fixtures exist to make reachable.
 */

function fixtureDesign(): SecurityDesign {
  const file = projectSpecFiles.deployed.find(
    (f) => f.path === "specs/design/security.json",
  );
  if (!file) throw new Error("the deployed scenario serves no security.json");
  const parsed = parseSecurityDesign(file.content);
  if (parsed.kind !== "ok") {
    throw new Error(`the fixture security.json is not valid v2: ${JSON.stringify(parsed)}`);
  }
  return parsed.doc;
}

const design = fixtureDesign();
const liveGroups = projectRolesView.roles ?? [];
const projectRoles = projectRolesView.projectRoles ?? [];
const liveUsers = projectRolesView.testUsers ?? [];

describe("the mock security design", () => {
  it("is a version-2 document the console's own parser accepts", () => {
    expect(design.version).toBe(2);
  });

  it("names only screens the fixture's wireframes declare", () => {
    const wireframes = projectSpecFiles.deployed.find(
      (f) => f.path === "specs/design/components/storefront/wireframes.dsl",
    );
    expect(wireframes).toBeDefined();
    for (const screen of design.screens) {
      expect(wireframes?.content).toContain(`screen ${screen.screen} `);
    }
  });

  // One of each kind, so the panel's two baseline rows are both non-empty.
  it("carries a public screen and a signed-in screen", () => {
    const { signedInScreens, publicScreens } = securityMatrix(design).baseline;
    expect(publicScreens).toHaveLength(1);
    expect(signedInScreens).toHaveLength(1);
  });

  it("leaves one action granted by nobody, for the matrix's empty row", () => {
    const ungranted = securityMatrix(design)
      .groups.flatMap((g) => g.rows)
      .filter((r) => r.grantedBy.length === 0)
      .map((r) => r.handle);
    expect(ungranted).toEqual(["catalog:export"]);
  });

  it("declares a self-service role and a service role, so both renderings show", () => {
    const matrix = securityMatrix(design);
    expect(matrix.columns.map((c) => c.enrolment)).toContain("self-service");
    expect(matrix.serviceColumns.map((c) => c.name)).toEqual(["ledger-sync"]);
  });
});

describe("the mock design and the mock directory agree", () => {
  it("reaches all three group badges", () => {
    const assigned = design.roles.flatMap((r) => r.assignTo ?? []);
    const byName = new Map(liveGroups.map((g) => [g.name, g]));

    // New at Build — declared, absent from the live catalog.
    expect(assigned).toContain("Compliance");
    expect(byName.has("Compliance")).toBe(false);
    // Not ours — present, made by hand.
    expect(byName.get("Administrators")?.platformCreated).toBe(false);
    // Reused — present, the platform's, and bound in more than one project.
    expect(byName.get("Finance")?.platformCreated).toBe(true);
    expect(byName.get("Finance")?.projects ?? 0).toBeGreaterThan(1);
  });

  it("carries the cross-project count on the assignment too", () => {
    const assignments = projectRoles.flatMap((r) => r.assignedTo ?? []);
    expect(assignments.find((a) => a.group === "Finance")?.projects).toBe(2);
  });

  it("gives every declared role a project role with the same grants", () => {
    expect(projectRoles.map((r) => r.name).sort()).toEqual(
      design.roles.map((r) => r.name).sort(),
    );
    for (const role of projectRoles) {
      expect(role.scopes ?? []).toEqual([...grantsOf(design, role.name)].sort());
    }
  });

  // Empty is the NORMAL shape for these two, not missing data.
  it("binds no group to the self-service role or the service role", () => {
    for (const role of design.roles) {
      if (role.enrolment !== "self-service" && roleKind(role) !== "service") continue;
      const live = projectRoles.find((r) => r.name === role.name);
      expect(live?.assignedTo).toEqual([]);
    }
  });

  it("references only roles the design declares, from every test user", () => {
    const declared = new Set(design.roles.map((r) => r.name));
    for (const user of liveUsers) {
      expect(user.roles ?? []).not.toHaveLength(0);
      for (const name of user.roles ?? []) expect(declared.has(name)).toBe(true);
      // `roleName` is roles[0] by construction on the wire.
      expect(user.roleName).toBe((user.roles ?? [])[0]);
    }
  });

  it("gives one login several roles, which is what v2 changed", () => {
    expect(liveUsers.some((u) => (u.roles ?? []).length > 1)).toBe(true);
  });

  it("agrees with the panel about which account name the build will generate", () => {
    for (const user of liveUsers) {
      if (!user.supplied) continue;
      expect(plannedUsersFor(design, user.roleName)).toEqual([
        { username: user.username, role: user.roleName, supplied: true },
      ]);
    }
    // …and the supplied state is actually reachable in mock mode.
    expect(liveUsers.some((u) => u.supplied)).toBe(true);
  });

  it("carries each login's scopes as the sorted union of its roles' grants", () => {
    for (const user of liveUsers) {
      const union = new Set((user.roles ?? []).flatMap((r) => grantsOf(design, r)));
      expect(user.scopes ?? []).toEqual([...union].sort());
    }
  });

  // `coldStart` is on the wire only until it is removed; it is always false.
  it("has no cold-start account left", () => {
    expect(liveUsers.every((u) => u.coldStart === false)).toBe(true);
  });
});

describe("the offline directory read", () => {
  it("keeps the project's own roles and drops only what the directory answers", () => {
    expect(projectRolesViewOffline.directoryAvailable).toBe(false);
    expect(projectRolesViewOffline.roles).toEqual([]);
    expect(projectRolesViewOffline.projectRoles).toHaveLength(projectRoles.length);
    // Scopes are read FROM the directory, so offline they are "unknown".
    for (const role of projectRolesViewOffline.projectRoles ?? []) {
      expect(role.scopes).toEqual([]);
      expect(role.resourceServer).not.toBe("");
    }
  });

  it("never claims an account does not exist when it could not be asked", () => {
    expect(
      (projectRolesViewOffline.testUsers ?? []).every((u) => u.exists === false),
    ).toBe(true);
  });
});
