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

import type { components } from "../../../generated/aep-api";

// The org's skill library as the Skills Page lists it: each skill's kind, the
// tags it carries (Disabled, Update to review), the kind filter and the search
// over it, and what Take updates would refresh. Copied from the old console
// (features/settings/skillKind.ts, skillsList.ts) and reshaped for one list.

type SkillSummary = components["schemas"]["SkillSummary"];
type SkillUpdate = components["schemas"]["SkillUpdate"];

/** Who owns a skill (CONTEXT.md, "Skill kind"), and nothing else. */
export type SkillKind = "platform" | "org" | "imported";

export const SKILL_KINDS: readonly SkillKind[] = ["platform", "org", "imported"];

/**
 * The contract types `kind` as a bare string. The retired `custom` folds into
 * `org`, and anything else unrecognised reads as `org` too, the default the
 * platform gives an unmarked SKILL.md.
 */
export function normalizeKind(kind: string): SkillKind {
  return (SKILL_KINDS as readonly string[]).includes(kind) ? (kind as SkillKind) : "org";
}

export const KIND_LABEL: Record<SkillKind, string> = {
  platform: "Platform",
  org: "Org",
  imported: "Imported",
};

/** One line on what each kind means, for the kind's tag. */
export const KIND_BLURB: Record<SkillKind, string> = {
  platform: "The platform's own: the workflows its agents follow. Read-only.",
  org: "Your organization's: the platform's defaults and the ones you added. Yours to edit; the platform may still offer updates.",
  imported: "Brought in from the AgentSkills ecosystem.",
};

export type KindFilter = SkillKind | "all";

/** One row of the Skills Page. */
export interface SkillRow {
  skill: SkillSummary;
  kind: SkillKind;
  /** Withheld from the platform's agents. */
  disabled: boolean;
  /** The platform changed it after the org did: an update waits on a review. */
  toReview: boolean;
}

/** The names the platform reports in one update state. */
function namesIn(updates: readonly SkillUpdate[], state: SkillUpdate["state"]): string[] {
  return updates.filter((u) => u.state === state).map((u) => u.name);
}

/**
 * What Take updates refreshes: every skill the platform moved and the org
 * never edited (state `update`). A skill the org edited is never touched by
 * it; one both moved (`conflict`) waits on a review instead.
 */
export function takeableUpdates(updates: readonly SkillUpdate[]): string[] {
  return namesIn(updates, "update");
}

/** Whether the platform changed this skill after the org edited it too. */
export function inConflict(updates: readonly SkillUpdate[], name: string): boolean {
  return updates.some((u) => u.name === name && u.state === "conflict");
}

/**
 * The rows shown: the skills of the chosen kind whose name, description or
 * kind holds the search, by name. The search matches the kind's label too,
 * since the tag reads "Org".
 */
export function skillRows(
  skills: readonly SkillSummary[],
  updates: readonly SkillUpdate[],
  { query, kind }: { query: string; kind: KindFilter },
): SkillRow[] {
  const q = query.trim().toLowerCase();
  const conflicts = new Set(namesIn(updates, "conflict"));
  return skills
    .map((skill) => ({
      skill,
      kind: normalizeKind(skill.kind),
      disabled: !skill.enabled,
      toReview: conflicts.has(skill.name),
    }))
    .filter((row) => kind === "all" || row.kind === kind)
    .filter(
      (row) =>
        !q ||
        [row.skill.name, row.skill.description, KIND_LABEL[row.kind]].some((field) => field.toLowerCase().includes(q)),
    )
    .sort((a, b) => a.skill.name.localeCompare(b.skill.name));
}
