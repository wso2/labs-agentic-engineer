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
