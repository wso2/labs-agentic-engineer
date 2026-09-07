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
import { chmodSync, mkdtempSync, mkdirSync, symlinkSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import {
  readSnapshot,
  filterTurnSnapshot,
  keepInTurnSnapshot,
  loadSkillsFromSnapshot,
  readReferenceAttachments,
  overlayReferenceTexts,
  MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES,
  SkillReadError,
} from "../src/conversation/load-workspace.js";
import { buildSkillCatalog } from "../src/agents/main/prompt.js";
import type { LoadedSkillBody } from "../src/agents/main/skill-source.js";

/**
 * Narrow a `load()` result to its body. These fixtures never declare an
 * audience, so a defined result is never a refusal — asserting that here
 * keeps every call site below a plain property access.
 */
function body(result: LoadedSkillBody | { refused: true } | undefined): LoadedSkillBody | undefined {
  assert.ok(result === undefined || "content" in result, "expected a body, not a refusal");
  return result as LoadedSkillBody | undefined;
}

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
    "specs/requirements/prd.md": "# Req\n",
    "specs/design/domain-model.md": "# Domain model\n",
    "specs/design/design.cell": "title Shop\n",
    "specs/design/system.dsl": "workspace {}\n",
    "specs/design/components/api/design.json": "{}\n",
    // Produced + consumed OpenAPI contracts: the two admitted *.yaml shapes —
    // a turn must be able to read back the spec it just stored.
    "specs/design/components/api/openapi.yaml": "openapi: 3.0.3\n",
    "specs/design/components/api/dependencies/stripe.openapi.yaml": "openapi: 3.0.3\n",
    // Everything below must be EXCLUDED from the turn input:
    "specs/design/components/api/workload.yaml": "kind: Workload\n", // arbitrary yaml, not one of the two admitted shapes
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
      "specs/design/components/api/dependencies/stripe.openapi.yaml",
      "specs/design/components/api/design.json",
      "specs/design/components/api/openapi.yaml",
      "specs/design/design.cell",
      "specs/design/domain-model.md",
      "specs/design/system.dsl",
      "specs/requirements/prd.md",
    ]);
    assert.equal(snap["specs/requirements/prd.md"], "# Req\n");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("filterTurnSnapshot mirrors the walk's rules over an in-memory map", () => {
  const filtered = filterTurnSnapshot({
    "a.md": "x",
    "b/system.dsl": "y",
    "b/design.json": "z",
    "specs/validation/validation-criteria.json": "keep",
    "specs/design/components/api/openapi.yaml": "keep",
    "specs/design/components/api/dependencies/stripe.openapi.yaml": "keep",
    "b/openapi.yaml": "drop", // not under specs/design/components/*/
    "b/x.gen.json": "drop",
    ".hidden/inner.md": "drop",
    "b/.dot.md": "drop",
  });
  assert.deepEqual(Object.keys(filtered).sort(), [
    "a.md",
    "b/design.json",
    "b/system.dsl",
    "specs/design/components/api/dependencies/stripe.openapi.yaml",
    "specs/design/components/api/openapi.yaml",
    "specs/validation/validation-criteria.json",
  ]);
});

// A user-uploaded reference is not an agent-authored spec artifact, so the
// extension allow-list is the wrong test for it: it admits a .md reference and
// drops a .txt or .csv one, which then sits in the snapshot with nothing
// putting it in front of the model. The folder decides.
test("keepInTurnSnapshot admits text references whatever their extension", () => {
  for (const ext of ["md", "txt", "csv", "tsv", "json", "yaml", "yml", "xml", "html", "rst"]) {
    assert.equal(
      keepInTurnSnapshot(`specs/requirements/references/brief.${ext}`),
      true,
      `.${ext} reference should be readable as text`,
    );
  }
  // Outside the references folder the old rules still hold — admitting these
  // globally would put arbitrary yaml and json into every turn.
  assert.equal(keepInTurnSnapshot("specs/design/workload.yaml"), false);
  assert.equal(keepInTurnSnapshot("specs/requirements/rows.csv"), false);
});

