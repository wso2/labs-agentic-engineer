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

import { SURFACES, type Surface } from "@aep/agent-stream";
import {
  EMPTY_SKILL_SOURCE,
  SERVICE_AUDIENCE,
  type LoadedSkillBody,
  type SkillSource,
} from "./skill-source.js";

/**
 * System instructions for the file-mutating main agent. Layer charter (#373):
 * this prompt carries ONLY the tool contract, the error-reaction table (write
 * gates fire unconditionally, so reactions must always be in context), and the
 * narration meta-rule — stage behavior lives in skills. TODO(#377 Phase D):
 * the skillsPinned placement hint in the SCHEMA_VIOLATION row moves to the
 * `architecture` skill when it lands.
 */
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
- Issue every edit you have already decided on in ONE step, as parallel tool calls — one call per edit.
  Waiting for each result before deciding the next edit costs a full round trip per edit and buys nothing
  when the edits are independent. Sequence them only when a later edit's anchor depends on an earlier
  edit's outcome.

Reacting to tool results (each result tells you the next move):
- ok:true — applied. status "already-applied" or "noop" means it was already done; do NOT retry, move on.
- NOT_UNIQUE — your anchor matched several lines (listed with line numbers); re-issue editFile with a longer
  oldString that includes a surrounding unique line.
- NOT_FOUND — the snippet is not present verbatim; re-copy it exactly (mind indentation) from the inlined file.
- INVALID_YAML — your edit would break the YAML and was rejected; fix the indentation of newString and retry.
- INVALID_JSON / SCHEMA_VIOLATION — a components/<name>/design.json write was rejected (broken JSON or a
  schema problem, listed in the message); re-emit the WHOLE corrected file with removeFile + addFile.
  skillsPinned (the skills that component's build needs) is a per-component key inside its design.json,
  not project-level frontmatter.
- INVALID_OPENAPI — an openapi.yaml write was rejected: it is not an OpenAPI 3.x document, or it declares no
  paths or no operations. Fix what the message names and re-emit the WHOLE corrected document with
  removeFile + addFile. Every write to an openapi.yaml is checked this way as it lands, so a spec that
  applied cleanly is already valid — never spend a tool call asking something else to validate it.
- INVALID_DSL — a wireframes .dsl write was rejected (bad lines listed with line numbers: an unknown
  keyword, a misplaced left/right/table-row, or retired x,y coordinates). Fix EVERY listed line and
  re-emit the WHOLE corrected file with removeFile + addFile — layout comes from structure, never
  from coordinates.

Narration: keep prose outside tool calls to a single short sentence by default. A LOADED skill may define
the narration for its own flow (what to say as you work, and how to close) — when one does, follow the skill,
unless a standing narration policy in this prompt overrides it. When the instruction is fully applied, stop.`;

/**
 * The name of the org-defaults skill, inlined into EVERY turn's system prompt.
 * The one skill that is not "guidance for a task" but standing policy: settled
 * sections answer interview questions the agent would otherwise ask the user,
 * and pin providers at design time. An agent that has to remember to load it
 * asks questions the org already answered.
 */
const ORG_DEFAULTS_SKILL = "organization";

/**
 * Skill names that are never offered in the catalog. The org defaults are
 * inlined below it; a surface's narration skill is inlined too, on the turns
 * whose caller named that surface. Neither is "guidance for a task", so a
 * catalog line would advertise a round-trip that returns either text the agent
 * is already holding or policy addressed to somebody else's user.
 */
const UNCATALOGUED_SKILLS: readonly string[] = [ORG_DEFAULTS_SKILL, ...SURFACES];

/**
 * The skill catalog appended to the END of the system prompt (ADR-0002): skill
 * names + one-line descriptions only, never bodies. It is identical across a
 * conversation's turns (the `_skills` snapshot pins the same catalog), so the
 * cacheable instruction prefix is preserved. Built through the `SkillSource`
 * seam. Returns "" for an empty catalog, leaving the base instructions
 * byte-identical to a skill-free turn.
 */
export function buildSkillCatalog(skills: SkillSource | undefined): string {
  const entries = (skills ?? EMPTY_SKILL_SOURCE).catalog().filter((e) => !UNCATALOGUED_SKILLS.includes(e.name));
  if (entries.length === 0) return "";
  // Audience split (ADR-0013): `loadable` is this service's own (design) rows —
  // the catalog and reference note below are built from those ONLY, so a
  // design-only library (every unmarked skill included) renders byte-identical
  // to the pre-audience catalog. `pinOnly` rows still get a line — the design
  // agent has to NAME a coding skill to pin it onto a component's design.json —
  // just no loadSkill invitation, appended as a separate block.
  const loadable = entries.filter((e) => e.audience.includes(SERVICE_AUDIENCE));
  const pinOnly = entries.filter((e) => !e.audience.includes(SERVICE_AUDIENCE));

  const lines = loadable.map((e) => `- ${e.name}: ${e.description}`).join("\n");
  // The reference note appears only when some LOADABLE skill carries reference
  // files — a reference note about a skill this agent cannot load is useless.
  const hasRefs = loadable.some((e) => e.hasReferences);
  const refNote = hasRefs
    ? " Some skills carry reference files: loadSkill lists their paths, and loadSkillReference(name, path) reads one — call it only when the skill's guidance points you there."
    : "";

  // Emitted ONLY when something is pin-only, so a design-only library keeps the
  // exact prompt text (and cached prefix) it had before audiences existed.
  // The lead-in wording adapts to whether anything is loadable at all, so a
  // pin-only-only library (`loadable` empty) doesn't dangle a "these skills"
  // referring to nothing above it.
  const pinLeadIn =
    loadable.length === 0
      ? `No skills here are loadable by this agent — every one below is the coding agent's, listed only so you can pin it.`
      : `These skills are the coding agent's — you cannot load them here.`;
  const pinBlock =
    pinOnly.length === 0
      ? ""
      : `\n\n${pinLeadIn} When a component's build needs one, add it to that component's skillsPinned ` +
        `in its design.json:\n\n` +
        pinOnly.map((e) => `- ${e.name}: ${e.description}`).join("\n");

  // If nothing is loadable (a pin-only-only library), skip the "call
  // loadSkill" invitation and its empty list — only the heading survives, and
  // `pinBlock` carries the rest of the explanation.
  const header =
    loadable.length === 0
      ? `

# Skills`
      : `

# Skills

You have access to skills — reusable guidance for specific tasks. Only their names and one-line descriptions are listed below; the full guidance is hidden until you load it. Call loadSkill ONCE with every relevant skill's name (names: [...]) to read their guidance BEFORE applying any of them — never guess a skill's contents.${refNote}

${lines}`;

  return header + pinBlock;
}

