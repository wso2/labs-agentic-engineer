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
import { START_COMMAND } from "@aep/contracts/commands";
import type { components } from "../../generated/aep-api";
import { createdProjects, recordCreatedProject } from "../createdProjects";
import { githubConnectedFixture } from "../fixtures/settings";
import { projects as seedProjects } from "../fixtures/projects";
import { startMockTurn } from "./conversation";

type Project = components["schemas"]["Project"];
type ProjectList = components["schemas"]["ProjectList"];
type CreateProjectRequest = components["schemas"]["CreateProjectRequest"];
type ApiError = components["schemas"]["Error"];

function allProjects(): Project[] {
  return [...seedProjects, ...createdProjects().map((c) => c.project)];
}

// Every repository name claimed: a seed project's repo is its own name, a
// created one's is what it asked for.
function takenRepoNames(): Set<string> {
  return new Set([...seedProjects.map((p) => p.name), ...createdProjects().map((c) => c.repoName)]);
}

const duplicateProjectError: ApiError = {
  code: "conflict",
  message: "A project with this name already exists",
};

export const projectsHandlers = [
  http.get("*/api/v1/projects", () => HttpResponse.json<ProjectList>({ items: allProjects() })),

  // Two 409s in the BFF, mocked as one check each: the project name, and the
  // GitHub repository name, which two differently named projects can both ask
  // for. The repo arm is what makes the form's field-level conflict reachable.
  // Try a seed project's name (acme-expenses) to see it.
  http.post("*/api/v1/projects", async ({ request }) => {
    const body = (await request.json()) as CreateProjectRequest;
    const repoName = body.repoName ?? body.name;
    if (allProjects().some((p) => p.name === body.name) || takenRepoNames().has(repoName)) {
      return HttpResponse.json(duplicateProjectError, { status: 409 });
    }
    // The prompt is the description when none is given, so the new card and
    // overview are not left blank.
    const description = body.description ?? body.prompt;
    const project: Project = {
      name: body.name,
      displayName: body.displayName ?? body.name,
      ...(description !== undefined && { description }),
      repoUrl: `https://github.com/${githubConnectedFixture.githubLogin}/${repoName}.git`,
      createdAt: new Date().toISOString(),
    };
    recordCreatedProject({ project, repoName, ...(body.prompt !== undefined && { prompt: body.prompt }) });
    // The platform fires the kickoff itself (#562), unless documents are
    // coming: then their upload fires it, and Continue without documents
    // has the chat send it.
    if (body.prompt && !body.referencesPending) startMockTurn(body.name, { instruction: START_COMMAND });
    return HttpResponse.json(project, { status: 201 });
  }),

  http.get("*/api/v1/projects/:projectName", ({ params }) => {
    const project = allProjects().find((p) => p.name === params.projectName);
    if (!project) {
      return HttpResponse.json(
        {
          code: "not_found",
          message: `Project ${String(params.projectName)} not found`,
        } satisfies ApiError,
        { status: 404 },
      );
    }
    return HttpResponse.json(project);
  }),

  // New project's reference upload, fired right after the create. The real
  // server stores the bytes off-git (ADR-0017), so the mock only checks the
  // request shape. The details step's Retry upload / Continue without
  // documents state: localStorage.setItem('aep:mock:project:references', 'error').
  http.post("*/api/v1/projects/:projectName/references", async ({ params, request }) => {
    if (localStorage.getItem("aep:mock:project:references") === "error") {
      return HttpResponse.json(
        { code: "internal_error", message: "Mock error scenario for the reference upload" } satisfies ApiError,
        { status: 500 },
      );
    }
    const files = (await request.formData()).getAll("files");
    if (files.length === 0) {
      return HttpResponse.json(
        { code: "invalid_request", message: "no reference documents" } satisfies ApiError,
        { status: 400 },
      );
    }
    // The documents are in: the held kickoff goes now.
    startMockTurn(String(params.projectName), { instruction: START_COMMAND });
    return new HttpResponse(null, { status: 204 });
  }),
];
