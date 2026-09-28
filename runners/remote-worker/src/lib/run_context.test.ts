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
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { appendSessionContext, skillsNotice } from "./run_context.js";

test("skillsNotice: workflow, pins and the catalog by name", () => {
  assert.equal(
    skillsNotice(["aep"], ["ballerina", "openapi-conventions"], ["aep", "ballerina", "openapi-conventions"]),
    "[skills] workflow: aep · pinned: ballerina, openapi-conventions · 3 available: aep, ballerina, openapi-conventions",
  );
});

test("skillsNotice: nothing pinned says so; a long catalog is a count", () => {
  const many = Array.from({ length: 30 }, (_, i) => `s${i}`);
  assert.equal(skillsNotice(["validation-task"], [], many), "[skills] workflow: validation-task · pinned: none · 30 available");
  assert.equal(skillsNotice(["aep"], [], []), "[skills] workflow: aep · pinned: none · 0 available");
});

test("appendSessionContext: one timestamped JSON line per record, creating the directory", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aep-ctx-"));
  const file = path.join(dir, "nested", "session-context.jsonl");
  try {
    appendSessionContext(file, { session: "lead", agent: "lead", appendix: true });
    appendSessionContext(file, { session: "lead", skill: "go" });
    const lines = fs.readFileSync(file, "utf8").trim().split("\n").map((l) => JSON.parse(l) as Record<string, unknown>);
    assert.equal(lines.length, 2);
    assert.equal(typeof lines[0].ts, "string");
    assert.deepEqual({ ...lines[1], ts: undefined }, { ts: undefined, session: "lead", skill: "go" });
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
