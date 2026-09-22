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
import { buildProjectDesign } from "../src/project-design.js";

const BUNDLE: Record<string, string> = {
  "specs/design/components/expense-api/design.json": JSON.stringify({
    name: "expense-api",
    type: "service",
    version: "0.2.0",
    language: "Go",
    buildpack: "docker",
    appPath: "expense-api",
    entrypoint: "deployment/service",
    exposure: "internet",
    skillsPinned: ["architecture", "openapi-conventions"],
    dependencies: [
      { kind: "platform-resource", name: "postgres", resourceType: "postgres" },
      { kind: "external", name: "email-gateway" },
    ],
    description: "The API service.",
  }),
  "specs/design/components/expense-api/openapi.yaml": "openapi: 3.0.3\n",
  "specs/design/components/expense-webapp/design.json": JSON.stringify({
    name: "expense-webapp",
    type: "webapp",
    version: "0.1.0",
    language: "TypeScript",
    buildpack: "docker",
    appPath: "expense-webapp",
    entrypoint: "deployment/webapp",
    exposure: "intranet",
    dependencies: [{ kind: "component", name: "expense-api" }],
    description: "The webapp.",
  }),
  "specs/design/components/expense-webapp/prototype.json": "{}\n",
  "specs/design/components/expense-webapp/wireframes.dsl": "screen A\n",
};

test("projects the bundle into the cell-diagram-compatible design json", () => {
  const dj = buildProjectDesign("expense-claim", BUNDLE);
  assert.equal(dj.name, "expense-claim");
  assert.equal(dj.modelVersion, "0.4.0");
  assert.deepEqual(dj.components.map((c) => c.id).sort(), ["expense-api", "expense-webapp"]);

  const api = dj.components.find((c) => c.id === "expense-api")!;
  assert.equal(api.type, "service");
  assert.equal(api.version, "0.2.0");
  assert.deepEqual(api.skillsPinned, ["architecture", "openapi-conventions"]);
  assert.equal(api.build.language, "Go");
  assert.equal(api.services!["expense-api"]!.deploymentMetadata.gateways.internet.isExposed, true);
  assert.deepEqual(api.connections, [
    { id: "datastore://postgres", type: "datastore", onPlatform: true },
    { id: "http://email-gateway", type: "http", onPlatform: false },
  ]);
  assert.equal(api.artifacts.openapi, "specs/design/components/expense-api/openapi.yaml");

  const web = dj.components.find((c) => c.id === "expense-webapp")!;
  assert.equal(web.type, "webapp");
  assert.equal(web.version, "0.1.0");
  assert.deepEqual(web.skillsPinned, []); // no skillsPinned authored for this component
  assert.equal(web.services, undefined); // webapps expose no services
  assert.deepEqual(web.connections, [{ id: "http://expense-api", type: "http", onPlatform: true }]);
  assert.equal(web.artifacts.prototype, "specs/design/components/expense-webapp/prototype.json");
  // A web-application's artifact is its prototype; a leftover wireframes.dsl
  // is not projected.
  assert.equal(web.artifacts.wireframes, undefined);
});



test("a bundle without a design tree projects to an empty component list", () => {
  const dj = buildProjectDesign("empty", { "requirements.md": "# hi\n" });
  assert.deepEqual(dj.components, []);
});

test("a non-standard component type flows through the projection untouched", () => {
  const files = {
    "specs/design/components/report-worker/design.json": JSON.stringify({
      name: "report-worker",
      type: "scheduled-task",
      version: "0.1.0",
      language: "Go",
      buildpack: "docker",
      appPath: "report-worker",
      entrypoint: "deployment/task",
      exposure: "intranet",
      dependencies: [],
      description: "Nightly report generation.",
    }),
  };
  const dj = buildProjectDesign("p", files);
  assert.equal(dj.components[0]!.type, "scheduled-task");
  assert.equal(dj.components[0]!.services, undefined); // only "service" exposes a service entry
});
