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

import type {
  PlanContextFile,
  PlanScope,
  PrototypeFeedback,
  Toolset,
  TurnAim,
  TurnScope,
  TurnSpec,
} from "@aep/agent-stream";

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

/**
 * The scope (S6): what the user was looking at when they sent the message. It
 * FOCUSES the turn and fences nothing — decided with the user on 2026-09-30:
 * every edit lands directly, and what the agent decides on the user's behalf
 * is tagged `*assumed*` in the requirements, which is the user's review. So
 * the note says what to read first and asks the agent to say where else it
 * wrote, never to hold a change back.
 */
const SCOPE_READ_FIRST = "Read specs/requirements/prd.md and that file before you change anything.";
const SCOPE_NO_FENCE =
  "This focuses the turn and fences nothing: make every change the message implies, in the file it belongs in, and say in your reply which other files you changed.";
const SCOPE_DESIGN_REVIEW =
  "The user is in the design review, looking at the design under specs/design/: read their message as being about the design. A point that is really a change to a requirement is made in the requirements file it belongs in; say so in your reply.";

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
 * So the two scoped edits share the one scoped-edit playbook and arrive at it
 * carrying the branch they want, with whatever the user clicked as its subject.
 *
 * The mapping is WORDING — "which branch of which playbook, said how" — so it
 * lives here with the rest of it, not in the parsers, which only ever yield
 * facts (`@aep/contracts/commands`, `internal/spec/start_command.go`).
 *
 * `/interview` is absent because its token already IS its skill; an unlisted token stays a plain skill load, which is what keeps
 * `/<org-skill>` working. `/feature`, `/actor`, `/amend` and `/settle` are the
 * console's older doors into what is now one loop, the `refine` skill.
 *
 * Read through `commandFlow`, never indexed directly: the key is a token the
 * user typed, and `/constructor` reaching `Object.prototype` would turn a
 * skill-not-found — which the agent reports cleanly — into a thrown turn.
 */
