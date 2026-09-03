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
 * Turn-instruction composition — the ONE place a `TurnSpec` becomes prompt text.
 *
 * Callers state facts (`{kind: "start", idea}`); this module decides wording.
 * That split is the whole architecture: the BFF has the database, git and the
 * project descriptor, this service has the model, and prompt text belongs with
 * the model. Before it, the same sentences were authored in Go and TS at once —
 * eight of them through a JSON→codegen pipeline, and four more (the divergence
 * note, the flow skill pointer, the milestone-scope block, the plan-context
 * fences) hand-copied straight into the Go composer, where the pipeline could
 * not see them drift. See design/ADR-0003.
 *
 * Wording lives here as plain template strings. There is no generator and no
 * second copy: edit the text, and every surface that dispatches a turn — the
 * console through aep-api, the playground, the evals — gets it, because they
 * all send a `TurnSpec` and none of them composes.
 */

import type { PlanContextFile, PlanScope, Toolset, TurnAim, TurnSpec } from "@aep/agent-stream";

// --- Wording -----------------------------------------------------------------

/** The kickoff. The start skill owns the interview; this only points at it. */
const START_INSTRUCTION = "Load the start skill and follow it.";

/**
 * The captured idea, appended to a `start` turn. Deliberately neutral about
 * provenance: the idea reaches us either typed inline (`/start an expense
 * tracker`) or captured at project creation and read back from the descriptor.
 */
const IDEA_PREFIX = "\n\nThe user's idea for this project:\n\n";

/**
 * The documents the user attached at project create, appended as PATHS. They
 * are NOT spec content — nothing commits them (console ADR-0017); the platform
 * overlays them into the turn's snapshot, so they are already in front of the
 * agent and this points rather than pastes. It also says what they are FOR,
 * because "some files exist" is not an instruction.
 *
 * Worded for start AND flow turns, which both carry references: "read them
 * before interviewing" is meaningless on a `/design` turn, which interviews
 * nobody. The shared line states their standing; each turn's own skill says
 * what to do with them.
 */
const REFERENCES_PREFIX =
  "\n\nThe user attached reference documents for this project. They are the " +
  "primary brief for what is being built, and the idea above is the anchor. " +
  "Read them before you plan or ask anything they already answer:\n\n";

/**
 * The ONE surviving content steer for spec turns (#373): flow behaviour lives
 * in skills, but a file created at a bare filename lands in the wrong place and
 * no skill is loaded early enough to prevent it.
 */
const SPEC_PATHS_RULE =
  "\n\nSpec sources live under specs/ (requirements under specs/requirements/, design under specs/design/) — when creating a file that does not exist yet, always use its full path, never a bare filename.";

/** The caller-pinned spec-bundle target. */
const TARGET_PREFIX = "\n\n(target: ";
const TARGET_CLOSE = ")";

/**
 * D20: the previous turn of this conversation FAILED, so the conversation
 * history claims work that git never received. Leads the instruction — the
 * agent has to reconcile the two before it does anything else.
 */
const PREVIOUS_TURN_FAILED_NOTE =
  "Note: your previous turn's changes were NOT applied; the workspace reflects the repository state.";

/**
 * The aimed-turn preamble (#666). Leads the instruction, so the model knows
 * WHAT it is being pointed at before it reads what to do with it.
 *
 * The names are LOCATORS, not content (console ADR-0024). They are how the
 * selection read when the user made it, and the agent is a live peer in the
 * same room — the user may have kept typing, a teammate may have edited. So the
 * note says to resolve them against the document as it stands and to SAY SO on
 * a miss: an agent that guesses which paragraph was meant is the one failure
 * this whole shape exists to avoid.
 */
const AIM_CHANGE =
  "The user has selected part of a document and wants it changed. Their request follows the selection.";
const AIM_DISCUSS =
  "The user has selected part of a document and wants to talk it through BEFORE anything changes. Take up their point about the selection; do not edit the document in this turn unless they ask you to.";
const AIM_RESOLVE_RULE =
  "Find these in the document as it stands now — the names above are how the selection read when they made it, and the document may have moved on since. If you cannot find one, say so and ask; never guess which part was meant.";
