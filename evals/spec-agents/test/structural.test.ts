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

import { strict as assert } from "node:assert";
import { test } from "node:test";
import { join } from "node:path";
import { FIXTURES_DIR } from "../src/config.js";
import type { SectionRunResult } from "../src/drivers/conversational.js";
import { designChecks, isAcyclic } from "../src/scoring/structural.js";

// The committed design fixture is the reference bundle shape (design.cell root,
// domain-model.md, flows/); every deterministic design check must pass on it,
// otherwise the scorer disagrees with the platform's own golden example.
test("designChecks: the lunch-coordinator design fixture passes every deterministic check", () => {
  const run: SectionRunResult = { section: "design", records: [], questionsAsked: 0, finishedInterview: true, answers: [] };
  const report = designChecks(join(FIXTURES_DIR, "lunch-coordinator-design"), run);
  const failed = report.checks.filter((c) => !c.ok).map((c) => `${c.name}: ${c.detail ?? ""}`);
  assert.deepEqual(failed, []);
});

test("isAcyclic: linear provider chains pass", () => {
  assert.ok(
    isAcyclic([
      { component: "webapp", dependsOn: ["api"] },
      { component: "api", dependsOn: ["db"] },
      { component: "db", dependsOn: [] },
    ]),
  );
});

test("isAcyclic: a cycle across components fails", () => {
  assert.ok(
    !isAcyclic([
      { component: "a", dependsOn: ["b"] },
      { component: "b", dependsOn: ["a"] },
    ]),
  );
});

test("isAcyclic: self-references are ignored (same-component task ordering)", () => {
  assert.ok(isAcyclic([{ component: "a", dependsOn: ["a"] }]));
});

test("isAcyclic: empty plan is trivially acyclic", () => {
  assert.ok(isAcyclic([]));
});
