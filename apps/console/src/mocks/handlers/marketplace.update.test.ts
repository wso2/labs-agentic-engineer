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
import { resetExternalResourceCatalog } from "../fixtures/marketplace";
import { marketplaceHandlers } from "./marketplace";

type RegisterExternalResourceRequest =
  components["schemas"]["RegisterExternalResourceRequest"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type ApiError = components["schemas"]["Error"];

const BASE = "http://localhost/api/v1";
const EXTERNAL = `${BASE}/dependencies/external-resources`;

const server = setupServer(...marketplaceHandlers);

function setScenario(scenario: "empty" | "some" | "error") {
  localStorage.setItem("aep:mock:marketplace", scenario);
}

function updateUrl(name: string): string {
  return `${EXTERNAL}/${name}`;
}

function stripeSeedUpdateBody(
  overrides: Partial<RegisterExternalResourceRequest> = {},
): RegisterExternalResourceRequest {
  return {
    name: "stripe",
    description: "Stripe payments API",
    consumptionInstructions: "Use the secret key as Bearer.",
    config: [
      { key: "api_key", secret: true, description: "Secret API key" },
      { key: "region", secret: false, description: "Stripe account region" },
    ],
    envValues: [
      { environment: "development", key: "api_key", value: "" },
      { environment: "development", key: "region", value: "us" },
      { environment: "staging-local", key: "api_key", value: "sk_stg" },
      { environment: "staging-local", key: "region", value: "eu" },
    ],
    resourceDocs: [
      { type: "openapi", url: "https://example.com/stripe/openapi.yaml" },
    ],
    ...overrides,
  };
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

function findCell(
  cells: ExternalResourceDTO["envCells"],
  environment: string,
  key: string,
) {
  return cells?.find((c) => c.environment === environment && c.key === key);
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

describe("PUT /dependencies/external-resources/{name}", () => {
  it("returns 404 when the name is not in the catalog", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("no-such"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(stripeSeedUpdateBody()),
    });
    expect(res.status).toBe(404);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("not_found");
  });

  it("returns 409 when the target is a Project External (empty envCells)", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("github"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(stripeSeedUpdateBody({ name: "github" })),
    });
    expect(res.status).toBe(409);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("conflict");
  });

  it("returns 400 when config key identity is mutated", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("stripe"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        stripeSeedUpdateBody({
          config: [
            { key: "api_key", secret: true, description: "Secret API key" },
            { key: "region", secret: false, description: "Stripe account region" },
            { key: "mode", secret: false, description: "Charge mode" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(400);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("bad_request");
  });

  it("keeps a configured secret when the body value is empty and still omits value", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("stripe"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(stripeSeedUpdateBody()),
    });
    expect(res.status).toBe(200);
    const updated = (await res.json()) as ExternalResourceDTO;
    const kept = findCell(updated.envCells, "development", "api_key");
    expect(kept?.status).toBe("configured");
    expect(kept?.value).toBeUndefined();
  });

  it("returns 400 when an unset secret is submitted empty", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("stripe"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        stripeSeedUpdateBody({
          envValues: [
            { environment: "development", key: "api_key", value: "" },
            { environment: "development", key: "region", value: "us" },
            { environment: "staging-local", key: "api_key", value: "" },
            { environment: "staging-local", key: "region", value: "eu" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(400);
    const err = (await res.json()) as ApiError;
    expect(err.code).toBe("bad_request");
  });

  it("emits the org-env matrix and drops production cells when the body only has org rows", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("stripe"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        stripeSeedUpdateBody({
          envValues: [
            { environment: "development", key: "api_key", value: "" },
            { environment: "development", key: "region", value: "us" },
            { environment: "staging-local", key: "api_key", value: "sk_stg" },
            { environment: "staging-local", key: "region", value: "eu" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(200);
    const updated = (await res.json()) as ExternalResourceDTO;
    const envs = new Set((updated.envCells ?? []).map((c) => c.environment));
    expect(envs.has("production")).toBe(false);
    expect(envs.has("staging-local")).toBe(true);
    expect(findCell(updated.envCells, "development", "api_key")?.status).toBe(
      "configured",
    );
    expect(findCell(updated.envCells, "development", "api_key")?.value).toBeUndefined();
    expect(findCell(updated.envCells, "staging-local", "api_key")?.status).toBe(
      "configured",
    );
    expect(findCell(updated.envCells, "staging-local", "region")?.value).toBe("eu");
  });

  it("replaces a non-secret envCells value from the body", async () => {
    setScenario("some");
    const res = await fetch(updateUrl("stripe"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        stripeSeedUpdateBody({
          envValues: [
            { environment: "development", key: "api_key", value: "" },
            { environment: "development", key: "region", value: "ap" },
            { environment: "staging-local", key: "api_key", value: "sk_stg" },
            { environment: "staging-local", key: "region", value: "eu" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(200);
    const updated = (await res.json()) as ExternalResourceDTO;
    expect(findCell(updated.envCells, "development", "region")?.value).toBe("ap");
  });

  it("returns 200 with replaced description, consumption, key descriptions, URL docs, and env matrix", async () => {
    setScenario("empty");
    const createdRes = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(registerBody()),
    });
    expect(createdRes.status).toBe(201);

    const res = await fetch(updateUrl("twilio"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          name: "other-name",
          description: "Twilio SMS (updated)",
          consumptionInstructions: "Use the auth token as Bearer. Rotate quarterly.",
          config: [
            { key: "auth_token", secret: true, description: "Auth token (rotated)" },
            { key: "from_number", secret: false, description: "From number (primary)" },
          ],
          envValues: [
            { environment: "development", key: "auth_token", value: "" },
            { environment: "development", key: "from_number", value: "+1999" },
            { environment: "staging-local", key: "auth_token", value: "" },
            { environment: "staging-local", key: "from_number", value: "+1888" },
          ],
          resourceDocs: [
            { type: "openapi", url: "https://example.com/twilio/openapi-v2.yaml" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(200);
    const updated = (await res.json()) as ExternalResourceDTO;
    expect(updated.name).toBe("twilio");
    expect(updated.description).toBe("Twilio SMS (updated)");
    expect(updated.consumptionInstructions).toBe(
      "Use the auth token as Bearer. Rotate quarterly.",
    );
    expect(updated.config?.[0]?.description).toBe("Auth token (rotated)");
    expect(updated.config?.[1]?.description).toBe("From number (primary)");
    expect(updated.resourceDocs?.[0]?.url).toBe(
      "https://example.com/twilio/openapi-v2.yaml",
    );
    expect(findCell(updated.envCells, "development", "auth_token")?.status).toBe(
      "configured",
    );
    expect(findCell(updated.envCells, "development", "auth_token")?.value).toBeUndefined();
    expect(findCell(updated.envCells, "development", "from_number")?.value).toBe("+1999");

    const listed = (await (await fetch(EXTERNAL)).json()) as ExternalResourceDTO[];
    expect(listed[0]?.description).toBe("Twilio SMS (updated)");
    expect(listed[0]?.resourceDocs?.[0]?.url).toBe(
      "https://example.com/twilio/openapi-v2.yaml",
    );
    expect(findCell(listed[0]?.envCells, "development", "from_number")?.value).toBe(
      "+1999",
    );
  });

  it("file create returns a path pointer and never content", async () => {
    setScenario("empty");
    const createdRes = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(registerBody()),
    });
    expect(createdRes.status).toBe(201);

    const res = await fetch(updateUrl("twilio"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          envValues: [
            { environment: "development", key: "auth_token", value: "" },
            { environment: "development", key: "from_number", value: "+1555" },
            { environment: "staging-local", key: "auth_token", value: "" },
            { environment: "staging-local", key: "from_number", value: "+1666" },
          ],
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
    expect(res.status).toBe(200);
    const raw = await res.text();
    expect(raw).not.toContain("# Twilio");
    expect(raw).not.toContain('"content"');
    const updated = JSON.parse(raw) as ExternalResourceDTO;
    expect(updated.resourceDocs).toEqual([
      { type: "documentation", path: "twilio/README.md" },
    ]);
  });

  it("URL-only write has no path", async () => {
    setScenario("empty");
    const createdRes = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          resourceDocs: [
            { type: "openapi", url: "https://example.com/twilio/openapi.yaml" },
          ],
        }),
      ),
    });
    expect(createdRes.status).toBe(201);
    const created = (await createdRes.json()) as ExternalResourceDTO;
    expect(created.resourceDocs?.[0]?.url).toBe(
      "https://example.com/twilio/openapi.yaml",
    );
    expect(created.resourceDocs?.[0]?.path).toBeUndefined();

    const res = await fetch(updateUrl("twilio"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          envValues: [
            { environment: "development", key: "auth_token", value: "" },
            { environment: "development", key: "from_number", value: "+1555" },
            { environment: "staging-local", key: "auth_token", value: "" },
            { environment: "staging-local", key: "from_number", value: "+1666" },
          ],
          resourceDocs: [
            { type: "openapi", url: "https://example.com/twilio/openapi-v2.yaml" },
          ],
        }),
      ),
    });
    expect(res.status).toBe(200);
    const updated = (await res.json()) as ExternalResourceDTO;
    expect(updated.resourceDocs?.[0]?.url).toBe(
      "https://example.com/twilio/openapi-v2.yaml",
    );
    expect(updated.resourceDocs?.[0]?.path).toBeUndefined();
  });

  it("returns 400 when a write row has both url and content", async () => {
    setScenario("empty");
    const createdRes = await fetch(EXTERNAL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(registerBody()),
    });
    expect(createdRes.status).toBe(201);

    const res = await fetch(updateUrl("twilio"), {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(
        registerBody({
          envValues: [
            { environment: "development", key: "auth_token", value: "" },
            { environment: "development", key: "from_number", value: "+1555" },
            { environment: "staging-local", key: "auth_token", value: "" },
            { environment: "staging-local", key: "from_number", value: "+1666" },
          ],
          resourceDocs: [
            {
              type: "openapi",
              url: "https://example.com/twilio/openapi.yaml",
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
