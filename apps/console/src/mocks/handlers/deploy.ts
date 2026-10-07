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
import type { components } from "../../generated/aep-api";
import { projectBuilds } from "../buildsState";

// The Deploy Page's reads in mock mode: a three-environment pipeline where a
// built version runs in Development (the newest the mock built), Xero as the
// one external dependency, and three test users. Values saved from a
// Configure card are kept for the session's page; nothing is promoted.

type ProjectStatus = components["schemas"]["ProjectStatus"];
type Environment = components["schemas"]["EnvironmentDTO"];
type Deployment = components["schemas"]["Deployment"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type DependencyReadiness = components["schemas"]["ProjectDependencyReadiness"];
type ProjectRolesView = components["schemas"]["ProjectRolesView"];

const environments: Environment[] = [
  { name: "development", displayName: "Development", isProduction: false, validation: "on", position: 0, promotesTo: "staging" },
  { name: "staging", displayName: "Staging", isProduction: false, validation: "off", position: 1, promotesTo: "production" },
  { name: "production", displayName: "Production", isProduction: true, validation: "off", position: 2 },
];

const COMPONENTS = [
  { name: "expense-webapp", displayName: "Expense web app", type: "web-application" },
  { name: "expense-api", displayName: "Expense API", type: "service" },
];

const design: ComponentDependencies[] = [
  {
    componentName: "expense-api",
    dependencies: [
      {
        kind: "external",
        name: "Xero",
        config: [
          { key: "clientId", description: "The Xero app's client ID" },
          { key: "clientSecret", secret: true },
          { key: "tenant", description: "The Xero organisation to post to" },
        ],
      },
    ],
  },
];

const roles: ProjectRolesView = {
  directoryAvailable: true,
  signIn: { issuer: "http://localhost:8097", clientId: "acme-expenses-public" },
  resourceServer: "https://api.acme-expenses.example",
  testUsers: [
    { username: "jane", owned: true, exists: true, supplied: false, roles: ["Employee"], scopes: ["expenses:write"] },
    { username: "priya", owned: true, exists: true, supplied: false, roles: ["Approver"], scopes: ["expenses:approve"] },
    { username: "finn", owned: true, exists: true, supplied: false, roles: ["Finance"], scopes: ["payroll:export"] },
  ],
};

const OPENAPI = `openapi: 3.0.3
info: { title: Expense API, version: "1" }
paths:
  /expenses:
    get: { summary: List my expenses, responses: { "200": { description: OK } } }
    post: { summary: Submit an expense, responses: { "201": { description: Created } } }
  /expenses/{id}/approve:
    post: { summary: Approve an expense, parameters: [{ name: id, in: path, required: true, schema: { type: string } }], responses: { "204": { description: Approved } } }
`;

// Development has Xero's values from the build; the rest wait for a Configure card.
const configured = new Set(["development"]);

/** The newest version the mock has built: what runs in Development. */
function runningVersion(projectName: string): string | null {
  return projectBuilds(projectName).filter((b) => b.status === "built").at(-1)?.version ?? null;
}

export const deployHandlers = [
  http.get("*/api/v1/dependencies/environments", () => HttpResponse.json(environments)),

  http.get("*/api/v1/projects/:projectName/status", ({ params }) => {
    const version = runningVersion(String(params.projectName));
    return HttpResponse.json<ProjectStatus>({
      build: { status: "idle", version: version ?? "" },
      deploy: {
        status: version ? "deployed" : "none",
        version: version ?? "",
        validation: version ? "passed" : "none",
        components: { ready: version ? COMPONENTS.length : 0, total: COMPONENTS.length },
      },
      hasDesign: true,
      hasSpec: true,
      hasTasks: false,
      phase: "tasks",
      repoStatus: "ready",
      repoUrl: "https://github.com/acme/acme-expenses",
      spec: { agent: "", design: true, dirty: false, exists: true, version: version ?? "" },
      specStatus: "approved",
    });
  }),

  http.get("*/api/v1/projects/:projectName/components", ({ params }) =>
    HttpResponse.json({ items: runningVersion(String(params.projectName)) ? COMPONENTS : [] }),
  ),

  http.get("*/api/v1/projects/:projectName/components/:componentName/deployments", ({ params }) => {
    const name = String(params.componentName);
    const items: Deployment[] = [
      {
        componentName: name,
        environment: "development",
        status: "Ready",
        endpointUrl: `https://${name}-development.acme.example`,
        createdAt: new Date(Date.now() - 20 * 60_000).toISOString(),
      },
    ];
    return HttpResponse.json({ items });
  }),

  http.get("*/api/v1/projects/:projectName/components/:componentName/openapi", ({ params }) =>
    HttpResponse.json({ componentName: String(params.componentName), componentType: "service", spec: OPENAPI }),
  ),

  http.get("*/api/v1/projects/:projectName/dependencies/readiness", ({ request }) => {
    const environment = new URL(request.url).searchParams.get("environment") ?? "development";
    const ready = configured.has(environment);
    return HttpResponse.json<DependencyReadiness>({
      configured: ready,
      dependencies: [
        {
          name: "Xero",
          state: ready ? "configured" : "unset",
          missingKeys: ready ? [] : ["clientId", "clientSecret", "tenant"],
        },
      ],
    });
  }),

  http.post("*/api/v1/projects/:projectName/dependencies/external-resources/:name/values", async ({ request }) => {
    const body = (await request.json()) as components["schemas"]["SaveValuesBody"];
    for (const environment of Object.keys(body.environments)) configured.add(environment);
    return HttpResponse.json({ status: "ok" });
  }),

  http.get("*/api/v1/projects/:projectName/design/dependencies", () => HttpResponse.json(design)),

  http.get("*/api/v1/projects/:projectName/roles", () => HttpResponse.json(roles)),

  http.post("*/api/v1/projects/:projectName/roles/test-users/:username/reveal", ({ params }) =>
    HttpResponse.json({ username: String(params.username), password: "mock-Passw0rd!" }),
  ),
];