// Natively-read binaries ride as file PARTS. Admitting one here would pour a
// PDF's bytes into the text map — the failure that channel exists to avoid.
test("keepInTurnSnapshot keeps natively-read binary references OUT of the text map", () => {
  for (const ext of ["pdf", "png", "jpg", "jpeg", "gif", "webp"]) {
    assert.equal(
      keepInTurnSnapshot(`specs/requirements/references/doc.${ext}`),
      false,
      `.${ext} reference must ride as a file part, not as text`,
    );
  }
});

test("keepInTurnSnapshot admits the two OpenAPI contract shapes but still rejects arbitrary yaml", () => {
  // Produced contract: specs/design/components/<c>/openapi.yaml
  assert.equal(keepInTurnSnapshot("specs/design/components/orders/openapi.yaml"), true);
  // Consumed contract: specs/design/components/<c>/dependencies/<dep>.openapi.yaml
  assert.equal(keepInTurnSnapshot("specs/design/components/orders/dependencies/stripe.openapi.yaml"), true);
  // Arbitrary *.yaml — including workload.yaml sitting right next to an admitted
  // spec — must stay excluded; only the two exact shapes above are admitted.
  assert.equal(keepInTurnSnapshot("specs/design/components/orders/workload.yaml"), false);
  assert.equal(keepInTurnSnapshot("workload.yaml"), false);
  assert.equal(keepInTurnSnapshot("specs/design/components/orders/openapi.yml"), false);
  // A `*` must not cross a path segment: nesting the dep name breaks the shape.
  assert.equal(keepInTurnSnapshot("specs/design/components/orders/dependencies/nested/stripe.openapi.yaml"), false);
});

// --- Reference PDF attachments (#384) -----------------------------------------

const REF_DIR = "specs/requirements/references";

