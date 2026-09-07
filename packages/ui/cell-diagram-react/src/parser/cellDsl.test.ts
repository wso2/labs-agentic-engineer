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
import { compileCellSource } from "../compiler/compileCellSource";
import { defaultSampleSource } from "../test/defaultSample";
import { parseCellDsl } from "./parseCellDsl";

const orderSystemSource = `title OrderCell
version v1

component WebApp web-app
component OrderAPI api
component OrderService service
component OrderDB database
component EventPublisher event

north CustomerApp -> WebApp : HTTPS
west AdminPortal -> OrderAPI : backoffice

WebApp -> OrderAPI
OrderAPI -> OrderService
OrderService -> OrderDB
OrderService -> EventPublisher : order.created

OrderService -> east InventoryAPI : reserve stock
OrderService -> south Stripe : payment`;

describe("parseCellDsl", () => {
  it("keeps metadata optional", () => {
    const result = parseCellDsl(`component API service
north -> API`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.title).toBeUndefined();
    expect(result.document.version).toBeUndefined();
  });

  it("parses component declarations without type labels", () => {
    const result = parseCellDsl(`component usersAPI
component api as OrderAPI

north -> usersAPI`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.components).toEqual([
      { id: "usersAPI", label: undefined, type: undefined, line: 1 },
      { id: "api", label: "OrderAPI", type: undefined, line: 2 }
    ]);
  });

  it("parses metadata, components, internal dependencies, and boundary dependencies", () => {
    const result = parseCellDsl(orderSystemSource);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.title).toBe("OrderCell");
    expect(result.document.version).toBe("v1");
    expect(result.document.components).toHaveLength(5);
    expect(result.document.edges).toEqual([
      {
        id: "north-CustomerApp-WebApp-10",
        source: "CustomerApp",
        target: "WebApp",
        direction: "north",
        kind: "inbound",
        label: "HTTPS",
        line: 10
      },
      {
        id: "west-AdminPortal-OrderAPI-11",
        source: "AdminPortal",
        target: "OrderAPI",
        direction: "west",
        kind: "inbound",
        label: "backoffice",
        line: 11
      },
      {
        id: "internal-WebApp-OrderAPI-13",
        source: "WebApp",
        target: "OrderAPI",
        direction: "internal",
        kind: "internal",
        label: undefined,
        line: 13
      },
      {
        id: "internal-OrderAPI-OrderService-14",
        source: "OrderAPI",
        target: "OrderService",
        direction: "internal",
        kind: "internal",
        label: undefined,
        line: 14
      },
      {
        id: "internal-OrderService-OrderDB-15",
        source: "OrderService",
        target: "OrderDB",
        direction: "internal",
        kind: "internal",
        label: undefined,
        line: 15
      },
      {
        id: "internal-OrderService-EventPublisher-16",
        source: "OrderService",
        target: "EventPublisher",
        direction: "internal",
        kind: "internal",
        label: "order.created",
        line: 16
      },
      {
        id: "east-OrderService-InventoryAPI-18",
        source: "OrderService",
        target: "InventoryAPI",
        direction: "east",
        kind: "outbound",
        label: "reserve stock",
        line: 18
      },
      {
        id: "south-OrderService-Stripe-19",
        source: "OrderService",
        target: "Stripe",
        direction: "south",
        kind: "outbound",
        label: "payment",
        line: 19
      }
    ]);
  });

  it("reports line and column diagnostics for duplicate components and unknown syntax", () => {
    const result = parseCellDsl(`title Broken
component API service
component API service
API -- DB`);

    expect(result.diagnostics).toEqual([
      {
        severity: "error",
        message: "Component \"API\" is already defined.",
        line: 3,
        column: 11
      },
      {
        severity: "error",
        message: "Unknown statement. Expected title, version, component, or dependency arrow.",
        line: 4,
        column: 1
      }
    ]);
  });

  it("skips a leading YAML frontmatter block, preserving line numbers", () => {
    const result = parseCellDsl(`---
sourceSpec: v3
---
title Fronted

component API service
API -- DB`);

    expect(result.document.title).toBe("Fronted");
    expect(result.document.components).toHaveLength(1);
    expect(result.diagnostics).toEqual([
      {
        severity: "error",
        message: "Unknown statement. Expected title, version, component, or dependency arrow.",
        line: 7,
        column: 1
      }
    ]);
  });

  it("parses boundary gateway exposure arrows", () => {
    const result = parseCellDsl(`title UntitledCell

component API service

west -> API
API -> south`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.edges).toEqual([
      {
        id: "west-west-API-5",
        source: "west",
        target: "API",
        direction: "west",
        kind: "exposure",
        label: undefined,
        line: 5
      },
      {
        id: "south-API-south-6",
        source: "API",
        target: "south",
        direction: "south",
        kind: "exposure",
        label: undefined,
        line: 6
      }
    ]);
  });

  it("rejects reserved keywords used as a component id", () => {
    const result = parseCellDsl(`component north service`);

    expect(result.document.components).toEqual([]);
    expect(result.diagnostics).toEqual([
      {
        severity: "error",
        message: '"north" is a reserved keyword and cannot be used as a component id.',
        line: 1,
        column: 11
      }
    ]);
  });

  it("rejects reserved keywords used as an external id", () => {
    const result = parseCellDsl(`north as api`);

    expect(result.document.externals).toEqual([]);
    expect(result.diagnostics).toEqual([
      {
        severity: "error",
        message: '"as" is a reserved keyword and cannot be used as an external id.',
        line: 1,
        column: 7
      }
    ]);
  });

  it("still allows reserved keywords as labels", () => {
    const result = parseCellDsl(`component api as component`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.components).toEqual([{ id: "api", label: "component", type: undefined, line: 1 }]);
  });

  it("parses aliased components and predeclared boundary externals", () => {
    const result = parseCellDsl(`component WebApp web-app
component odb as OrderDB database

north CustomerApp
west ap as AdminPortal webapp
south CustomerDB database
east inv as InventoryAPI`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.components).toEqual([
      { id: "WebApp", type: "web-app", label: undefined, line: 1 },
      { id: "odb", type: "database", label: "OrderDB", line: 2 }
    ]);
    expect(result.document.externals).toEqual([
      { id: "CustomerApp", direction: "north", label: undefined, type: undefined, line: 4 },
      { id: "ap", direction: "west", label: "AdminPortal", type: "webapp", line: 5 },
      { id: "CustomerDB", direction: "south", label: undefined, type: "database", line: 6 },
      { id: "inv", direction: "east", label: "InventoryAPI", type: undefined, line: 7 }
    ]);
  });

  it("parses quoted multi-word labels for components and externals", () => {
    const result = parseCellDsl(`component ldb as "Datastore" database
component odb as "Order Datastore"
south adb as "Azure Postgre" database
east inv as "Inventory API"`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.components).toEqual([
      { id: "ldb", label: "Datastore", type: "database", line: 1 },
      { id: "odb", label: "Order Datastore", type: undefined, line: 2 }
    ]);
    expect(result.document.externals).toEqual([
      { id: "adb", direction: "south", label: "Azure Postgre", type: "database", line: 3 },
      { id: "inv", direction: "east", label: "Inventory API", type: undefined, line: 4 }
    ]);
  });

  it("keeps the unquoted multi-word heuristic for backward compatibility", () => {
    const result = parseCellDsl(`component ldb as Datastore database
south adb as Azure Postgre database`);

    expect(result.diagnostics).toEqual([]);
    expect(result.document.components).toEqual([{ id: "ldb", label: "Datastore", type: "database", line: 1 }]);
    expect(result.document.externals).toEqual([
      { id: "adb", direction: "south", label: "Azure Postgre", type: "database", line: 2 }
    ]);
  });
});

