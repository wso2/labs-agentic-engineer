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

// Resolves skill NAMES against the `.claude/skills/` mirror the BFF already
// wrote into the project clone — zero network, a filesystem check only.
//
// Whose names depends on the caller, and the module deliberately does not care:
// a coding run brings the pins from skills_resolver.ts, a validation run brings
// the on-demand set from runner.ts. Both are asking one question — is this in
// the mirror — so the answer is `present` / `dangling` and nothing about why the
// caller wanted it. Naming the pin case in here is what let an empty allowlist
// look correct: "nothing is pinned" and "nothing may be loaded" are different
// facts, and one identifier was carrying both.
//
// A name with no `.claude/skills/<name>/SKILL.md` is DANGLING, not an error: the
// caller warns and carries on with the rest. Missing guidance degrades a build;
// aborting the run over it loses the build entirely.
//
// Two jobs, because the SDK draws the line in a place the design did not
// anticipate. `skills:` is an ALLOWLIST — a mirrored skill absent from it is
// rejected outright by the Skill tool ("not in this session's skills
// allowlist"), so for a coding run, listing only the pins would leave the rest
// of the mirror as inert files on disk. Hence `listMirroredSkills`: everything
// the BFF decided that build may use is what its session allows.
//
// And nothing in that array is "preloaded" in the sense of arriving in context
// — the model gets each skill's name and description, and the body only on
// invocation. Verified: an agent holding a listed skill could not state a
// codeword written in its body until it called the Skill tool. A pin is a
// statement that the guidance is needed for THIS work, so `readSkillBodies`
// puts those bodies in front of the model at startup rather than hoping it
// chooses to look.

import fs from "node:fs";
import path from "node:path";

export interface SkillPresence {
  /** Requested names with a readable .claude/skills/<name>/SKILL.md. */
  present: string[];
  /** Requested names with no matching copy on disk. */
  dangling: string[];
}

export const SKILLS_MIRROR_DIR = path.join(".claude", "skills");

/**
 * Partition `names` into present vs dangling, checked against
 * `<workspace>/.claude/skills/<name>/SKILL.md`. Never throws — an absent
 * `.claude/skills/` directory simply means every name is dangling.
 *
 * Caller-agnostic on purpose. Two callers ask the same question of different
 * lists: a coding run checks the pins it will inject as bodies, a validation run
 * checks the skills it will allow the Skill tool to load. So the warning names
 * the mirror and the path, not the reason the caller wanted the name — the
 * mechanism is "is this in the mirror", and only the label differed.
 */
export async function resolveSkillPresence(
  workspace: string,
  names: string[],
  log: (line: string) => void = () => {},
): Promise<SkillPresence> {
  const present: string[] = [];
  const dangling: string[] = [];

  for (const name of names) {
    const skillMdPath = path.join(workspace, SKILLS_MIRROR_DIR, name, "SKILL.md");
    try {
      await fs.promises.access(skillMdPath, fs.constants.R_OK);
      present.push(name);
    } catch {
      dangling.push(name);
      log(
        `[skills-presence] ⚠️  skill ${JSON.stringify(name)} is not in the mirror — no ` +
          `.claude/skills/${name}/SKILL.md — skipping (guidance degraded, build continues)`,
      );
    }
  }

  return { present, dangling };
}

/**
 * Every skill in the mirror — the set this session may use at all.
 *
 * A directory without a readable `SKILL.md` is not a skill (the same rule the
 * BFF and the local mirror apply), and an absent mirror yields an empty list
 * rather than an error: a project with no attached skills is ordinary.
 */
