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
 * The Security panel's live half in mock mode.
 *
 * Scenarios, not just the happy path. The panel's most interesting states are
 * the awkward ones — a role the platform does not own, a username that already
 * belongs to somebody, a directory that is down — and none of them is reachable
 * on demand against a real identity provider. They are switchable here by
 * project name so the whole panel is developable without a backend:
 *
 *  - any project              → the full catalog, three test-user states
 *  - `roles-offline`          → `directoryAvailable: false`
 *  - `roles-empty`            → a design that declares no roles
 *  - `roles-locked`           → every mutation 404s (the org/project fence)
 */

import { http, HttpResponse } from "msw";
import {
  projectRolesView,
  projectRolesViewEmpty,
  projectRolesViewOffline,
} from "../fixtures/roles";

/** The scenario a project name selects. */
function viewFor(projectName: string) {
  switch (projectName) {
    case "roles-offline":
      return projectRolesViewOffline;
    case "roles-empty":
      return projectRolesViewEmpty;
    default:
      return projectRolesView;
  }
}

// A mock password that is obviously a mock. Real ones are 192 bits of
// base64url; this one must never be mistaken for a credential in a screenshot.
const MOCK_PASSWORD = "mocknotreal";

/** The fence's answer: one shape for "you may not touch this", never a reason. */
function refused() {
  return HttpResponse.json(
    { error: "no such test user for this project" },
    { status: 404 },
  );
}

export const rolesHandlers = [
  http.get("*/api/v1/projects/:projectName/roles", ({ params }) =>
    HttpResponse.json(viewFor(String(params.projectName))),
  ),

  http.post(
    "*/api/v1/projects/:projectName/roles/test-users/:username/reveal",
    ({ params }) => {
      if (String(params.projectName) === "roles-locked") return refused();
      return HttpResponse.json({
        username: String(params.username),
        password: MOCK_PASSWORD,
        rotatedAt: null,
      });
    },
  ),

  http.post(
    "*/api/v1/projects/:projectName/roles/test-users/:username/rotate",
    ({ params }) => {
      if (String(params.projectName) === "roles-locked") return refused();
      return HttpResponse.json({
        username: String(params.username),
        // Visibly different from the revealed one, so a rotate is observable.
        password: `${MOCK_PASSWORD}-rotated`,
        rotatedAt: new Date().toISOString(),
      });
    },
  ),

  http.delete(
    "*/api/v1/projects/:projectName/roles/test-users/:username",
    ({ params }) => {
      if (String(params.projectName) === "roles-locked") return refused();
      return HttpResponse.json({ status: "deleted" });
    },
  ),
];
