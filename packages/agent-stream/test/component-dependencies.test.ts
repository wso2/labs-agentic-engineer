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

/**
 * Write-gate behavior for a component's dependencies against the cell — the
 * design's source of truth (ADR-0020): a dependency the cell does not declare
 * is refused while the agent can still add the node.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkComponentDependencies } from "../src/component-dependencies.ts";
import { FileBundle } from "../src/bundle.ts";

const CELL = `title Expense claims

component expense-webapp as "Expense Tracker" web-application
component expense-api as "Expense API" service

east user-auth as "Thunder Auth" identity-server

north -> expense-webapp
expense-webapp -> expense-api
expense-api -> user-auth
`;

const API = "specs/design/components/expense-api/design.json";

const design = (deps: unknown[]) =>
  JSON.stringify(
    {
      name: "expense-api",
      type: "service",
      version: "0.1.0",
      language: "Ballerina",
      buildpack: "docker",
      appPath: "expense-api",
      entrypoint: "deployment/service",
      exposure: "intranet",
      description: "Owns claims.",
      dependencies: deps,
    },
    null,
    2,
  );

const bundle = (cell: string | null = CELL) =>
  new FileBundle(cell === null ? {} : { "specs/design/design.cell": cell });

test("dependencies that are cell nodes pass — a thunder-app on the east edge included", () => {
  const ok = design([
    { kind: "platform-resource", name: "user-auth", resourceType: "thunder-app" },
    { kind: "component", name: "expense-webapp" },
  ]);
  assert.equal(checkComponentDependencies(API, ok, bundle()), null);
});

test("the database found during enrichment and never drawn is refused, with the cell statement to add", () => {
  // The exact shape from employees-submit-expense-iopll: expense-db in the
  // design.json, absent from the cell, absent from the Architecture diagram.
  const bad = design([{ kind: "platform-resource", name: "expense-db", resourceType: "postgres-cnpg" }]);
  const p = checkComponentDependencies(API, bad, bundle());
  assert.equal(p?.code, "UNKNOWN_DEPENDENCY");
  assert.match(p!.message, /`expense-db` \(platform-resource\)/);
  assert.match(p!.message, /component expense-db as "…" database/);
  assert.match(p!.message, /components: expense-webapp, expense-api; boundary externals: user-auth/);
});

test("each kind is pointed at the boundary it belongs on", () => {
  const bad = design([
    { kind: "org-service", name: "payroll-core" },
    { kind: "external", name: "slack", style: "rest-api" },
  ]);
  const p = checkComponentDependencies(API, bad, bundle());
  assert.match(p!.message, /`payroll-core` \(org-service\) — declare it as another project's service: `east payroll-core/);
  assert.match(p!.message, /`slack` \(external\) — declare it as a third-party system: `south slack/);
});

test("no dependencies, another path, or a document the schema gate owns: not judged", () => {
  assert.equal(checkComponentDependencies(API, design([]), bundle()), null);
  assert.equal(checkComponentDependencies("specs/design/domain-model.md", "{}", bundle()), null);
  assert.equal(checkComponentDependencies(API, "{ not json", bundle()), null);
});

test("dependencies before the cell exists are refused until the cell is written", () => {
  const p = checkComponentDependencies(API, design([{ kind: "component", name: "expense-webapp" }]), bundle(null));
  assert.equal(p?.code, "UNKNOWN_DEPENDENCY");
  assert.match(p!.message, /design.cell is not in the bundle yet/);
});

test("FileBundle: the rejected design.json leaves the bundle unchanged; adding the node to the cell unblocks it", () => {
  const b = bundle();
  const withDb = design([{ kind: "platform-resource", name: "expense-db", resourceType: "postgres-cnpg" }]);
  const res = b.addFile(API, withDb);
  assert.equal(res.ok, false);
  assert.equal(res.ok === false && res.code, "UNKNOWN_DEPENDENCY");
  assert.equal(b.has(API), false);
  const edit = b.editFile(
    "specs/design/design.cell",
    'component expense-api as "Expense API" service\n',
    'component expense-api as "Expense API" service\ncomponent expense-db as "Expense DB" database\n',
  );
  assert.equal(edit.ok, true);
  assert.equal(b.addFile(API, withDb).ok, true);
});
