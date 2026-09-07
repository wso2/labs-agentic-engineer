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
import { parseKnownComponents, parseTaskContextFile } from "../src/task-context.js";

test("parseKnownComponents collects sorted, unique component dir names", () => {
  const files = {
    "specs/requirements/prd.md": "…",
    "specs/design/domain-model.md": "…",
    "specs/design/components/order-service/design.json": "{}",
    "specs/design/components/order-service/openapi.yaml": "…",
    "specs/design/components/user-service/design.json": "{}",
    "tasks/7.md": "…",
  };
  assert.deepEqual(parseKnownComponents(files), ["order-service", "user-service"]);
});

test("parseTaskContextFile parses frontmatter + body and defaults optional fields", () => {
  const content =
    "---\nissueNumber: 42\ncomponent: order-service\ntitle: Implement order-service\n" +
    "dependsOn:\n  - user-service\norigin: spec-plan\n" +
    "specTag: requirements-v3\ndesignTag: design-v5\n---\n## Scope\n\nDo the thing.\n";
  const t = parseTaskContextFile("tasks/42.md", content);
  assert.ok(t);
  assert.equal(t.issueNumber, 42);
  assert.equal(t.component, "order-service");
  assert.equal(t.title, "Implement order-service");
  assert.deepEqual(t.dependsOn, ["user-service"]);
  assert.equal(t.origin, "spec-plan");
  assert.equal(t.designTag, "design-v5");
  assert.match(t.body, /## Scope/);
});

test("parseTaskContextFile tolerates missing optional fields", () => {
  const t = parseTaskContextFile("tasks/9.md", "---\ncomponent: catalog\ntitle: Build catalog\n---\nbody\n");
  assert.ok(t);
  assert.equal(t.issueNumber, 9); // fell back to the filename number
  assert.deepEqual(t.dependsOn, []);
  assert.equal(t.origin, "spec-plan"); // defaulted
});

test("parseTaskContextFile returns null for a non-task path", () => {
  assert.equal(parseTaskContextFile("specs/design/domain-model.md", "---\na: b\n---\nx"), null);
});

test("parseTaskContextFile degrades gracefully on a malformed rendering (no crash)", () => {
  assert.equal(parseTaskContextFile("tasks/3.md", "no frontmatter here"), null);
  assert.equal(parseTaskContextFile("tasks/3.md", "---\n: : bad yaml\n---\nx"), null);
  // Missing required component/title → not addressable.
  assert.equal(parseTaskContextFile("tasks/3.md", "---\nissueNumber: 3\n---\nbody"), null);
});