/**
 * The org's standing defaults, appended to the system prompt of every turn.
 *
 * Placed here rather than in the per-turn eager block because it applies to
 * every turn regardless of flow: it is part of the agent's standing context,
 * not a property of the request. Being system-prompt-stable also makes it
 * cache-friendly the day prompt caching is switched on — the body is identical
 * across a conversation's turns, since the `_skills` snapshot pins it.
 *
 * Absent from the org's snapshot (an older org, or one that never seeded it) →
 * "", leaving the prompt byte-identical to a turn without it. A skill the
 * audience gate refuses is likewise skipped: `load()` returns `{refused: true}`,
 * which carries no content.
 *
 * The heading is this composer's, not the file's — it is what makes the block a
 * sibling of `# Skills` in the prompt, and an org may edit the body to anything.
 * The seeded skill therefore carries no title of its own.
 */
export function buildOrgDefaultsBlock(skills: SkillSource | undefined): string {
  const body = (skills ?? EMPTY_SKILL_SOURCE).load(ORG_DEFAULTS_SKILL);
  if (body === undefined || !("content" in body)) return "";
  const content = body.content.trim();
  if (content === "") return "";
  return `

# Organization defaults

${content}`;
}

/**
 * The surface's narration policy, appended to the system prompt of every turn
 * whose caller named a surface (#580).
 *
 * The right vocabulary belongs to the SURFACE, not to the skill: `design.cell`
 * is exactly the right word for someone standing in the repo and names nothing
 * on a console user's screen. So the difference rides a skill the console's
 * caller asks for, and the shared flow skills stay byte-identical everywhere.
 *
 * Standing policy rather than a catalog entry, for the reason the org defaults
 * are: an agent that has to remember to load its narration rules will forget
 * one and quote a path. It is also why the block is LAST — the base
 * instructions let a loaded skill define its own flow's narration, and this is
 * the standing policy that clause yields to.
 *
 * A surface names its own skill (`console` → `skills/console/`). An absent or
 * empty body → "", leaving the prompt byte-identical to a surface-free turn,
 * the same graceful absence the org block has.
 */
