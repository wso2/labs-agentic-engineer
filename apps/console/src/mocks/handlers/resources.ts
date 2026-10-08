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
import { externalResources, orgEndpoints, platformTypes } from "../fixtures/resources";

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type RegisterExternalResourceRequest = components["schemas"]["RegisterExternalResourceRequest"];
type PromoteExternalResourceRequest = components["schemas"]["PromoteExternalResourceRequest"];
type EnvValueCellDTO = components["schemas"]["EnvValueCellDTO"];

// What a project can depend on, in mock mode, kept for the browser session:
// the Resources Page and the Resource card read and write it. A reload
// starts over.

let external: ExternalResourceDTO[] = externalResources.map((r) => ({ ...r }));

function refused(message: string, status = 400) {
  return HttpResponse.json({ code: "RESOURCE_INVALID", message }, { status });
}

/** The org value plane after a write: every key in every environment it names, secrets never echoed. */
function cellsOf(body: RegisterExternalResourceRequest, before: readonly EnvValueCellDTO[]): EnvValueCellDTO[] {
  const secret = new Set(body.config.filter((k) => k.secret).map((k) => k.key));
  return body.envValues.map(({ environment, key, value }) => {
    const held = before.find((c) => c.environment === environment && c.key === key)?.status === "configured";
    const status = value.trim() || held ? "configured" : "unset";
    return { environment, key, status, ...(secret.has(key) ? {} : { value }) };
  });
}

function recordOf(body: RegisterExternalResourceRequest, before: ExternalResourceDTO | undefined): ExternalResourceDTO {
  const contract = body.contract
    ? { type: body.contract.type, path: `${body.name}/${body.contract.fileName ?? "contract"}` }
    : before?.contract;
  return {
    name: body.name,
    provider: body.provider ?? "",
    description: body.description,
    consumptionInstructions: body.consumptionInstructions,
    scope: "org",
    config: body.config,
    envCells: cellsOf(body, before?.envCells ?? []),
    ...(contract ? { contract } : {}),
    resourceDocs: (body.resourceDocs ?? []).map((d) => ({ type: d.type, ...(d.url ? { url: d.url } : { path: d.path ?? "" }) })),
    consumers: before?.consumers ?? [],
  };
}

export const resourcesHandlers = [
  http.get("*/api/v1/dependencies/platform-resource-types", () => HttpResponse.json(platformTypes)),
  http.get("*/api/v1/dependencies/org-endpoints", () => HttpResponse.json(orgEndpoints)),
  http.get("*/api/v1/dependencies/external-resources", () => HttpResponse.json(external)),

  http.post("*/api/v1/dependencies/external-resources", async ({ request }) => {
    const body = (await request.json()) as RegisterExternalResourceRequest;
    if (external.some((r) => r.name === body.name)) return refused(`${body.name} already exists`, 409);
    const record = recordOf(body, undefined);
    external = [...external, record];
    return HttpResponse.json(record, { status: 201 });
  }),

  http.put("*/api/v1/dependencies/external-resources/:name", async ({ params, request }) => {
    const name = String(params.name);
    const before = external.find((r) => r.name === name && r.scope === "org");
    if (!before) return refused(`external resource ${name} not found`, 404);
    const body = (await request.json()) as RegisterExternalResourceRequest;
    const record = recordOf({ ...body, name }, before);
    external = external.map((r) => (r === before ? record : r));
    return HttpResponse.json(record);
  }),

  http.delete("*/api/v1/dependencies/external-resources/:name", ({ params }) => {
    const name = String(params.name);
    const record = external.find((r) => r.name === name && r.scope === "org");
    if (record && (record.consumers ?? []).length > 0) return refused(`${name} is in use`, 409);
    external = external.filter((r) => r !== record);
    return new HttpResponse(null, { status: 204 });
  }),

  http.post("*/api/v1/projects/:projectName/dependencies/external-resources/:name/promote", async ({ params, request }) => {
    const project = String(params.projectName);
    const name = String(params.name);
    const held = external.find((r) => r.name === name && r.scope === "project" && r.project === project);
    if (!held) return refused(`project ${project} has no external dependency ${name}`, 404);
    if (external.some((r) => r.name === name && r.scope === "org")) return refused(`${name} is already registered`, 409);
    const body = (await request.json()) as PromoteExternalResourceRequest;
    if (!body.consumptionInstructions.trim()) return refused("consumptionInstructions is required");
    // The project's own values carry over wherever none was typed.
    const envs = ["development", "staging", "production"];
    const record: ExternalResourceDTO = {
      ...held,
      scope: "org",
      consumptionInstructions: body.consumptionInstructions,
      envCells: envs.flatMap((environment) =>
        (held.config ?? []).map((k) => ({ environment, key: k.key, status: "configured" as const })),
      ),
    };
    delete record.project;
    external = external.map((r) => (r === held ? record : r));
    return HttpResponse.json(record, { status: 201 });
  }),
];
