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

import { editedBody, joinSkillMd, newSkillMd, oneLine, splitFrontmatter, withDescription } from "./skillMd";

// The Skill card's one draft: what the person has changed since it opened,
// and the whole SKILL.md its one Save writes. Nothing reaches the platform
// until Save; closing with a dirty draft asks first.

/** The skill as saved, which the draft is measured against; null for a new skill, which does not exist yet. */
export interface SavedSkill {
  name: string;
  description: string;
  skillMd: string;
}

export interface SkillDraft {
  /** Typed for a new skill; a saved skill's name is fixed (the platform refuses a rename). */
  name: string;
  description: string;
  /** The body as the editor now writes it, or null while it reads as it did when opened. */
  body: string | null;
}

export function draftOf(saved: SavedSkill | null): SkillDraft {
  return { name: saved?.name ?? "", description: saved?.description ?? "", body: null };
}

/** Whether Save has anything to write, and closing anything to lose. */
export function isDirty(draft: SkillDraft, saved: SavedSkill | null): boolean {
  if (!saved) return draft.name.trim() !== "" || draft.description.trim() !== "" || (draft.body ?? "").trim() !== "";
  return oneLine(draft.description) !== oneLine(saved.description) || draft.body !== null;
}

/**
 * The whole file Save writes. A saved skill keeps every byte the draft did
 * not change: its frontmatter save the description, and its body unless the
 * body was edited.
 */
export function skillMdOf(draft: SkillDraft, saved: SavedSkill | null): string {
  if (!saved) return newSkillMd(draft.name.trim(), draft.description, draft.body ?? "");
  const { frontmatter, body } = splitFrontmatter(saved.skillMd);
  const descriptionChanged = oneLine(draft.description) !== oneLine(saved.description);
  const nextFrontmatter =
    frontmatter === null
      ? `name: ${saved.name}\n${withDescription("", draft.description).trim()}`
      : descriptionChanged
        ? withDescription(frontmatter, draft.description)
        : frontmatter;
  const nextBody = draft.body === null ? body : editedBody(draft.body);
  if (frontmatter === null || descriptionChanged || draft.body !== null) return joinSkillMd(nextFrontmatter, nextBody);
  return saved.skillMd;
}

const NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;
const NAME_MAX = 55;

/**
 * What stops a Save, by field, before the platform is asked: the platform
 * holds the rest (reserved names, a name already taken) and says so itself.
 * `new` is refused here because it is the New skill card's own address.
 */
export function draftProblems(draft: SkillDraft, isNew: boolean): { name?: string; description?: string } {
  const problems: { name?: string; description?: string } = {};
  const name = draft.name.trim();
  if (isNew) {
    if (!name) problems.name = "Name the skill.";
    else if (!NAME.test(name)) problems.name = "Lowercase letters and digits, words joined by single hyphens.";
    else if (name.length > NAME_MAX) problems.name = `At most ${NAME_MAX} characters.`;
    else if (name === "new") problems.name = "Choose another name: “new” is taken by the console.";
  }
  const description = oneLine(draft.description);
  if (!description) problems.description = "Say in one line what the skill is for.";
  else if (description.length > 1024) problems.description = "At most 1024 characters.";
  return problems;
}