describe("compileCellSource", () => {
  it("compiles the bundled default sample", () => {
    const result = compileCellSource(defaultSampleSource);

    expect(result.diagnostics).toEqual([]);
    expect(result.model?.components.map((component) => component.id)).toEqual(["WebApp", "orders", "odb", "ep"]);
    expect(result.model?.externals.map((external) => `${external.direction}:${external.id}`)).toEqual([
      "north:ca",
      "north:pp",
      "west:ap",
      "east:inventories",
      "east:customers",
      "south:Stripe",
      "south:SendGrid"
    ]);
  });

  it("creates a normalized model and external nodes for the order system sample", () => {
    const result = compileCellSource(orderSystemSource);

    expect(result.diagnostics).toEqual([]);
    expect(result.model).toMatchObject({
      title: "OrderCell",
      version: "v1"
    });
    expect(result.model?.components.map((component) => component.id)).toEqual([
      "WebApp",
      "OrderAPI",
      "OrderService",
      "OrderDB",
      "EventPublisher"
    ]);
    expect(result.model?.externals.map((external) => `${external.direction}:${external.id}`)).toEqual([
      "north:CustomerApp",
      "west:AdminPortal",
      "east:InventoryAPI",
      "south:Stripe"
    ]);
  });

  it("infers internal components from dependency usage", () => {
    const result = compileCellSource(`north -> usersAPI
WebApp -> OrderAPI
north Customer -> WebApp
OrderAPI -> south Stripe`);

    expect(result.diagnostics).toEqual([]);
    expect(result.model?.components).toEqual([
      { id: "usersAPI" },
      { id: "WebApp" },
      { id: "OrderAPI" }
    ]);
    expect(result.model?.externals.map((external) => `${external.direction}:${external.id}`)).toEqual([
      "north:Customer",
      "south:Stripe"
    ]);
    expect(result.model?.edges).toEqual([
      expect.objectContaining({
        source: "north",
        target: "usersAPI",
        direction: "north",
        kind: "exposure"
      }),
      expect.objectContaining({
        source: "WebApp",
        target: "OrderAPI",
        direction: "internal",
        kind: "internal"
      }),
      expect.objectContaining({
        source: "Customer",
        target: "WebApp",
        direction: "north",
        kind: "inbound"
      }),
      expect.objectContaining({
        source: "OrderAPI",
        target: "Stripe",
        direction: "south",
        kind: "outbound"
      })
    ]);
  });

  it("normalizes predeclared external dependencies with inferred internal components", () => {
    const result = compileCellSource(`north CustomerApp webapp
east InventoryAPI api

CustomerApp -> WebApp : HTTPS
OrderAPI -> InventoryAPI : reserve stock`);

    expect(result.diagnostics).toEqual([]);
    expect(result.model?.components).toEqual([{ id: "WebApp" }, { id: "OrderAPI" }]);
    expect(result.model?.externals).toEqual([
      { id: "CustomerApp", direction: "north", label: undefined, type: "webapp", line: 1 },
      { id: "InventoryAPI", direction: "east", label: undefined, type: "api", line: 2 }
    ]);
    expect(result.model?.edges).toEqual([
      expect.objectContaining({
        source: "CustomerApp",
        target: "WebApp",
        direction: "north",
        kind: "inbound",
        label: "HTTPS"
      }),
      expect.objectContaining({
        source: "OrderAPI",
        target: "InventoryAPI",
        direction: "east",
        kind: "outbound",
        label: "reserve stock"
      })
    ]);
  });

  it("compiles gateway exposures without creating external nodes", () => {
    const result = compileCellSource(`component usersAPI

north -> usersAPI
usersAPI -> east`);

    expect(result.diagnostics).toEqual([]);
    expect(result.model?.components).toEqual([{ id: "usersAPI", label: undefined, type: undefined, line: 1 }]);
    expect(result.model?.externals).toEqual([]);
    expect(result.model?.edges).toEqual([
      expect.objectContaining({
        source: "north",
        target: "usersAPI",
        direction: "north",
        kind: "exposure"
      }),
      expect.objectContaining({
        source: "usersAPI",
        target: "east",
        direction: "east",
        kind: "exposure"
      })
    ]);
  });

  it("rejects boundary arrows that violate inbound and outbound cell architecture directions", () => {
    const result = compileCellSource(`component usersAPI api

usersAPI -> north
usersAPI -> west
east -> usersAPI
south -> usersAPI
east InventoryAPI -> usersAPI
usersAPI -> north CustomerApp`);

    expect(result.model).toBeNull();
    expect(result.diagnostics).toEqual([
      {
        severity: "error",
        message: "North boundary connections must flow into the cell. Use \"north -> usersAPI\".",
        line: 3,
        column: 1
      },
      {
        severity: "error",
        message: "West boundary connections must flow into the cell. Use \"west -> usersAPI\".",
        line: 4,
        column: 1
      },
      {
        severity: "error",
        message: "East boundary connections must flow out of the cell. Use \"usersAPI -> east\".",
        line: 5,
        column: 1
      },
      {
        severity: "error",
        message: "South boundary connections must flow out of the cell. Use \"usersAPI -> south\".",
        line: 6,
        column: 1
      },
      {
        severity: "error",
        message: "East boundary connections must flow out of the cell.",
        line: 7,
        column: 1
      },
      {
        severity: "error",
        message: "North boundary connections must flow into the cell.",
        line: 8,
        column: 1
      }
    ]);
  });

  it("uses declarations to resolve aliases and plain arrows involving externals", () => {
    const result = compileCellSource(`component WebApp web-app
component api as OrderAPI api
component odb as OrderDB database

north CustomerApp webapp
east inv as InventoryAPI api

CustomerApp -> WebApp : HTTPS
WebApp -> api
api -> odb
api -> inv : reserve stock`);

    expect(result.diagnostics).toEqual([]);
    expect(result.model?.title).toBeUndefined();
    expect(result.model?.components).toEqual([
      { id: "WebApp", type: "web-app", label: undefined, line: 1 },
      { id: "api", type: "api", label: "OrderAPI", line: 2 },
      { id: "odb", type: "database", label: "OrderDB", line: 3 }
    ]);
    expect(result.model?.externals).toEqual([
      { id: "CustomerApp", direction: "north", label: undefined, type: "webapp", line: 5 },
      { id: "inv", direction: "east", label: "InventoryAPI", type: "api", line: 6 }
    ]);
    expect(result.model?.edges).toEqual([
      expect.objectContaining({
        source: "CustomerApp",
        target: "WebApp",
        direction: "north",
        kind: "inbound",
        label: "HTTPS"
      }),
      expect.objectContaining({
        source: "WebApp",
        target: "api",
        direction: "internal",
        kind: "internal"
      }),
      expect.objectContaining({
        source: "api",
        target: "odb",
        direction: "internal",
        kind: "internal"
      }),
      expect.objectContaining({
        source: "api",
        target: "inv",
        direction: "east",
        kind: "outbound",
        label: "reserve stock"
      })
    ]);
  });
});

// PRD phasing and story citations left the cell grammar: both retired forms
// must read as visible errors, never silently parse into something else.
describe("removed phase/stories syntax", () => {
  it("rejects a phase statement as unknown", () => {
    const { diagnostics } = parseCellDsl("phase 1\ncomponent api service");
    expect(diagnostics.some((d) => d.severity === "error" && d.line === 1)).toBe(true);
  });

  it("rejects a component story-citation suffix", () => {
    const { document, diagnostics } = parseCellDsl("component api service [stories: 1, 2]");
    expect(diagnostics.some((d) => d.severity === "error" && /design\.json/.test(d.message))).toBe(true);
    expect(document.components).toHaveLength(0);
  });
});
