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
import { readSkillsPinned, readProjectSkillsPinned, resolveTaskSkills } from "./skills_resolver.js";

// tmpTree materialises a { relPath: content } map under a fresh temp dir and
// returns the root. Directories are created as needed.
async function tmpTree(files: Record<string, string>): Promise<string> {
  const root = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-skills-test-"));
  for (const [rel, content] of Object.entries(files)) {
    const full = path.join(root, rel);
    await fs.promises.mkdir(path.dirname(full), { recursive: true });
    await fs.promises.writeFile(full, content);
  }
  return root;
}

// ---- readSkillsPinned ------------------------------------------------------

test("readSkillsPinned: parses the component design.json skillsPinned array", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({
      skillsPinned: ["go", "react-webapp"],
    }),
  });
  assert.deepEqual(await readSkillsPinned(ws, "api"), ["go", "react-webapp"]);
});

test("readSkillsPinned: absent design.json → []", async () => {
  const ws = await tmpTree({ "README.md": "no design here" });
  assert.deepEqual(await readSkillsPinned(ws, "api"), []);
});

test("readSkillsPinned: design.json with no skillsPinned → []", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ title: "x" }),
  });
  assert.deepEqual(await readSkillsPinned(ws, "api"), []);
});

test("readSkillsPinned: malformed design.json → []", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": "{ not valid json",
  });
  assert.deepEqual(await readSkillsPinned(ws, "api"), []);
});

test("readSkillsPinned: non-string entries are filtered out", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({
      skillsPinned: ["go", 42, null],
    }),
  });
  assert.deepEqual(await readSkillsPinned(ws, "api"), ["go"]);
});

test("readSkillsPinned: reads only the named component, not others", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go"] }),
    "specs/design/components/webapp/design.json": JSON.stringify({
      skillsPinned: ["react-webapp"],
    }),
  });
  assert.deepEqual(await readSkillsPinned(ws, "api"), ["go"]);
  assert.deepEqual(await readSkillsPinned(ws, "webapp"), ["react-webapp"]);
});

// ---- readProjectSkillsPinned (milestone scope) -----------------------------

test("readProjectSkillsPinned: unions every component, de-duplicated, in component order", async () => {
  const ws = await tmpTree({
    "specs/design/components/webapp/design.json": JSON.stringify({
      skillsPinned: ["react-webapp", "go"],
    }),
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go"] }),
    "specs/design/components/worker/design.json": JSON.stringify({ skillsPinned: ["go", "temporal"] }),
  });
  // Sorted component order (api, webapp, worker); "go" appears once, first-seen.
  assert.deepEqual(await readProjectSkillsPinned(ws), ["go", "react-webapp", "temporal"]);
});

test("readProjectSkillsPinned: does NOT read a component named after the milestone sentinel", async () => {
  // The regression: a milestone Job carries AEP_COMPONENT_NAME=aep-milestone,
  // which never names a real component — the union must still find the skills.
  const ws = await tmpTree({
    "specs/design/components/workout-tracker-webapp/design.json": JSON.stringify({
      skillsPinned: ["react-webapp"],
    }),
  });
  assert.deepEqual(await readSkillsPinned(ws, "aep-milestone"), []);
  assert.deepEqual(await readProjectSkillsPinned(ws), ["react-webapp"]);
});

test("readProjectSkillsPinned: absent components dir → [] with a warning", async () => {
  const ws = await tmpTree({ "README.md": "no specs here" });
  const lines: string[] = [];
  assert.deepEqual(await readProjectSkillsPinned(ws, (l) => lines.push(l)), []);
  assert.ok(
    lines.some((l) => l.includes("specs/design/components/ not found")),
    `expected a not-found warning, got ${JSON.stringify(lines)}`,
  );
});

test("readProjectSkillsPinned: components without / with malformed design.json contribute nothing", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go"] }),
    "specs/design/components/broken/design.json": "{ not json",
    "specs/design/components/undesigned/README.md": "no design.json yet",
  });
  assert.deepEqual(await readProjectSkillsPinned(ws), ["go"]);
});

test("readProjectSkillsPinned: skips dot-dirs and stray files", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go"] }),
    "specs/design/components/.cache/design.json": JSON.stringify({ skillsPinned: ["leaked"] }),
    "specs/design/components/notes.md": "stray file",
  });
  assert.deepEqual(await readProjectSkillsPinned(ws), ["go"]);
});

// ---- resolveTaskSkills (thin scope dispatch, no clone/network) -------------

test("resolveTaskSkills: component scope reads one design", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go"] }),
    "specs/design/components/webapp/design.json": JSON.stringify({ skillsPinned: ["react-webapp"] }),
  });
  const out = await resolveTaskSkills({ workspace: ws, scope: { kind: "component", componentName: "api" } });
  assert.deepEqual(out, ["go"]);
});

test("resolveTaskSkills: project scope unions every component", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go"] }),
    "specs/design/components/webapp/design.json": JSON.stringify({ skillsPinned: ["react-webapp"] }),
  });
  const out = await resolveTaskSkills({ workspace: ws, scope: { kind: "project" } });
  assert.deepEqual(out, ["go", "react-webapp"]);
});

test("resolveTaskSkills: a missing specs/design/ yields no pins and no error", async () => {
  const ws = await tmpTree({ "README.md": "empty project" });
  assert.deepEqual(
    await resolveTaskSkills({ workspace: ws, scope: { kind: "component", componentName: "api" } }),
    [],
  );
  assert.deepEqual(await resolveTaskSkills({ workspace: ws, scope: { kind: "project" } }), []);
});

// A design may name any skill, but a run's workflow is the runner's: a pinned
// `validation-task` would ride an implementation run beside `aep`, and a pinned
// `aep` would load twice.
test("resolveTaskSkills: a pin on a workflow skill is dropped, and said so", async () => {
  const ws = await tmpTree({
    "specs/design/components/api/design.json": JSON.stringify({ skillsPinned: ["go", "validation-task", "aep"] }),
  });
  const lines: string[] = [];
  const out = await resolveTaskSkills({ workspace: ws, scope: { kind: "project" }, log: (l) => lines.push(l) });
  assert.deepEqual(out, ["go"]);
  assert.ok(lines.some((l) => l.includes("validation-task") && l.includes("aep")), lines.join("\n"));
});