/**
 * The fence, decided on #654: the selection is the SUBJECT, not a cage. A hard
 * fence was rejected — a spec has legitimate ripples (rename an actor and the
 * stories mention it; settle a decision and its entry leaves Open Questions),
 * and the product's safety net is visibility, review marks and undo, not a
 * cage. What the rule forbids is the other failure: unrelated tidying the user
 * never pointed at. Change turns only — a Discuss edits nothing.
 */
const AIM_FENCE_RULE =
  "The selection is the subject, not a cage: make the requested change at the named place. Touch other parts only where this change makes them wrong — consistency, never improvement — and leave everything else exactly as it is. If the right fix lies somewhere other than the selection, make it there and say so plainly in your reply.";

/** No interview is possible (the playground's headless phases). */
const HEADLESS_NOTE =
  "\n\nNo interview is possible in this run: do not call ask_question or ask_questions. Generate on stated assumptions and mark each assumption as assumed in the document.";

/**
 * The plan-turn directive. Per the layer charter it carries ONLY the skill
 * pointer, the bundle paths and the existing-Tasks fence — the planning
 * invariants live once in the task-plan system prompt, the mechanics once in
 * the task-planning skill.
 */
const PLAN_INSTRUCTION =
  'Plan the implementation Tasks for this project. Load the task-planning skill and follow it. The design is under specs/design/ and the requirements under specs/requirements/. When a "Milestone scope" section lists in-scope stories, cover every story marked NEEDS TASKS and leave COVERED stories\' Tasks untouched. Existing open Tasks (if any) are listed at the end of this message for reference — add Tasks ONLY for work they do not cover, and do not recreate or update the listed Tasks in this turn.';

const PLAN_CONTEXT_HEADER = "\n\n## Existing open Tasks in this version (reference)\n";

/**
 * Commands whose token is NOT the skill they load (#579).
 *
 * A command names the user's INTENT — `/feature` says what they came to do —
 * while a skill name is engineer-facing and routes by catalog description.
 * `amend` never needed renaming; it needed to stop being what the user reads.
 * So the three scoped edits share the one scoped-edit playbook and arrive at it
 * carrying the branch they want, with whatever the user clicked as its subject.
 *
 * The mapping is WORDING — "which branch of which playbook, said how" — so it
 * lives here with the rest of it, not in the parsers, which only ever yield
 * facts (`@aep/contracts/commands`, `internal/spec/start_command.go`).
 *
 * `/settle` and `/design` are absent because their token already IS their
 * skill; an unlisted token stays a plain skill load, which is what keeps
 * `/<org-skill>` working.
 *
 * Read through `commandFlow`, never indexed directly: the key is a token the
 * user typed, and `/constructor` reaching `Object.prototype` would turn a
 * skill-not-found — which the agent reports cleanly — into a thrown turn.
 */
const COMMAND_FLOWS: Record<string, { skill: string; scope: (subject: string) => string }> = {
  feature: { skill: "amend", scope: (s) => (s ? `Add a feature: ${s}` : "Add a feature.") },
  actor: { skill: "amend", scope: (s) => (s ? `Add an actor: ${s}` : "Add an actor.") },
  expand: {
    skill: "amend",
    scope: (s) => (s ? `Go deeper on this feature: ${s}` : "Go deeper on a feature."),
  },
};

/**
 * SUPPORTING skills a flow needs beyond its own (#335 latency). The flow's own
 * skill is always inlined — see `eagerSkillsFor` — so this map holds only the
 * extras that skill's playbook then walks. A flow absent here inlines just its
 * own skill; anything else it names loads lazily.
 *
 * This is a property of the FLOW, not of the call, which is why it lives beside
 * the wording rather than riding the wire: a console CTA, a typed command and a
 * playground run all reach the same map, so they cannot diverge.
 *
 * `organization` is deliberately NOT here, though the start/amend flows used to
 * name it: the org's standing defaults now ride the standing system
 * instructions on EVERY turn (see `buildOrgDefaultsBlock`), so listing it as a
 * per-flow eager skill would inline the same body twice.
 */
