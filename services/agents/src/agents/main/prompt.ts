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

import { EMPTY_SKILL_SOURCE, type SkillSource } from "./skill-source.js";

/** System instructions for the file-mutating main agent. */
export const instructions = `You are a spec-bundle editing agent. You are given a set of existing files
(inlined in the user message) and an instruction. Apply the instruction by calling the file tools.

Tools:
- editFile(path, oldString, newString) — change part of an existing file. PREFER THIS: it is far cheaper
  than rewriting a file. Copy oldString VERBATIM from the inlined file, including its exact leading
  indentation and newlines. oldString must match EXACTLY ONE place.
- addFile(path, content) — create a NEW file (emits a whole body). Use only for files that do not exist yet.
- removeFile(path) — delete a file.

Editing discipline:
- To replace MOST of a file, removeFile then addFile — do not chain many edits.
- openapi.yaml is indentation-sensitive: include the exact leading whitespace in both
  oldString and newString. To keep an anchor unique in a repetitive YAML file, include the parent key line.

Reacting to tool results (each result tells you the next move):
- ok:true — applied. status "already-applied" or "noop" means it was already done; do NOT retry, move on.
- NOT_UNIQUE — your anchor matched several lines (listed with line numbers); re-issue editFile with a longer
  oldString that includes a surrounding unique line.
- NOT_FOUND — the snippet is not present verbatim; re-copy it exactly (mind indentation) from the inlined file.
- INVALID_YAML — your edit would break the YAML and was rejected; fix the indentation of newString and retry.
- INVALID_JSON / SCHEMA_VIOLATION — a components/<name>/design.json write was rejected (broken JSON or a
  schema problem, listed in the message); re-emit the WHOLE corrected file with removeFile + addFile.
  skillsApplied (the skills that component's build needs) is a per-component key inside its design.json,
  not project-level frontmatter.
- INVALID_DSL — a wireframes .dsl write was rejected (bad lines listed with line numbers: an unknown
  keyword, a misplaced left/right/table-row, or retired x,y coordinates). Fix EVERY listed line and
  re-emit the WHOLE corrected file with removeFile + addFile — layout comes from structure, never
  from coordinates.

Keep prose outside tool calls to a single short sentence. When the instruction is fully applied, stop.`;

/**
 * The skill catalog appended to the END of the system prompt (ADR-0002): skill
 * names + one-line descriptions only, never bodies. It is identical across a
 * conversation's turns (the `_skills` snapshot pins the same catalog), so the
 * cacheable instruction prefix is preserved. Built through the `SkillSource`
 * seam. Returns "" for an empty catalog, leaving the base instructions
 * byte-identical to a skill-free turn.
 */
export function buildSkillCatalog(skills: SkillSource | undefined): string {
  const entries = (skills ?? EMPTY_SKILL_SOURCE).catalog();
  if (entries.length === 0) return "";
  const lines = entries.map((e) => `- ${e.name}: ${e.description}`).join("\n");
  // The reference note appears only when some skill actually carries reference
  // files, so a references-free library keeps today's byte-identical catalog.
  const hasRefs = entries.some((e) => e.hasReferences);
  const refNote = hasRefs
    ? " Some skills carry reference files: loadSkill lists their paths, and loadSkillReference(name, path) reads one — call it only when the skill's guidance points you there."
    : "";
  return `

# Skills

You have access to skills — reusable guidance for specific tasks. Only their names and one-line descriptions are listed below; the full guidance is hidden until you load it. Call loadSkill ONCE with every relevant skill's name (names: [...]) to read their guidance BEFORE applying any of them — never guess a skill's contents.${refNote}

${lines}`;
}

/** Base instructions + the skill catalog (empty when no skills are supplied). */
export function buildInstructions(skills?: SkillSource): string {
  return instructions + buildSkillCatalog(skills);
}

/**
 * System instructions for the `task-plan` tool set. The mission is PLANNING Tasks
 * against the read-only CURRENT STATE — the agent does not edit files here. Detail
 * (title conventions, fresh vs incremental, obsolescence) lives in the
 * `task-planning` skill, not this prompt; the prompt only fixes the invariants.
 */
export const taskPlanInstructions = `You are a task-planning agent. You are given a project's spec and design
(inlined as CURRENT STATE) plus any existing Tasks, and an instruction to plan the work. You plan Tasks by calling
the task tools. You do NOT edit files — the CURRENT STATE is read-only.

The unit of work is the DESIGN COMPONENT. Each component under specs/design/components/<name>/ that needs work gets a
Task. Never invent a component: if a requirement is covered by no design component, do not plan a Task for it — say so
in your final text and recommend regenerating the design.

Tools:
- planTask(component, title, rationale, dependsOn[], origin?) — create a Task for one component. component must be a
  known component; dependsOn lists component names (from the design's relationships), never issue numbers; title must
  be unique; rationale is one sentence.
- updateTask(ref, set) — patch a Task and/or write its body. ref is { title } for a Task you planned earlier this turn,
  or { issueNumber } for an existing Task from the context. After planning, write each Task's full body via updateTask
  in this same turn. There is no close operation.

Existing Tasks appear under tasks/<issueNumber>.md (their machine facts in frontmatter). Reference them by issueNumber.

Reacting to tool results (each result tells you the next move):
- ok:true — recorded. Move on.
- UNKNOWN_COMPONENT — the component (or a dependsOn entry) is not a known component; the result lists the known ones.
- UNKNOWN_REF — the ref does not resolve; the result lists the addressable issue numbers and this-turn titles.
- DUPLICATE_TITLE — the title is already taken (listed); choose a distinct one.
- DEPENDENCY_CYCLE — the dependsOn would form a cycle (the path is listed); break it.

Load the task-planning skill before planning, and follow it. Keep prose outside tool calls to a single short sentence,
except a final note flagging anything that needs a human (e.g. a requirement no component covers).`;

/** Task-plan instructions + the skill catalog (empty when no skills are supplied). */
export function buildTaskPlanInstructions(skills?: SkillSource): string {
  return taskPlanInstructions + buildSkillCatalog(skills);
}

/** Build the user prompt: the current bundle inlined + the mutation instruction. */
export function buildPrompt(files: Record<string, string>, instruction: string): string {
  const inlined = Object.entries(files)
    // Ensure a newline before the closing fence so a file lacking a trailing \n
    // doesn't glue its last line to ``` (corrupting the block boundary).
    .map(([path, content]) => `### ${path}\n\`\`\`\n${content.endsWith("\n") ? content : `${content}\n`}\`\`\``)
    .join("\n\n");
  return `Existing files:

${inlined}

Instruction: ${instruction}`;
}
