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
 * Agent SSE event contract — the typed, documented surface over the main spec
 * agent's turn stream.
 *
 * The wire stays RAW `StreamPart` (the SDK's `TextStreamPart`, one frame per
 * part); this module does NOT add an envelope. It exists so the producer (the
 * Express SSE route in `server.ts`), the eval, and the playground share ONE
 * definition of: the emitted event catalog, the payloads carried inside the
 * frames (`OpResult`, the per-tool `*Input` shapes), the reviewable `Change`
 * projection, and the turn-request body (`TurnRequest`).
 *
 * Ownership: this module is the leaf source of truth for the wire, published by
 * the `@aep/agent-stream` package so the producer (the agents service SSE
 * route), the fold consumers (evals, playground, console), and the BFF share ONE
 * definition. The domain (`bundle.ts`) and the agents-service `tool.ts` import
 * these types and their Zod schemas carry a compile-time drift guard asserting
 * they stay assignable to the `*Input` types here — so there is no
 * hand-maintained parallel copy. This stream is NOT part of the generated
 * OpenAPI contracts in `packages/contracts` (raw AI SDK frames aren't
 * OpenAPI-representable); the package stays free of any AI-SDK dependency.
 */

// --- Result payloads (the `tool-result.output` value) -----------------------

/** The file-mutation operations the main agent performs. */
export type Op = "add" | "edit" | "remove";

/** Error codes the `FileBundle` returns to steer one-step self-correction. */
export type ErrCode =
  | "ALREADY_EXISTS"
  | "INVALID_PATH"
  | "NOT_FOUND"
  | "NOT_UNIQUE"
  | "NO_SUCH_FILE"
  | "EMPTY_OLD_STRING"
  | "INVALID_YAML"
  | "INVALID_JSON"
  | "SCHEMA_VIOLATION"
  | "INVALID_DSL"
  | "INVALID_OPENAPI"
  | "INVALID_DIAGRAM"
  | "UNKNOWN_PARTICIPANT"
  | "UNKNOWN_DEPENDENCY"
  | "PROTECTED_PATH";

/** A candidate line echoed back for NOT_UNIQUE / NOT_FOUND re-anchoring. */
export interface MatchCandidate {
  line: number;
  text: string;
}

/**
 * A successful op. Carries NO file content — a successful result is just the
 * status; the model anchors later edits from its own tool-call args plus the
 * re-inlined CURRENT STATE, and consumers reconstruct file state by folding the
 * stream (see `Change` / `applyToolCall`).
 */
export interface OpOk {
  ok: true;
  path: string;
  op: Op;
  /** `applied` = state changed; `already-applied`/`noop` = idempotent no-change. */
  status: "applied" | "already-applied" | "noop";
}

/** A failed op. Keeps the self-correction payload (candidates / count). */
export interface OpErr {
  ok: false;
  path: string;
  op: Op;
  code: ErrCode;
  message: string;
  /** Populated for NOT_UNIQUE / NOT_FOUND to steer one-step re-anchoring. */
  candidates?: MatchCandidate[];
  count?: number;
}

export type OpResult = OpOk | OpErr;

// --- Per-tool input shapes (the `tool-call.input` value) --------------------
//
// These are the WIRE source of truth. The Zod `inputSchema`s in
// `@aep/agents` `tool.ts` carry a compile-time assert that `z.infer<schema>`
// stays equal to these — divergence fails that package's typecheck.

export interface AddFileInput {
  path: string;
  content: string;
}

export interface EditFileInput {
  path: string;
  oldString: string;
  newString: string;
}

export interface RemoveFileInput {
  path: string;
}

// --- ask_question / ask_questions (HITL, console ADR-0012 / #270) ------------
//
// The structured human-in-the-loop questions the main agent asks. Two tools:
//   `ask_question`  — ONE question (a single card).
//   `ask_questions` — a LIST of questions answered together (a form card).
// Both ride the ordinary `tool-call` frame — no dedicated event kind — and the
// console renders them as native question cards. The turn ENDS at the call
// (`hasToolCall` stop condition); the user's answer(s) return as the NEXT turn's
// plain-text instruction (`buildAnswerInstruction` / `buildAnswersInstruction`),
// never a new channel.

/** The wire tool NAMES — as much contract as the input shapes. One definition
 *  here so a rename cannot silently split producer (agents) and renderer
 *  (console). */
export const ASK_QUESTION_TOOL = "ask_question" as const;
export const ASK_QUESTIONS_TOOL = "ask_questions" as const;

