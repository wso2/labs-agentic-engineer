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
 * The eval's skill resolver (ADR-0002/§12). Skills never travel in the turn
 * payload; the evals/playground resolve the repo-root library here and
 * MATERIALIZE it into the fixture mount's `_skills` snapshot
 * (`EvalWorkspace.materializeSkills`), which the service reads like any caller's.
 * This module reads repo-root `skills/<id>/SKILL.md`, parses the frontmatter for
 * `name`+`description`, and takes the body as `content`. Only `evals/` touches
 * the fs.
 */

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { parse as parseYaml } from "yaml";
// Reuse the bundle's single frontmatter grammar + LF canonicalizer so the
// SKILL.md fence parsing can't drift from the spec-file fence parsing.
import { FRONTMATTER_RE, lf } from "@aep/agent-stream";

/**
 * One resolved repo skill — the eval-side shape `materializeSkills` renders into
 * a fixture `_skills` snapshot (flat `skills/<name>/SKILL.md` + references).
 */
export interface RepoSkill {
  /** Stable id; the snapshot dir name and what the catalog lists. */
  name: string;
  /** One-line summary rendered into the SKILL.md frontmatter. */
  description: string;
  /** The full guidance body (frontmatter stripped). */
  content: string;
  /** Optional `references/<file>.md` → body (agentskills.io structure). */
  references?: Record<string, string>;
}

/** Split a `SKILL.md` into its YAML frontmatter block and markdown body. */
function splitFrontmatter(raw: string): { frontmatter: string; body: string } {
  const text = lf(raw);
  const m = FRONTMATTER_RE.exec(text);
  if (!m) return { frontmatter: "", body: text };
  return { frontmatter: m[1] ?? "", body: text.slice(m[0].length) };
}

/** Parse one `SKILL.md` into a `RepoSkill`; `name` falls back to the directory id. */
export function parseSkill(dirId: string, raw: string): RepoSkill {
  const { frontmatter, body } = splitFrontmatter(raw);
  let fm: Record<string, unknown> = {};
  if (frontmatter.trim() !== "") {
    const parsed = parseYaml(frontmatter) as unknown;
    if (parsed && typeof parsed === "object") fm = parsed as Record<string, unknown>;
  }
  const name = typeof fm.name === "string" && fm.name.trim() !== "" ? fm.name : dirId;
  const description = typeof fm.description === "string" ? fm.description : "";
  return { name, description, content: body.trim() };
}

/**
 * Load the whole skill library from `skillsDir` (repo-root `skills/`): every
 * immediate subdirectory holding a `SKILL.md`, sorted by id. Returns `[]` when
 * the directory is absent, so a checkout without `skills/` runs skill-free.
 */
export function loadRepoSkills(skillsDir: string): RepoSkill[] {
  if (!existsSync(skillsDir)) return [];
  return readdirSync(skillsDir, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .sort((a, b) => a.name.localeCompare(b.name))
    .flatMap((d) => {
      try {
        const skill = parseSkill(d.name, readFileSync(join(skillsDir, d.name, "SKILL.md"), "utf8"));
        const references = loadReferences(join(skillsDir, d.name));
        return [references ? { ...skill, references } : skill];
      } catch {
        return []; // a directory without a readable SKILL.md is simply not a skill
      }
    });
}

/**
 * Read one skill's `SKILL.md` untouched — full raw bytes, frontmatter and
 * all — as opposed to `loadRepoSkills`'s `Skill.content`, which is trimmed
 * and frontmatter-stripped for the `loadSkill` progressive-disclosure wire
 * shape (ADR-0002). The task-planner's `taskBreakdownSkill.body` is NOT that
 * wire shape: aep-api pushes the embedded task-breakdown SKILL.md's raw bytes
 * (see services/aep-api/internal/feature/task/planner_skill.go), so the
 * playground must read the same raw bytes rather than reuse the
 * frontmatter-stripped `content` a caller resolved for the skill catalog.
 * Returns `undefined` when `<skillsDir>/<dirId>/SKILL.md` doesn't exist.
 */
export function readSkillRaw(skillsDir: string, dirId: string): string | undefined {
  const path = join(skillsDir, dirId, "SKILL.md");
  if (!existsSync(path)) return undefined;
  return readFileSync(path, "utf8");
}

/**
 * Read `references/*.md` for one skill dir into a path→body map keyed
 * `references/<file>` (the wire shape `loadSkillReference` addresses).
 * Returns undefined — not an empty map — when the dir is absent or empty, so a
 * references-free skill stays byte-identical.
 */
function loadReferences(skillDir: string): Record<string, string> | undefined {
  const refsDir = join(skillDir, "references");
  if (!existsSync(refsDir)) return undefined;
  const out: Record<string, string> = {};
  for (const f of readdirSync(refsDir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    if (!f.isFile() || !f.name.endsWith(".md")) continue;
    out[`references/${f.name}`] = readFileSync(join(refsDir, f.name), "utf8");
  }
  return Object.keys(out).length > 0 ? out : undefined;
}
