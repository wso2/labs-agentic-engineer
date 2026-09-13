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
 * Fixtures for the Security panel's live half.
 *
 * They are chosen to make every state the panel can render reachable in mock
 * mode, because most of them are otherwise only reachable against a real
 * identity provider in an awkward condition. They are reconciled against the
 * v2 security document in `fixtures/project.ts`, so the design half and the
 * live half have something to agree and disagree about:
 *
 *  - `roles` is the shared ORG GROUP catalog, which is what a design's
 *    `assignTo` names. `Compliance` is deliberately ABSENT — that is what
 *    renders "New at Build". `Administrators` is present and NOT the
 *    platform's, which renders "Not ours" and is the guard that keeps a
 *    platform test account out of the group that administers the platform.
 *    `Finance` is the platform's and holds roles in TWO projects, which is the
 *    cross-project count a role card shows.
 *  - `projectRoles` is what THIS project owns (phase 2). A self-service role
 *    and a service role both carry an EMPTY `assignedTo`, which is the normal
 *    shape for them and not missing data.
 *  - `testUsers` covers the three account states: the platform's own account,
 *    an account the design never named (`supplied`), and a username that
 *    already belongs to somebody else — plus one login holding TWO roles,
 *    which is what v2 changed.
 */

import type { components } from "../../generated/aep-api";

type RolesView = components["schemas"]["ProjectRolesView"];

/** Every grant of this project's roles is on this resource server. */
const RESOURCE_SERVER = "https://aep.wso2.com/orgs/acme/projects/demo-shop";

export const projectRolesView: RolesView = {
  directoryAvailable: true,
  roles: [
    {
      name: "Administrators",
      description: "System administrators",
      // Made by hand, so the platform leaves it alone and enrols nobody.
      platformCreated: false,
      memberCount: 3,
      projects: 1,
    },
    {
      name: "Finance",
      description: "Finance and audit staff",
      platformCreated: true,
      memberCount: 4,
      // Two DISTINCT projects bind a role to this group: the reuse a designer
      // has to see before deciding to bind a third.
      projects: 2,
    },
    // `Compliance` is deliberately ABSENT from this catalog while the design
    // declares it — that is what renders "New at Build".
  ],
  projectRoles: [
    {
      name: "Shopper",
      directoryName: "demo-shop/Shopper",
      resourceServer: RESOURCE_SERVER,
      scopes: ["catalog:read", "orders:place", "orders:read"],
      // Self-service: the storefront's registration flow assigns it per
      // account, so no group holds it.
      assignedTo: [],
    },
    {
      name: "Compliance Admin",
      directoryName: "demo-shop/Compliance Admin",
      resourceServer: RESOURCE_SERVER,
      scopes: [
        "orders:approve",
        "orders:read",
        "orders:read-all",
        "orders:refund",
      ],
      assignedTo: [
        { group: "Compliance", projects: 1 },
        { group: "Administrators", projects: 1 },
      ],
    },
    {
      name: "Viewer",
      directoryName: "demo-shop/Viewer",
      resourceServer: RESOURCE_SERVER,
      scopes: ["catalog:read", "orders:read", "orders:read-all"],
      assignedTo: [{ group: "Finance", projects: 2 }],
    },
    {
      name: "ledger-sync",
      directoryName: "demo-shop/ledger-sync",
      resourceServer: RESOURCE_SERVER,
      scopes: ["orders:read-all"],
      // A service role's principal is an application, not a person.
      assignedTo: [],
    },
  ],
  testUsers: [
    {
      username: "test-compliance-admin",
      roleName: "Compliance Admin",
      roles: ["Compliance Admin"],
      scopes: [
        "orders:approve",
        "orders:read",
        "orders:read-all",
        "orders:refund",
      ],
      supplied: false,
      coldStart: false,
      exists: true,
      owned: true,
      rotatedAt: "2026-08-20T09:14:00Z",
      referencingProjects: ["expenses"],
      referencingCount: 2,
    },
    {
      username: "test-viewer",
      roleName: "Viewer",
      roles: ["Viewer"],
      scopes: ["catalog:read", "orders:read", "orders:read-all"],
      // The design named none, so this is the name the build will generate.
      supplied: true,
      coldStart: false,
      exists: false,
      owned: false,
      rotatedAt: null,
      referencingProjects: ["expenses"],
      referencingCount: 1,
    },
    {
      username: "jsmith",
      // v2 lets one login hold several roles; `roleName` is roles[0] and is on
      // the wire only until the console has moved off it.
      roleName: "Compliance Admin",
      roles: ["Compliance Admin", "Shopper"],
      scopes: [
        "catalog:read",
        "orders:approve",
        "orders:place",
        "orders:read",
        "orders:read-all",
        "orders:refund",
      ],
      supplied: false,
      coldStart: false,
      // Present on the directory, but NOT the platform's: refused, never
      // adopted. The panel warns and offers no action.
      exists: true,
      owned: false,
      rotatedAt: null,
      referencingProjects: ["expenses"],
      referencingCount: 1,
    },
  ],
};

/**
 * The degraded read: the identity provider could not be reached.
 *
 * `projectRoles` survives, because it is derived from the platform's own
 * records — everything but the `scopes`, which are read from the directory and
 * are therefore "unknown" rather than "grants nothing".
 */
export const projectRolesViewOffline: RolesView = {
  directoryAvailable: false,
  roles: [],
  projectRoles: (projectRolesView.projectRoles ?? []).map((r) => ({
    ...r,
    scopes: [],
  })),
  testUsers: (projectRolesView.testUsers ?? []).map((u) => ({
    ...u,
    // `exists` is meaningless in this state, and the panel must not render it
    // as "does not exist".
    exists: false,
    scopes: [],
  })),
};

/** A project whose design declares no roles at all. */
export const projectRolesViewEmpty: RolesView = {
  directoryAvailable: true,
  roles: [],
  projectRoles: [],
  testUsers: [],
};