/** The answer instructions' leading markers (consumers may detect answers by them). */
export const ANSWER_PREFIX = 'Answer to "' as const;
export const ANSWERS_PREFIX = "Answers:" as const;

/** One candidate answer on a question. */
export interface AskQuestionOption {
  /** Short display text — the value echoed back in the serialized answer. */
  label: string;
  /**
   * The full explanation of this choice, rendered on the option card: what it
   * means, what it implies, and its trade-offs — enough for the user to decide.
   */
  description?: string;
  /** At most ONE option per question carries this — the agent's recommendation. */
  recommended?: boolean;
  /**
   * Selecting this option means the user must TYPE their answer (an
   * "Other" / "Something else" escape hatch): the form focuses the free-text
   * field and requires text before submit.
   */
  freeText?: boolean;
}

/**
 * The `ask_question` tool input — a single structured question. WIRE source of
 * truth; drift-guarded against the agents-service Zod schema.
 */
export interface AskQuestionInput {
  question: string;
  /**
   * Context rendered under the question heading: why the agent is asking, what
   * the decision affects, and anything the user needs to answer well.
   */
  detail?: string;
  /**
   * 0–5 options; labels must be unique (they are the selection identity).
   * EMPTY means the answer must be typed — the form renders only the
   * free-text field, no choice cards.
   */
  options: AskQuestionOption[];
  /** True → several options may be chosen together (checkboxes, not radios). */
  multiSelect?: boolean;
}

/**
 * The `ask_questions` tool input — a LIST of questions answered as one form.
 * Each entry is a full `AskQuestionInput`.
 */
export interface AskQuestionsInput {
  /** 1–8 questions rendered together; each answered independently. */
  questions: AskQuestionInput[];
}

/** One question's answer: the chosen option labels plus an optional free-text note. */
export interface QuestionAnswer {
  selected: string[];
  freeText?: string;
}

function serializeBody(selected: string[], freeText?: string): string {
  const labels = selected.join(", ");
  const note = freeText?.trim() ?? "";
  return labels && note ? `${labels} — ${note}` : labels || note;
}

/**
 * Serialize a SINGLE question's answer into the next turn's instruction:
 * `Answer to "<question>": <label>[, <label>] — <note>`. Selection and note
 * combine; a free-text-only answer omits the labels.
 */
export function buildAnswerInstruction(
  question: string,
  selected: string[],
  freeText?: string,
): string {
  return `${ANSWER_PREFIX}${question}": ${serializeBody(selected, freeText)}`;
}

/**
 * Serialize a BATCH of answers (one per question) into the next turn's
 * instruction:
 *
 *   Answers:
 *   - "<q1>": <label>[, <label>] — <note>
 *   - "<q2>": <label>
 *
 * The agent reads it as an ordinary user message — no structured channel.
 */
export function buildAnswersInstruction(
  answers: Array<{ question: string; selected: string[]; freeText?: string }>,
): string {
  const lines = answers.map(
    (a) => `- "${a.question}": ${serializeBody(a.selected, a.freeText)}`,
  );
  return `${ANSWERS_PREFIX}\n${lines.join("\n")}`;
}

// --- declare_plan (fire-and-forget, console ADR-0025 / #576) -----------------
//
// The agent declares the spec-bundle paths it is ABOUT to write, so the
// console's spec rail can render a checklist (planned → writing → done) and an
// honest count instead of only a log of what already happened. Unlike the HITL
// tools above, the call does NOT end the turn: `execute` resolves immediately
// and the agent keeps working (the fire-and-forget class, console ADR-0025).
//
// The plan may GROW: the design agent writes the cell first, and only the cell
// fixes the component set, so later calls add the per-component files.
// Consumers take the UNION of every call — first-seen order kept, restated
// paths ignored — and support no removal. What becomes of an entry the run
// never writes depends on how the work ENDED: a clean finish dissolves the
// plan whole (the documents are the record), while a failure keeps the
// remainder standing as unfinished work until a later declaring call replaces
// it. A question mid-flight ends neither — the work resumes in the turn that
// answers.

/** The wire tool NAME — one definition, same rule as the question tools. */
export const DECLARE_PLAN_TOOL = "declare_plan" as const;

/**
 * The `declare_plan` tool input. WIRE source of truth.
 *
 * NOT yet drift-guarded: the tool is registered — with the Zod schema and the
 * `Equal<>` assert every other input here carries — when the agents-service
 * half lands via the handshake. Until then the console renders this shape from
 * typed mocks and no producer emits it.
 */
