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
 * The skill-supply seam (ADR-0002 + shared-workspace-volume): ONE interface
 * the catalog builder (`prompt.ts`) and the skill loaders
 * (`tools/skill-tools.ts`) consume. The one production implementation is
 * `SnapshotSkillSource` (`conversation/load-workspace.ts`): the catalog is
 * scanned from the turn's immutable `_skills` snapshot dir and
 * `loadSkill`/`loadSkillReference` read bodies from disk ON DEMAND, so
 * progressive disclosure is truly lazy (D4 immutability makes the mid-turn
 * reads race-free). A skill-free turn uses `EMPTY_SKILL_SOURCE`.
 */

/**
 * Which agent a skill's guidance is written for (ADR-0013): the DESIGN agent
 * authors a project's spec, design and Task plan; the CODING agent implements a
 * component. Ownership (`metadata.aep.kind`) and audience are independent — a
 * skill can be the org's to edit while being the coding agent's to read.
 */
export type SkillAudience = "design" | "coding";

/**
 * Every audience — what a skill declaring none resolves to. Narrowing is
 * opt-in, so an unmarked skill (and any an org authors without knowing the
 * field exists) stays readable by whoever asks.
 */
export const ALL_AUDIENCES: readonly SkillAudience[] = ["design", "coding"];

/** One catalog row: what the system prompt shows — never a body. */
export interface SkillCatalogEntry {
  name: string;
  description: string;
  /** True when the skill carries any aux file (docs, scripts, assets, …) — drives the loadSkillReference tool + catalog note. */
  hasReferences: boolean;
  /**
   * Audiences permitted to load this skill. Rows OUTSIDE this service's own
   * audience still appear in the catalog: the design agent has to name a coding
   * skill to pin it onto a component, so hiding the row would break that
   * handoff — only the body is withheld.
   */
  audience: readonly SkillAudience[];
}

/** A loaded skill body plus its addressable reference paths. */
export interface LoadedSkillBody {
  /** The full guidance body (frontmatter stripped). */
  content: string;
  /** Every aux file path under the skill dir (docs, scripts, assets, …) `loadSkillReference` can read. */
  references: string[];
}

/**
 * The result of resolving one reference path: `undefined` for an unknown
 * name/path (never resolved raw against the fs — allowlisted against
 * `load(name).references`), `{ binary: true }` for a file that isn't valid
 * UTF-8 text (model-context surfaces never inline binary), otherwise the text.
 */
export type LoadedReference = { content: string } | { binary: true } | undefined;

/**
 * One load attempt: the body, a refusal (this consumer's audience is not among
 * the skill's), or undefined for an unknown name. A refusal is a THIRD state on
 * purpose — collapsing it into undefined reads to the agent as "no such skill",
 * inviting it to distrust the catalog and skip pinning instead. Mirrors
 * `LoadedReference`'s union shape.
 */
export type SkillLoadResult = LoadedSkillBody | { refused: true } | undefined;

/**
 * The audience this service reads skills as. The agents service IS the design
 * agent — the coding agent runs in the remote-worker runner and never calls
 * here — so the audience is a property of the process, not of a request.
 */
export const SERVICE_AUDIENCE: SkillAudience = "design";

/**
 * Non-ENOENT I/O while reading a skill file from the `_skills` snapshot.
 * Distinct from a missing skill (`load` → `undefined`): callers must NOT treat
 * this as `unknown skills`. Thrown by `SnapshotSkillSource`; caught by
 * `buildSkillTools` into a loud tool error.
 */
export class SkillReadError extends Error {
  readonly path: string;
  constructor(path: string, cause?: unknown) {
    const detail = cause instanceof Error ? cause.message : String(cause ?? "I/O error");
    super(`could not read skill file ${path}: ${detail}`);
    this.name = "SkillReadError";
    this.path = path;
    if (cause !== undefined) {
      (this as Error & { cause?: unknown }).cause = cause;
    }
  }
}

/** The seam both tool sets and the prompt builder read skills through. */
export interface SkillSource {
  /** The ordered catalog (order fixes the prompt's listing and the `available` echo). */
  catalog(): readonly SkillCatalogEntry[];
  /**
   * Body + reference paths for one skill; a refusal when the skill is out of
   * this consumer's audience; `undefined` for an unknown / vanished name
   * (ENOENT). Throws `SkillReadError` on other I/O faults. Three distinct
   * outcomes, none collapsed into another: a refusal is not a miss, and an I/O
   * fault is not a miss either.
   */
  load(name: string): SkillLoadResult;
  /**
   * One reference file: text content, a binary marker, or `undefined` (unknown
   * name/path / ENOENT). Throws `SkillReadError` on other I/O faults.
   */
  loadReference(name: string, path: string): LoadedReference;
}

/** The skill-free source (empty catalog, no loaders) consumers default to. */
export const EMPTY_SKILL_SOURCE: SkillSource = {
  catalog: () => [],
  load: () => undefined,
  loadReference: () => undefined,
};
