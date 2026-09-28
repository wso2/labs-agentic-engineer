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

// Per-task skills PIN resolution — reads which skills the run's design(s)
// applied, from the PROJECT clone the runner already has on disk.
//
// The BFF now mirrors the org's coding-relevant skills into the project repo
// at `.claude/skills/` (the resolved files themselves — SKILL.md + refs,
// already filtered to what this project may load). That mirror is what
// `workspace.ts` clones and `runner.ts` runs the agent with `cwd:` set to, so
// Claude Code discovers it NATIVELY: no clone, no plugin synthesis. All that
// is left here is reading skillsPinned[] at the scope the run works at:
//
//   - a single component's specs/design/components/<name>/design.json, or
//   - for a milestone cycle (spans the whole milestone, may touch any
//     component) — the union across every component's design.json.
//
// This module never touches `.claude/skills/`; see skills_presence.ts for
// resolving pin names against the mirror that is actually on disk.
//
// Scope is the caller's to state, never inferred from AEP_COMPONENT_NAME: a
// milestone Job carries a sentinel there, so reading a design file by that
// name silently resolves nothing. See SkillsScope.

import fs from "node:fs";
import path from "node:path";
import { WORKFLOW_SKILLS } from "./skills_presence.js";

/**
 * Which design files contribute `skillsPinned` to this run.
 *
 * `component` is the single-component read: one named component is being built,
 * so only its design.json applies. `project` is the union across every
 * component — the milestone loop works a whole milestone on one branch and may
 * touch any component in it, so there is no single design.json to read.
 *
 * A milestone Job's AEP_COMPONENT_NAME is the `aep-milestone` sentinel (it only
 * has to be a valid k8s label value), so it must never be used to select a
 * design file; a run at milestone scope passes `project` instead.
 */
export type SkillsScope =
  | { kind: "component"; componentName: string }
  | { kind: "project" };

export interface ResolveTaskSkillsArgs {
  /** The project work tree (holds specs/design/components/<name>/design.json). */
  workspace: string;
  /** Which component design files contribute skillsPinned. */
  scope: SkillsScope;
  /** Per-line log sink; defaults to console.log. */
  log?: (line: string) => void;
}

/**
 * Resolve the run's pinned skill NAMES at the given scope. Returns an empty
 * list (NOT an error) when the design applies no skills or the design files
 * are absent — there is no network involved, so nothing here throws.
 *
 * A pin on a workflow skill is dropped: the runner already preloads the run's
 * own workflow, and a pinned one would either load it twice or load another
 * run kind's procedure beside it.
 */
export async function resolveTaskSkills(args: ResolveTaskSkillsArgs): Promise<string[]> {
  const log = args.log ?? ((l: string) => console.log(l));
  const pinned =
    args.scope.kind === "project"
      ? await readProjectSkillsPinned(args.workspace, log)
      : await readSkillsPinned(args.workspace, args.scope.componentName, log);
  const workflows = pinned.filter((name) => WORKFLOW_SKILLS.includes(name));
  if (workflows.length > 0) {
    log(`[skills-resolve] ignoring pin(s) on workflow skill(s): ${workflows.join(", ")} — the runner picks a run's workflow`);
  }
  return pinned.filter((name) => !WORKFLOW_SKILLS.includes(name));
}

// The component's authored design file; its `skillsPinned` key lists the
// skills THIS component's build needs (per-component — the project design.md
// no longer carries skills).
const componentDesignRel = (component: string) =>
  `specs/design/components/${component}/design.json`;

// Reads the building component's design.json and returns its `skillsPinned`
// (bare skill names). Absent file / absent field → []. Malformed JSON → [].
export async function readSkillsPinned(
  workspace: string,
  componentName: string,
  log: (line: string) => void = () => {},
): Promise<string[]> {
  const rel = componentDesignRel(componentName);
  let raw: string;
  try {
    raw = await fs.promises.readFile(path.join(workspace, rel), "utf-8");
  } catch {
    log(`[skills-resolve] ⚠️  ${rel} not found — proceeding with no applied skills`);
    return [];
  }
  try {
    const parsed = JSON.parse(raw) as { skillsPinned?: unknown } | null;
    const applied = parsed?.skillsPinned;
    if (Array.isArray(applied)) {
      return applied.filter((s): s is string => typeof s === "string");
    }
  } catch {
    /* malformed design.json → no skills */
  }
  return [];
}

// The directory every component's authored design file lives under.
const COMPONENTS_DIR = "specs/design/components";

// Reads the union of `skillsPinned` across every component's design.json —
// the milestone-scope read. Components are visited in sorted order and names
// de-duplicated first-seen, so the result is deterministic for a given tree.
// An absent components directory → [] (a project with no designed components
// yet); an individual component's absent/malformed design.json contributes
// nothing, exactly as at component scope.
export async function readProjectSkillsPinned(
  workspace: string,
  log: (line: string) => void = () => {},
): Promise<string[]> {
  let entries: fs.Dirent[];
  try {
    entries = await fs.promises.readdir(path.join(workspace, COMPONENTS_DIR), {
      withFileTypes: true,
    });
  } catch {
    log(`[skills-resolve] ⚠️  ${COMPONENTS_DIR}/ not found — proceeding with no applied skills`);
    return [];
  }

  const components = entries
    .filter((e) => e.isDirectory() && !e.name.startsWith("."))
    .map((e) => e.name)
    .sort();

  const union: string[] = [];
  const seen = new Set<string>();
  const contributors: string[] = [];
  for (const component of components) {
    // Quiet per-component read: at project scope a component without a
    // design.json is ordinary, not the warning-worthy case it is at component
    // scope, where the named component's file is the one thing being asked for.
    const names = await readSkillsPinned(workspace, component);
    if (names.length > 0) contributors.push(component);
    for (const name of names) {
      if (seen.has(name)) continue;
      seen.add(name);
      union.push(name);
    }
  }

  log(
    `[skills-resolve] milestone scope: ${union.length} skill(s) across ${contributors.length}/${components.length} component(s)` +
      (contributors.length > 0 ? ` (${contributors.join(", ")})` : ""),
  );
  return union;
}