const FLOW_SUPPORTING_SKILLS: Record<string, string[]> = {
  // The interview mechanics both playbooks defer to, plus the shape of the
  // document they both write. `prd-contract` is a sibling skill rather than a
  // `start` reference so an amend turn can hold the contract without also
  // inlining the cold-start interview playbook, whose frame ("the idea comes to
  // you", the coverage walk over an empty document) is wrong for a scoped edit.
  start: ["grilling", "prd-contract"],
  amend: ["grilling", "prd-contract"],
  // `/settle` revises a document that already exists — it asks, then writes the
  // answer where it belongs — so it needs the same two as its siblings.
  settle: ["grilling", "prd-contract"],
  // `grilling` first: the design flow interviews too (#578 removed the
  // "do not interview the user again" clause), and the question mechanics are
  // no more optional here than on a start turn. Then the rest of the design
  // lineup, in the order the `design` skill walks it.
  // `design` names every one, so the model's first act was always to batch-load
  // the set: one model step, and ~70KB arriving as a tool RESULT — landing AFTER
  // the turn prompt's cache breakpoint, where it is re-prefilled per step rather
  // than read. Inlined, the same bytes sit INSIDE the marked prompt, cached from
  // the first step and again on the next turn.
  //
  // Three of them are conditional (a project with no `web-application` never
  // writes a wireframes.dsl), but which components exist is decided DURING the
  // turn — there is nothing to condition on when the prompt is composed, and a
  // cached read costs a tenth of a re-prefill. Org-authored design skills stay
  // lazy: this map is flow wording and cannot know a given org's catalog.
  design: ["grilling", "cell-design", "architecture", "security-design", "openapi-conventions", "wireframes", "validation-criteria"],
};

/** The branch a command names, or undefined for a token that IS its skill. */
function commandFlow(token: string): { skill: string; scope: (subject: string) => string } | undefined {
  return Object.hasOwn(COMMAND_FLOWS, token) ? COMMAND_FLOWS[token] : undefined;
}

/** The extras a flow inlines beyond its own skill. */
function supportingSkills(skill: string): string[] {
  return Object.hasOwn(FLOW_SUPPORTING_SKILLS, skill) ? (FLOW_SUPPORTING_SKILLS[skill] ?? []) : [];
}

// --- Composition -------------------------------------------------------------

/** Turn-level modifiers — facts the caller supplies, never text it formats. */
export interface TurnModifiers {
  /** The spec-bundle path this turn should write to. */
  target?: string | undefined;
  /** D20: the previous turn of this conversation failed (see the note above). */
  previousTurnFailed?: boolean | undefined;
  /** No interview is possible in this run. */
  headless?: boolean | undefined;
  /**
   * What the user pointed at, and what for (#666). A FACT the caller supplies —
   * the console never formats these words, because a preamble written there
   * would have to ride `instruction` and would then appear in the transcript as
   * something the user said and did not.
   */
  aim?: TurnAim | undefined;
}

/**
 * A `TurnSpec` plus its modifiers, as the instruction text the agent receives.
 *
 * Shape: `[failure note] [aim note] <body> [spec-paths rule] [target] [headless note]`.
 * The spec-paths rule is a property of the KIND — plan turns write no spec
 * files, so they never carry it — which is why it is not a caller flag.
 */
export function composeInstruction(turn: TurnSpec, mods: TurnModifiers = {}): string {
  const body = turn.kind === "plan" ? planBody(turn) : specBody(turn) + SPEC_PATHS_RULE + target(mods.target);
  const lead = mods.previousTurnFailed ? PREVIOUS_TURN_FAILED_NOTE + "\n\n" : "";
  return lead + aimNote(mods.aim) + body + (mods.headless ? HEADLESS_NOTE : "");
}

/**
 * The selection, as the model reads it. Empty for every unaimed turn, so an
 * ordinary chat message is byte-identical to what it was before this existed.
 */