const COMMAND_FLOWS: Record<string, { skill: string; scope: (subject: string) => string }> = {
  feature: { skill: "refine", scope: (s) => (s ? `Add a feature: ${s}` : "Add a feature.") },
  actor: { skill: "refine", scope: (s) => (s ? `Add an actor: ${s}` : "Add an actor.") },
  amend: { skill: "refine", scope: (s) => s },
  settle: { skill: "refine", scope: (s) => (s ? `Settle this point: ${s}` : "Settle the Open Questions, one at a time.") },
  // `/design F1 F2` names the features this run designs (E1); bare, it designs
  // every designable feature, and the skill says so.
  design: { skill: "design", scope: (s) => (s ? `Design these features: ${s}` : "") },
  // The plural walks every open dependency; the singular's token IS its skill.
  "resolve-dependencies": {
    skill: "resolve-dependency",
    scope: () => "Walk every external dependency that is still open, one at a time, until each is resolved or what it needs from me is named.",
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
  // One feature's interview, and the change loop after the kickoff: both ask
  // (grilling) and write the requirements (prd-contract).
  interview: ["grilling", "prd-contract"],
  refine: ["grilling", "prd-contract"],
  // `/resolve-dependency` asks (grilling) and writes a dependency file whose
  // shape and research playbook the architecture skill owns.
  "resolve-dependency": ["grilling", "architecture"],
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
  //
  // `acceptance-criteria` writes the Gherkin features a validation run drives
  // (ADR-0029), authored from the PRD alone.
  design: ["grilling", "cell-design", "architecture", "security-design", "openapi-conventions", "wireframes", "agent-building", "acceptance-criteria"],
  // `/prototype` derives from the finished design, so it reads what that design
  // wrote: the cell, the roles and the API. The design-system skill says how an
  // Oxygen screen is composed from the kit's components; the kit itself is in
  // the `prototype` skill.
  prototype: ["cell-design", "security-design", "openapi-conventions", "oxygen-ui-design-system"],
};

/**
 * What a flow READS, said where the turn starts. Most flows discover their
 * inputs by walking their own playbook; a flow that is purely DERIVED from
 * artifacts already on disk names them, so the agent opens the right files
 * first instead of rediscovering the design tree. Keyed by the skill the flow
 * loads; a flow absent here gets no brief.
 *
 * Each brief is one self-contained instruction, appended after the skill
 * pointer. A flow with more than one brief for different turn shapes picks
 * between them in `specBody`, so a second brief is added there rather than
 * folded into this text.
 */
const FLOW_BRIEFS: Record<string, string> = {
  prototype:
    "Generate the prototype of each web-application the design declares. The design is the input: read " +
    "specs/design/design.cell for the web-application components, the roles in specs/design/security.json, " +
    "each web-application's API (the openapi.yaml of every component it depends on), the numbered user " +
    "stories in specs/requirements/prd.md, and the key flows in specs/design/flows/*.md. Cover them: every " +
    "design flow a web-application's users walk becomes a flow of its prototype, and every user story gets at " +
    "least one screen, unless the product gives it no view (a platform sign-in, a backend job, a machine-facing " +
    "endpoint); name any story you set aside in your closing. Per web-application write " +
    "specs/design/components/<component>/prototype.json (the manifest) first and then prototype.tsx beside it " +
    "(the screens), and change no other file. When component names follow this brief, write only those " +
    "prototypes; otherwise write one for every web-application. Where a prototype already exists, revise it " +
    "with edits and keep its ids stable.",
};

/** The brief a flow's skill carries, or undefined. */
function flowBrief(skill: string): string | undefined {
  return Object.hasOwn(FLOW_BRIEFS, skill) ? FLOW_BRIEFS[skill] : undefined;
}

/**
 * The revision brief of a `/prototype` turn that carries a reviewer's feedback
 * batch. It replaces the generation brief: the turn revises ONE prototype, not
 * every one the design declares. The reviewer's words are quoted verbatim, one
 * quote line per text line, so a request can never read as instruction text
 * and nothing the reviewer wrote is paraphrased.
 */
function feedbackBrief(feedback: PrototypeFeedback): string {
  const dir = `specs/design/components/${feedback.component}`;
  const n = feedback.requests.length;
  const requests = feedback.requests.map((r, i) => {
    const where = [`screen "${r.screenId}"`, ...(r.flowId ? [`flow "${r.flowId}"`] : []), `role "${r.roleId}"`, `display state "${r.stateId}"`];
    const about = r.elementIds.length > 0 ? `Elements (ids): ${r.elementIds.join(", ")}` : "Elements: none selected, so the request is about the whole screen";
    const quoted = r.text.split(/\r?\n/).map((line) => `> ${line}`).join("\n");
    return `Request ${i + 1}\nWhere: ${where.join(", ")}\n${about}\nThe reviewer wrote:\n${quoted}`;
  });
  return (
    `Revise the prototype of the web-application "${feedback.component}" from a reviewer's feedback. This is a revision, ` +
    `not a generation: do not write any other component's prototype. The reviewer looked at the revision with hash ` +
    `${feedback.prototypeHash} and made ${n === 1 ? "one request" : `${n} requests`} below, each pointing at the ` +
    `ids shown on the screen, in the role and in the display state named. Read ${dir}/prototype.json and ` +
    `${dir}/prototype.tsx first and change only those two files, with edits; if they no longer match what the ` +
    `request describes, apply what still makes sense and say what differs. Apply every request you can. Keep every ` +
    `manifest key and element id you do not need to change, so the next round of feedback still lines up. Decline a ` +
    `request only when it conflicts with the design (the cell, the security roles, the API or the stories), and ` +
    `say which part of the design it conflicts with. A request that names no element is about the whole screen. ` +
    `Finish by answering each request by its number, as applied (with what you changed) or declined (with why).\n\n` +
    requests.join("\n\n")
  );
}

/** The branch a command names, or undefined for a token that IS its skill. */
function commandFlow(token: string): { skill: string; scope: (subject: string) => string } | undefined {
  return Object.hasOwn(COMMAND_FLOWS, token) ? COMMAND_FLOWS[token] : undefined;
}

/** The extras a flow inlines beyond its own skill. */
function supportingSkills(skill: string): string[] {
  return Object.hasOwn(FLOW_SUPPORTING_SKILLS, skill) ? (FLOW_SUPPORTING_SKILLS[skill] ?? []) : [];
}

// --- Composition -------------------------------------------------------------

/**
 * A turn's scope as the composer takes it: the feature scope carries the file
 * the caller found for it (null when the feature has no file yet), so this
 * module stays a pure function of facts.
 */
export type ScopeFact =
  | { kind: "feature"; feature: string; file: string | null }
  | { kind: "design-review" };

const FEATURE_FILES = "specs/requirements/features/";

/**
 * A wire scope as a fact: a feature scope finds its file among the turn's
 * paths (`features/F2-<slug>.md`, skills/prd-contract), or null when the
 * feature has none yet.
 */
export function scopeFactFor(scope: TurnScope, paths: Iterable<string>): ScopeFact {
  if (scope.kind === "design-review") return scope;
  const prefix = `${FEATURE_FILES}${scope.feature}-`;
  const files = [...paths]
    .filter((path) => path.startsWith(prefix) && path.endsWith(".md") && !path.slice(prefix.length).includes("/"))
    .sort();
  return { kind: "feature", feature: scope.feature, file: files[0] ?? null };
}

/** Turn-level modifiers — facts the caller supplies, never text it formats. */
export interface TurnModifiers {
  /** What the user was looking at when they sent this turn (S6); absent = the whole product. */
  scope?: ScopeFact | undefined;
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
 * Shape: `[failure note] [scope note] [aim note] <body> [spec-paths rule] [headless note]`.
 * The spec-paths rule is a property of the KIND — plan turns write no spec
 * files, so they never carry it — which is why it is not a caller flag. A plan
 * turn has no scope either: it plans from the whole design.
 */
export function composeInstruction(turn: TurnSpec, mods: TurnModifiers = {}): string {
  const body = turn.kind === "plan" ? planBody(turn) : specBody(turn) + SPEC_PATHS_RULE;
  const lead = mods.previousTurnFailed ? PREVIOUS_TURN_FAILED_NOTE + "\n\n" : "";
  const scope = turn.kind === "plan" ? "" : scopeNote(mods.scope);
  return lead + scope + aimNote(mods.aim) + body + (mods.headless ? HEADLESS_NOTE : "");
}

/**
 * What the user was looking at, as the model reads it. Empty for an unscoped
 * turn, so a turn about the whole product is byte-identical to one sent before
 * scopes existed.
 */
export function scopeNote(scope: ScopeFact | undefined): string {
  if (!scope) return "";
  if (scope.kind === "design-review") return `${SCOPE_DESIGN_REVIEW}\n\n`;
  const where = scope.file
    ? `whose file is ${scope.file}`
    : `whose file (specs/requirements/features/${scope.feature}-<name>.md) is not in the requirements yet`;
  const sentences = [`The user is looking at feature ${scope.feature}, ${where}: read their message as being about that feature.`];
  if (scope.file) sentences.push(SCOPE_READ_FIRST);
  sentences.push(SCOPE_NO_FENCE);
  return `${sentences.join(" ")}\n\n`;
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
      const skill = command?.skill ?? turn.skill;
      const brief = turn.prototypeFeedback ? feedbackBrief(turn.prototypeFeedback) : flowBrief(skill);
      const base = `Load the ${skill} skill and follow it.` + (brief ? `\n\n${brief}` : "");
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
      return s.title ? `- Story ${s.id}: ${s.title} — ${status}` : `- Story ${s.id} — ${status}`;
    })
    .join("\n");
  return (
    `\n\n## Milestone scope (spec ${scope.tag})\n\n` +
    "Plan Tasks so every story marked NEEDS TASKS below is covered. COVERED stories already have Tasks — leave them alone.\n\n" +
    featureRows(scope) +
    rows +
    "\n"
  );
}

