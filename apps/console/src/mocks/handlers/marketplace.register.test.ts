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

// @vitest-environment jsdom

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import type { components } from "../../generated/aep-api";
import {
  marketplaceLoadError,
  resetExternalResourceCatalog,
  seedExternalResources,
  seedOrgEnvironments,
  type MarketplaceScenario,
} from "../fixtures/marketplace";
import { marketplaceHandlers } from "./marketplace";

type RegisterExternalResourceRequest =
  components["schemas"]["RegisterExternalResourceRequest"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type EnvironmentDTO = components["schemas"]["EnvironmentDTO"];
type ApiError = components["schemas"]["Error"];

const BASE = "http://localhost/api/v1";
const ENVIRONMENTS = `${BASE}/dependencies/environments`;
const EXTERNAL = `${BASE}/dependencies/external-resources`;

const server = setupServer(...marketplaceHandlers);

function setScenario(scenario: MarketplaceScenario) {
  localStorage.setItem("aep:mock:marketplace", scenario);
}

function registerBody(
  overrides: Partial<RegisterExternalResourceRequest> = {},
): RegisterExternalResourceRequest {
  return {
    name: "twilio",
    description: "Twilio SMS",
    consumptionInstructions: "Use the auth token as Bearer.",
    config: [
      { key: "auth_token", secret: true, description: "Auth token" },
      { key: "from_number", secret: false, description: "From number" },
    ],
    envValues: [
      { environment: "development", key: "auth_token", value: "sk_dev" },
      { environment: "development", key: "from_number", value: "+1555" },
      { environment: "staging-local", key: "auth_token", value: "sk_stg" },
      { environment: "staging-local", key: "from_number", value: "+1666" },
    ],
    ...overrides,
  };
}

beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});

beforeEach(() => {
  localStorage.clear();
  resetExternalResourceCatalog();
});

describe("GET /dependencies/environments", () => {
  it("returns 500 marketplaceLoadError when the scenario is error", async () => {
    setScenario("error");
    const res = await fetch(ENVIRONMENTS);
    expect(res.status).toBe(500);
    await expect(res.json()).resolves.toEqual(marketplaceLoadError);
  });

  it("returns [] when the scenario is empty", async () => {
    setScenario("empty");
    const res = await fetch(ENVIRONMENTS);
    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual([]);
  });

  it("returns seedOrgEnvironments (development, staging-local) when the scenario is some", async () => {
    setScenario("some");
    const res = await fetch(ENVIRONMENTS);
    expect(res.status).toBe(200);
    const body = (await res.json()) as EnvironmentDTO[];
    expect(body).toEqual(seedOrgEnvironments);
    expect(body.map((e) => e.name)).toEqual(["development", "staging-local"]);
  });
});

describe("POST /dependencies/external-resources", () => {
  it("returns 400 when a required field is missing", async () => {
    setScenario("empty");
    const incomplete: Partial<RegisterExternalResourceRequest> = { ...registerBody() };
    delete incomplete.name;
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(incomplete),
    });
    expect(res.status).toBe(400);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("bad_request");
  });

  it("returns 400 when an env value is empty", async () => {
    setScenario("empty");
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          envValues: [
            { environment: "development", key: "auth_token", value: "" },
            { environment: "development", key: "from_number", value: "+1555" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(400);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("bad_request");
  });

  it("returns 409 when the logical name already exists in the catalog", async () => {
    setScenario("some");
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(registerBody({ name: "stripe" })),
    });
    expect(res.status).toBe(409);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("conflict");
  });

  it("returns 201 Registered DTO: one envCell per submitted envValue; secrets omit value", async () => {
    setScenario("empty");
    const req = registerBody();
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(req),
    });
    expect(res.status).toBe(201);
    const created = (await res.json()) as ExternalResourceDTO;
    expect(created.name).toBe("twilio");
    expect(created.envCells).toHaveLength(req.envValues.length);
    for (const cell of created.envCells ?? []) {
      expect(cell.status).toBe("configured");
      if (cell.key === "auth_token") {
        expect(cell.value).toBeUndefined();
      } else {
        expect(cell.value).toBeTruthy();
      }
    }
    const fromNumberDev = created.envCells?.find(
      (c) => c.key === "from_number" && c.environment === "development",
    );
    expect(fromNumberDev?.value).toBe("+1555");
  });

  it("appends the Registered DTO so a later GET list returns the new card", async () => {
    setScenario("empty");
    const createdRes = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(registerBody()),
    });
    expect(createdRes.status).toBe(201);

    const listedRes = await fetch(EXTERNAL);
    expect(listedRes.status).toBe(200);
    const listed = (await listedRes.json()) as ExternalResourceDTO[];
    expect(listed.map((r) => r.name)).toEqual(["twilio"]);
    expect(listed[0]?.envCells).toHaveLength(4);
  });

  it("does not mutate the seed list when appending under the some scenario", async () => {
    setScenario("some");
    const seedLen = seedExternalResources.length;
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(registerBody()),
    });
    expect(res.status).toBe(201);

    const listed = (await (await fetch(EXTERNAL)).json()) as ExternalResourceDTO[];
    expect(listed.map((r) => r.name)).toEqual(["stripe", "github", "twilio"]);
    expect(seedExternalResources).toHaveLength(seedLen);
  });

  it("POST fileName+content returns a path pointer and never content", async () => {
    setScenario("empty");
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          resourceDocs: [
            {
              type: "documentation",
              fileName: "README.md",
              content: "# Twilio\n",
            },
          ],
        }),
      ),
    });
    expect(res.status).toBe(201);
    const raw = await res.text();
    expect(raw).not.toContain('"content"');
    expect(raw).not.toContain("# Twilio");
    const created = JSON.parse(raw) as ExternalResourceDTO;
    expect(created.resourceDocs).toEqual([
      { type: "documentation", path: "twilio/README.md" },
    ]);
  });

  it("POST both url and content returns 400", async () => {
    setScenario("empty");
    const res = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          resourceDocs: [
            {
              type: "openapi",
              url: "https://example.com/openapi.yaml",
              content: "# nope\n",
            },
          ],
        }),
      ),
    });
    expect(res.status).toBe(400);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("bad_request");
  });
});
