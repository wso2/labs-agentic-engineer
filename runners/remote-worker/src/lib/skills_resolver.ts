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

// Per-task skills resolution from a local `org-skills` clone.
//
// Replaces the retired S2S skills-pull (GET /internal/v1/executions/{id}/skills):
// the runner already clones the project repo and holds an org-wide GitHub PAT,
// so it clones the org's `org-skills` repo too and resolves the design's applied
// skills locally — no second BFF round-trip, one git-based delivery mechanism.
// See docs/design/coding-runner-skills-clone.md.
//
// Flow (per task):
//   1. Read specs/design/design.md → skillsApplied[] from the PROJECT clone.
//   2. git clone --depth 1 the org-skills repo into a scratch dir OUTSIDE the
//      work tree (so its nested .git never enters the agent's git status).
//   3. For each bare name resolve skills/<name>/SKILL.md (+ references/*.md) and
//      its kind (frontmatter metadata.aep.kind; absent → "org"). Missing names
//      are dropped (parity with the old server-side ResolveMany warn-and-skip).
//
// The resulting SkillResolution[] feeds materializeSkills unchanged.

import fs from "node:fs";
import path from "node:path";
import { exec } from "node:child_process";
import { promisify } from "node:util";
import { parse as parseYaml } from "yaml";
import { shellQuote } from "./workspace.js";
import type { SkillKind, SkillResolution } from "./skills_materializer.js";

const execAsync = promisify(exec);

// The single root design file whose frontmatter carries skillsApplied — pinned
// by the BFF artifact store (specs/design/design.md, key `skillsApplied`).
const DESIGN_REL = "specs/design/design.md";
const SKILLS_ROOT = "skills";
const KNOWN_KINDS: readonly SkillKind[] = ["platform", "org", "custom", "imported"];

// Leading YAML frontmatter fence (LF-normalized), same grammar the BFF and
// @aep/design-projection use.
const FRONTMATTER_RE = /^---\n([\s\S]*?)\n---/;

export interface ResolveTaskSkillsArgs {
  /** The project work tree (holds specs/design/design.md). */
  workspace: string;
  /** AEP_SKILLS_REPO_URL — the org's `org-skills` clone URL. */
  skillsRepoURL: string;
  /** Org-wide GitHub PAT (x-access-token) for the clone. */
  pat: string;
  /** Scratch dir to clone org-skills into — MUST be outside `workspace`. */
  scratchDir: string;
  /** Per-line log sink; defaults to console.log. */
  log?: (line: string) => void;
  /** Injected for tests; defaults to the real `git clone --depth 1`. */
  clone?: (repoURL: string, pat: string, destDir: string) => Promise<void>;
}

/**
 * Resolve the design's applied skills from a fresh org-skills clone. Returns an
 * empty list (NOT an error) when the design applies no skills or design.md is
 * absent. Throws only on a clone failure — the caller degrades to the base
 * plugin.
 */
export async function resolveTaskSkills(args: ResolveTaskSkillsArgs): Promise<SkillResolution[]> {
  const log = args.log ?? ((l: string) => console.log(l));

  const names = await readSkillsApplied(args.workspace, log);
  if (names.length === 0) {
    log("[skills-resolve] design applies no skills — nothing to materialise");
    return [];
  }

  const clone = args.clone ?? cloneSkillsRepo;
  await clone(args.skillsRepoURL, args.pat, args.scratchDir);

  return resolveSkillsFromClone(args.scratchDir, names, log);
}

// readSkillsApplied reads specs/design/design.md from the project clone and
// returns its frontmatter `skillsApplied` (a sequence of bare skill names).
// Absent file → loud warn + []; present-but-no-skills → quiet [].
export async function readSkillsApplied(
  workspace: string,
  log: (line: string) => void = () => {},
): Promise<string[]> {
  const designPath = path.join(workspace, DESIGN_REL);
  let raw: string;
  try {
    raw = await fs.promises.readFile(designPath, "utf-8");
  } catch {
    log(`[skills-resolve] ⚠️  ${DESIGN_REL} not found — proceeding with no applied skills`);
    return [];
  }
  const block = frontmatterBlock(raw);
  if (!block) return [];
  try {
    const fm = parseYaml(block) as unknown;
    const applied = (fm as { skillsApplied?: unknown } | null)?.skillsApplied;
    if (Array.isArray(applied)) {
      return applied.filter((s): s is string => typeof s === "string");
    }
  } catch {
    /* malformed frontmatter → no skills */
  }
  return [];
}

