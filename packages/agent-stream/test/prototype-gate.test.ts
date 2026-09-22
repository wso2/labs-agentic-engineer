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
 * Write-gate behavior for a web-application component's
 * `specs/design/components/<component>/prototype.json`. The rules themselves
 * are `@aep/prototype-model`'s (`parsePrototypeModel`), shared with the
 * console and — through the published JSON Schema — the Go save gate; these
 * tests pin how the FileBundle applies them: a refused write leaves the bundle
 * byte-for-byte unchanged and says, with a stable code, what to fix.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

import { FileBundle, type OpErr, type OpOk } from "../src/bundle.ts";
import { checkPrototype } from "../src/prototype-gate.ts";

const PATH = "specs/design/components/approvals-portal/prototype.json";

const require = createRequire(import.meta.url);
const FIXTURE = readFileSync(require.resolve("@aep/prototype-model/fixtures/expense-approval.json"), "utf8");

/** The fixture as a mutable object, for the negative cases. */
function doc(): Record<string, unknown> {
  return JSON.parse(FIXTURE) as Record<string, unknown>;
}

function expectOk(r: OpOk | OpErr): OpOk {
  assert.equal(r.ok, true, `expected ok, got ${JSON.stringify(r)}`);
  return r as OpOk;
}

function expectErr(r: OpOk | OpErr): OpErr {
  assert.equal(r.ok, false, `expected err, got ${JSON.stringify(r)}`);
  return r as OpErr;
}

test("a valid prototype is admitted at its component path", () => {
  const b = new FileBundle();
  const r = expectOk(b.addFile(PATH, FIXTURE));
  assert.equal(r.status, "applied");
  assert.equal(b.read(PATH), FIXTURE);
});

test("a valid edit to a prototype is admitted", () => {
  const b = new FileBundle({ [PATH]: FIXTURE });
  expectOk(b.editFile(PATH, '"text": "Approval queue"', '"text": "Expenses to approve"'));
  assert.ok(b.read(PATH)!.includes("Expenses to approve"));
});

test("malformed JSON is refused with INVALID_JSON and nothing is written", () => {
  const b = new FileBundle();
  const r = expectErr(b.addFile(PATH, FIXTURE.slice(0, 200)));
  assert.equal(r.code, "INVALID_JSON");
  assert.equal(b.has(PATH), false);
});

test("an edit that breaks the JSON is refused and the file is unchanged", () => {
  const b = new FileBundle({ [PATH]: FIXTURE });
  const r = expectErr(b.editFile(PATH, '"schemaVersion": 1,', '"schemaVersion": 1'));
  assert.equal(r.code, "INVALID_JSON");
  assert.equal(b.read(PATH), FIXTURE);
});

test("a schema failure is refused with INVALID_PROTOTYPE naming the rule and the path", () => {
  const bad = doc();
  (bad["screens"] as Record<string, unknown>[])[0]!["content"] = [{ kind: "carousel", id: "carousel.1" }];
  const b = new FileBundle();
  const r = expectErr(b.addFile(PATH, JSON.stringify(bad)));
  assert.equal(r.code, "INVALID_PROTOTYPE");
  assert.match(r.message, /SCHEMA_VIOLATION at screens\[0\]\.content\[0\]\.kind/);
  assert.equal(b.has(PATH), false);
});

test("an unsupported version is refused with INVALID_PROTOTYPE", () => {
  const r = checkPrototype(PATH, JSON.stringify({ ...doc(), schemaVersion: 2 }));
  assert.equal(r?.code, "INVALID_PROTOTYPE");
  assert.match(r!.message, /UNSUPPORTED_VERSION at schemaVersion/);
});

test("a duplicate id is refused with INVALID_PROTOTYPE", () => {
  const b = new FileBundle({ [PATH]: FIXTURE });
  const r = expectErr(b.editFile(PATH, '"id": "heading.new"', '"id": "heading.queue"'));
  assert.equal(r.code, "INVALID_PROTOTYPE");
  assert.match(r.message, /DUPLICATE_ID at screens\[2\]\.content\[0\]\.id/);
  assert.equal(b.read(PATH), FIXTURE);
});

test("a dangling reference is refused with INVALID_PROTOTYPE", () => {
  const r = checkPrototype(PATH, JSON.stringify({ ...doc(), defaultScreenId: "screen.ghost" }));
  assert.equal(r?.code, "INVALID_PROTOTYPE");
  assert.match(r!.message, /UNKNOWN_REFERENCE at defaultScreenId/);
});

test("a component that disagrees with its directory is refused with PROTOTYPE_COMPONENT_MISMATCH", () => {
  const b = new FileBundle();
  const r = expectErr(b.addFile("specs/design/components/customer-portal/prototype.json", FIXTURE));
  assert.equal(r.code, "PROTOTYPE_COMPONENT_MISMATCH");
  assert.match(r.message, /"approvals-portal" does not match its directory "customer-portal"/);
});

test("a refusal tells the model the write did not land", () => {
  const r = checkPrototype(PATH, JSON.stringify({ ...doc(), defaultScreenId: "screen.ghost" }));
  assert.match(r!.message, /file is unchanged/);
});

test("prototype.json anywhere else is not gated as a prototype", () => {
  for (const path of ["specs/requirements/prototype.json", "specs/design/prototype.json"]) {
    assert.equal(checkPrototype(path, "{not json"), null, path);
  }
});
