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
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { readSnapshot, filterTurnSnapshot, loadSkillsFromSnapshot } from "./load-workspace.js";
import { buildSkillCatalog } from "../agents/main/prompt.js";

function makeTree(files: Record<string, string | Buffer>): string {
  const root = mkdtempSync(join(tmpdir(), "aep-snap-"));
  for (const [rel, content] of Object.entries(files)) {
    const abs = join(root, rel);
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, content);
  }
  return root;
}

test("readSnapshot walks recursively with POSIX keys and applies the turn filter", () => {
  const root = makeTree({
    "specs/requirements/requirements.md": "# Req\n",
    "specs/design/design.md": "# Design\n",
    "specs/design/system.dsl": "workspace {}\n",
    "specs/design/components/api/design.json": "{}\n",
    // Everything below must be EXCLUDED from the turn input:
    "specs/design/components/api/openapi.yaml": "openapi: 3.0.3\n", // not .md/.dsl/design.json
    "specs/design/components/api/api.gen.json": "{}\n", // derived projection
    "specs/design/wireframe.excalidraw": "{}\n", // derived scene
    "src/main.go": "package main\n", // code
    ".hidden.md": "dot\n", // dot-entry
    ".git/config.md": "dot dir\n", // dot dir
    "specs/binary.md": Buffer.from([0x68, 0x00, 0x69]), // NUL → binary
  });
  try {
    const snap = readSnapshot(root);
    assert.deepEqual(Object.keys(snap).sort(), [
      "specs/design/components/api/design.json",
      "specs/design/design.md",
      "specs/design/system.dsl",
      "specs/requirements/requirements.md",
    ]);
    assert.equal(snap["specs/requirements/requirements.md"], "# Req\n");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("filterTurnSnapshot mirrors the walk's rules over an in-memory map", () => {
  const filtered = filterTurnSnapshot({
    "a.md": "x",
    "b/system.dsl": "y",
    "b/design.json": "z",
    "b/openapi.yaml": "drop",
    "b/x.gen.json": "drop",
    ".hidden/inner.md": "drop",
    "b/.dot.md": "drop",
  });
  assert.deepEqual(Object.keys(filtered).sort(), ["a.md", "b/design.json", "b/system.dsl"]);
});

const SKILL_MD = (name: string, description: string, body: string): string =>
  `---\nname: ${name}\ndescription: ${description}\n---\n\n${body}\n`;

test("skills snapshot: FLAT layout (skills/<name>/) with kind in frontmatter", () => {
  const root = makeTree({
    // The current org-skills repo shape: no kind dirs; kind lives in
    // metadata.aep.kind (and is irrelevant to the catalog scan).
    "skills/go/SKILL.md": SKILL_MD("go", "Go conventions", "Write idiomatic Go."),
    "skills/high-level-architecture/SKILL.md":
      "---\nname: high-level-architecture\ndescription: derive components\nmetadata:\n  aep:\n    kind: platform\n---\n\nComponents live under specs/design.\n",
    "skills/org-style/SKILL.md": SKILL_MD("org-style", "house style", "Use our tone."),
    "skills/org-style/references/tone.md": "REF BODY — tone guide",
    // A dir without SKILL.md that is NOT a legacy kind dir is not a skill:
    "skills/broken/readme.md": "no SKILL.md here",
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(
      source.catalog().map((e) => e.name),
      ["go", "high-level-architecture", "org-style"],
    );
    assert.equal(source.load("go")?.content, "Write idiomatic Go.");
    assert.equal(source.load("high-level-architecture")?.content, "Components live under specs/design.");
    assert.equal(source.loadReference("org-style", "references/tone.md"), "REF BODY — tone guide");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("skills snapshot: mixed flat + legacy — flat wins a duplicate name", () => {
  const root = makeTree({
    "skills/go/SKILL.md": SKILL_MD("go", "flat copy", "FLAT BODY"),
    "skills/builtin/go/SKILL.md": SKILL_MD("go", "legacy copy", "LEGACY BODY"),
    "skills/flow/task-planning/SKILL.md": SKILL_MD("task-planning", "plan tasks", "PLAN BODY"),
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(
      source.catalog().map((e) => e.name),
      ["go", "task-planning"],
    );
    assert.equal(source.catalog()[0]?.description, "flat copy");
    assert.equal(source.load("go")?.content, "FLAT BODY");
    assert.equal(source.load("task-planning")?.content, "PLAN BODY");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("skills snapshot: LEGACY nested layout still scans (old snapshots), lazy bodies, references", () => {
  const root = makeTree({
    "skills/builtin/go/SKILL.md": SKILL_MD("go", "Go conventions", "Write idiomatic Go."),
    "skills/flow/high-level-architecture/SKILL.md": SKILL_MD(
      "high-level-architecture",
      "derive components",
      "Components live under specs/design/components.",
    ),
    "skills/custom/org-style/SKILL.md": SKILL_MD("org-style", "house style", "Use our tone.\n\nSee references/tone.md."),
    "skills/custom/org-style/references/tone.md": "REF BODY — tone guide",
    "skills/custom/org-style/references/notes.txt": "not a .md — never addressable",
    // A dir without SKILL.md is not a skill:
    "skills/imported/broken/readme.md": "no SKILL.md here",
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    // Deterministic order: kinds sorted (builtin < custom < flow < imported), dirs sorted within.
    assert.deepEqual(
      source.catalog().map((e) => e.name),
      ["go", "org-style", "high-level-architecture"],
    );
    assert.deepEqual(
      source.catalog().map((e) => e.hasReferences),
      [false, true, false],
    );

    // Lazy body read (frontmatter stripped, trimmed) + reference listing.
    assert.equal(source.load("go")?.content, "Write idiomatic Go.");
    assert.deepEqual(source.load("org-style")?.references, ["references/tone.md"]);
    assert.equal(source.loadReference("org-style", "references/tone.md"), "REF BODY — tone guide");

    // Misses are undefined; reference paths are allowlisted (no raw fs resolution).
    assert.equal(source.load("nope"), undefined);
    assert.equal(source.loadReference("org-style", "references/missing.md"), undefined);
    assert.equal(source.loadReference("org-style", "../../../etc/passwd"), undefined);
    assert.equal(source.loadReference("org-style", "references/notes.txt"), undefined);
    assert.equal(source.loadReference("go", "references/tone.md"), undefined);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("frontmatter name falls back to the dir name; missing skills/ dir → empty catalog", () => {
  const root = makeTree({
    "skills/custom/from-dir/SKILL.md": "---\ndescription: no name field\n---\n\nBody.\n",
  });
  const empty = mkdtempSync(join(tmpdir(), "aep-snap-empty-"));
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(
      source.catalog().map((e) => e.name),
      ["from-dir"],
    );
    assert.equal(source.catalog()[0]?.description, "no name field");

    assert.deepEqual(loadSkillsFromSnapshot(empty).catalog(), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
    rmSync(empty, { recursive: true, force: true });
  }
});

test("the SnapshotSkillSource catalog + system-prompt rendering match the snapshot's literal entries", () => {
  const root = makeTree({
    "skills/builtin/alpha/SKILL.md": SKILL_MD("alpha", "does A", "BODY A"),
    "skills/custom/beta/SKILL.md": SKILL_MD("beta", "does B", "BODY B\n\nSee references/deep.md."),
    "skills/custom/beta/references/deep.md": "DEEP",
  });
  try {
    const fromDisk = loadSkillsFromSnapshot(root);
    assert.deepEqual(fromDisk.catalog(), [
      { name: "alpha", description: "does A", hasReferences: false },
      { name: "beta", description: "does B", hasReferences: true },
    ]);

    const catalog = buildSkillCatalog(fromDisk);
    assert.match(catalog, /- alpha: does A/);
    assert.match(catalog, /- beta: does B/);
    assert.match(catalog, /loadSkillReference/); // beta carries references

    // And the lazily-loaded body is the literal SKILL.md body (frontmatter stripped).
    assert.equal(fromDisk.load("beta")?.content, "BODY B\n\nSee references/deep.md.");
    assert.deepEqual(fromDisk.load("beta")?.references, ["references/deep.md"]);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