export interface DeclarePlanInput {
  /**
   * Full repo-relative spec-bundle paths (`specs/design/domain-model.md`), in the
   * order the turn intends to write them. The paths are the identity the
   * console reconciles file mutations against — no display names ride here;
   * naming a document is the console's job.
   */
  paths: string[];
}

// --- Skills (progressive disclosure, ADR-0002) ------------------------------
//
// Skills are GUIDANCE, not code, and they never travel on the wire: the turn's
// `WorkspaceRef.skillsRef` names an immutable `_skills` snapshot on the shared
// mount, and the service reads the catalog (and, lazily, the bodies) from
// there. The system prompt shows only a name+description catalog; the agent
// pulls a body on demand via the `loadSkill` tool. The body enters context only
// when loaded, and then persists as a tool result in message history.

/**
 * The `loadSkill` tool input. WIRE source of truth; drift-guarded in `tool.ts`.
 * Takes ALL the skills a turn needs in one call — batching keeps skill loading
 * to a single agent step instead of one step per skill.
 */
export interface LoadSkillInput {
  names: string[];
}

/** One resolved skill body inside a `loadSkill` result. */
export interface LoadedSkill {
  name: string;
  content: string;
  /** Reference paths for `loadSkillReference` — the body says when each is worth reading. */
  references?: string[];
}

/**
 * The `loadSkill` tool result. `ok: false` still carries every skill that DID
 * resolve plus the missing names and the available catalog, so the model
 * self-corrects in one round-trip (cf. NOT_FOUND) by re-calling for the
 * corrected missing names only.
 */
export type LoadSkillResult =
  | { ok: true; skills: LoadedSkill[] }
  | {
      ok: false;
      error: string;
      skills: LoadedSkill[];
      missing: string[];
      available: string[];
      /** Names refused because they are written for a different agent — visible in the catalog so they can be pinned, but not loadable here. */
      refused?: string[];
    };

/** The `loadSkillReference` tool input. WIRE source of truth; drift-guarded in `tool.ts`. */
export interface LoadSkillReferenceInput {
  name: string;
  path: string;
}

/**
 * The `loadSkillReference` tool result. A miss lists what IS addressable so the
 * model self-corrects in one round-trip: the skills carrying references (name
 * miss) or that skill's reference paths (path miss).
 */
export type LoadSkillReferenceResult =
  | { ok: true; name: string; path: string; content: string }
  | { ok: false; name: string; path: string; error: string; available: string[] };

// --- MCP discovery (caller-supplied, dependency-management migration Phase 5) -
//
// The org's dependency-discovery MCP server (aep-api
// `internal/feature/dependencies/mcp_server.go`, mounted at `POST
// /internal/v1/mcp`) lists read-only tools (list_external_resources,
// get_external_resource_schema, list_org_endpoints,
// list_platform_resource_types) so the main agent proposes `dependencies`
// entries that reuse resources/endpoints already registered in the org instead
// of inventing new names/shapes. Mirrors `WorkspaceRef`: the CALLER (the BFF)
// resolves the endpoint and mints a short-lived, org-bound bearer token, and
// pushes both in the turn payload; the service never reads either from its own
// env. Omitted → no `tools/list` fetch and no discovery tools registered
// (byte-identical to a turn without `mcp`).

/** Caller-supplied MCP discovery endpoint for this turn. */
export interface McpConfig {
  /** The MCP JSON-RPC endpoint (aep-api's `/internal/v1/mcp`, org-bound). */
  url: string;
  /**
   * Bearer token for that endpoint. Short-lived (minted per call, ~5 min TTL on
   * the aep-api side, `AudienceMCP`/`aep-api-mcp`) — a turn that outlives it sees
   * the discovery tools 401 partway through; `loadMcpTools` degrades that to "no
   * tools" up front, but a mid-turn `tools/call` 401 surfaces as a failed tool
   * call (best-effort, not retried).
   */
  token: string;
}

/**
 * Caller-supplied collab-room reference for a room-scoped turn (#86 phase 4).
 * Mirrors `McpConfig`: the BFF resolves the room and forwards the caller's
 * bearer; the service never reads either from its own env (the ws URL alone
 * comes from service config — the BFF doesn't know the agents-side route).
 * Present → the agents service joins the room as a live Yjs peer, reads the
 * file bundle FROM the doc, and applies file ops to it; nothing is committed
 * to git (persistence is the #86 phase-3 committer). Omitted → the
 * committed-truth snapshot turn, byte-identical to today.
 */
