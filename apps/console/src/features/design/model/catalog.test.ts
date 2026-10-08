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
import { designCatalog, registeredResourcesOf } from "./catalog";

// A real design: the per-feature expense-tracker design the design eval wrote
// (evals/spec-agents/scenarios/fixtures/expense-tracker-design), read as the
// room holds it.
const ROOT = "../../../../../../evals/spec-agents/scenarios/fixtures/expense-tracker-design/";
const raw = import.meta.glob<string>(
  "../../../../../../evals/spec-agents/scenarios/fixtures/expense-tracker-design/specs/**/*.{md,json,yaml,cell,dsl,feature}",
  { query: "?raw", import: "default", eager: true },
);
const files = Object.fromEntries(Object.entries(raw).map(([path, content]) => [path.slice(ROOT.length), content]));

describe("the design catalog, from a design's files", () => {
  const catalog = designCatalog(files);

  it("titles the wireframe viewer Wireframes, so it is not mistaken for the Prototype tab", () => {
    expect(catalog.find((a) => a.source.kind === "prototype")?.title).toBe("Wireframes: expense-webapp");
  });

  it("lists what the design yields, business first, then technical, then the acceptance criteria", () => {
    expect(catalog.map((a) => [a.id, a.depth, a.source.kind])).toEqual([
      ["prototype-expense-webapp", "business", "prototype"],
      ["flow-claim-decision", "business", "document"],
      ["roles", "business", "roles"],
      ["data", "business", "document"],
      ["architecture", "technical", "architecture"],
      ["expense-api", "technical", "contract"],
      ["expense-webapp", "technical", "contract"],
      ["security", "technical", "security"],
      ["acceptance-F1", "business", "acceptance"],
      ["acceptance-F2", "business", "acceptance"],
      ["acceptance-F3", "business", "acceptance"],
    ]);
  });

  it("covers the features whose stories each serves", () => {
    expect(catalog.find((a) => a.id === "expense-api")?.features).toEqual(["F1", "F2", "F3"]);
    expect(catalog.find((a) => a.id === "acceptance-F2")?.features).toEqual(["F2"]);
  });

  it("says who can do what from the roles' grants", () => {
    const roles = catalog.find((a) => a.id === "roles")?.source;
    expect(roles?.kind === "roles" && roles.roles.length > 0 && roles.rows.some((r) => r.grants.some(Boolean))).toBe(true);
  });

  it("has no prototype for a product with no web app", () => {
    const apiOnly = Object.fromEntries(Object.entries(files).filter(([p]) => !p.includes("expense-webapp")));
    expect(designCatalog(apiOnly).some((a) => a.source.kind === "prototype")).toBe(false);
  });
});

describe("the organization's resources a component reuses", () => {
  const design = JSON.stringify({
    dependencies: [
      { kind: "external", name: "currency-service" },
      { kind: "external", name: "payroll-service" },
      { kind: "platform-resource", name: "orders-db" },
    ],
  });
  const files = {
    "specs/design/dependencies/currency-service/dependency.json": JSON.stringify({
      name: "currency-service",
      resource: { ref: "currency-service", name: "currency-service" },
    }),
    "specs/design/dependencies/payroll-service/dependency.json": JSON.stringify({
      name: "payroll-service",
      resource: { name: "payroll-service", provider: "Xero" },
    }),
  };

  it("are the external dependencies whose resource is a copy of a registered one", () => {
    expect(registeredResourcesOf(files, design)).toEqual(["currency-service"]);
  });
});