export function aimNote(aim: TurnAim | undefined): string {
  if (!aim) return "";
  const lead = aim.intent === "discuss" ? AIM_DISCUSS : AIM_CHANGE;
  const rows = aim.anchor.nodes
    .map((n) => {
      const where = n.context ? ` (under ${n.context})` : "";
      return `- the ${n.kind} "${n.name}"${where}`;
    })
    .join("\n");
  const fence = aim.intent === "change" ? `${AIM_FENCE_RULE}\n\n` : "";
  return `${lead}\n\nIn ${aim.anchor.file}:\n${rows}\n\n${AIM_RESOLVE_RULE}\n\n${fence}`;
}

/** The instruction head for every kind that edits the spec bundle. */
function specBody(turn: Exclude<TurnSpec, { kind: "plan" }>): string {
  switch (turn.kind) {
    case "chat":
      // Ordinary chat rides verbatim — the user's words are the instruction.
      return turn.text;
    case "flow": {
      // A `/<command>` is a keyboard shortcut for "load a skill and follow it".
      // An unknown skill is NOT an error here: `loadSkill` reports not-found
      // and the agent says so, which is a better failure than a client-side
      // allowlist that goes stale against the org's catalog.
      const command = commandFlow(turn.skill);
      const base = `Load the ${command?.skill ?? turn.skill} skill and follow it.`;
      // A command that names a BRANCH says which one, and carries whatever the
      // user clicked as the branch's subject; everything else passes the user's
      // trailing text through untouched.
      const scoped = command ? command.scope(turn.text?.trim() ?? "") : turn.text?.trim();
      const withText = scoped ? `${base}\n\n${scoped}` : base;
      // Reference documents ride flows the same way they ride start turns:
      // a flow generates artifacts, and an attached sketch IS the brief for
      // wireframes. No documents → byte-identical to a plain flow turn.
      return withText + references(turn.references);
    }
    case "start":
      // A blank idea appends NOTHING, leaving a bare skill load — the start
      // skill then asks the user for it. References behave the same way: no
      // documents, no paragraph, so a docless kickoff is unchanged.
      return START_INSTRUCTION + idea(turn.idea) + references(turn.references);
  }
}

/** The plan turn: directive, milestone scope, then the existing-Task renders. */
function planBody(turn: Extract<TurnSpec, { kind: "plan" }>): string {
  return PLAN_INSTRUCTION + scopeBlock(turn.scope) + planContext(turn.taskContext);
}

function idea(raw: string | undefined): string {
  const trimmed = (raw ?? "").trim();
  return trimmed === "" ? "" : IDEA_PREFIX + trimmed;
}

function references(paths: string[] | undefined): string {
  const listed = (paths ?? []).map((p) => p.trim()).filter((p) => p !== "");
  return listed.length === 0 ? "" : REFERENCES_PREFIX + listed.map((p) => `- ${p}`).join("\n");
}

function target(raw: string | undefined): string {
  const trimmed = (raw ?? "").trim();
  return trimmed === "" ? "" : TARGET_PREFIX + trimmed + TARGET_CLOSE;
}

/**
 * The milestone's story coverage. COVERED stories already have Tasks, so the
 * planner must leave them alone — this block is what makes a delta pass a delta
 * rather than a re-plan.
 */
function scopeBlock(scope: PlanScope | undefined): string {
  if (!scope || scope.stories.length === 0) return "";
  const rows = scope.stories
    .map((s) => {
      const status = s.covered ? "COVERED" : "NEEDS TASKS";
      return s.title ? `- Story ${s.number}: ${s.title} — ${status}` : `- Story ${s.number} — ${status}`;
    })
    .join("\n");
  return (
    `\n\n## Milestone scope (spec ${scope.tag})\n\n` +
    "Plan Tasks so every story marked NEEDS TASKS below is covered. COVERED stories already have Tasks — leave them alone.\n\n" +
    rows +
    "\n"
  );
}

/**
 * Existing-Task renders as deterministic sections, sorted by path so the same
 * inputs always produce the same prompt. They keep their historical
 * `tasks/<n>.md` names so the model's mental layout is unchanged.
 */