export interface CollabConfig {
  /** The room id (`spec-<org>-<project>`), resolved by the BFF. */
  roomId: string;
  /**
   * The caller's bearer, forwarded request-scoped (#86 decision 7): the
   * collab server's BFF oracle validates it exactly like a browser join.
   */
  token: string;
}

/** Runtime guard for an untrusted `collab` value (the server's pre-stream 400 check). */
export function isCollabConfig(v: unknown): v is CollabConfig {
  if (typeof v !== "object" || v === null) return false;
  const c = v as Record<string, unknown>;
  return typeof c.roomId === "string" && c.roomId !== "" && typeof c.token === "string" && c.token !== "";
}

// --- The reviewable change (§7) ---------------------------------------------

/**
 * A reviewable projection of one `tool-result` part: the op intent plus its
 * result. A browser folds these into a live diff; the eval reconstructs files
 * via `applyToolCall` instead. Pure field projection — see `toChange` in
 * `@aep/agents` `change.ts`.
 */
export interface Change {
  toolCallId: string;
  toolName: string;
  op: Op;
  path: string;
  /** editFile payload. */
  oldString?: string;
  newString?: string;
  /** addFile payload. */
  content?: string;
  result: OpResult;
}

// --- The turn request (the `POST /conversations/:id/turns` body) -------------

/**
 * The shared-volume workspace reference (shared-workspace-volume, D9): IDs +
 * shas only — **no filesystem path ever crosses the boundary**. The
 * agents service derives
 * `$WORKSPACE_MOUNT_ROOT/repos/<org>/<proj>/<repoSlug>/snapshots/<ref>/` (and
 * the `_skills` analog) itself from these fields, so a hostile payload has no
 * path input to traverse: the tenancy fence is structural, not validated-in.
 */
export interface WorkspaceRef {
  /**
   * The namespaced conversation id, `org_<orgId>--proj_<projectId>--<useCase>--<uuid>`.
   * Must equal the URL `:id`; supplies the org/proj path segments and the org
   * value asserted against the caller's `X-Org-Id` claim (the IDOR fence).
   */
  conversationId: string;
  /** Per-dispatch uuid (turn attribution/tracing; never used in path derivation). */
  turnId: string;
  /** The repo directory segment (`git_repositories.repo_slug`; validated slug format). */
  repoSlug: string;
  /** 40-hex committed base sha — the turn reads `snapshots/<ref>/`. */
  ref: string;
  /** 40-hex `_skills` head sha — skills load from `_skills/org-skills/snapshots/<skillsRef>/`. */
  skillsRef: string;
}

/**
 * What a turn is FOR, as facts rather than prose. The caller states the intent
 * and the values only it can know (the captured idea, the milestone scope);
 * the agents service turns that into instruction text. No caller composes
 * prompt wording — that is the whole point of this type.
 *
 *  - `chat`  — an ordinary user message, sent verbatim.
 *  - `flow`  — a `/<command>`: load a skill and follow it, with the user's
 *              trailing text (if any) riding along. `skill` carries the
 *              command's TOKEN as typed; most tokens are the skill name, and
 *              the few that name a branch of one instead resolve in the agents
 *              service, which is where wording lives. `references` names the
 *              attached reference documents exactly as on `start` — a flow
 *              generates artifacts (wireframes above all) that must be
 *              grounded in an attached sketch or spec.
 *  - `start` — the project kickoff. `idea` is what the user asked for, read by
 *              the BFF from `specs/.agentic-engineer.toml` — a dot-led path
 *              stripped from every turn snapshot, so the agent cannot read it
 *              itself. Absent/blank → the start skill asks the user instead.
 *              `references` names the documents attached at project create
 *              (paths under `specs/requirements/references/`, listed by the
 *              BFF). Paths only: they are ordinary spec content the agent
 *              reads from its own snapshot. Absent/empty → nothing is said.
 *  - `plan`  — Task planning. `scope` is the milestone's story coverage and
 *              `taskContext` the existing-Task renders; both are platform
 *              state, not repository files, so they cannot ride the snapshot.
 */
export type TurnSpec =
  | { kind: "chat"; text: string }
  | { kind: "flow"; skill: string; text?: string; references?: string[] }
  | { kind: "start"; idea?: string; references?: string[] }
  | { kind: "plan"; scope?: PlanScope; taskContext?: PlanContextFile[] };

