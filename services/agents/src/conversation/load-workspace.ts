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
 * Workspace-shape input loading (shared-volume-clone-architecture §12, D4): the
 * ONLY module that reads the shared mount. Both readers take a directory that
 * `snapshot-path.ts` derived and stat-checked — nothing here touches untrusted
 * paths, and nothing here writes.
 *
 *  - `readSnapshot(dir)` walks an immutable per-SHA repo snapshot into the
 *    in-memory `files` map a turn runs against (adapted from the proven
 *    playground `threads.ts` walk — POSIX-relative keys, dot-entries and binary
 *    skipped),
 *    with the aep-api `genai.keepInSnapshot` filter mirrored on top so the turn
 *    input stays a pure function of the sha while derived artifacts stay out.
 *  - `loadSkillsFromSnapshot(dir)` scans the `_skills` snapshot's
 *    `skills/<kind>/<name>/SKILL.md` catalog (frontmatter only — bodies are NOT
 *    retained) and returns a `SkillSource` whose `loadSkill`/
 *    `loadSkillReference` read from disk ON DEMAND: progressive disclosure is
 *    truly lazy, and D4 immutability makes the mid-turn reads race-free.
 */

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { basename, join } from "node:path";
import { parse as parseYaml } from "yaml";
// Reuse the bundle's single frontmatter grammar + LF canonicalizer so SKILL.md
// fence parsing cannot drift from the spec-file fence parsing (same approach as
// the eval-side resolver in `evals/skills.ts`, which this adapts).
import { FRONTMATTER_RE, lf } from "@aep/agent-stream";
import type { SkillCatalogEntry, SkillSource, LoadedSkillBody } from "../agents/main/skill-source.js";

// --- The repo snapshot → `files` map -----------------------------------------

/**
 * The turn-snapshot filter — mirrors aep-api `genai.keepInSnapshot`: keep
 * agent-authored sources (`*.md`, `*.dsl`, component `design.json`) and drop
 * everything else (derived `.excalidraw`/`*.gen.json` projections, code, …).
 */
export function keepInTurnSnapshot(path: string): boolean {
  if (path.endsWith(".md") || path.endsWith(".dsl")) return true;
  return basename(path) === "design.json";
}

/**
 * Apply the walk's skip rules to an in-memory map (parity helper for callers
 * that mirror the server's view, e.g. the eval fold): dot-led path segments and
 * filtered-out paths drop; values are assumed text.
 */
export function filterTurnSnapshot(files: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [path, content] of Object.entries(files)) {
    if (path.split("/").some((seg) => seg.startsWith("."))) continue;
    if (!keepInTurnSnapshot(path)) continue;
    out[path] = content;
  }
  return out;
}

function walk(dir: string, rel: string, out: Record<string, string>): void {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name.startsWith(".")) continue; // dot-entries (.git, .DS_Store, …)
    const abs = join(dir, e.name);
    const key = rel ? `${rel}/${e.name}` : e.name; // POSIX-relative key
    if (e.isDirectory()) {
      walk(abs, key, out);
      continue;
    }
    if (!e.isFile()) continue;
    if (!keepInTurnSnapshot(key)) continue;
    const buf = readFileSync(abs);
    if (buf.includes(0)) continue; // a NUL byte → binary; the agent only edits text
    out[key] = buf.toString("utf8");
  }
}

/**
 * Read one immutable per-SHA snapshot dir into the in-memory `files` map a turn
 * runs against (feeding `new FileBundle(map)` / `new TaskPlan(map)` and
 * `buildPrompt` byte-unchanged). Adapted from `playground/threads.ts`
 * `readSnapshot` (copied — `src/` must not import the playground tree).
 */
export function readSnapshot(snapshotDir: string): Record<string, string> {
  const out: Record<string, string> = {};
  walk(snapshotDir, "", out);
  return out;
}

// --- The `_skills` snapshot → lazy SkillSource --------------------------------

/** Split a `SKILL.md` into frontmatter fields + body (adapted from `evals/skills.ts`). */
function parseSkillMd(raw: string): { name?: string; description: string; body: string } {
  const text = lf(raw);
  const m = FRONTMATTER_RE.exec(text);
  const frontmatter = m?.[1] ?? "";
  const body = m ? text.slice(m[0].length) : text;
  let fm: Record<string, unknown> = {};
  if (frontmatter.trim() !== "") {
    try {
      const parsed = parseYaml(frontmatter) as unknown;
      if (parsed && typeof parsed === "object") fm = parsed as Record<string, unknown>;
    } catch {
      // Unparseable frontmatter → treat as absent; the dir name still names the skill.
    }
  }
  return {
    ...(typeof fm.name === "string" && fm.name.trim() !== "" ? { name: fm.name } : {}),
    description: typeof fm.description === "string" ? fm.description : "",
    body,
  };
}

