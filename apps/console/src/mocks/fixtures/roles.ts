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
 * mode, because three of them are otherwise only reachable against a real
 * identity provider in an awkward condition:
 *
 *  - a role the platform did NOT create (`Administrators`) — the "Not ours"
 *    badge, and the guard that keeps a platform test account out of the group
 *    that administers the platform;
 *  - an account whose username already belongs to somebody else — the "Name
 *    already taken" warning;
 *  - a role that does not exist yet — "New at Build".
 */

import type { components } from "../../generated/aep-api";

type RolesView = components["schemas"]["ProjectRolesView"];

export const projectRolesView: RolesView = {
  directoryAvailable: true,
  roles: [
    {
      name: "Administrators",
      description: "System administrators group",
      // Made by hand, so the platform leaves it alone and enrols nobody.
      platformCreated: false,
      memberCount: 1,
    },
    {
      name: "Compliance Admin",
      description: "Approves and audits submitted claims.",
      platformCreated: true,
      memberCount: 1,
    },
    // `Viewer` is deliberately ABSENT from this catalog while the design
    // declares it — that is what renders "New at Build".
  ],
  testUsers: [
    {
      username: "test-compliance-admin",
      roleName: "Compliance Admin",
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
      // The design named none, so this is the name the build will generate.
      supplied: true,
      coldStart: true,
      exists: false,
      owned: false,
      rotatedAt: null,
      referencingProjects: ["expenses"],
      referencingCount: 1,
    },
    {
      username: "jsmith",
      roleName: "Compliance Admin",
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

/** The degraded read: the identity provider could not be reached. */
export const projectRolesViewOffline: RolesView = {
  directoryAvailable: false,
  roles: [],
  testUsers: (projectRolesView.testUsers ?? []).map((u) => ({
    ...u,
    // `exists` is meaningless in this state, and the panel must not render it
    // as "does not exist".
    exists: false,
  })),
};

/** A project whose design declares no roles at all. */
export const projectRolesViewEmpty: RolesView = {
  directoryAvailable: true,
  roles: [],
  testUsers: [],
};