/** The turn kinds a `TurnSpec` may declare (the server's pre-stream 400 check). */
export const TURN_KINDS = ["chat", "flow", "start", "plan"] as const;

export type TurnKind = (typeof TURN_KINDS)[number];

/**
 * What the user pointed at when they aimed a turn at part of a spec document
 * (#666; console ADR-0024).
 *
 * It LOCATES; it never carries the selected content. The agent joins the spec
 * room as a live peer, so between the selection and the turn starting the user
 * may keep typing and a teammate may edit too — content captured client-side
 * would be a photograph of a document that has since moved. The agent resolves
 * these names against the document in its OWN turn snapshot, and says so in its
 * reply when it cannot find one. That is the designed failure, not an error.
 */
export interface TurnAnchor {
  /** The authored spec file the selection resolves to. One view, one file. */
  file: string;
  /** The selected nodes, in document order. */
  nodes: TurnAnchorNode[];
}

/** One selected node — the name the agent resolves, and what the transcript shows. */
export interface TurnAnchorNode {
  /**
   * A structured view supplies the node's own name (`POST /rounds`); markdown
   * has none, so it supplies a bounded excerpt of the block's RENDERED text.
   * Bounded is the load-bearing part: an excerpt that grows with the selection
   * is the carried content this shape exists to avoid.
   */
  name: string;
  /** The node's vocabulary word — `paragraph`, `operation`, `external dependency`. */
  kind: string;
  /** Where it sits, when the name cannot stand alone: a heading path, a parent. */
  context?: string;
}

/**
 * An anchored turn's aim (#666): what was pointed at, and what for.
 *
 * The two travel together or not at all — an intent with nothing to point at
 * says nothing, and an anchor with no intent leaves the wording undecided.
 *
 * `change` rewrites the named nodes in place; `discuss` opens the same
 * selection as a grilling. They differ ONLY in how the preamble is phrased,
 * which is why this is a fact on the turn rather than a `/command` the console
 * prefixes onto the user's own words.
 */
export interface TurnAim {
  anchor: TurnAnchor;
  intent: TurnAimIntent;
}

export type TurnAimIntent = "change" | "discuss";

/**
 * A turn's display record (#463): the raw client-sent instruction and the
 * acting user. `author` mirrors the console's live author shape
 * (`{id: email, displayName}`) so a rehydrated row is attributable — and
 * self-vs-teammate distinguishable — exactly like a live one; it is omitted
 * for M2M callers with no human identity.
 */
export interface TurnJournal {
  text: string;
  author?: { id: string; displayName: string };
  /**
   * File NAMES attached to this message (#428) — never bytes. The display read
   * replaces a user row's content with `text`, so without these a reload would
   * show the agent discussing a document that appears nowhere in the thread.
   * Names only: the journal is a DISPLAY record, and a chip is not a download.
   */
  attachments?: string[];
  /**
   * What this message was aimed at (#666). Journaled for the same reason as
   * attachment names: the console renders it as a tag above the message, and
   * without the journal a reload would leave "make this shorter" with nothing
   * saying what "this" was. A record of the words but not the target is not a
   * record of what happened.
   */
  anchor?: TurnAnchor;
}

/**
 * One chat attachment (#428): conversation-scoped model content the user
 * attached to a single message.
 *
 * Carried INLINE on the turn request rather than by path, which is the whole
 * difference from `TurnSpec.references`. A reference is stored and overlaid into
 * the turn's snapshot, so it can be named by path; an attachment is never
 * written to disk anywhere (console ADR-0019), so the bytes have to travel with
 * the request that carries them. Once the turn runs they are durable as parts of
 * the conversation's history, and nothing needs to keep a second copy.
 */
export interface TurnAttachment {
  /**
   * The original file name. Also the DEDUPE KEY: the turn loop drops an
   * attachment whose name the conversation history already holds, so re-sending
   * one costs nothing.
   */
  name: string;
  /**
   * The media type the model reads it as — `application/pdf`, one of the four
   * image types, or `text/plain` for every text format. Those are the only
   * document types the Anthropic provider maps, so a `.csv` arrives as
   * `text/plain` rather than `text/csv`.
   */
  mediaType: string;
  /** base64 of the raw bytes. */
  data: string;
}

/** Runtime guard for one untrusted `TurnAttachment`. */
export function isTurnAttachment(v: unknown): v is TurnAttachment {
  if (v === null || typeof v !== "object") return false;
  const a = v as Record<string, unknown>;
  return (
    typeof a.name === "string" &&
    a.name.trim() !== "" &&
    typeof a.mediaType === "string" &&
    a.mediaType.trim() !== "" &&
    typeof a.data === "string"
  );
}

