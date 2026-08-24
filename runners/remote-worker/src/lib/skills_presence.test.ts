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
import { onDemandSkills } from "./runner.js";
import { listMirroredSkills, readSkillBodies, resolveSkillPresence } from "./skills_presence.js";

async function tmpTree(files: Record<string, string>): Promise<string> {
  const root = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-skills-presence-test-"));
  for (const [rel, content] of Object.entries(files)) {
    const full = path.join(root, rel);
    await fs.promises.mkdir(path.dirname(full), { recursive: true });
    await fs.promises.writeFile(full, content);
  }
  return root;
}

// The composition oneshot's validation branch performs. Asserted here rather
// than left to the entrypoint because the empty allowlist that shipped was
// invisible in exactly this gap: each half was correct alone.
test("validation allowlist: the mirror's playwright-cli becomes the allowlist", async () => {
  const ws = await tmpTree({
    ".claude/skills/playwright-cli/SKILL.md": "---\nname: playwright-cli\n---\n\n# cli\n",
    ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\n# go\n",
  });
  const { present, dangling } = await resolveSkillPresence(ws, onDemandSkills("validation"));
  // `go` is in the mirror and still NOT allowed: a validation run builds nothing.
  assert.deepEqual(present, ["playwright-cli"]);
  assert.deepEqual(dangling, []);
});

// An org that has the skill disabled gets no mirror copy (skill_mirror.go), so
// the name can be absent. It must be said out loud — silence is what let the
// allowlist bug survive weeks of green runs.
test("validation allowlist: a mirror without playwright-cli warns and allows nothing", async () => {
  const ws = await tmpTree({
    ".claude/skills/aep-validation/SKILL.md": "---\nname: aep-validation\n---\n\n# v\n",
  });
  const lines: string[] = [];
  const { present, dangling } = await resolveSkillPresence(ws, onDemandSkills("validation"), (l) =>
    lines.push(l),
  );
  assert.deepEqual(present, []);
  assert.deepEqual(dangling, ["playwright-cli"]);
  assert.ok(
    lines.some((l) => l.includes("playwright-cli") && l.includes("not in the mirror")),
    `expected a named miss, got ${JSON.stringify(lines)}`,
  );
});

test("resolveSkillPresence: all present → all preloaded, none dangling", async () => {
  const ws = await tmpTree({
    ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\n# go\n",
    ".claude/skills/react-webapp/SKILL.md": "---\nname: react-webapp\n---\n\n# react-webapp\n",
  });
  const out = await resolveSkillPresence(ws, ["go", "react-webapp"]);
  assert.deepEqual(out.present, ["go", "react-webapp"]);
  assert.deepEqual(out.dangling, []);
});

test("resolveSkillPresence: a missing one is reported and the rest still preload", async () => {
  const ws = await tmpTree({
    ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\n# go\n",
  });
  const lines: string[] = [];
  const out = await resolveSkillPresence(ws, ["go", "does-not-exist"], (l) => lines.push(l));
  assert.deepEqual(out.present, ["go"]);
  assert.deepEqual(out.dangling, ["does-not-exist"]);
  assert.ok(
    lines.some((l) => l.includes("does-not-exist")),
    `expected a dangling-pin warning, got ${JSON.stringify(lines)}`,
  );
});

test("resolveSkillPresence: no .claude/skills/ at all → everything dangling, no throw", async () => {
  const ws = await tmpTree({ "README.md": "no skills mirror here" });
  const out = await resolveSkillPresence(ws, ["go", "react-webapp"]);
  assert.deepEqual(out.present, []);
  assert.deepEqual(out.dangling, ["go", "react-webapp"]);
});

test("resolveSkillPresence: empty pin list → empty result, no fs access", async () => {
  const ws = await tmpTree({ "README.md": "no skills mirror here" });
  const out = await resolveSkillPresence(ws, []);
  assert.deepEqual(out, { present: [], dangling: [] });
});

// `skills:` is an ALLOWLIST — a mirrored skill omitted from it is rejected by
// the Skill tool ("not in this session's skills allowlist"), so the run must
// list the WHOLE mirror or the unpinned copies are inert files on disk.
test("listMirroredSkills: every mirrored skill is listed, sorted", async () => {
  const ws = await tmpTree({
    ".claude/skills/react-webapp/SKILL.md": "---\nname: react-webapp\n---\n\nB\n",
    ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\nB\n",
    ".claude/skills/api-management/SKILL.md": "---\nname: api-management\n---\n\nB\n",
  });
  assert.deepEqual(await listMirroredSkills(ws), ["api-management", "go", "react-webapp"]);
});

test("listMirroredSkills: a directory without a readable SKILL.md is not a skill", async () => {
  const ws = await tmpTree({
    ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\nB\n",
    ".claude/skills/not-a-skill/README.md": "just docs\n",
  });
  assert.deepEqual(await listMirroredSkills(ws), ["go"]);
});

test("listMirroredSkills: no mirror at all is empty, not an error", async () => {
  const ws = await tmpTree({ "README.md": "x" });
  assert.deepEqual(await listMirroredSkills(ws), []);
});

// Nothing in `skills:` arrives in context — the model sees a name and a
// description, and a body only when it invokes the skill. Verified against the
// SDK: an agent holding a listed skill could not state a codeword written in
// that skill's body until it called the Skill tool. A pin means the guidance IS
// needed, so the body goes into the system prompt.
test("readSkillBodies: each pinned body is included, fenced by name", async () => {
  const ws = await tmpTree({
    ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\nBuild Go like THIS.\n",
    ".claude/skills/react-webapp/SKILL.md": "---\nname: react-webapp\n---\n\nReact rules.\n",
  });
  const out = await readSkillBodies(ws, ["go", "react-webapp"]);
  assert.match(out, /<skill name="go">/);
  assert.match(out, /Build Go like THIS\./);
  assert.match(out, /<skill name="react-webapp">/);
  assert.match(out, /React rules\./);
  // The model must be told these are already loaded, or it re-invokes the Skill
  // tool and pays for the same body twice.
  assert.match(out, /ALREADY in your context/);
});

test("readSkillBodies: nothing pinned is the empty string, so appending is a no-op", async () => {
  const ws = await tmpTree({ ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\nB\n" });
  assert.equal(await readSkillBodies(ws, []), "");
});

test("readSkillBodies: a dangling pin is skipped, the rest still load", async () => {
  const ws = await tmpTree({ ".claude/skills/go/SKILL.md": "---\nname: go\n---\n\nGo body.\n" });
  const out = await readSkillBodies(ws, ["go", "vanished"]);
  assert.match(out, /Go body\./);
  assert.ok(!out.includes("vanished"));
});
