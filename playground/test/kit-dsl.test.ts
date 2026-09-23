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
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { compileDslArtifacts } from "../src/kit/dsl.js";

const DSL = `screen Login
  input "Email"
  button "Sign In" primary
`;

function tempThread(): string {
  return mkdtempSync(join(tmpdir(), "aep-dsl-"));
}

test("compiles a changed wireframes.dsl into a sibling .excalidraw", () => {
  const dir = tempThread();
  try {
    const rel = "specs/design/components/webapp/wireframes.dsl";
    mkdirSync(dirname(join(dir, rel)), { recursive: true });
    writeFileSync(join(dir, rel), DSL);

    const results = compileDslArtifacts(dir, [rel]);
    assert.equal(results.length, 1);
    assert.equal(results[0]!.ok, true);
    assert.equal(results[0]!.outPath, "specs/design/components/webapp/wireframes.excalidraw");
    const scene = JSON.parse(readFileSync(join(dir, results[0]!.outPath), "utf8"));
    assert.equal(scene.type, "excalidraw");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("reports a parse failure without writing the sibling", () => {
  const dir = tempThread();
  try {
    writeFileSync(join(dir, "wireframes.dsl"), "not a valid dsl at all\n");
    const results = compileDslArtifacts(dir, ["wireframes.dsl"]);
    assert.equal(results[0]!.ok, false);
    assert.ok(results[0]!.error);
    assert.equal(existsSync(join(dir, "wireframes.excalidraw")), false);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("ignores non-dsl paths and deleted dsl files", () => {
  const dir = tempThread();
  try {
    assert.deepEqual(compileDslArtifacts(dir, ["domain-model.md", "gone/wireframes.dsl"]), []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
