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

type PromoteExternalResourceRequest =
  components["schemas"]["PromoteExternalResourceRequest"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type ApiError = components["schemas"]["Error"];

const BASE = "http://localhost/api/v1";
const EXTERNAL = `${BASE}/dependencies/external-resources`;
const PROMOTE = `${BASE}/projects/team-expenses/dependencies/external-resources/fx-rates/promote`;

const server = setupServer(...marketplaceHandlers);

function promote(body: PromoteExternalResourceRequest, url = PROMOTE) {
  return fetch(url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
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
  localStorage.setItem("aep:mock:marketplace", "some");
  resetExternalResourceCatalog();
});

describe("POST /projects/:project/dependencies/external-resources/:name/promote", () => {
  it("turns the project's row into the organization's record, carrying development over", async () => {
    const res = await promote({
      consumptionInstructions: "Call /latest.json once per conversion.",
      envValues: [
        { environment: "staging-local", key: "OPENEXCHANGERATES_APP_ID", value: "stg" },
        { environment: "staging-local", key: "FX_BASE", value: "EUR" },
      ],
    });
    expect(res.status).toBe(201);
    const record = (await res.json()) as ExternalResourceDTO;
    expect(record.scope).toBe("org");
    expect(record.provider).toBe("Open Exchange Rates");
    expect(record.contract).toEqual({ type: "openapi", path: "fx-rates/openapi.yaml" });
    expect(record.envCells?.every((c) => c.status === "configured")).toBe(true);

    const list = (await (await fetch(EXTERNAL)).json()) as ExternalResourceDTO[];
    const fx = list.filter((r) => r.name === "fx-rates");
    expect(fx).toHaveLength(1);
    expect(fx[0]?.scope).toBe("org");
  });

  it("refuses an environment with nothing typed and nothing to carry over", async () => {
    const res = await promote({ consumptionInstructions: "Use it.", envValues: [] });
    expect(res.status).toBe(400);
    const err = (await res.json()) as ApiError;
    expect(err.message).toContain('"staging-local"');
  });

  it("refuses without instructions, and 404s a row the project does not hold", async () => {
    expect((await promote({ consumptionInstructions: " " })).status).toBe(400);
    expect(
      (
        await promote(
          { consumptionInstructions: "Use it." },
          `${BASE}/projects/team-expenses/dependencies/external-resources/ghost/promote`,
        )
      ).status,
    ).toBe(404);
  });
});