/**
 * The features and product-wide items the version carries (B3): what the
 * planner cuts Tasks by. Empty for a scope that names none, which keeps a
 * story-only scope's block byte-identical.
 */
function featureRows(scope: PlanScope): string {
  let out = "";
  if (scope.features?.length) {
    const rows = scope.features.map((f) => {
      const name = f.name ? ` ${f.name}` : "";
      const needs = f.needs?.length ? ` — needs ${f.needs.join(", ")}` : "";
      return `- ${f.id}${name}${needs}`;
    });
    out += "Features this version builds — one Task per feature per component that serves it:\n\n" + rows.join("\n") + "\n\n";
  }
  if (scope.productWide?.length) {
    const rows = scope.productWide.map((p) => {
      const text = p.text ? `: ${p.text}` : "";
      const applies = p.appliesTo?.length ? ` (applies to ${p.appliesTo.join(", ")})` : "";
      return `- ${p.id}${text}${applies}`;
    });
    out += "Product-wide requirements this version carries — each component's foundation Task builds them:\n\n" + rows.join("\n") + "\n\n";
  }
  return out === "" ? "" : out + "Stories:\n\n";
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
function instructedSkill(turn: TurnSpec, scope?: TurnScope): string | undefined {
  switch (turn.kind) {
    case "start":
      return "start";
    case "plan":
      return "task-planning";
    case "flow":
      return commandFlow(turn.skill)?.skill ?? turn.skill;
    case "chat":
      // A message sent with a feature open is a change to the requirements
      // (S4): the loop's playbook rides it. Other chat loads nothing up front;
      // the agent loads a skill when the message needs one.
      return scope?.kind === "feature" ? "refine" : undefined;
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

/**
 * The reference documents left out of this turn because the model on the
 * connection cannot read them (an image on a model without vision, a scanned
 * PDF where PDFs are read as text), each with the reason.
 *
 * The reference paragraph still lists them by path, so without this the model
 * would take a document it never received as read, or go looking for it. The
 * note makes the gap explicit and tells it to ask rather than guess. Repeated
 * on every turn the reference is re-listed, which is every turn it would have
 * been sent.
 */
export function unreadableReferencesNote(refs: readonly { filename: string; reason: string }[] | undefined): string {
  if (!refs || refs.length === 0) return "";
  return (
    `${refs.length === 1 ? "This reference document was" : "These reference documents were"} ` +
    `left out because the model on this connection cannot read ${refs.length === 1 ? "it" : "them"}; ` +
    `do not assume what ${refs.length === 1 ? "it says" : "they say"}, and ask the user if the work depends on ` +
    `${refs.length === 1 ? "it" : "them"}:\n` +
    refs.map((r) => `- ${r.filename}: ${r.reason}`).join("\n") +
    "\n\n"
  );
}

/**
 * What stands in a replayed message for an image the current model cannot read
 * (`historyFor`): an image stored while the conversation ran on a model with
 * vision would make a model without it refuse the whole request. Names the
 * file, so the model still knows the image existed and what it was called.
 */
export function imageLeftOutOfHistory(filename: string | undefined): string {
  return `[${filename ? `The image ${filename}` : "An image"} was left out here: the model on this connection does not read images.]`;
}

export function eagerSkillsFor(turn: TurnSpec, scope?: TurnScope): string[] {
  const instructed = instructedSkill(turn, scope);
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
