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

import type { ProjectRolesLiveState } from "../api/roles";
import type { SecurityDesign } from "../api/securityDesign";
import { groupReachLine, groupRows } from "./groupRows";

function doc(over: Partial<SecurityDesign> = {}): SecurityDesign {
  return {
    version: 3,
    permissions: [
      { resource: "claims", component: "api", actions: [{ handle: "read" }] },
    ],
    groups: [],
    roles: [],
    testUsers: [],
    ...over,
  };
}

function role(
  name: string,
  over: Partial<SecurityDesign["roles"][number]> = {},
): SecurityDesign["roles"][number] {
  return {
    name,
    description: `What ${name} may do`,
    stories: [1],
    grants: ["claims:read"],
    ...over,
  };
}

function live(
  over: Partial<ProjectRolesLiveState> = {},
): ProjectRolesLiveState {
  return {
    directoryAvailable: true,
    roles: [],
    projectRoles: [],
    testUsers: [],
    ...over,
  };
}

describe("groupRows — which groups the page lists", () => {
  it("lists a declared group and an assignTo-only group in one list", () => {
    const rows = groupRows(
      doc({
        groups: [{ name: "Employees", description: "Everyone who claims" }],
        roles: [role("Employee", { assignTo: ["Employees", "Finance"] })],
      }),
      undefined,
    );

    expect(rows.map((r) => r.name)).toEqual(["Employees", "Finance"]);
    expect(rows[0]!.declared).toBe(true);
    expect(rows[0]!.description).toBe("Everyone who claims");
    expect(rows[1]!.declared).toBe(false);
    expect(rows[1]!.description).toBeUndefined();
  });

  it("names each group once however many roles assign to it", () => {
    const rows = groupRows(
      doc({
        roles: [
          role("Employee", { assignTo: ["Finance"] }),
          role("Approver", { assignTo: ["finance"] }),
        ],
      }),
      undefined,
    );

    expect(rows).toHaveLength(1);
    expect(rows[0]!.name).toBe("Finance");
    expect(rows[0]!.roles).toEqual(["Employee", "Approver"]);
  });

  // A service principal holds an application token and a self-service account
  // is enrolled by the app itself. Neither reaches an org group, so neither may
  // put one in a list whose whole subject is the directory.
  it("ignores the assignTo of a service or self-service role", () => {
    const rows = groupRows(
      doc({
        roles: [
          role("Ledger Sync", { kind: "service", assignTo: ["Robots"] }),
          role("Shopper", { enrolment: "self-service", assignTo: ["Shoppers"] }),
        ],
      }),
      undefined,
    );

    expect(rows).toEqual([]);
  });
});

describe("groupRows — what the directory says about each", () => {
  it("is new when the directory does not have it", () => {
    const rows = groupRows(
      doc({ roles: [role("Admin", { assignTo: ["Finance"] })] }),
      live({ roles: [{ name: "Something Else", platformCreated: true }] }),
    );

    expect(rows[0]!.status).toBe("new");
    expect(rows[0]!.projects).toBeNull();
  });

  // The DIRECTORY decides, not the document: a group this design declares that
  // an earlier build already created is what Build will find, so it is existing.
  it("is existing for a declared group the directory already holds", () => {
    const rows = groupRows(
      doc({
        groups: [{ name: "Finance", description: "Approves what we pay for" }],
        roles: [role("Admin", { assignTo: ["Finance"] })],
      }),
      live({ roles: [{ name: "finance", platformCreated: true, memberCount: 4 }] }),
    );

    expect(rows[0]!.status).toBe("existing");
    expect(rows[0]!.declared).toBe(true);
    expect(rows[0]!.platformCreated).toBe(true);
    expect(rows[0]!.memberCount).toBe(4);
  });

  it("marks a hand-made group existing but not the platform's", () => {
    const rows = groupRows(
      doc({ roles: [role("Admin", { assignTo: ["Administrators"] })] }),
      live({ roles: [{ name: "Administrators", platformCreated: false }] }),
    );

    expect(rows[0]!.status).toBe("existing");
    expect(rows[0]!.platformCreated).toBe(false);
  });

  it("answers null — no chip — when the directory could not be read", () => {
    const rows = groupRows(
      doc({ roles: [role("Admin", { assignTo: ["Finance"] })] }),
      live({
        directoryAvailable: false,
        roles: [{ name: "Finance", platformCreated: true }],
      }),
    );

    expect(rows[0]!.status).toBeNull();
  });

  // The per-assignment count is a fact about THIS project's binding; the
  // catalog's is an aggregate that can only be close.
  it("prefers the platform record's count over the catalog's", () => {
    const rows = groupRows(
      doc({ roles: [role("Admin", { assignTo: ["Finance"] })] }),
      live({
        roles: [{ name: "Finance", platformCreated: true, projects: 9 }],
        projectRoles: [
          {
            name: "Admin",
            resourceServer: "https://aep.wso2.com/orgs/a/projects/b",
            assignedTo: [{ group: "finance", projects: 2 }],
          },
        ],
      }),
    );

    expect(rows[0]!.projects).toBe(2);
  });

  // Absent, never zero: the BFF omits a count it could not take, and a
  // confident zero would read as "nobody uses this".
  it("drops a zero or missing count rather than printing it", () => {
    const rows = groupRows(
      doc({ roles: [role("Admin", { assignTo: ["Finance"] })] }),
      live({
        roles: [
          { name: "Finance", platformCreated: true, projects: 0, memberCount: 0 },
        ],
      }),
    );

    expect(rows[0]!.projects).toBeNull();
    expect(rows[0]!.memberCount).toBeNull();
    expect(groupReachLine(rows[0]!)).toBe("");
  });
});

describe("groupReachLine", () => {
  it("joins the two counts, singular where it must be", () => {
    const [both, members, projects] = groupRows(
      doc({
        roles: [role("Admin", { assignTo: ["A", "B", "C"] })],
      }),
      live({
        roles: [
          { name: "A", platformCreated: true, memberCount: 4, projects: 2 },
          { name: "B", platformCreated: true, memberCount: 1 },
          { name: "C", platformCreated: true, projects: 1 },
        ],
      }),
    );

    expect(groupReachLine(both!)).toBe("4 members · holds roles in 2 projects");
    expect(groupReachLine(members!)).toBe("1 member");
    expect(groupReachLine(projects!)).toBe("holds roles in 1 project");
  });
});
