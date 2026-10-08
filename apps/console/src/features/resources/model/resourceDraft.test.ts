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
import {
  cellKey,
  draftOfResource,
  isResourceDirty,
  promoteProblems,
  promoteRequest,
  resourceProblems,
  resourceRequest,
} from "./resourceDraft";

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

const environments = ["development", "production"];

const saved: ExternalResourceDTO = {
  name: "currency-service",
  provider: "Open Exchange Rates",
  description: "Live exchange rates",
  consumptionInstructions: "Cache rates for an hour.",
  scope: "org",
  config: [
    { key: "APP_ID", secret: true, description: "App ID" },
    { key: "BASE_URL", secret: false, description: "Where the API is" },
  ],
  consumers: [],
  contract: { type: "openapi", path: "currency-service/openapi.yaml" },
  resourceDocs: [{ type: "documentation", url: "https://docs.openexchangerates.org" }],
  envCells: [
    { environment: "development", key: "APP_ID", status: "configured" },
    { environment: "development", key: "BASE_URL", status: "configured", value: "https://dev.example" },
    { environment: "production", key: "APP_ID", status: "configured" },
    { environment: "production", key: "BASE_URL", status: "configured", value: "https://api.example" },
  ],
};

describe("a registered resource's draft", () => {
  it("opens from the record, its secrets blank", () => {
    const draft = draftOfResource(saved);
    expect(draft.values).toEqual({
      [cellKey("development", "APP_ID")]: "",
      [cellKey("development", "BASE_URL")]: "https://dev.example",
      [cellKey("production", "APP_ID")]: "",
      [cellKey("production", "BASE_URL")]: "https://api.example",
    });
    expect(isResourceDirty(draft, saved)).toBe(false);
  });

  it("is dirty once a field, a value or the contract changes, and not for whitespace", () => {
    const draft = draftOfResource(saved);
    expect(isResourceDirty({ ...draft, provider: "OXR" }, saved)).toBe(true);
    expect(isResourceDirty({ ...draft, provider: "Open Exchange Rates " }, saved)).toBe(false);
    expect(isResourceDirty({ ...draft, values: { ...draft.values, [cellKey("production", "APP_ID")]: "s3cret" } }, saved)).toBe(true);
    expect(isResourceDirty({ ...draft, contract: { ...draft.contract, url: "https://x/openapi.json" } }, saved)).toBe(true);
  });

  it("is dirty for New resource once anything is typed", () => {
    const draft = draftOfResource(null);
    expect(isResourceDirty(draft, null)).toBe(false);
    expect(isResourceDirty({ ...draft, name: "maps" }, null)).toBe(true);
  });
});

describe("what stops a Save", () => {
  it("lets a saved record keep its secrets by leaving them blank", () => {
    expect(resourceProblems(draftOfResource(saved), { saved, environments })).toBeNull();
  });

  it("asks a new record for every field, key and value", () => {
    const problems = resourceProblems(draftOfResource(null), { saved: null, environments });
    expect(problems).toMatchObject({
      name: "Required.",
      provider: "Required.",
      description: "Required.",
      consumptionInstructions: "Required.",
      keyRows: [{ key: "Required.", description: "Required." }],
    });
    const named = {
      ...draftOfResource(null),
      name: "maps",
      provider: "Mapbox",
      description: "Maps",
      consumptionInstructions: "Server side only.",
      keys: [{ key: "TOKEN", description: "Token", secret: true }],
    };
    expect(resourceProblems(named, { saved: null, environments })?.values).toEqual({
      [cellKey("development", "TOKEN")]: "Required.",
      [cellKey("production", "TOKEN")]: "Required.",
    });
    expect(resourceProblems({ ...named, name: "new" }, { saved: null, environments })?.name).toMatch(/taken/);
  });

  it("refuses a key twice and an empty file", () => {
    const draft = draftOfResource(saved);
    const twice = { ...draft, keys: [...draft.keys, { key: "APP_ID", description: "again", secret: true }] };
    expect(resourceProblems(twice, { saved, environments })?.keyRows[2]).toEqual({ key: "Each key once." });
    const empty = { ...draft, contract: { ...draft.contract, fileName: "openapi.yaml", content: "" } };
    expect(resourceProblems(empty, { saved, environments })?.contract).toBeDefined();
  });
});

describe("what a Save sends", () => {
  it("writes every value, keeps the resource docs and leaves the contract alone unless a new one is given", () => {
    const body = resourceRequest(draftOfResource(saved), { saved, environments });
    expect(body.envValues).toHaveLength(4);
    expect(body.resourceDocs).toEqual([{ type: "documentation", url: "https://docs.openexchangerates.org" }]);
    expect(body.contract).toBeUndefined();
    const withFile = { ...draftOfResource(saved), contract: { type: "openapi" as const, url: "https://x", fileName: "a.yaml", content: "openapi: 3.1.0" } };
    expect(resourceRequest(withFile, { saved, environments }).contract).toEqual({ type: "openapi", fileName: "a.yaml", content: "openapi: 3.1.0" });
  });
});

describe("Promote to organization", () => {
  const keys = [
    { key: "XERO_ID", secret: true },
    { key: "XERO_SECRET", secret: true },
    { key: "XERO_URL", secret: false },
  ];

  it("asks for instructions, and for every secret of an environment or none", () => {
    expect(promoteProblems({ consumptionInstructions: "", values: {} }, { keys, environments })).toEqual({
      consumptionInstructions: "Say how a project should use it.",
      environments: {},
    });
    const half = { consumptionInstructions: "Use it.", values: { [cellKey("production", "XERO_ID")]: "id" } };
    expect(promoteProblems(half, { keys, environments })?.environments).toHaveProperty("production");
    expect(promoteProblems({ consumptionInstructions: "Use it.", values: {} }, { keys, environments })).toBeNull();
  });

  it("sends only the values typed, the rest carried over from the project", () => {
    const draft = { consumptionInstructions: " Use it. ", values: { [cellKey("development", "XERO_URL")]: "https://dev" } };
    expect(promoteRequest(draft, { keys, environments })).toEqual({
      consumptionInstructions: "Use it.",
      envValues: [{ environment: "development", key: "XERO_URL", value: "https://dev" }],
    });
  });
});
