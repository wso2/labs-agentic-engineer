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
 * Write-gate behavior for the two design.json rules this branch adds — the
 * canonical `web-application` type (aliases rejected) and the optional
 * `endpoint` block. These assert the zod source of truth directly; the Go fold
 * gate (agentfold/designgate.go) has its own parity tests, and the two must
 * agree — a design that passes one gate MUST pass the other.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkComponentDesign } from "../src/component-design-schema.ts";

const PATH = "specs/design/components/web/design.json";

/** A minimal design.json whose `name` matches PATH's directory ("web"). */
function design(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    name: "web",
    type: "web-application",
    version: "0.1.0",
    language: "typescript",
    buildpack: "docker",
    appPath: ".",
    entrypoint: "index.html",
    exposure: "internet",
    dependencies: [],
    description: "the SPA",
    ...overrides,
  });
}

test("accepts the canonical web-application type", () => {
  assert.equal(checkComponentDesign(PATH, design()), null);
});

for (const alias of ["webapp", "web-app", "webApplication", "web application", "WEBAPP", " web-app "]) {
  test(`rejects the web-application alias ${JSON.stringify(alias)}`, () => {
    const problem = checkComponentDesign(PATH, design({ type: alias }));
    assert.equal(problem?.code, "SCHEMA_VIOLATION");
    assert.match(problem!.message, /web-application/);
  });
}

test("accepts an endpoint block with a name", () => {
  assert.equal(checkComponentDesign(PATH, design({ endpoint: { name: "api" } })), null);
});

test("rejects an endpoint block with an empty name", () => {
  const problem = checkComponentDesign(PATH, design({ endpoint: { name: "" } }));
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
});

test("rejects an endpoint block with an unknown key", () => {
  const problem = checkComponentDesign(PATH, design({ endpoint: { name: "api", port: 8080 } }));
  assert.equal(problem?.code, "SCHEMA_VIOLATION");
});

// --- an external dependency is a reference; its definition has its own file --

test("accepts an external dependency as a bare reference (kind + name)", () => {
  const doc = design({ dependencies: [{ kind: "external", name: "stripe", description: "charges shipping" }] });
  assert.equal(checkComponentDesign(PATH, doc), null);
});

for (const moved of [
  { style: "rest-api" },
  { package: "npm:stripe" },
  { specPath: "https://example.com/openapi.json" },
  { candidates: [{ name: "a", style: "sdk" }, { name: "b", style: "sdk" }] },
  { config: [{ key: "STRIPE_API_KEY", secret: true }] },
]) {
  const field = Object.keys(moved)[0]!;
  test(`rejects "${field}" on a component's external dependency, pointing at the dependency file`, () => {
    const doc = design({ dependencies: [{ kind: "external", name: "stripe", ...moved }] });
    const problem = checkComponentDesign(PATH, doc);
    assert.equal(problem?.code, "SCHEMA_VIOLATION");
    assert.match(problem!.message, /specs\/design\/dependencies\/stripe\/dependency\.json/);
    assert.match(problem!.message, new RegExp(`"${field}"`));
  });
}

// --- buildpack is pinned to "docker" (agent write-gate only; see the .refine) -

test("accepts buildpack docker", () => {
  assert.equal(checkComponentDesign(PATH, design({ buildpack: "docker" })), null);
});

for (const buildpack of ["go", "react", "node", "nodejs", "Docker", ""]) {
  test(`rejects non-docker buildpack ${JSON.stringify(buildpack)}`, () => {
    const problem = checkComponentDesign(PATH, design({ buildpack }));
    assert.equal(problem?.code, "SCHEMA_VIOLATION");
    assert.match(problem!.message, /buildpack|docker/);
  });
}

// --- platform-stamped dependency wiring -------------------------------------
// The Go fold gate has the mirror of this block (agentfold/designgate_test.go
// TestValidateComponentDesign_Wiring). `wiring` is ACCEPTED rather than rejected
// as agent-authored — unlike status/reason it is persisted in design.json, and
// the design agent reads-edits-writes the file, so a rejection rule would reject
// its own echo. Design save re-derives and overwrites it, which is what makes
// authoring moot. A MALFORMED wiring still rejects: half-stamped or wrongly-typed
// renders an unusable workload.yaml resource entry, which is worse than an absent
// wiring the coding agent reports as a platform fault.

const wiredDep = (wiring: unknown) => ({
  kind: "platform-resource",
  name: "orders-db",
  resourceType: "postgres-cnpg",
  wiring,
});

// The sibling endpoint the derivation stamps for a `component` dependency. Its
// `component` is the SCOPED OpenChoreo name — the value an agent left to guess
// gets wrong (it writes the friendly one, the connection never resolves, and the
// consumer's ReleaseBinding never reaches Ready).
const SIBLING_ENDPOINT = {
  component: "todo-api99-todo-api",
  name: "http",
  visibility: "project",
  envBindings: { address: "TODO_API_URL" },
};

for (const [name, wiring] of [
  ["the platform-stamped shape", { ref: "shop-orders-db", envBindings: { host: "ORDERS_DB_HOST", port: "ORDERS_DB_PORT" } }],
  ["no outputs bound yet", { ref: "shop-orders-db", envBindings: {} }],
  ["the sibling endpoint variant", { endpoint: SIBLING_ENDPOINT }],
] as const) {
  test(`accepts dependency wiring: ${name}`, () => {
    assert.equal(checkComponentDesign(PATH, design({ dependencies: [wiredDep(wiring)] })), null);
  });
}

for (const [name, wiring] of [
  ["not an object", "shop-orders-db"],
  ["ref missing", { envBindings: { host: "ORDERS_DB_HOST" } }],
  ["ref empty", { ref: "", envBindings: {} }],
  ["envBindings missing", { ref: "shop-orders-db" }],
  ["envBindings not an object", { ref: "shop-orders-db", envBindings: "ORDERS_DB_HOST" }],
  ["env var not a string", { ref: "shop-orders-db", envBindings: { port: 5432 } }],
  ["unknown property", { ref: "shop-orders-db", envBindings: {}, values: { host: "db" } }],
  // The endpoints[] variant is all-or-nothing too, and exclusive with the
  // resources[] one — OpenChoreo silently ignores a partial entry.
  ["endpoint not an object", { endpoint: "todo-api99-todo-api" }],
  ["endpoint component missing", { endpoint: { ...SIBLING_ENDPOINT, component: undefined } }],
  ["endpoint component empty", { endpoint: { ...SIBLING_ENDPOINT, component: "" } }],
  ["endpoint name missing", { endpoint: { ...SIBLING_ENDPOINT, name: undefined } }],
  ["endpoint visibility missing", { endpoint: { ...SIBLING_ENDPOINT, visibility: undefined } }],
  ["endpoint envBindings missing", { endpoint: { ...SIBLING_ENDPOINT, envBindings: undefined } }],
  ["endpoint env var not a string", { endpoint: { ...SIBLING_ENDPOINT, envBindings: { address: 8080 } } }],
  ["endpoint unknown property", { endpoint: { ...SIBLING_ENDPOINT, project: "todo-api99" } }],
  ["both variants at once", { ref: "shop-orders-db", envBindings: {}, endpoint: SIBLING_ENDPOINT }],
] as const) {
  test(`rejects malformed dependency wiring: ${name}`, () => {
    const problem = checkComponentDesign(PATH, design({ dependencies: [wiredDep(wiring)] }));
    assert.equal(problem?.code, "SCHEMA_VIOLATION");
  });
}
