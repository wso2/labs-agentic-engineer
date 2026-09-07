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
import { FileBundle, type OpErr, type OpOk } from "../src/bundle.js";
import { SEED_FILES } from "./seed.js";

const OPENAPI = "specs/design/components/hello-api/openapi.yaml";
const DESIGN = "specs/design/design.cell";
const REQUIREMENTS = "specs/requirements/prd.md";

function fresh(): FileBundle {
  return new FileBundle(SEED_FILES);
}
function expectOk(r: OpOk | OpErr): OpOk {
  assert.equal(r.ok, true, `expected ok, got ${JSON.stringify(r)}`);
  return r as OpOk;
}
function expectErr(r: OpOk | OpErr): OpErr {
  assert.equal(r.ok, false, `expected err, got ${JSON.stringify(r)}`);
  return r as OpErr;
}

test("editFile: ambiguous anchor returns NOT_UNIQUE with candidate line numbers", () => {
  const b = fresh();
  const r = expectErr(b.editFile(OPENAPI, "Hello, World!", "Hi there!"));
  assert.equal(r.code, "NOT_UNIQUE");
  assert.ok((r.count ?? 0) >= 2, "expected >= 2 matches in openapi.yaml");
  assert.ok(r.candidates && r.candidates.length >= 2, "expected candidate lines echoed");
  for (const c of r.candidates!) assert.ok(c.line > 0 && typeof c.text === "string");
  // Rejected: bundle is unchanged.
  assert.ok(b.read(OPENAPI)!.includes("Hello, World!"));
});

test("editFile: a unique anchor applies, and re-running is idempotent (ALREADY_APPLIED)", () => {
  const b = fresh();
  const anchor = 'example: "Hello, World!"';
  const ok = expectOk(b.editFile(OPENAPI, anchor, 'example: "Hi there!"'));
  assert.equal(ok.status, "applied");
  assert.ok(b.read(OPENAPI)!.includes('example: "Hi there!"'));

  const again = expectOk(b.editFile(OPENAPI, anchor, 'example: "Hi there!"'));
  assert.equal(again.status, "already-applied", "second identical edit must not wedge the loop");
});

test("editFile: missing snippet returns NOT_FOUND with closest-line hints", () => {
  const b = fresh();
  const r = expectErr(b.editFile(OPENAPI, "operationId: getHelloWorld", "operationId: zzzAbsent"));
  assert.equal(r.code, "NOT_FOUND");
  assert.ok(r.candidates && r.candidates.some((c) => c.text.includes("operationId")));
});

test("editFile: a result that would break YAML is rejected, bundle untouched", () => {
  const b = fresh();
  const before = b.read(OPENAPI)!;
  const r = expectErr(b.editFile(OPENAPI, "openapi: 3.0.3", 'openapi: 3.0.3\nbad: "unterminated'));
  assert.equal(r.code, "INVALID_YAML");
  assert.equal(b.read(OPENAPI), before, "rejected edit must leave the file byte-for-byte unchanged");
});

test("editFile: newString with $-patterns is inserted literally (no replace() interpretation)", () => {
  const b = fresh();
  expectOk(b.editFile(REQUIREMENTS, "a hello world response", "a $& response"));
  assert.ok(b.read(REQUIREMENTS)!.includes("a $& response"));
});

test("addFile: create, NOOP on identical re-add, ALREADY_EXISTS on conflicting re-add", () => {
  const b = fresh();
  const path = "specs/design/components/hello-api/notes.md";
  assert.equal(expectOk(b.addFile(path, "hi\n")).status, "applied");
  assert.equal(expectOk(b.addFile(path, "hi\n")).status, "noop");
  assert.equal(expectErr(b.addFile(path, "different\n")).code, "ALREADY_EXISTS");
});

test("addFile: invalid YAML body is rejected and not created", () => {
  const b = fresh();
  const path = "specs/design/components/hello-api/broken.yaml";
  assert.equal(expectErr(b.addFile(path, "foo: : :\n")).code, "INVALID_YAML");
  assert.equal(b.has(path), false);
});

test("removeFile: protects roots, deletes components, NOOP on absent", () => {
  const b = fresh();
  assert.equal(expectErr(b.removeFile(REQUIREMENTS)).code, "PROTECTED_PATH");
  assert.equal(expectErr(b.removeFile(DESIGN)).code, "PROTECTED_PATH");
  assert.equal(expectOk(b.removeFile(OPENAPI)).status, "applied");
  assert.equal(b.has(OPENAPI), false);
  assert.equal(expectOk(b.removeFile(OPENAPI)).status, "noop");
});

// --- component design.json validation (authored JSON, schema-gated) ---------

const CD_PATH = "specs/design/components/expense-api/design.json";
const CD_VALID = JSON.stringify({
  name: "expense-api",
  type: "service",
  version: "0.1.0",
  language: "Go",
  buildpack: "docker",
  appPath: "expense-api",
  entrypoint: "deployment/service",
  exposure: "internet",
  dependencies: [{ kind: "platform-resource", name: "postgres", resourceType: "postgres" }],
  description: "Owns claims and approvals.",
});

test("addFile: a valid component design.json is accepted", () => {
  const b = fresh();
  assert.equal(expectOk(b.addFile(CD_PATH, CD_VALID)).status, "applied");
});

test("addFile: malformed JSON at a component design.json path returns INVALID_JSON, no write", () => {
  const b = fresh();
  const r = expectErr(b.addFile(CD_PATH, '{ "name": "expense-api", '));
  assert.equal(r.code, "INVALID_JSON");
  assert.equal(b.has(CD_PATH), false);
});

test("addFile: schema-violating design.json returns SCHEMA_VIOLATION naming the problems", () => {
  const b = fresh();
  const bad = JSON.parse(CD_VALID) as Record<string, unknown>;
  delete bad.type;
  bad.exposure = "public"; // not internet|intranet
  const r = expectErr(b.addFile(CD_PATH, JSON.stringify(bad)));
  assert.equal(r.code, "SCHEMA_VIOLATION");
  assert.match(r.message, /type/);
  assert.match(r.message, /exposure/);
  assert.equal(b.has(CD_PATH), false);
});

test("addFile: design.json name must match the component directory", () => {
  const b = fresh();
  const bad = { ...(JSON.parse(CD_VALID) as Record<string, unknown>), name: "other-name" };
  const r = expectErr(b.addFile(CD_PATH, JSON.stringify(bad)));
  assert.equal(r.code, "SCHEMA_VIOLATION");
  assert.match(r.message, /name.*expense-api/);
});

test("editFile: an edit that breaks a design.json returns INVALID_JSON and leaves it unchanged", () => {
  const b = fresh();
  expectOk(b.addFile(CD_PATH, CD_VALID));
  const r = expectErr(b.editFile(CD_PATH, '"type":"service"', '"type":"service",,'));
  assert.equal(r.code, "INVALID_JSON");
  assert.equal(b.read(CD_PATH), CD_VALID); // byte-for-byte untouched
});

test("addFile: ordinary .json files elsewhere are NOT schema-gated", () => {
  const b = fresh();
  assert.equal(expectOk(b.addFile("specs/notes/random.json", '{"anything": true}')).status, "applied");
});

test("addFile: any component type string is accepted at design time (support-gating is a later phase)", () => {
  const b = fresh();
  const task = { ...(JSON.parse(CD_VALID) as Record<string, unknown>), type: "scheduled-task" };
  assert.equal(expectOk(b.addFile(CD_PATH, JSON.stringify(task))).status, "applied");
});