/** Runtime guard for an absent-or-valid `attachments` array. */
export function isTurnAttachmentsOrAbsent(v: unknown): v is TurnAttachment[] | undefined {
  return v === undefined || (Array.isArray(v) && v.every(isTurnAttachment));
}

/**
 * Runtime guard for an untrusted `aim` value (#666).
 *
 * A malformed anchor is rejected WHOLE rather than partially: an aim naming
 * fewer nodes than the user selected would send the agent at the wrong scope
 * quietly, which is worse than a clean 400 the caller can see.
 */
/**
 * The anchor's size ceilings. An anchor LOCATES and never carries content
 * (console ADR-0024), so every field is bounded — a locator that grows with
 * the selection is the carried payload the shape exists to avoid, arriving
 * through a hand-built request instead of the console. `name`'s 200 mirrors
 * the contract's maxLength; the rest bound what the contract leaves open.
 */
export const TURN_AIM_LIMITS = {
  nodes: 50,
  file: 512,
  name: 200,
  kind: 64,
  context: 512,
} as const;

export function isTurnAim(v: unknown): v is TurnAim {
  if (v === null || typeof v !== "object") return false;
  const a = v as Record<string, unknown>;
  if (a.intent !== "change" && a.intent !== "discuss") return false;
  const anchor = a.anchor;
  if (anchor === null || typeof anchor !== "object") return false;
  const { file, nodes } = anchor as Record<string, unknown>;
  if (typeof file !== "string" || file.trim() === "" || file.length > TURN_AIM_LIMITS.file) {
    return false;
  }
  if (!Array.isArray(nodes) || nodes.length === 0 || nodes.length > TURN_AIM_LIMITS.nodes) {
    return false;
  }
  return nodes.every((n) => {
    if (n === null || typeof n !== "object") return false;
    const node = n as Record<string, unknown>;
    return (
      typeof node.name === "string" &&
      node.name.trim() !== "" &&
      node.name.length <= TURN_AIM_LIMITS.name &&
      typeof node.kind === "string" &&
      node.kind.trim() !== "" &&
      node.kind.length <= TURN_AIM_LIMITS.kind &&
      (node.context === undefined ||
        (typeof node.context === "string" && node.context.length <= TURN_AIM_LIMITS.context))
    );
  });
}

/** The milestone a plan turn is scoped to, and which of its stories already have Tasks. */
export interface PlanScope {
  /** The spec tag the milestone is pinned to. */
  tag: string;
  stories: { number: number; title?: string; covered: boolean }[];
}

/** One existing-Task render passed as read-only planning context. */
export interface PlanContextFile {
  /** Historical `tasks/<n>.md` name — kept so the model's mental layout is unchanged. */
  path: string;
  body: string;
}

/**
 * The turn-request body (D9/§12): the body carries a `WorkspaceRef` and the
 * service reads the file snapshot AND the skills from the shared read-only
 * mount — no file content or skill bodies ever cross the wire.
 *
 * `filesChangedExternally` flags an out-of-band edit so the server prepends a
 * CURRENT-STATE-authoritative note. The producer (server) validates an
 * untrusted body against this shape; the eval client (and the BFF) construct
 * it — one definition, no drift.
 */
