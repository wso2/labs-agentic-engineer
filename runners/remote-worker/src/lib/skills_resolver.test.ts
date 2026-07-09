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
import {
  readSkillsApplied,
  resolveSkillsFromClone,
  resolveKind,
  resolveTaskSkills,
} from "./skills_resolver.js";

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

const skillMD = (name: string, kind?: string): string => {
  const meta = kind ? `metadata:\n  aep:\n    kind: ${kind}\n` : "";
  return `---\nname: ${name}\ndescription: does ${name}.\n${meta}---\n\n# ${name}\n`;
};

// ---- readSkillsApplied ------------------------------------------------------

test("readSkillsApplied: parses the frontmatter sequence", async () => {
  const ws = await tmpTree({
    "specs/design/design.md": "---\nskillsApplied:\n  - go\n  - react-webapp\n---\n\n# design\n",
  });
  assert.deepEqual(await readSkillsApplied(ws), ["go", "react-webapp"]);
});

test("readSkillsApplied: absent design.md → []", async () => {
  const ws = await tmpTree({ "README.md": "no design here" });
  assert.deepEqual(await readSkillsApplied(ws), []);
});

test("readSkillsApplied: design with no skillsApplied → []", async () => {
  const ws = await tmpTree({ "specs/design/design.md": "---\ntitle: x\n---\n\n# design\n" });
  assert.deepEqual(await readSkillsApplied(ws), []);
});

test("readSkillsApplied: non-string entries are filtered out", async () => {
  const ws = await tmpTree({
    "specs/design/design.md": "---\nskillsApplied:\n  - go\n  - 42\n  - null\n---\n",
  });
  assert.deepEqual(await readSkillsApplied(ws), ["go"]);
});

// ---- resolveKind ------------------------------------------------------------

test("resolveKind: known kinds pass through; absent/unknown → org", () => {
  assert.equal(resolveKind(skillMD("s", "platform")), "platform");
  assert.equal(resolveKind(skillMD("s", "org")), "org");
  assert.equal(resolveKind(skillMD("s", "custom")), "custom");
  assert.equal(resolveKind(skillMD("s", "imported")), "imported");
  assert.equal(resolveKind(skillMD("s")), "org"); // unmarked
  assert.equal(resolveKind(skillMD("s", "wat")), "org"); // unknown
  assert.equal(resolveKind(skillMD("s", "  platform  ")), "platform"); // trimmed
  assert.equal(resolveKind("no frontmatter here"), "org");
});

// ---- resolveSkillsFromClone -------------------------------------------------

test("resolveSkillsFromClone: builds materializedName from kind + reads references", async () => {
  const clone = await tmpTree({
    "skills/go/SKILL.md": skillMD("go", "org"),
    "skills/go/references/style.md": "# go style",
    "skills/payments/SKILL.md": skillMD("payments", "custom"),
  });

  const out = await resolveSkillsFromClone(clone, ["go", "payments"]);
  assert.equal(out.length, 2);

  const go = out.find((s) => s.materializedName === "org-go");
  assert.ok(go, "expected org-go");
  assert.equal(go!.kind, "org");
  assert.deepEqual(go!.references, { "references/style.md": "# go style" });

  const pay = out.find((s) => s.materializedName === "custom-payments");
  assert.ok(pay, "expected custom-payments");
  assert.equal(pay!.kind, "custom");
  assert.deepEqual(pay!.references, {}); // no references dir
});

test("resolveSkillsFromClone: unmarked SKILL.md resolves as org kind", async () => {
  const clone = await tmpTree({ "skills/mystery/SKILL.md": skillMD("mystery") });
  const out = await resolveSkillsFromClone(clone, ["mystery"]);
  assert.equal(out.length, 1);
  assert.equal(out[0].kind, "org");
  assert.equal(out[0].materializedName, "org-mystery");
});

test("resolveSkillsFromClone: missing names are dropped (warn-and-skip parity)", async () => {
  const clone = await tmpTree({ "skills/go/SKILL.md": skillMD("go", "org") });
  const out = await resolveSkillsFromClone(clone, ["go", "does-not-exist"]);
  assert.deepEqual(out.map((s) => s.materializedName), ["org-go"]);
});

test("resolveSkillsFromClone: path-traversal names are rejected", async () => {
  const clone = await tmpTree({ "skills/go/SKILL.md": skillMD("go", "org") });
  const out = await resolveSkillsFromClone(clone, ["../secrets", "a/b", "go"]);
  assert.deepEqual(out.map((s) => s.materializedName), ["org-go"]);
});

// ---- resolveTaskSkills (orchestrator, injected clone) -----------------------

test("resolveTaskSkills: end-to-end with an injected clone", async () => {
  const ws = await tmpTree({
    "specs/design/design.md": "---\nskillsApplied:\n  - go\n---\n",
  });
  const cloneSrc = await tmpTree({ "skills/go/SKILL.md": skillMD("go", "org") });
  const scratchDir = path.join(os.tmpdir(), "aep-skills-orch", "task-1");

  let clonedRepoURL: string | undefined;
  let cloneCount = 0;
  const out = await resolveTaskSkills({
    workspace: ws,
    skillsRepoURL: "https://github.com/acme/org-skills",
    pat: "tok",
    scratchDir,
    clone: async (repoURL, _pat, dest) => {
      clonedRepoURL = repoURL;
      cloneCount += 1;
      // Fake the clone: copy the fixture tree into the scratch dir.
      await fs.promises.cp(cloneSrc, dest, { recursive: true });
    },
  });

  assert.equal(cloneCount, 1, "clone must be invoked once when skills are applied");
  assert.equal(clonedRepoURL, "https://github.com/acme/org-skills");
  assert.equal(out.length, 1);
  assert.equal(out[0].materializedName, "org-go");
});

test("resolveTaskSkills: no applied skills → no clone, empty result", async () => {
  const ws = await tmpTree({ "specs/design/design.md": "---\ntitle: x\n---\n" });
  let cloned = false;
  const out = await resolveTaskSkills({
    workspace: ws,
    skillsRepoURL: "https://github.com/acme/org-skills",
    pat: "tok",
    scratchDir: path.join(os.tmpdir(), "aep-skills-noop", "task-2"),
    clone: async () => {
      cloned = true;
    },
  });
  assert.equal(cloned, false, "clone must be skipped when no skills are applied");
  assert.deepEqual(out, []);
});