test("readReferenceAttachments: a .pdf reference becomes a native file part with its exact bytes", () => {
  const pdfBytes = Buffer.from("%PDF-1.4 fake but binary-ish bytes \x00\x01\x02");
  const root = makeTree({ [`${REF_DIR}/brief.pdf`]: pdfBytes });
  try {
    const parts = readReferenceAttachments(root, [`${REF_DIR}/brief.pdf`]);
    assert.equal(parts.length, 1);
    const part = parts[0]!;
    assert.equal(part.type, "file");
    assert.equal(part.mediaType, "application/pdf");
    assert.equal(Buffer.from(part.data as string, "base64").toString("hex"), pdfBytes.toString("hex"));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("readReferenceAttachments: the .pdf match is case-insensitive", () => {
  const pdfBytes = Buffer.from("upper-case extension");
  const root = makeTree({ [`${REF_DIR}/BRIEF.PDF`]: pdfBytes });
  try {
    const parts = readReferenceAttachments(root, [`${REF_DIR}/BRIEF.PDF`]);
    assert.equal(parts.length, 1);
    assert.equal(parts[0]?.mediaType, "application/pdf");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("readReferenceAttachments: non-pdf references are ignored — they are already in the text snapshot", () => {
  const root = makeTree({ [`${REF_DIR}/notes.md`]: "# Notes\n", [`${REF_DIR}/notes.txt`]: "plain\n" });
  try {
    assert.deepEqual(readReferenceAttachments(root, [`${REF_DIR}/notes.md`, `${REF_DIR}/notes.txt`]), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("readReferenceAttachments: a missing pdf is skipped, never throws", () => {
  const root = mkdtempSync(join(tmpdir(), "aep-refs-"));
  try {
    assert.deepEqual(readReferenceAttachments(root, [`${REF_DIR}/absent.pdf`]), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("readReferenceAttachments: absent/empty references list yields no parts", () => {
  const root = mkdtempSync(join(tmpdir(), "aep-refs-"));
  try {
    assert.deepEqual(readReferenceAttachments(root, undefined), []);
    assert.deepEqual(readReferenceAttachments(root, []), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// The largest raw file that can fit the ENCODED budget — derived here rather
// than exported from the module, which has no production consumer for it.
const MAX_RAW_BYTES = Math.floor(MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES / 4) * 3;

test("readReferenceAttachments: a file over the cap is skipped, not truncated or thrown", () => {
  const big = Buffer.alloc(MAX_RAW_BYTES + 1, 1);
  const root = makeTree({ [`${REF_DIR}/huge.pdf`]: big });
  try {
    assert.deepEqual(readReferenceAttachments(root, [`${REF_DIR}/huge.pdf`]), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// The cap is on ENCODED bytes, and base64 costs 4 wire bytes per 3 raw. A file
// just under the budget in raw bytes is already over it once encoded — the
// request Anthropic sees is what has to fit.
test("readReferenceAttachments: the cap counts base64 bytes, not raw ones", () => {
  const rawJustUnderBudget = Buffer.alloc(MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES - 1, 1);
  const root = makeTree({ [`${REF_DIR}/wide.pdf`]: rawJustUnderBudget });
  try {
    assert.deepEqual(readReferenceAttachments(root, [`${REF_DIR}/wide.pdf`]), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// One budget for the whole turn: two PDFs that each pass on their own must not
// overrun the request together. The one that does not fit is dropped; the ones
// already attached stay.
test("readReferenceAttachments: the budget is spent across the turn's references", () => {
  const half = Buffer.alloc(Math.floor((MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES / 4) * 3 * 0.6), 1);
  const root = makeTree({
    [`${REF_DIR}/first.pdf`]: half,
    [`${REF_DIR}/second.pdf`]: half,
    [`${REF_DIR}/tiny.pdf`]: Buffer.from("%PDF-1.4 small"),
  });
  try {
    const parts = readReferenceAttachments(root, [
      `${REF_DIR}/first.pdf`,
      `${REF_DIR}/second.pdf`,
      `${REF_DIR}/tiny.pdf`,
    ]);
    // first fits; second would blow the budget; tiny still gets its chance
    // behind it — one oversized document must not starve the rest.
    assert.deepEqual(
      parts.map((p) => p.filename),
      [`${REF_DIR}/first.pdf`, `${REF_DIR}/tiny.pdf`],
    );
    const encoded = parts.reduce((n, p) => n + (p.data as string).length, 0);
    assert.ok(
      encoded <= MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES,
      `attachments encoded to ${encoded} bytes, over the ${MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES} budget`,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// The snapshot is a checkout of a user's repo, so a reference can be a committed
// symlink. `resolve` alone cannot see that — the path is textually in bounds —
// so the file's real path is what gets fenced.
test("readReferenceAttachments: a symlinked reference pointing out of the snapshot is refused", () => {
  const root = makeTree({ [`${REF_DIR}/brief.pdf`]: Buffer.from("in bounds") });
  const outside = makeTree({ "secret.pdf": Buffer.from("must never be read") });
  try {
    symlinkSync(join(outside, "secret.pdf"), join(root, REF_DIR, "escape.pdf"));
    assert.deepEqual(readReferenceAttachments(root, [`${REF_DIR}/escape.pdf`]), []);
    // The honest neighbour in the same dir still reads.
    assert.equal(readReferenceAttachments(root, [`${REF_DIR}/brief.pdf`]).length, 1);
  } finally {
    rmSync(root, { recursive: true, force: true });
    rmSync(outside, { recursive: true, force: true });
  }
});

// A PDF reaches the model through exactly one channel. It is not in the text
// snapshot because `keepInTurnSnapshot` admits no `.pdf` — not because of the
// walk's NUL-byte skip — so even a PDF whose bytes happen to carry no NUL stays
// out of `files` and cannot ride the turn twice.
test("readSnapshot: a NUL-free PDF is still not in the text snapshot", () => {
  const root = makeTree({
    [`${REF_DIR}/brief.pdf`]: Buffer.from("%PDF-1.4 no nul bytes in here at all"),
    [`${REF_DIR}/notes.md`]: "# Notes\n",
  });
  try {
    assert.deepEqual(Object.keys(readSnapshot(root)), [`${REF_DIR}/notes.md`]);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("readReferenceAttachments: a reference that would escape the snapshot dir is refused, never read", () => {
  const root = makeTree({ [`${REF_DIR}/brief.pdf`]: Buffer.from("in bounds") });
  const outside = makeTree({ "secret.pdf": Buffer.from("must never be read") });
  try {
    const outsideName = outside.split("/").pop();
    const parts = readReferenceAttachments(root, [`../${outsideName}/secret.pdf`]);
    assert.deepEqual(parts, []);
  } finally {
    rmSync(root, { recursive: true, force: true });
    rmSync(outside, { recursive: true, force: true });
  }
});

const SKILL_MD = (name: string, description: string, body: string): string =>
  `---\nname: ${name}\ndescription: ${description}\n---\n\n${body}\n`;

test("skills snapshot: FLAT layout (skills/<name>/) with kind in frontmatter", () => {
  const root = makeTree({
    // The current org-skills repo shape: no kind dirs; kind lives in
    // metadata.aep.kind (and is irrelevant to the catalog scan).
    "skills/go/SKILL.md": SKILL_MD("go", "Go conventions", "Write idiomatic Go."),
    "skills/architecture/SKILL.md":
      "---\nname: architecture\ndescription: derive components\nmetadata:\n  aep:\n    kind: platform\n---\n\nComponents live under specs/design.\n",
    "skills/org-style/SKILL.md": SKILL_MD("org-style", "house style", "Use our tone."),
    "skills/org-style/references/tone.md": "REF BODY — tone guide",
    // A dir without SKILL.md that is NOT a legacy kind dir is not a skill:
    "skills/broken/readme.md": "no SKILL.md here",
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(
      source.catalog().map((e) => e.name),
      ["architecture", "go", "org-style"],
    );
    assert.equal(body(source.load("go"))?.content, "Write idiomatic Go.");
    assert.equal(body(source.load("architecture"))?.content, "Components live under specs/design.");
    assert.deepEqual(source.loadReference("org-style", "references/tone.md"), { content: "REF BODY — tone guide" });
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
    assert.equal(body(source.load("go"))?.content, "FLAT BODY");
    assert.equal(body(source.load("task-planning"))?.content, "PLAN BODY");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("skills snapshot: LEGACY nested layout still scans (old snapshots), lazy bodies, references", () => {
  const root = makeTree({
    "skills/builtin/go/SKILL.md": SKILL_MD("go", "Go conventions", "Write idiomatic Go."),
    "skills/flow/architecture/SKILL.md": SKILL_MD(
      "architecture",
      "derive components",
      "Components live under specs/design/components.",
    ),
    "skills/custom/org-style/SKILL.md": SKILL_MD("org-style", "house style", "Use our tone.\n\nSee references/tone.md."),
    "skills/custom/org-style/references/tone.md": "REF BODY — tone guide",
    "skills/custom/org-style/references/notes.txt": "any extension is addressable, not just .md",
    // A dir without SKILL.md is not a skill:
    "skills/imported/broken/readme.md": "no SKILL.md here",
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    // Deterministic order: kinds sorted (builtin < custom < flow < imported), dirs sorted within.
    assert.deepEqual(
      source.catalog().map((e) => e.name),
      ["go", "org-style", "architecture"],
    );
    assert.deepEqual(
      source.catalog().map((e) => e.hasReferences),
      [false, true, false],
    );

    // Lazy body read (frontmatter stripped, trimmed) + full aux-file listing (any extension).
    assert.equal(body(source.load("go"))?.content, "Write idiomatic Go.");
    assert.deepEqual(body(source.load("org-style"))?.references, ["references/notes.txt", "references/tone.md"]);
    assert.deepEqual(source.loadReference("org-style", "references/tone.md"), { content: "REF BODY — tone guide" });
    assert.deepEqual(source.loadReference("org-style", "references/notes.txt"), {
      content: "any extension is addressable, not just .md",
    });

    // Misses are undefined; reference paths are allowlisted (no raw fs resolution).
    assert.equal(source.load("nope"), undefined);
    assert.equal(source.loadReference("org-style", "references/missing.md"), undefined);
    assert.equal(source.loadReference("org-style", "../../../etc/passwd"), undefined);
    assert.equal(source.loadReference("go", "references/tone.md"), undefined);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("skills snapshot: full Agent Skills structure — recursive walk lists every aux file, SKILL.md and dotfiles skipped", () => {
  const root = makeTree({
    "skills/toolkit/SKILL.md": SKILL_MD("toolkit", "full aux structure", "See scripts/run.mjs and assets/logo.png."),
    "skills/toolkit/references/a.md": "REF A",
    "skills/toolkit/scripts/run.mjs": "export default () => {};\n",
    // PNG magic bytes + a NUL — not valid UTF-8 text.
    "skills/toolkit/assets/logo.png": Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x00, 0x0d, 0x0a]),
    "skills/toolkit/extra/deep/n.txt": "nested text",
    "skills/toolkit/.hidden.txt": "dotfile — skipped",
    "skills/toolkit/.hiddendir/x.md": "dot dir — skipped",
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(body(source.load("toolkit"))?.references, [
      "assets/logo.png",
      "extra/deep/n.txt",
      "references/a.md",
      "scripts/run.mjs",
    ]);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("loadSkillReference: text file → content, binary file → a binary marker (never inlined into context)", () => {
  const root = makeTree({
    "skills/toolkit/SKILL.md": SKILL_MD("toolkit", "full aux structure", "See scripts/run.mjs and assets/logo.png."),
    "skills/toolkit/scripts/run.mjs": "export default () => {};\n",
    "skills/toolkit/assets/logo.png": Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x00, 0x0d, 0x0a]),
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(source.loadReference("toolkit", "scripts/run.mjs"), { content: "export default () => {};\n" });
    assert.deepEqual(source.loadReference("toolkit", "assets/logo.png"), { binary: true });
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
    // These fixtures declare no audience, so each resolves to every audience —
    // the permissive default that keeps an unmarked skill loadable (ADR-0013).
    assert.deepEqual(fromDisk.catalog(), [
      { name: "alpha", description: "does A", hasReferences: false, audience: ["design", "coding"] },
      { name: "beta", description: "does B", hasReferences: true, audience: ["design", "coding"] },
    ]);

    const catalog = buildSkillCatalog(fromDisk);
    assert.match(catalog, /- alpha: does A/);
    assert.match(catalog, /- beta: does B/);
    assert.match(catalog, /loadSkillReference/); // beta carries references

    // And the lazily-loaded body is the literal SKILL.md body (frontmatter stripped).
    assert.equal(body(fromDisk.load("beta"))?.content, "BODY B\n\nSee references/deep.md.");
    assert.deepEqual(body(fromDisk.load("beta"))?.references, ["references/deep.md"]);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("load throws SkillReadError on non-ENOENT I/O (not collapsed to a miss)", () => {
  const root = makeTree({
    "skills/go/SKILL.md": SKILL_MD("go", "Go guidance", "Write idiomatic Go."),
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.ok(source.load("go"));
    const skillMd = join(root, "skills/go/SKILL.md");
    chmodSync(skillMd, 0);
    assert.throws(() => source.load("go"), (err: unknown) => {
      assert.ok(err instanceof SkillReadError);
      assert.match(err.message, /could not read skill file/);
      return true;
    });
    // Unknown name stays a soft miss — never a SkillReadError.
    assert.equal(source.load("nope"), undefined);
  } finally {
    try {
      chmodSync(join(root, "skills/go/SKILL.md"), 0o644);
    } catch {
      /* restore best-effort so rmSync can clean up */
    }
    rmSync(root, { recursive: true, force: true });
  }
});

test("catalog scan fails when a SKILL.md exists but is unreadable", () => {
  const root = makeTree({
    "skills/go/SKILL.md": SKILL_MD("go", "Go guidance", "Write idiomatic Go."),
  });
  try {
    chmodSync(join(root, "skills/go/SKILL.md"), 0);
    assert.throws(() => loadSkillsFromSnapshot(root), (err: unknown) => err instanceof SkillReadError);
  } finally {
    try {
      chmodSync(join(root, "skills/go/SKILL.md"), 0o644);
    } catch {
      /* restore best-effort */
    }
    rmSync(root, { recursive: true, force: true });
  }
});

test("listReferences throws SkillReadError when an aux dir is unreadable", () => {
  const root = makeTree({
    "skills/toolkit/SKILL.md": SKILL_MD("toolkit", "full aux", "See references/."),
    "skills/toolkit/references/a.md": "REF A",
  });
  try {
    const source = loadSkillsFromSnapshot(root);
    assert.deepEqual(body(source.load("toolkit"))?.references, ["references/a.md"]);
    chmodSync(join(root, "skills/toolkit/references"), 0);
    assert.throws(() => source.load("toolkit"), (err: unknown) => err instanceof SkillReadError);
  } finally {
    try {
      chmodSync(join(root, "skills/toolkit/references"), 0o755);
    } catch {
      /* restore best-effort */
    }
    rmSync(root, { recursive: true, force: true });
  }
});

// A room-scoped turn's CURRENT STATE comes from the collab room, and the room
// (correctly) excludes reference documents — so text references must ride in
// from the GIT snapshot, or a room turn silently loses the user's brief. A
// live /start did exactly that: claim.md was listed in the steer, present in
// the snapshot, and absent from the prompt.
test("overlayReferenceTexts: git reference texts join the room files", () => {
  const room = { "specs/requirements/prd.md": "# PRD (room)" };
  const git = {
    "specs/requirements/prd.md": "# PRD (git, stale)",
    [`${REF_DIR}/claim.md`]: "Single web app.",
    "README.md": "root",
  };
  const merged = overlayReferenceTexts(room, git);
  assert.equal(merged[`${REF_DIR}/claim.md`], "Single web app.");
  // The room stays the authority for everything that is not a reference.
  assert.equal(merged["specs/requirements/prd.md"], "# PRD (room)");
  assert.equal(merged["README.md"], undefined);
});

test("overlayReferenceTexts: git wins over a stale room copy of a reference", () => {
  // Rooms seeded before the exclusion existed may still carry reference
  // entries (possibly doubled or mangled) — git is the authority for them.
  const room = { [`${REF_DIR}/claim.md`]: "poisoned room copy" };
  const git = { [`${REF_DIR}/claim.md`]: "the real content" };
  assert.equal(overlayReferenceTexts(room, git)[`${REF_DIR}/claim.md`], "the real content");
});

test("overlayReferenceTexts: no references → the room files return unchanged", () => {
  const room = { "specs/requirements/prd.md": "# PRD" };
  const merged = overlayReferenceTexts(room, { "README.md": "hi" });
  assert.deepEqual(merged, room);
});

test("readReferenceAttachments: image references become native image-typed file parts (#383 follow-up)", () => {
  const png = Buffer.from("89504e470d0a1a0a0000", "hex"); // PNG magic + padding
  const jpg = Buffer.from("ffd8ffe000104a464946", "hex"); // JPEG magic
  const root = makeTree({ [`${REF_DIR}/mockup.png`]: png, [`${REF_DIR}/photo.JPG`]: jpg });
  try {
    const parts = readReferenceAttachments(root, [`${REF_DIR}/mockup.png`, `${REF_DIR}/photo.JPG`]);
    assert.equal(parts.length, 2);
    assert.equal(parts[0]?.mediaType, "image/png");
    assert.equal(Buffer.from(parts[0]?.data as string, "base64").toString("hex"), png.toString("hex"));
    assert.equal(parts[1]?.mediaType, "image/jpeg");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
