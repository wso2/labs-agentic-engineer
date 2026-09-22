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
 * The validator's contract: one structural pass (the Zod schema, published as
 * JSON Schema for the Go gate), then one reference pass over a global ID index.
 * Every case in `validation-cases.json` is asserted here AND by the Go twin in
 * services/aep-api/internal/platform/prototypespec, so the two gates cannot
 * disagree on a code or a path.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  PROTOTYPE_SCHEMA_VERSION,
  parsePrototypeModel,
  type PrototypeModelV1,
} from "../src/index.ts";
import { caseFile, patched } from "./cases.ts";

test("the schema version is 1", () => {
  assert.equal(PROTOTYPE_SCHEMA_VERSION, 1);
});

test("a valid document parses to the same model", () => {
  const valid = patched([]) as PrototypeModelV1;
  assert.deepEqual(parsePrototypeModel(valid), { ok: true, model: valid });
});

for (const c of caseFile.cases) {
  test(`parity case: ${c.name}`, () => {
    const result = parsePrototypeModel(patched(c.patch), c.component === undefined ? {} : { component: c.component });
    if (c.issues.length === 0) {
      assert.equal(result.ok, true, result.ok ? "" : JSON.stringify(result.issues));
      return;
    }
    assert.equal(result.ok, false, "expected the document to be refused");
    if (result.ok) return;
    for (const issue of result.issues) {
      assert.ok(issue.message.length > 0, "every issue carries a message");
    }
    const codeOnly = c.issues.every((i) => i.path === undefined);
    if (codeOnly) {
      const codes = new Set(result.issues.map((i) => i.code));
      assert.deepEqual([...codes], [...new Set(c.issues.map((i) => i.code))]);
      return;
    }
    assert.deepEqual(
      result.issues.map((i) => ({ code: i.code, path: i.path })),
      c.issues,
    );
  });
}

test("a value that is not an object is a schema violation at the root", () => {
  for (const value of [null, [], "prototype", 1]) {
    const result = parsePrototypeModel(value);
    assert.equal(result.ok, false);
    if (!result.ok) assert.equal(result.issues[0]?.code, "SCHEMA_VIOLATION");
  }
});

test("a structural issue names its JSON path", () => {
  const result = parsePrototypeModel(
    patched([{ op: "set", path: ["screens", 0, "content", 1, "kind"], value: "carousel" }]),
  );
  assert.equal(result.ok, false);
  if (!result.ok) assert.equal(result.issues[0]?.path, "screens[0].content[1].kind");
});

test("an issue message names what is wrong", () => {
  const result = parsePrototypeModel(patched([{ op: "set", path: ["flows", 0, "roleId"], value: "auditor" }]));
  assert.equal(result.ok, false);
  if (!result.ok) assert.match(result.issues[0]!.message, /auditor/);
});

test("the parsed model does not alias the input", () => {
  const input = patched([]);
  const result = parsePrototypeModel(input);
  assert.equal(result.ok, true);
  if (result.ok) assert.notEqual(result.model, input);
});
