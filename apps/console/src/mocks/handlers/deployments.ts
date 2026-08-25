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

import { HttpResponse, http } from "msw";
import {
  deploymentDetail,
  projectDeployments,
  projectDeploymentsWithProduction,
  runtimeLogs,
} from "../fixtures/deployments";
import { projectSectionError, type ProjectScenario } from "../fixtures/project";

// The Deployments board, the deployment detail page and the runtime log
// (ADR-0020). Split out of handlers/project.ts because these three back one
// surface and its fixtures are their own file.

function scenario(): ProjectScenario {
  const chosen = localStorage.getItem(
    "aep:mock:project",
  ) as ProjectScenario | null;
  return chosen ?? "deployed";
}

/** Has the reader asked to see a promoted production deployment? */
function withProduction(): boolean {
  return localStorage.getItem("aep:mock:deployments") === "production";
}

/**
 * Is the runtime-log endpoint pretending not to exist?
 *
 * This is the state that matters most on this surface: the frontend lands on
 * the branch BEFORE the backend does (#609's handshake), so "the endpoint 404s"
 * is not a hypothetical — it is what every reader sees until aep-api ships.
 * The page must degrade to a note, and this switch is how that gets verified:
 *   localStorage.setItem('aep:mock:runtime-logs', 'missing')
 */
function runtimeLogsMissing(): boolean {
  return localStorage.getItem("aep:mock:runtime-logs") === "missing";
}

export const deploymentsHandlers = [
  http.get("*/api/v1/projects/:projectName/deployments", ({ request }) => {
    const s = scenario();
    if (s === "error") {
      return HttpResponse.json(projectSectionError, { status: 500 });
    }
    const all = withProduction()
      ? projectDeploymentsWithProduction
      : projectDeployments[s];

    // The query parameters are honoured rather than ignored: a mock that
    // returns everything regardless makes a broken filter look like it works.
    const url = new URL(request.url);
    const environment = url.searchParams.get("environment");
    const items = environment
      ? all.filter((d) => d.environment === environment)
      : all;
    return HttpResponse.json({ items });
  }),

  http.get(
    "*/api/v1/projects/:projectName/deployments/:deploymentId",
    ({ params }) => {
      if (scenario() === "error") {
        return HttpResponse.json(projectSectionError, { status: 500 });
      }
      const detail = deploymentDetail(String(params.deploymentId));
      if (!detail) {
        // A real 404, so the detail page's unknown-id state is reachable
        // instead of rendering a blank card.
        return HttpResponse.json(
          { title: "Not Found", status: 404, detail: "deployment not found" },
          { status: 404 },
        );
      }
      return HttpResponse.json(detail);
    },
  ),

  http.get(
    "*/api/v1/projects/:projectName/components/:componentName/runtime-logs",
    ({ params, request }) => {
      if (runtimeLogsMissing()) {
        return HttpResponse.json(
          { title: "Not Found", status: 404, detail: "runtime logs unavailable" },
          { status: 404 },
        );
      }
      if (scenario() === "error") {
        return HttpResponse.json(projectSectionError, { status: 500 });
      }
      const windowSeconds = Number(
        new URL(request.url).searchParams.get("windowSeconds") ?? 3600,
      );
      return HttpResponse.json(
        runtimeLogs(String(params.componentName), windowSeconds),
      );
    },
  ),
];
