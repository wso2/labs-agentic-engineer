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

import { http, HttpResponse } from "msw";
import {
  BUILD_SCENARIOS,
  buildPreflight,
  mockVersionTags,
  type BuildScenario,
} from "../fixtures/build";
import type { components } from "../../generated/aep-api";

type BuildRequest = components["schemas"]["BuildRequest"];

function scenario(): BuildScenario {
  const raw = localStorage.getItem("aep:mock:build");
  return raw && BUILD_SCENARIOS.includes(raw as BuildScenario)
    ? (raw as BuildScenario)
    : "changes";
}

export const buildHandlers = [
  http.get("*/api/v1/projects/:projectName/build/preflight", () =>
    HttpResponse.json(buildPreflight(scenario())),
  ),

  // The cut. The name the dialog sends becomes the tag; an empty one means a
  // rebuild, which reuses the version the tree already matches. A name in use
  // is the race the field's own check cannot close — the server answers it.
  http.post(
    "*/api/v1/projects/:projectName/build",
    async ({ request }) => {
      const body = (await request.json()) as BuildRequest;
      const preflight = buildPreflight(scenario());
      const version = body.version ?? "";
      if (version && mockVersionTags.includes(version)) {
        return HttpResponse.json(
          {
            code: "conflict",
            message: `A version named ${version} already exists.`,
          },
          { status: 409 },
        );
      }
      return HttpResponse.json({
        tag: version || preflight.currentVersion || "v1",
      });
    },
  ),
];