export function buildNarrationBlock(skills: SkillSource | undefined, surface: Surface | undefined): string {
  if (surface === undefined) return "";
  const body = (skills ?? EMPTY_SKILL_SOURCE).load(surface);
  if (body === undefined || !("content" in body)) return "";
  const content = body.content.trim();
  if (content === "") return "";
  return `

# Narration policy

${content}`;
}

/**
 * Base instructions + the skill catalog + the org's standing defaults + the
 * surface's narration policy (each empty when its source is). The catalog stays
 * immediately after the base instructions so its "call loadSkill" invitation
 * reads against the skill list it introduces.
 */
export function buildInstructions(skills?: SkillSource, surface?: Surface): string {
  return instructions + buildSkillCatalog(skills) + buildOrgDefaultsBlock(skills) + buildNarrationBlock(skills, surface);
}

/**
 * The eager-skill preamble for one turn (#335 latency): the caller already
 * knows these skills apply, so their bodies are inlined ahead of the
 * instruction and the model skips the `loadSkill` round-trip — a whole model
 * step before any useful output. Rides the PER-TURN user prompt, never the
 * system prompt (whose cacheable prefix must stay byte-stable across turns).
 * Unknown names skip silently — the snapshot is the authority; "" when nothing
 * resolves, leaving the prompt byte-identical to an eager-free turn.
 *
 * A REFUSAL skips the same way. `load()` has three states (body / `{refused}` /
 * undefined, see `SkillLoadResult`), and an org is free to mark a skill this
 * service's audience may not read — `wireframes` and `openapi-conventions` are
 * pin targets for the coding agent as much as guidance for this one. Narrowing on
 * `!== undefined` alone let a refusal through to `body.content.trim()` and threw,
 * failing the whole turn over guidance it could simply have gone without.
 *
 * `surface` re-states the narration precedence INSIDE this block (#580). The
 * policy itself rides the system prompt, but the bodies inlined here are what
 * argue with it — `design` and `architecture` both close on a one-line pointer
 * to `specs/design/`, which a console user cannot open. Those mandates are
 * deliberately kept (a path pointer is genuinely useful in a terminal), so the
 * override has to reach the model in the same message as the text it overrides,
 * not only in the standing instructions above it. No surface → no line, and the
 * block is byte-identical to an eager block composed before surfaces existed.
 */
export function buildEagerSkillsBlock(
  skills: SkillSource | undefined,
  names?: string[],
  surface?: Surface,
): string {
  if (!names?.length || !skills) return "";
  const bodies = names
    .map((name) => ({ name, body: skills.load(name) }))
    .filter((s): s is { name: string; body: LoadedSkillBody } => s.body !== undefined && "content" in s.body);
  if (bodies.length === 0) return "";
  const blocks = bodies
    .map(({ name, body }) => `## Skill: ${name}\n\n${body.content.trim()}`)
    .join("\n\n");
  const precedence =
    surface === undefined
      ? ""
      : `\n\nWhere any skill above defines narration for its own flow — what to say as you work, how to close — ` +
        `the Narration policy in your system instructions overrides it.`;
  return `The following skill guidance is ALREADY LOADED for this turn — apply it directly and do NOT call loadSkill for these names:

${blocks}${precedence}

---

`;
}

/**
 * System instructions for the `task-plan` tool set. The mission is PLANNING Tasks
 * against the read-only CURRENT STATE — the agent does not edit files here. Detail
 * (title conventions, fresh vs incremental, obsolescence) lives in the
 * `task-planning` skill, not this prompt; the prompt only fixes the invariants.
 */
export const taskPlanInstructions = `You are a task-planning agent. You are given a project's spec and design
(inlined under "Existing files:") plus any existing Tasks, and an instruction to plan the work. You plan Tasks by
calling the task tools. You do NOT edit files — the existing files are read-only context.

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

Keep prose outside tool calls to a single short sentence, except a final note flagging anything that needs a human
(e.g. a requirement no component covers).`;

/**
 * Task-plan instructions + the skill catalog + the org's standing defaults +
 * the surface's narration policy. The planner gets the org block for the same
 * reason the editing agent does: a filled entry pins a provider or a stack,
 * which is exactly what the Tasks it writes will be built against. It gets the
 * narration policy because its closing note — the requirement no component
 * covers — is read by the same person, on the same screen.
 */
export function buildTaskPlanInstructions(skills?: SkillSource, surface?: Surface): string {
  return (
    taskPlanInstructions + buildSkillCatalog(skills) + buildOrgDefaultsBlock(skills) + buildNarrationBlock(skills, surface)
  );
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