function planContext(files: PlanContextFile[] | undefined): string {
  if (!files?.length) return "";
  const sorted = [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  return PLAN_CONTEXT_HEADER + sorted.map((f) => `\n--- ${f.path} ---\n${f.body}\n`).join("");
}

// --- Derived turn properties -------------------------------------------------

/**
 * The skill this turn's instruction tells the model to load by name — `start`,
 * `task-planning`, or whichever skill a `/<skill>` command names. A plain chat
 * turn names none: the user's words are the instruction.
 */
function instructedSkill(turn: TurnSpec): string | undefined {
  switch (turn.kind) {
    case "start":
      return "start";
    case "plan":
      return "task-planning";
    case "flow":
      return commandFlow(turn.skill)?.skill ?? turn.skill;
    case "chat":
      return undefined;
  }
}

/**
 * Which skills to inline up front for this turn (empty for kinds with none).
 *
 * The instructed skill always leads: every non-chat instruction opens with "Load
 * the <skill> skill and follow it", so sending the catalog and waiting for the
 * model to ask for a body we already hold spends a whole model step — measured at
 * 3.8s on `/start` and 3.6s on a plan turn — before a single useful token. If we
 * name a skill in the instruction, we ship it. Resolution runs through the
 * `SkillSource`, so an ORG-authored flow (`/<their-skill>`) inlines too, and a
 * name that resolves to nothing is skipped silently — `loadSkill` then reports it
 * missing exactly as before.
 */
/**
 * The files the user attached to THIS message (#428), named so the model can
 * resolve a pronoun to one.
 *
 * The attachment already rides the same user message as a document block, so the
 * model can read it without being told. What it cannot do reliably is work out
 * that "add this as a separate form" REFERS to the document — a bare `this` with
 * no antecedent in the text reads as ambiguous, and the honest response to an
 * ambiguous instruction is to ask, which is exactly what a live turn did.
 * Naming the files supplies the antecedent.
 *
 * Deliberately separate from the reference-document paragraph: a reference was
 * attached at project CREATE and is standing context, an attachment belongs to
 * one message. Saying "attached for this project" about a screenshot the user
 * just dropped would misdescribe it.
 *
 * Paths are not used here — an attachment has none, because nothing stores it
 * (console ADR-0019).
 */
export function attachmentsNote(names: string[] | undefined): string {
  const listed = (names ?? []).map((n) => n.trim()).filter((n) => n !== "");
  if (listed.length === 0) return "";
  return (
    `The user attached ${listed.length === 1 ? "this file" : "these files"} to this message` +
    `, and ${listed.length === 1 ? "it is" : "they are"} included above as document content — ` +
    `read ${listed.length === 1 ? "it" : "them"} before asking about anything ` +
    `${listed.length === 1 ? "it already answers" : "they already answer"}:\n` +
    listed.map((n) => `- ${n}`).join("\n") +
    "\n\n"
  );
}

export function eagerSkillsFor(turn: TurnSpec): string[] {
  const instructed = instructedSkill(turn);
  if (instructed === undefined) return [];
  return [instructed, ...supportingSkills(instructed)];
}


/**
 * Which tool set the turn needs. Planning registers `planTask`/`updateTask` and
 * NO file tools; everything else mutates the bundle. Derived rather than sent:
 * two ways to say it is two ways to disagree.
 */
export function toolsetFor(turn: TurnSpec): Toolset {
  return turn.kind === "plan" ? "task-plan" : "files";
}

/**
 * Synthetic Marketplace register project (console `MARKETPLACE_CHAT_PROJECT`).
 * Follow-up answers on this thread are classified as `chat`, not the
 * `/register-external-resource` flow — they still need the draft tool.
 */
export const MARKETPLACE_REGISTER_PROJECT = "__marketplace_register__";

/** Marketplace register chat needs a draft tool the files set does not carry. */
export function wantsRegisterDraftTool(turn: TurnSpec, projectId?: string): boolean {
  if (projectId === MARKETPLACE_REGISTER_PROJECT) return true;
  return turn.kind === "flow" && turn.skill === "register-external-resource";
}