export async function listMirroredSkills(workspace: string): Promise<string[]> {
  const dir = path.join(workspace, SKILLS_MIRROR_DIR);
  let entries: fs.Dirent[];
  try {
    entries = await fs.promises.readdir(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const names: string[] = [];
  for (const e of entries) {
    if (!e.isDirectory() || e.name.startsWith(".")) continue;
    try {
      await fs.promises.access(path.join(dir, e.name, "SKILL.md"), fs.constants.R_OK);
      names.push(e.name);
    } catch {
      continue;
    }
  }
  return names.sort((a, b) => a.localeCompare(b));
}

/**
 * The system-prompt appendix that actually puts pinned guidance in context.
 *
 * Each body is fenced with its name so the model can tell where one skill's
 * instructions end and the next begin, and told that these are already loaded
 * — without that, an agent reading guidance it also sees in the skill listing
 * will helpfully re-invoke the Skill tool and pay for the body twice.
 *
 * Returns an empty string when nothing is pinned, so a caller can pass it
 * straight through: appending "" must not perturb a run with no pins.
 */
export async function readSkillBodies(workspace: string, names: readonly string[]): Promise<string> {
  const sections: string[] = [];
  for (const name of names) {
    try {
      const body = await fs.promises.readFile(
        path.join(workspace, SKILLS_MIRROR_DIR, name, "SKILL.md"),
        "utf8",
      );
      sections.push(`<skill name="${name}">\n${body.trim()}\n</skill>`);
    } catch {
      continue; // dangling — already warned by resolveSkillPresence
    }
  }
  if (sections.length === 0) return "";
  return (
    `# Skills pinned to this work\n\n` +
    `The design pinned these skills to the component(s) you are building, so their full ` +
    `instructions are included below and are ALREADY in your context — you do not need to ` +
    `invoke the Skill tool to read them again. Other skills are listed in your skill catalog ` +
    `and you can invoke those on demand as usual.\n\n` +
    sections.join("\n\n")
  );
}

/**
 * Thrown when the mirror is missing a skill the run cannot proceed without.
 * A distinct type so an entrypoint can report "the platform did not deliver the
 * workflow" rather than a generic startup crash.
 */
export class MissingWorkflowSkillError extends Error {
  readonly missing: string[];
  constructor(workspace: string, missing: string[]) {
    super(
      `the project's ${SKILLS_MIRROR_DIR}/ carries no ${missing.join(", ")} — ` +
        `the platform's skill sync did not reach ${workspace}. Refusing to start: a coding run ` +
        `without its workflow skill has no procedure to follow.`,
    );
    this.name = "MissingWorkflowSkillError";
    this.missing = missing;
  }
}

/**
 * The always-on bodies: the run's own workflow, read from the mirror like every
 * other skill, and FATAL when absent.
 *
 * This is the one place the runner refuses to degrade. Every other skill is
 * optional — a missing pin is a warning and the build proceeds — but the `aep`
 * skill IS the run's procedure, so a session without it does not do a smaller
 * version of the job, it improvises one. That failure is invisible from the
 * outside: the agent explores the repo, writes something plausible, and the run
 * reports success. #361 documents the same shape from the other direction, where
 * an unlisted skill was silently rejected and the agent compensated by grepping
 * SKILL.md out of the tree for a whole release.
 *
 * The mirror is written at project creation, at pre-tag and again at dispatch,
 * and each of those writes is best-effort by design — none may fail a creation,
 * publish or dispatch. This turns the one case that matters into a loud build
 * failure at the point where the cause is still obvious.
 *
 * Synchronous because `runClaudeQuery` is, and because this must throw before a
 * session exists rather than mid-stream.
 */
export function requireWorkflowBodies(workspace: string, names: readonly string[]): string {
  const sections: string[] = [];
  const missing: string[] = [];
  for (const name of names) {
    try {
      const body = fs.readFileSync(path.join(workspace, SKILLS_MIRROR_DIR, name, "SKILL.md"), "utf8");
      sections.push(`<skill name="${name}">\n${body.trim()}\n</skill>`);
    } catch {
      missing.push(name);
    }
  }
  if (missing.length > 0) throw new MissingWorkflowSkillError(workspace, missing);
  return (
    `# Your workflow\n\n` +
    `The skill(s) below define how this run works — discovery, ordering, fan-out, ` +
    `verification and how to finish. They are ALREADY in your context; you do not need to ` +
    `invoke the Skill tool to read them again.\n\n` +
    sections.join("\n\n")
  );
}