export interface TurnRequest {
  /**
   * What this turn is for. The agents service composes the instruction text
   * from it — see `TurnSpec`.
   */
  turn: TurnSpec;
  /**
   * The spec-bundle path this turn should write to, when the caller pins one.
   * The service renders it into the instruction; callers never format it.
   */
  target?: string;
  /**
   * The previous turn of this conversation FAILED (D20): its changes never
   * reached git, though the conversation history claims they did. The service
   * prepends the note that reconciles the two.
   */
  previousTurnFailed?: boolean;
  /**
   * No interview is possible in this run (the playground's headless phases):
   * the service tells the agent to generate on stated assumptions rather than
   * calling the question tools.
   */
  headless?: boolean;
  /** Where to read files + skills from the shared mount (IDs + shas only). */
  workspace: WorkspaceRef;
  filesChangedExternally?: boolean;
  /**
   * Caller-supplied MCP discovery endpoint for this turn (dependency-management
   * migration Phase 5). Present → the turn loop fetches `tools/list` from it
   * (best-effort) and registers each as a dynamic tool, merged under a
   * shadow-guard so a discovered tool can never shadow a built-in one. Omitted →
   * no fetch, no discovery tools (byte-identical to an mcp-free turn).
   */
  mcp?: McpConfig;
  /**
   * The turn's display record (#463): the raw client-sent instruction (exactly
   * what the sender's UI rendered as the user bubble) plus a best-effort acting
   * user. Journaled beside the transcript and served for user rows on the
   * get-conversation read — never woven into the model prompt. Omitted (older
   * callers, evals) → no journal entry; the read falls back to the raw stored
   * message for that turn.
   */
  journal?: TurnJournal;
  /**
   * Room-scoped turn (#86 phase 4): join this collab room as a live Yjs peer,
   * read files from the doc, apply ops to the doc, commit nothing. Omitted →
   * the committed-truth snapshot turn (byte-identical to today).
   */
  collab?: CollabConfig;
  /**
   * Attach Anthropic's provider-executed `web_search` tool for this turn
   * (external-dependency-discovery #252) — lets the model verify a candidate
   * external API/SDK actually exists before proposing a `dependencies` entry
   * for it. The caller (BFF) sets this true under the SAME condition as `mcp`
   * (design-generate or any collab room-scoped turn), but unlike `mcp` it
   * needs no BFF-minted credential. Registered only when true AND the turn's
   * model is actually Anthropic; omitted/false, or a non-Anthropic model, →
   * the tool map is byte-identical to a turn without it.
   */
  webSearch?: boolean;
  /**
   * Chat attachments for THIS turn (#428) — bytes inline, see `TurnAttachment`.
   * Absent/empty → the turn's messages are byte-identical to one built before
   * this field existed.
   */
  attachments?: TurnAttachment[];
  /**
   * Where the person reading this turn's prose is sitting (#580). The right
   * vocabulary belongs to the SURFACE, not to the skill: in a local run the
   * user is standing in the repo, so `design.cell` is the right word; in the
   * console it names nothing on screen. The agents service inlines the
   * surface's narration skill into the system prompt — see
   * `buildNarrationBlock`. Omitted → no narration policy, and the prompt is
   * byte-identical to a turn without it (the playground's case).
   *
   * This is the one turn property that genuinely cannot be derived: it is who
   * is asking, not what is being asked for.
   */
  surface?: Surface;
}

/**
 * The surfaces a turn's prose can be read on. A surface's narration policy is
 * the skill of the SAME NAME (`skills/console/`), so there is no second table
 * mapping one to the other.
 */
export const SURFACES = ["console"] as const;

export type Surface = (typeof SURFACES)[number];

/** Runtime guard for a `Surface` value. */
export function isSurface(v: unknown): v is Surface {
  return (SURFACES as readonly unknown[]).includes(v);
}

/**
 * The registrable tool sets. NOT a wire field: the agents service derives the
 * set from `TurnSpec.kind` (`plan` → task-plan, everything else → files), so a
 * caller cannot ask for a tool set that disagrees with what its turn is for.
 */
export const TOOLSETS = ["files", "task-plan"] as const;

export type Toolset = (typeof TOOLSETS)[number];

/** Runtime guard for a `Toolset` value. */
export function isToolset(v: unknown): v is Toolset {
  return (TOOLSETS as readonly unknown[]).includes(v);
}

/**
 * Runtime guard for an untrusted `turn` value (the server's pre-stream 400
 * check). Validates the discriminant AND the fields that kind requires, so a
 * malformed body fails with a status code rather than composing a nonsense
 * instruction: a `flow` with no skill, or a `chat` with no text, is not a turn.
 * Optional fields are checked for TYPE when present and ignored when absent;
 * unknown extra keys are tolerated (forward compatibility, as everywhere else
 * on this body).
 */
export function isTurnSpec(v: unknown): v is TurnSpec {
  if (v === null || typeof v !== "object") return false;
  const t = v as Record<string, unknown>;
  const str = (x: unknown): boolean => typeof x === "string";
  const optStr = (x: unknown): boolean => x === undefined || typeof x === "string";
  const optStrArr = (x: unknown): boolean => x === undefined || (Array.isArray(x) && x.every(str));
  switch (t.kind) {
    case "chat":
      return str(t.text) && (t.text as string).trim() !== "";
    case "flow":
      return str(t.skill) && (t.skill as string).trim() !== "" && optStr(t.text) && optStrArr(t.references);
    case "start":
      return optStr(t.idea) && optStrArr(t.references);
    case "plan":
      return isPlanScopeOrAbsent(t.scope) && isPlanContextOrAbsent(t.taskContext);
    default:
      return false;
  }
}

