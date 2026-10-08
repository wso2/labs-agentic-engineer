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

import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import { environmentColumns, promotionOrder, type BoardInput } from "./pipeline";

type Environment = components["schemas"]["EnvironmentDTO"];

// Acme Expenses on a three-environment pipeline: v2 built and running in
// Development, the web app and its API; nothing promoted yet.

const env = (name: string, displayName: string, position: number, promotesTo?: string): Environment => ({
  name,
  displayName,
  position,
  isProduction: name === "production",
  validation: "off",
  ...(promotesTo ? { promotesTo } : {}),
});

const environments = [
  env("development", "Development", 0, "staging"),
  env("staging", "Staging", 1, "production"),
  env("production", "Production", 2),
];

const acme: BoardInput = {
  environments,
  components: [
    { name: "expense-webapp", displayName: "Expense web app", type: "web-application" },
    { name: "expense-api", type: "service" },
  ],
  deployments: [
    {
      componentName: "expense-webapp",
      environment: "development",
      status: "Ready",
      endpointUrl: "https://expense-webapp-dev.example",
      createdAt: "2026-10-03T10:58:00Z",
    },
    { componentName: "expense-api", environment: "development", status: "Ready", endpointUrl: "https://expense-api-dev.example" },
  ],
  deploy: { status: "deployed", version: "v2" },
  readiness: new Map([
    ["development", { configured: true, dependencies: [{ name: "Xero", state: "configured" as const, missingKeys: [] }] }],
    ["staging", { configured: false, dependencies: [{ name: "Xero", state: "unset" as const, missingKeys: ["clientSecret"] }] }],
  ]),
};

describe("promotionOrder", () => {
  it("puts the environments in the pipeline's order, whatever order they arrive in", () => {
    const shuffled = [environments[2]!, environments[0]!, environments[1]!];
    expect(promotionOrder(shuffled).map((e) => e.name)).toEqual(["development", "staging", "production"]);
  });
});

describe("environmentColumns", () => {
  it("draws a column per environment in promotion order, each naming the next", () => {
    const columns = environmentColumns({ ...acme, environments: [...environments].reverse() });
    expect(columns.map((c) => [c.label, c.next?.label ?? null])).toEqual([
      ["Development", "Staging"],
      ["Staging", "Production"],
      ["Production", null],
    ]);
    expect(columns.map((c) => c.entry)).toEqual([true, false, false]);
  });

  it("shows the version the newest build deployed in the first environment, with its components and their URLs", () => {
    const [dev] = environmentColumns(acme);
    expect(dev).toMatchObject({ version: "v2", running: true, state: { label: "Running", tone: "success" } });
    expect(dev?.components.map((c) => [c.displayName, c.url])).toEqual([
      ["Expense web app", "https://expense-webapp-dev.example"],
      ["expense-api", "https://expense-api-dev.example"],
    ]);
  });

  it("names no version and no components where nothing is deployed", () => {
    const [, staging] = environmentColumns(acme);
    expect(staging).toMatchObject({ version: null, running: false, components: [], state: { label: "Nothing running" } });
  });

  it("lists a component not deployed yet in the first environment, where a build lands", () => {
    const [dev] = environmentColumns({ ...acme, deployments: acme.deployments.slice(0, 1), deploy: undefined });
    expect(dev?.components.map((c) => [c.name, c.kind])).toEqual([
      ["expense-webapp", "ready"],
      ["expense-api", "not-deployed"],
    ]);
  });

  it("follows the deploy aggregate in the first environment and the deployments elsewhere", () => {
    expect(environmentColumns({ ...acme, deploy: { status: "deploying", version: "v3" } })[0]?.state.label).toBe("Deploying");
    const failing = environmentColumns({
      ...acme,
      deployments: [...acme.deployments, { componentName: "expense-api", environment: "staging", status: "ReleaseFailed" }],
    });
    expect(failing[1]?.state).toEqual({ label: "Deploy failed", tone: "error" });
  });

  it("says whether each dependency has its values, and waits for an environment's readiness", () => {
    const [dev, staging, production] = environmentColumns(acme);
    expect(dev?.dependencies?.map((d) => d.label)).toEqual(["Xero connected"]);
    expect(staging?.dependencies?.map((d) => d.label)).toEqual(["Xero needs values"]);
    expect(production?.dependencies).toBeNull();
  });
});
