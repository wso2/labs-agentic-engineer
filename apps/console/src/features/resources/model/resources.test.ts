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
  addressedKind,
  deleteBlocker,
  filterResources,
  findResource,
  promoteBlocker,
  resourceAddress,
  resourceEntries,
  resourceSearch,
} from "./resources";

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

const rates: ExternalResourceDTO = {
  name: "currency-service",
  provider: "Open Exchange Rates",
  description: "Live exchange rates",
  scope: "org",
  config: [{ key: "APP_ID", secret: true, description: "App ID" }],
  consumers: [],
};
const xero = (project: string): ExternalResourceDTO => ({
  name: "payroll-service",
  provider: "Xero",
  description: "Payroll",
  scope: "project",
  project,
  config: [{ key: "XERO_TOKEN", secret: true }],
  consumers: [],
});

const entries = resourceEntries({
  platform: [{ name: "postgres-cnpg", description: "A Postgres database" }],
  external: [rates, xero("equipment-loans"), xero("acme-expenses")],
  endpoints: [{ name: "payroll-service", project: "hr-core", endpoint: "api", type: "HTTP", namespaceVisible: true }],
});

describe("the Resources Page's one list", () => {
  it("holds every kind, by name, a project's own resources each with its project", () => {
    expect(entries.map((e) => [e.kind, e.name, "project" in e ? e.project : null])).toEqual([
      ["registered", "currency-service", null],
      ["project", "payroll-service", "acme-expenses"],
      ["project", "payroll-service", "equipment-loans"],
      ["endpoint", "payroll-service", "hr-core"],
      ["platform", "postgres-cnpg", null],
    ]);
  });

  it("filters by kind: External is the registered and the project-held, From projects the endpoints", () => {
    expect(filterResources(entries, { query: "", filter: "platform" }).map((e) => e.name)).toEqual(["postgres-cnpg"]);
    expect(filterResources(entries, { query: "", filter: "external" }).map((e) => e.kind)).toEqual(["registered", "project", "project"]);
    expect(filterResources(entries, { query: "", filter: "endpoints" }).map((e) => e.kind)).toEqual(["endpoint"]);
  });

  it("searches the name, provider, description and project", () => {
    expect(filterResources(entries, { query: "open exchange", filter: "all" }).map((e) => e.name)).toEqual(["currency-service"]);
    expect(filterResources(entries, { query: "equipment", filter: "all" }).map((e) => e.kind)).toEqual(["project"]);
    expect(filterResources(entries, { query: "postgres", filter: "external" })).toEqual([]);
  });

  it("reads an external resource with no scope by its org values", () => {
    const [legacy] = resourceEntries({
      platform: [],
      external: [{ name: "old", project: "p", config: [], consumers: [], envCells: [{ environment: "dev", key: "K", status: "unset" }] }],
      endpoints: [],
    });
    expect(legacy?.kind).toBe("registered");
  });
});

describe("a Resource card's address", () => {
  it("names the kind and project wherever the name alone could collide, and round-trips", () => {
    for (const entry of entries) {
      const { name, search } = resourceAddress(entry);
      expect(findResource(entries, name, resourceSearch({ ...search }))).toBe(entry);
    }
    expect(resourceAddress(entries[0]!).search).toEqual({});
    expect(resourceAddress(entries[1]!).search).toEqual({ project: "acme-expenses" });
  });

  it("drops what the route does not know, and reads the kind from what is left", () => {
    expect(resourceSearch({ kind: "nonsense", project: "", extra: 1 })).toEqual({});
    expect(addressedKind({})).toBe("registered");
    expect(addressedKind({ project: "acme-expenses" })).toBe("project");
    expect(addressedKind({ kind: "endpoint", project: "hr-core" })).toBe("endpoint");
  });

  it("finds nothing for a resource the organization does not have", () => {
    expect(findResource(entries, "payroll-service", {})).toBeUndefined();
    expect(findResource(entries, "payroll-service", { project: "nowhere" })).toBeUndefined();
  });
});

describe("promote and delete", () => {
  it("promotes a project's resource that names its provider and keys, under a name the organization has free", () => {
    expect(promoteBlocker(xero("acme-expenses"), new Set(["currency-service"]))).toBeNull();
    expect(promoteBlocker(xero("acme-expenses"), new Set(["payroll-service"]))).toMatch(/already registered/);
    expect(promoteBlocker({ ...xero("p"), provider: "" }, new Set())).toMatch(/provider/);
    expect(promoteBlocker({ ...xero("p"), config: [] }, new Set())).toMatch(/no keys/);
  });

  it("keeps a registered resource a component uses", () => {
    expect(deleteBlocker(rates)).toBeNull();
    expect(deleteBlocker({ ...rates, consumers: [{ projectId: "p", componentName: "api" }] })).toMatch(/A component uses it/);
  });
});