function isPlanScopeOrAbsent(v: unknown): boolean {
  if (v === undefined) return true;
  if (v === null || typeof v !== "object") return false;
  const s = v as Record<string, unknown>;
  if (typeof s.tag !== "string") return false;
  if (!Array.isArray(s.stories)) return false;
  return s.stories.every((row) => {
    if (row === null || typeof row !== "object") return false;
    const r = row as Record<string, unknown>;
    return (
      typeof r.number === "number" &&
      typeof r.covered === "boolean" &&
      (r.title === undefined || typeof r.title === "string")
    );
  });
}

function isPlanContextOrAbsent(v: unknown): boolean {
  if (v === undefined) return true;
  if (!Array.isArray(v)) return false;
  return v.every((row) => {
    if (row === null || typeof row !== "object") return false;
    const r = row as Record<string, unknown>;
    return typeof r.path === "string" && typeof r.body === "string";
  });
}

// --- The terminal manifest (shared-workspace-volume D14) --------------------

/**
 * Per-turn token usage carried on the terminal manifest (#249). Field names are
 * the pinned cross-runtime wire shape — the coding-runner progress `result`
 * event carries the identical object and the aep-api parses both against one
 * definition, so they must not drift. Tokens only, no cost figure: USD is
 * derived server-side from tokens + model (console ADR-0011).
 */
export interface TurnUsage {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  /** The resolved model id the turn ran on (the server-side pricing key). */
  model: string;
}

/**
 * The terminal manifest frame — ALWAYS emitted (possibly empty) after a turn
 * completes successfully, before `[DONE]`; a severed/failed stream carries NO
 * manifest, which is exactly what lets the consumer (the aep-api fold) treat
 * its absence as "do not commit". Covers ONLY the paths mutated THIS turn:
 * `files` maps each still-present touched path to the sha256 (lowercase hex,
 * over the UTF-8 bytes) of its final content; `deleted` lists the touched
 * paths no longer present. A chat-only or task-plan turn emits
 * `{files: {}, deleted: []}`. Structurally a `StreamPart` (open type), named
 * here so both fold sides hash-check against ONE definition.
 */
export interface ManifestPart {
  type: "manifest";
  /** path → sha256 hex of the final content, for every path mutated this turn and still present. */
  files: Record<string, string>;
  /** Paths mutated this turn that are no longer present at turn end. */
  deleted: string[];
  /**
   * The turn's token spend (#249). Present on every manifest the agents
   * service emits today; optional so older producers/recorded streams stay
   * valid. Manifest-only ⇒ a failed/severed turn carries no usage (v1).
   */
  usage?: TurnUsage;
}

// --- The emitted event catalog ----------------------------------------------

/**
 * The documented subset of `StreamPart` `type`s the SSE route emits, one SSE
 * frame each. The wire carries the raw part verbatim; this catalog names what a
 * consumer can expect to see. `manifest` (D14) is terminal: at most one, after
 * the loop's last `finish` and before `[DONE]`, only on a successful turn.
 */
export const AGENT_SSE_EVENT_TYPES = [
  "text-delta",
  "tool-input-start",
  "tool-input-delta",
  // The per-call completion signal for one tool's ARGUMENTS: a step may issue
  // several calls, and this is the frame that says this one's are fully written.
  // For a file tool the arguments are the file body, which makes it the moment
  // the file is complete.
  "tool-input-end",
  "tool-call",
  // The call's VERDICT, and it rides that call's own `tool-call` — a file write
  // is applied and reported at its own `tool-input-end`
  // (`services/agents/src/agents/main/tools/write-ledger.ts`), not at the tail
  // of the step. That matters because the SDK underneath does the opposite: it
  // queues a step's calls and executes them all after the whole assistant
  // message has streamed, which for a step batching five `addFile`s would leave
  // file 1's verdict waiting on file 5's body. Exactly ONE result per call
  // reaches the wire. The ordering is pinned by
  // `services/agents/test/frame-order.test.ts` (it needs the real SDK loop).
  "tool-result",
  "tool-error",
  "error",
  "finish",
  "manifest",
] as const;

export type AgentSseEventType = (typeof AGENT_SSE_EVENT_TYPES)[number];

/** The terminal sentinel sent after the last frame: `data: [DONE]`. */
export const SSE_DONE = "[DONE]" as const;