/** Sorted `references/<file>.md` paths for one skill dir (readdir at call time — D4-immutable). */
function listReferences(skillDir: string): string[] {
  const refsDir = join(skillDir, "references");
  if (!existsSync(refsDir)) return [];
  return readdirSync(refsDir, { withFileTypes: true })
    .filter((f) => f.isFile() && f.name.endsWith(".md") && !f.name.startsWith("."))
    .map((f) => `references/${f.name}`)
    .sort();
}

interface CatalogRow extends SkillCatalogEntry {
  /** Absolute skill dir — the lazy loaders read SKILL.md/references from here. */
  dir: string;
}

/**
 * The disk-backed `SkillSource` over one immutable `_skills` snapshot. The
 * constructor scans ONLY the catalog surface (frontmatter name/description +
 * whether references exist); bodies are read from disk when `loadSkill` /
 * `loadSkillReference` actually run.
 */
export class SnapshotSkillSource implements SkillSource {
  private readonly rows: CatalogRow[];
  private readonly byName: Map<string, CatalogRow>;

  constructor(skillsSnapshotDir: string) {
    this.rows = scanCatalog(skillsSnapshotDir);
    this.byName = new Map(this.rows.map((r) => [r.name, r] as const));
  }

  catalog(): readonly SkillCatalogEntry[] {
    return this.rows.map(({ name, description, hasReferences }) => ({ name, description, hasReferences }));
  }

  load(name: string): LoadedSkillBody | undefined {
    const row = this.byName.get(name);
    if (row === undefined) return undefined;
    let raw: string;
    try {
      raw = readFileSync(join(row.dir, "SKILL.md"), "utf8");
    } catch {
      return undefined; // vanished mid-turn — impossible for an immutable snapshot, but stay honest
    }
    return { content: parseSkillMd(raw).body.trim(), references: listReferences(row.dir) };
  }

  loadReference(name: string, path: string): string | undefined {
    const row = this.byName.get(name);
    if (row === undefined) return undefined;
    // Fence-by-allowlist: the model-supplied path must be one of the LISTED
    // reference paths (never resolved raw against the fs).
    if (!listReferences(row.dir).includes(path)) return undefined;
    try {
      return readFileSync(join(row.dir, path), "utf8");
    } catch {
      return undefined;
    }
  }
}

/**
 * The RETIRED kind path-segments of the pre-flat org-skills layout
 * (`skills/<kindDir>/<name>/`). Old per-SHA snapshots keep this shape forever,
 * so the scan tolerates it alongside the current flat layout.
 */
const LEGACY_KIND_DIRS = new Set(["builtin", "flow", "custom", "imported"]);

/**
 * Scan the snapshot's skill catalog into rows. The current shape is FLAT —
 * `<snapshotDir>/skills/<name>/SKILL.md` with the kind in frontmatter
 * (`metadata.aep.kind`; irrelevant to this scan) — and the legacy nested shape
 * `skills/<kindDir>/<name>/SKILL.md` is tolerated for old snapshots.
 * Deterministic order: flat dirs sorted first, then legacy kind dirs sorted
 * with skill dirs sorted within each; a duplicate skill NAME keeps its first
 * occurrence (so a flat copy wins over a legacy one). A dir without a readable
 * SKILL.md is simply not a skill; a snapshot without `skills/` yields an empty
 * catalog.
 */
function scanCatalog(snapshotDir: string): CatalogRow[] {
  const skillsRoot = join(snapshotDir, "skills");
  if (!existsSync(skillsRoot)) return [];
  const rows: CatalogRow[] = [];
  const seen = new Set<string>();
  const addSkillDir = (dir: string, id: string): void => {
    let raw: string;
    try {
      raw = readFileSync(join(dir, "SKILL.md"), "utf8");
    } catch {
      return;
    }
    const parsed = parseSkillMd(raw);
    const name = parsed.name ?? id; // fallback: the dir name IS the skill id
    if (seen.has(name)) return;
    seen.add(name);
    rows.push({
      name,
      description: parsed.description,
      hasReferences: listReferences(dir).length > 0,
      dir,
    });
  };

  // Flat layout first (a dir holding SKILL.md directly IS a skill — even one
  // named like a kind word; those names are reserved server-side).
  const legacyKindDirs: string[] = [];
  for (const entry of listDirs(skillsRoot)) {
    const dir = join(skillsRoot, entry);
    if (existsSync(join(dir, "SKILL.md"))) {
      addSkillDir(dir, entry);
    } else if (LEGACY_KIND_DIRS.has(entry)) {
      legacyKindDirs.push(entry);
    }
  }
  for (const kind of legacyKindDirs) {
    for (const id of listDirs(join(skillsRoot, kind))) {
      addSkillDir(join(skillsRoot, kind, id), id);
    }
  }
  return rows;
}

function listDirs(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true })
    .filter((d) => d.isDirectory() && !d.name.startsWith("."))
    .map((d) => d.name)
    .sort((a, b) => a.localeCompare(b));
}

/** Build the lazy, disk-backed skill source over one `_skills` snapshot dir. */
export function loadSkillsFromSnapshot(skillsSnapshotDir: string): SnapshotSkillSource {
  return new SnapshotSkillSource(skillsSnapshotDir);
}
