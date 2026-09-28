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

import { test } from "node:test";
import assert from "node:assert/strict";
import type { ProjectDesign } from "../src/types.js";
import { toCellDiagramProject } from "../src/cell-diagram.js";
import { buildProjectDesign } from "../src/project-design.js";

const DESIGN: ProjectDesign = {
  modelVersion: "0.4.0",
  id: "expense-claim",
  name: "expense-claim",
  components: [
    {
      id: "expense-api",
      type: "service",
      version: "0.2.0",
      skillsPinned: ["architecture"],
      build: { language: "Go" },
      services: {
        "expense-api": {
          id: "expense-api",
          label: "expense-api",
          type: "http",
          deploymentMetadata: { gateways: { internet: { isExposed: true }, intranet: { isExposed: false } } },
        },
      },
      connections: [
        { id: "datastore://postgres", type: "datastore", onPlatform: true },
        { id: "connector://email-gateway", type: "connector", onPlatform: false },
      ],
      artifacts: {},
    },
    {
      id: "expense-webapp",
      type: "webapp",
      version: "0.1.0",
      skillsPinned: [],
      build: { language: "TypeScript" },
      connections: [{ id: "http://expense-api", type: "http", onPlatform: true }],
      artifacts: {},
    },
  ],
};

test("maps ProjectDesign to the cell-diagram Project shape (legacy buildProjectModel conventions)", () => {
  const p = toCellDiagramProject(DESIGN);
  assert.equal(p.id, "project");
  assert.equal(p.name, "expense-claim");
  assert.deepEqual(p.components.map((c) => c.id).sort(), ["expense-api", "expense-webapp"]);

  const web = p.components.find((c) => c.id === "expense-webapp")!;
  assert.equal(web.type, "web-app"); // cell diagram spells it web-app, not webapp
  assert.equal(web.buildPack, "TypeScript");
  // A webapp exposes a :web service whose dependencyIds point at sibling :api services.
  assert.deepEqual(web.services["expense-webapp:web"]!.dependencyIds, ["expense-api:api"]);
  // The sibling edge targets the in-project component.
  assert.deepEqual(web.connections, [
    { id: "default:project:expense-api", label: "expense-api", onPlatform: true },
  ]);

  const api = p.components.find((c) => c.id === "expense-api")!;
  assert.equal(api.type, "service");
  assert.equal(api.services["expense-api:api"]!.deploymentMetadata.gateways.internet.isExposed, true);
  // Non-component targets (datastore, external connector) group under external-apis.
  // The edge's type and on/off-platform signal must survive into the diagram model
  // (dropping them collapses every external kind into a generic East API — #164).
  assert.deepEqual(api.connections, [
    {
      id: "default:external-apis:postgres",
      label: "postgres",
      type: "datastore",
      onPlatform: true,
      tooltip: "datastore",
    },
    {
      id: "default:external-apis:email-gateway",
      label: "email-gateway",
      type: "connector",
      onPlatform: false,
      tooltip: "connector (off-platform)",
    },
  ]);
});

test("an ai-agent component renders as its own kind, not a plain service", () => {
  const design: ProjectDesign = {
    modelVersion: "0.4.0",
    id: "p",
    name: "p",
    components: [
      {
        id: "leave-agent",
        type: "ai-agent",
        version: "0.1.0",
        skillsPinned: [],
        build: {},
        connections: [],
        artifacts: {},
      },
    ],
  };

  const p = toCellDiagramProject(design);
  const node = p.components.find((c) => c.id === "leave-agent");
  assert.equal(node?.type, "ai-agent");
});

test("a platform-resource dependency carries type:datastore + onPlatform:true, not a plain external API", () => {
  const design: ProjectDesign = {
    modelVersion: "0.4.0",
    id: "p",
    name: "p",
    components: [
      {
        id: "orders-api",
        type: "service",
        version: "0.1.0",
        skillsPinned: [],
        build: {},
        services: {
          "orders-api": {
            id: "orders-api",
            label: "orders-api",
            type: "http",
            deploymentMetadata: { gateways: { internet: { isExposed: false }, intranet: { isExposed: true } } },
          },
        },
        // Stage 1's DEP_KIND_EDGE mapping for kind "platform-resource".
        connections: [{ id: "datastore://postgres", type: "datastore", onPlatform: true }],
        artifacts: {},
      },
    ],
  };

  const p = toCellDiagramProject(design);
  const api = p.components.find((c) => c.id === "orders-api")!;
  assert.deepEqual(api.connections, [
    {
      id: "default:external-apis:postgres",
      label: "postgres",
      type: "datastore",
      onPlatform: true,
      tooltip: "datastore",
    },
  ]);
  // The lib's isConnectorConnection fires on type "datastore"/"connector" — this is
  // what routes the node to the South bound instead of the generic East external API.
  assert.equal(api.connections[0]!.type, "datastore");
});

test("off-platform `external` deps are distinguished from on-platform `org-service` deps", () => {
  const design: ProjectDesign = {
    modelVersion: "0.4.0",
    id: "p",
    name: "p",
    components: [
      {
        id: "billing-api",
        type: "service",
        version: "0.1.0",
        skillsPinned: [],
        build: {},
        connections: [
          // org-service: another on-platform project's service.
          { id: "http://payments-service", type: "http", onPlatform: true },
          // external: an off-platform SaaS.
          { id: "http://stripe", type: "http", onPlatform: false },
        ],
        artifacts: {},
      },
    ],
  };

  const p = toCellDiagramProject(design);
  const api = p.components.find((c) => c.id === "billing-api")!;
  const orgService = api.connections.find((c) => c.label === "payments-service")!;
  const external = api.connections.find((c) => c.label === "stripe")!;
  assert.equal(orgService.onPlatform, true);
  assert.equal(external.onPlatform, false);
  // Both are HTTP edges — onPlatform, not type, is what distinguishes them.
  assert.equal(orgService.type, "http");
  assert.equal(external.type, "http");
});

test("end-to-end: an authored platform-resource dependency survives both projection stages as a datastore edge", () => {
  const bundle: Record<string, string> = {
    "specs/design/components/orders-api/design.json": JSON.stringify({
      name: "orders-api",
      type: "service",
      version: "0.1.0",
      language: "Go",
      buildpack: "docker",
      appPath: "orders-api",
      entrypoint: "deployment/service",
      exposure: "intranet",
      dependencies: [{ kind: "platform-resource", name: "postgres", resourceType: "postgres" }],
      description: "The orders API.",
    }),
  };

  const design = buildProjectDesign("orders", bundle);
  const p = toCellDiagramProject(design);
  const api = p.components.find((c) => c.id === "orders-api")!;
  assert.deepEqual(api.connections, [
    {
      id: "default:external-apis:postgres",
      label: "postgres",
      type: "datastore",
      onPlatform: true,
      tooltip: "datastore",
    },
  ]);
});
