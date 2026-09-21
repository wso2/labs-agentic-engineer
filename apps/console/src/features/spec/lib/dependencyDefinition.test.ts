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
import { consumedAsLabel, parseDependencyDefinition } from "./dependencyDefinition";

const parsed = (file: unknown) => {
  const result = parseDependencyDefinition(JSON.stringify(file));
  if (!result.ok) throw new Error(result.message);
  return result.definition;
};

describe("consumedAsLabel", () => {
  it("says how the component talks to the system, from the contract's kind", () => {
    expect(consumedAsLabel("openapi")).toBe("REST API");
    expect(consumedAsLabel("graphql")).toBe("GraphQL");
    expect(consumedAsLabel("sdk")).toBe("SDK");
  });

  it("is empty while no contract settles it, and never invents a word", () => {
    expect(consumedAsLabel(undefined)).toBe("");
    expect(consumedAsLabel("asyncapi")).toBe("asyncapi");
  });
});

describe("parseDependencyDefinition", () => {
  it("reads the resource block as written", () => {
    expect(
      parsed({
        name: "currency-service",
        resource: {
          name: "currency-service",
          provider: "Open Exchange Rates",
          contract: { type: "openapi", path: "openapi.yaml", origin: "provider" },
        },
        provenance: { registry: "currency-service/openapi.yaml", readOn: "2026-09-16" },
      }).resource.contract,
    ).toEqual({ type: "openapi", path: "openapi.yaml", origin: "provider" });
  });

  it("says what is wrong with a file it cannot read", () => {
    expect(parseDependencyDefinition("{not json").ok).toBe(false);
    const bad = parseDependencyDefinition(JSON.stringify({ name: "x", resource: { name: "x", contract: { type: "openapi" } } }));
    expect(bad.ok).toBe(false);
  });
});

// Old repos still hold the flat file. The platform lifts it on read and the
// console renders the FILE, so it lifts the same way — or every definition
// written before the resource block reads as a parse error.
describe("parseDependencyDefinition — the retired flat shape", () => {
  it("lifts provider, description and config into the resource block", () => {
    const file = parsed({
      name: "dhl-courier",
      provider: "DHL",
      description: "Shipment tracking.",
      config: [{ key: "DHL_API_KEY", secret: true }],
    });
    expect(file.resource).toEqual({
      name: "dhl-courier",
      provider: "DHL",
      description: "Shipment tracking.",
      config: [{ key: "DHL_API_KEY", secret: true }],
    });
  });

  it("turns style plus the contract file name into the contract object", () => {
    expect(parsed({ name: "d", style: "rest-api", contract: "openapi.yaml" }).resource.contract)
      .toEqual({ type: "openapi", path: "openapi.yaml" });
    expect(parsed({ name: "d", style: "graphql", contract: "schema.graphql" }).resource.contract)
      .toEqual({ type: "graphql", path: "schema.graphql" });
    expect(parsed({ name: "d", style: "sdk", sdk: "sdk.json" }).resource.contract)
      .toEqual({ type: "sdk", path: "sdk.json" });
  });

  it("reads source org as a copy, under the dependency's own name", () => {
    expect(parsed({ name: "currency-service", source: "org", provider: "Open Exchange Rates" }).resource.ref)
      .toBe("currency-service");
  });

  it("reads the acceptance record as the contract's, and its origin as assumed", () => {
    expect(
      parsed({
        name: "d",
        style: "rest-api",
        contract: "openapi.yaml",
        assumed: { by: "admin", at: "2026-09-08T10:00:00Z" },
      }).resource.contract,
    ).toEqual({
      type: "openapi",
      path: "openapi.yaml",
      origin: "assumed",
      accepted: { by: "admin", at: "2026-09-08T10:00:00Z" },
    });
  });

  it("reads fetchedAt as readOn and drops sliced with the sliced contracts", () => {
    expect(
      parsed({ name: "d", provenance: { sourceUrl: "https://x/y", sliced: true, fetchedAt: "2026-09-08" } })
        .provenance,
    ).toEqual({ sourceUrl: "https://x/y", readOn: "2026-09-08" });
  });

  it("reads the retired candidates as suggestions", () => {
    expect(
      parsed({ name: "mail", candidates: [{ name: "sendgrid", style: "rest-api", package: "npm:x" }] }).suggestions,
    ).toEqual([{ name: "sendgrid", style: "rest-api" }]);
  });

  it("leaves a file that already has a resource block alone", () => {
    const file = parsed({
      name: "d",
      resource: { name: "d", provider: "DHL" },
      suggestions: undefined,
    });
    expect(file.resource.provider).toBe("DHL");
  });
});