// resolveSkillsFromClone maps each bare name to skills/<name>/SKILL.md (+ any
// references/*.md) in the flat org-skills layout, deriving the kind from the
// SKILL.md frontmatter. Missing/unsafe names are dropped with a warning.
export async function resolveSkillsFromClone(
  cloneDir: string,
  names: string[],
  log: (line: string) => void = () => {},
): Promise<SkillResolution[]> {
  const out: SkillResolution[] = [];
  for (const name of names) {
    if (!isSafeSkillName(name)) {
      log(`[skills-resolve] skipping unsafe skill name ${JSON.stringify(name)}`);
      continue;
    }
    const skillDir = path.join(cloneDir, SKILLS_ROOT, name);
    let skillMd: string;
    try {
      skillMd = await fs.promises.readFile(path.join(skillDir, "SKILL.md"), "utf-8");
    } catch {
      log(`[skills-resolve] applied skill ${JSON.stringify(name)} not found in org-skills — skipping`);
      continue;
    }
    const kind = resolveKind(skillMd);
    const references = await readReferences(skillDir);
    out.push({ materializedName: `${kind}-${name}`, kind, skillMd, references });
  }
  return out;
}

// cloneSkillsRepo shallow-clones the org-skills repo into destDir, authing https
// URLs with the org-wide PAT (file:// origins — tests — pass through unauthed).
// Wipes destDir first so a resumed pod's stale dir never blocks `git clone`.
async function cloneSkillsRepo(repoURL: string, pat: string, destDir: string): Promise<void> {
  await fs.promises.rm(destDir, { recursive: true, force: true });
  await fs.promises.mkdir(path.dirname(destDir), { recursive: true });
  const authedURL = repoURL.replace("https://", `https://x-access-token:${pat}@`);
  const cmd = `git clone --depth 1 ${shellQuote(authedURL)} ${shellQuote(destDir)}`;
  await execAsync(cmd, {
    env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
    maxBuffer: 16 * 1024 * 1024,
  });
}

// readReferences returns references/<file>.md → content for a skill dir (empty
// when the skill ships no references). Keys match the materializer's contract.
async function readReferences(skillDir: string): Promise<Record<string, string>> {
  const refs: Record<string, string> = {};
  const refDir = path.join(skillDir, "references");
  let entries: fs.Dirent[];
  try {
    entries = await fs.promises.readdir(refDir, { withFileTypes: true });
  } catch {
    return refs; // no references/ dir
  }
  for (const e of entries) {
    if (!e.isFile() || !e.name.endsWith(".md")) continue;
    refs[`references/${e.name}`] = await fs.promises.readFile(path.join(refDir, e.name), "utf-8");
  }
  return refs;
}

// resolveKind mirrors the BFF's frontmatterKind: trimmed metadata.aep.kind when
// it names a known kind, else "org" (an unmarked SKILL.md is an org skill).
export function resolveKind(skillMd: string): SkillKind {
  const block = frontmatterBlock(skillMd);
  if (!block) return "org";
  try {
    const fm = parseYaml(block) as { metadata?: { aep?: { kind?: unknown } } } | null;
    const raw = fm?.metadata?.aep?.kind;
    const k = typeof raw === "string" ? raw.trim() : "";
    if ((KNOWN_KINDS as readonly string[]).includes(k)) return k as SkillKind;
  } catch {
    /* malformed frontmatter → org */
  }
  return "org";
}

// frontmatterBlock returns the YAML between the leading `---` fences, or null.
function frontmatterBlock(raw: string): string | null {
  const m = FRONTMATTER_RE.exec(raw.replace(/\r\n/g, "\n").replace(/^﻿/, "").trimStart());
  return m && m[1] ? m[1] : null;
}

// isSafeSkillName rejects empties and any path-traversal shapes — a bare name
// maps to skills/<name>/ and must not escape the clone.
function isSafeSkillName(name: string): boolean {
  return (
    name.trim() !== "" &&
    !name.includes("/") &&
    !name.includes("\\") &&
    !name.includes("..")
  );
}
