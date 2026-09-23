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
 * The local mirror applies production's copy rule. It used to be a blunt
 * directory copy, which put design-only skills into a build's clone — a mirror
 * that can never occur in production, hiding the very filtering the feature
 * exists to do. These pin the rule so it cannot regress to a `cp -r`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  audienceIncludesCoding,
  mirrorLocalSkillLibrary,
  selectMirroredSkills,
  skillAudience,
} from "./local_skill_mirror.js";

const md = (name: string, audience?: string): string =>
  `---\nname: ${name}\ndescription: d.\nmetadata:\n  aep:\n    kind: org\n` +
  (audience === undefined ? "" : `    audience: ${audience}\n`) +
  `---\n\nBODY of ${name}\n`;

function libraryDir(skills: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-local-lib-"));
  for (const [name, body] of Object.entries(skills)) {
    mkdirSync(join(dir, name), { recursive: true });
    writeFileSync(join(dir, name, "SKILL.md"), body);
  }
  return dir;
}

test("skillAudience reads the declared list; anything else is permissive", () => {
  assert.deepEqual(skillAudience(md("go", "[coding]")), ["coding"]);
  assert.deepEqual(skillAudience(md("wire", "[design, coding]")), ["design", "coding"]);
  // Absent, empty, unrecognised and unparseable all resolve to EVERY audience —
  // narrowing is opt-in, so a skill is never hidden by a frontmatter mistake.
  assert.deepEqual(skillAudience(md("plain")), ["design", "coding"]);
  assert.deepEqual(skillAudience(md("bogus", "[nonsense]")), ["design", "coding"]);
  assert.deepEqual(skillAudience("no frontmatter at all"), ["design", "coding"]);
  assert.ok(audienceIncludesCoding(["coding"]));
  assert.ok(!audienceIncludesCoding(["design"]));
});

test("the rule admits coding skills and withholds design-only ones", () => {
  const lib = [
    { name: "go", skillMd: md("go", "[coding]") },
    { name: "planning", skillMd: md("planning", "[design]") },
    { name: "wireframes", skillMd: md("wireframes", "[design, coding]") },
    { name: "unmarked", skillMd: md("unmarked") },
  ];
  const { copied, skipped } = selectMirroredSkills(lib, new Set(), new Set());
  assert.deepEqual(copied, ["go", "wireframes", "unmarked"]);
  assert.deepEqual(skipped, [{ name: "planning", reason: "design-only" }]);
});

test("disabled is withheld, but a pin overrides BOTH axes", () => {
  const lib = [
    { name: "go", skillMd: md("go", "[coding]") },
    { name: "react", skillMd: md("react", "[coding]") },
    { name: "planning", skillMd: md("planning", "[design]") },
  ];
  // go is disabled but pinned → still copied (the drift guard: an admin toggle
  // must not break a build already designed against it). planning is
  // design-only but pinned → also copied. react is merely disabled → withheld.
  const { copied, skipped } = selectMirroredSkills(lib, new Set(["go", "planning"]), new Set(["go", "react"]));
  assert.deepEqual(copied, ["go", "planning"]);
  assert.deepEqual(skipped, [{ name: "react", reason: "disabled" }]);
});

test("only the admitted skills reach .claude/skills/", async () => {
  const skillsDir = libraryDir({
    go: md("go", "[coding]"),
    planning: md("planning", "[design]"),
    wireframes: md("wireframes", "[design, coding]"),
  });
  const workspace = mkdtempSync(join(tmpdir(), "aep-local-ws-"));

  await mirrorLocalSkillLibrary(skillsDir, workspace, new Set(), "github");

  const at = (n: string) => join(workspace, ".claude", "skills", n, "SKILL.md");
  assert.ok(existsSync(at("go")), "coding skill must be mirrored");
  assert.ok(existsSync(at("wireframes")), "dual-audience skill must be mirrored");
  // The regression this file exists for: a whole-tree copy put this in a
  // build's clone, where production would never have written it.
  assert.ok(!existsSync(at("planning")), "design-only skill must NOT reach a build's clone");
});

test("a pinned design-only skill IS mirrored", async () => {
  const skillsDir = libraryDir({ planning: md("planning", "[design]") });
  const workspace = mkdtempSync(join(tmpdir(), "aep-local-ws-"));

  await mirrorLocalSkillLibrary(skillsDir, workspace, new Set(["planning"]), "github");

  assert.ok(existsSync(join(workspace, ".claude", "skills", "planning", "SKILL.md")));
});
