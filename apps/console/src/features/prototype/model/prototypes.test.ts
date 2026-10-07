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
import type { DesignArtifact } from "../../design/api/designModel";
import { appPrototypes, manifestPath, revisingIn, sourcePath, webApplications } from "./prototypes";

const manifest = JSON.stringify({
  schemaVersion: 3,
  name: "Expenses",
  entryScreen: "screen.home",
  roles: [{ id: "employee", name: "Employee" }],
  states: [{ id: "state.default", name: "Default" }],
  screens: [{ id: "screen.home", name: "Home", roleIds: ["employee"] }],
  flows: [],
});
const source = 'import { defineApp } from "@wso2/prototype-kit";\nexport default defineApp({ screens: {} });\n';

function contract(id: string, design: object): DesignArtifact {
  return {
    id,
    title: `Component and contract: ${id}`,
    depth: "technical",
    features: [],
    addedIn: 1,
    changedIn: 1,
    source: { kind: "contract", design: JSON.stringify(design), openapi: null },
  };
}

describe("webApplications", () => {
  it("names each component whose design is a web application, and nothing else", () => {
    const artifacts = [
      contract("expense-web", { name: "expense-web", type: "web-application" }),
      contract("expense-api", { name: "expense-api", type: "service" }),
      contract("admin-web", { type: "web-application" }),
      { ...contract("broken", {}), source: { kind: "contract" as const, design: "{", openapi: null } },
    ];
    expect(webApplications(artifacts)).toEqual(["admin-web", "expense-web"]);
  });

  it("is empty for an API-only product", () => {
    expect(webApplications([contract("api", { type: "service" })])).toEqual([]);
  });
});

describe("appPrototypes", () => {
  const both = { [manifestPath("expense-web")]: manifest, [sourcePath("expense-web")]: source };

  it("is none before either file is written", () => {
    expect(appPrototypes(["expense-web"], {}, null)).toEqual([
      { component: "expense-web", status: "none", problem: null, files: null, exists: false },
    ]);
  });

  it("is ready once the manifest parses and the source is there", () => {
    const [p] = appPrototypes(["expense-web"], both, null);
    expect(p).toMatchObject({ status: "ready", problem: null, exists: true });
    expect(p!.files).toMatchObject({ manifestText: manifest, source, manifest: { name: "Expenses", entryScreen: "screen.home" } });
  });

  it.each([
    ["the manifest is missing", { [sourcePath("expense-web")]: source }, /prototype\.json is missing/],
    ["the source is missing", { [manifestPath("expense-web")]: manifest }, /prototype\.tsx is missing/],
    ["the manifest is not JSON", { ...both, [manifestPath("expense-web")]: "{" }, /prototype\.json is invalid: .*not valid JSON/],
    ["the manifest breaks the schema", { ...both, [manifestPath("expense-web")]: JSON.stringify({ schemaVersion: 3 }) }, /prototype\.json is invalid/],
  ])("is invalid, with the reason, when %s", (_, files, reason) => {
    const [p] = appPrototypes(["expense-web"], files, null);
    expect(p).toMatchObject({ status: "invalid", files: null, exists: true });
    expect(p!.problem).toMatch(reason);
  });

  it("is revising while a /prototype turn for it runs, still showing the last version", () => {
    const [p] = appPrototypes(["expense-web"], both, revisingIn("/prototype expense-web"));
    expect(p).toMatchObject({ status: "revising", exists: true });
    expect(p!.files).not.toBeNull();
  });

  it("revises every web application on a bare /prototype, and only the named one otherwise", () => {
    const statuses = (instruction: string) => appPrototypes(["admin-web", "expense-web"], {}, revisingIn(instruction)).map((p) => p.status);
    expect(statuses("/prototype")).toEqual(["revising", "revising"]);
    expect(statuses("/prototype admin-web")).toEqual(["revising", "none"]);
    expect(statuses("/design F1")).toEqual(["none", "none"]);
  });
});
