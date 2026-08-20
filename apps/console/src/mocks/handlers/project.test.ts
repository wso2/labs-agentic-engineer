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

import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { resetConfiguredReadiness } from "../fixtures/project";
import { projectHandlers } from "./project";

const server = setupServer(...projectHandlers);
const endpoint =
  "http://localhost/api/v1/projects/acme/components/storefront/dependencies/stripe/status?environment=development";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  localStorage.clear();
  resetConfiguredReadiness();
  server.resetHandlers();
});
afterAll(() => server.close());

describe("component dependency status mock", () => {
  it.each([
    ["configured", true, "configured"],
    ["unset", false, "unset"],
    ["not-provisioned", false, "not-provisioned"],
  ] as const)("serves the %s development scenario", async (scenario, ready, valueState) => {
    localStorage.setItem("aep:mock:component-dependency-status", scenario);

    const response = await fetch(endpoint);

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      outputs: [],
      ready,
      status: ready ? "Ready" : "Pending",
      valueState,
    });
  });

  it("serves a deliberate empty success without inventing value readiness", async () => {
    localStorage.setItem("aep:mock:component-dependency-status", "empty");

    const response = await fetch(endpoint);

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      outputs: [],
      ready: false,
      status: "unknown",
    });
  });

  it("falls back to the project scenario when nothing is overridden", async () => {
    // No localStorage key at all — the path every mock-mode page actually takes.
    // The explicit-override tests above all skip it, so a regression in the
    // scenario-derived branch would go unnoticed.
    const response = await fetch(endpoint);

    expect(response.status).toBe(200);
    const body = await response.json();
    expect(body).toMatchObject({ outputs: expect.any(Array) });
    expect(body).toHaveProperty("ready");
  });

  it("serves a deliberate typed error", async () => {
    localStorage.setItem("aep:mock:component-dependency-status", "error");

    const response = await fetch(endpoint);

    expect(response.status).toBe(500);
    expect(await response.json()).toEqual({
      code: "dependency_status_error",
      message: "Mock error loading component dependency status",
    });
  });
});

describe("external value save refreshes readiness", () => {
  const readinessEndpoint =
    "http://localhost/api/v1/projects/acme/dependencies/readiness";
  const valuesEndpoint =
    "http://localhost/api/v1/projects/acme/dependencies/external-resources/stripe/values";

  it("reflects a saved resource in the next readiness read, and its aggregate", async () => {
    const before = await (await fetch(readinessEndpoint)).json();
    const stripeBefore = before.dependencies.find(
      (d: { name: string }) => d.name === "stripe",
    );
    expect(stripeBefore?.state).not.toBe("configured");

    const saved = await fetch(valuesEndpoint, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ values: { API_KEY: "sk-live" } }),
    });
    expect(saved.status).toBe(200);

    // The write has to be visible to the READ — the mock is the dev-time
    // surface the Builds page drives, so a save that does not move readiness
    // would make the page look broken with no server involved.
    const after = await (await fetch(readinessEndpoint)).json();
    const stripeAfter = after.dependencies.find(
      (d: { name: string }) => d.name === "stripe",
    );
    expect(stripeAfter?.state).toBe("configured");
    expect(after.configured).toBe(
      after.dependencies.every(
        (d: { state: string }) => d.state === "configured",
      ),
    );
  });
});
