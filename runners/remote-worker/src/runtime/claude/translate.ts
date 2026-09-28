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

// The CLAUDE ADAPTER: Claude Agent SDK messages in, run events v2 out.
//
// It is the only module in the platform that knows which coding runtime is
// running. Everything downstream — the runner loop, the watchdog, aep-api, the
// console, the playground — reads the canonical events this produces, so a
// second runtime is a second adapter and nothing else.
//
// **The agent tree is DECLARED, not inferred.** That is the whole difference
// between this and the v1 translator it replaces. v1 had no "an agent started"
// event: a subagent existed on the feed because a line happened to carry
// `emitterId`, it ended when a `tool_result` for the fan-out call arrived, and
// depth was not modelled at all. So a console could not draw a tree, a reader
// could not tell two concurrent agents apart from one flapping agent, and a
// depth-2 child was flattened onto its parent. The SDK has been declaring all
// of it for a while and we were discarding the declaration:
//
//   task_started {task_id, tool_use_id, description, subagent_type,
//                 is_backgrounded, spawn_depth, task_type}
//
// is an agent's birth certificate. `task_id` is the agent's identity for its
// whole life, `spawn_depth` is its depth, and `tool_use_id` is the call that
// spawned it — which is also the `parent_tool_use_id` stamped on every message
// forwarded from inside it. Those two facts together are what make the tree
// reconstructible from the stream alone:
//
//   tool_use_id → the agent that ISSUED the call   (issuedBy, below)
//   tool_use_id → the agent the call STARTED        (spawnedAgent, below)
//
// A depth-2 child's `task_started` names its spawning `tool_use_id`; the
// assistant message that carried that `tool_use` was itself forwarded from its
// parent, so `issuedBy` answers "whose child is this" without a single guess.
// Measured on `test/fixtures/probe1-background-fanout.jsonl`: gamma's child
// arrives with `spawn_depth: 2` and resolves to gamma, not to the lead.
//
// **Attribution is one field.** Every event carries `agentId`; the lead is the
// literal string `lead` (see emitter.ts). No consumer infers an author from
// anything else — not from a tool name, not from an absence.
//
// **Runtime names never reach a consumer's logic.** `tool` carries the SDK's own
// tool name because that is what a row prints, but fan-out is `agent_started`,
// never "a `tool_result` whose tool is called `Agent`".
//
// The adapter is a per-run factory. Its state — the agent registry, the
// in-flight calls, the heartbeat clocks — describes ONE run, and two runs
// sharing it would mislabel lines rather than merely lose detail.
//
// **What is NOT this runtime's lives beside the feed, not here.** A shell
// command read as a commit, a push or a `gh` call, a failed call's output read
// down to its diagnosis, and an authoring call's line delta are
// `lib/progress/tool_rows.ts`; the contract's field caps and the heartbeat
// budget are `lib/progress/adapter_common.ts`. This file pulls the facts out of
// the SDK's message shapes and hands them over, so a second runtime's
// translator produces the same rows from its own shapes.

import {
  LEAD_AGENT_ID,
  type AgentStatus,
  type RunEvent,
  type RunEventInput,
  type RunEventModelUsage,
  type RunEventUsage,
} from "../../lib/progress/emitter.js";
import { cap, createHeartbeatLimiter, MAX_REPORT, planStatus, reportFrom, trimSummary } from "../../lib/progress/adapter_common.js";
import {
  editDelta,
  failureDetail,
  failureText,
  shellEvents,
  summaryFromInput,
  writeDelta,
  type LineDelta,
} from "../../lib/progress/tool_rows.js";
import { num, obj, str } from "../fields.js";
import type { RuntimeArtifact } from "../port.js";

// The SDK's fan-out tool has gone by both names across versions — `Agent` is
// what the current one emits, `Task` is what the older allow-lists and skill
// prose still call it. Recognising both means a rename does not silently drop
// every spawned agent's label back to anonymous. This is the ONE place either
// name appears; downstream sees `agent_started`.
const FANOUT_TOOLS = new Set(["Task", "Agent"]);

// A fanned-out agent, as opposed to a backgrounded shell command. The SDK
// reports both through the same task_* messages and tells them apart only by
// this field: `local_agent` is another agent working on our behalf and its
// lines belong to IT; `local_bash` is a command some agent put in the
// background, and it belongs to that agent.
const TASK_TYPE_AGENT = "local_agent";

// The harness's task-LIST tools. They carry the lead's plan, which v2 puts on
// the feed as `work_item {source: "plan"}` — and they are deliberately no
// longer denied (see DISALLOWED_TOOLS in runner.ts). `TaskGet`/`TaskList` are
// reads and produce nothing.
const PLAN_CREATE_TOOL = "TaskCreate";
const PLAN_UPDATE_TOOL = "TaskUpdate";
const PLAN_READ_TOOLS = new Set(["TaskGet", "TaskList"]);

// Tools whose input tells us how many lines an agent's work added or removed.
// See `lineDelta` for why this is counted at all.
const AUTHORING_TOOLS = new Set(["Write", "Edit"]);

// A run can outlive far more tool calls than it ever gets results for
// (interrupted calls, a stream that ends mid-flight), so the pending map is
// capped and evicted oldest-first. Losing the oldest entry costs a duration and
// a tool name on a line whose call is long past; leaking is unbounded memory in
// a pod. Map preserves insertion order, which is what makes this one line.
const MAX_PENDING_CALLS = 2000;

/**
 * The token usage of the run so far, off an SDK `result` message: the folded
 * aggregate AND the per-model split.
 *
 * The split is what makes the number usable. Cost is stamped per model, against
 * that model's own rate row, so an aggregate that folded two models — and
 * therefore reports `model: ""` — cannot be priced at all. Mixed-model runs are
 * not exotic: the runtime reaches for small-model helpers of its own, and a
 * fan-out call can name a model alias. The runner pins every alias to the org's
 * model today, but emitting only the total is how those runs became unpriceable.
 *
 * Two more things about this are counter-intuitive and both are measured.
 *
 * **`modelUsage` is CUMULATIVE across turns.** The SDK's own documentation says
 * so ("each result carries the running total so far, so read the latest rather
 * than summing"), and probe 2 confirms it: its first result reports 20 input
 * tokens, its second 48 — which is the first turn's 20 plus the second's 28,
 * not the second's alone. So a consumer must read the LATEST `turn_ended` and
 * never add several up, and the run's total is exactly the last turn's number.
 * That is why `run_settled` can simply carry the last `turn_ended`'s usage.
 *
 * **Versioned release ids collapse onto their canonical model.** Two dated
 * releases of one model are still a single-model run, and aep-api's rate table
 * is keyed by the alias.
 *
 * `costUsd` is deliberately `null`. The SDK reports a `total_cost_usd` estimate,
 * but the platform stamps a run's cost from its own `model_rates` at capture
 * time — two prices for one run is two answers to one question — and the
 * contract says as much ("null when no stamp exists"). Returns undefined when
 * the result carried no usage at all.
 */
function usageFromResult(m: Record<string, unknown>): RunEventUsage | undefined {
  const usage = m.usage;
  if (!usage || typeof usage !== "object") return undefined;

  // One accumulator per canonical model, in first-seen order. The map IS the
  // split: folding straight to a total would throw away the only thing that
  // makes a mixed-model run priceable, because the aggregate's `model` is then
  // "" and no rate row answers to that.
  const perModel = new Map<string, RunEventModelUsage>();
  for (const [key, val] of Object.entries(obj(m.modelUsage))) {
    const entry = obj(val);
    // Versioned release ids collapse onto their canonical model: two dated
    // releases of one model share a rate row, so they are one slice, not two.
    const canonical = str(entry.canonicalModel) || key;
    const slice = perModel.get(canonical) ?? {
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
      model: canonical,
      // The platform stamps USD from its own rates at capture — see the header.
      costUsd: null,
    };
    slice.inputTokens += num(entry.inputTokens);
    slice.outputTokens += num(entry.outputTokens);
    slice.cacheReadTokens += num(entry.cacheReadInputTokens);
    slice.cacheCreationTokens += num(entry.cacheCreationInputTokens);
    perModel.set(canonical, slice);
  }

  const models = [...perModel.values()];
  const totals = models.reduce(
    (acc, s) => ({
      inputTokens: acc.inputTokens + s.inputTokens,
      outputTokens: acc.outputTokens + s.outputTokens,
      cacheReadTokens: acc.cacheReadTokens + s.cacheReadTokens,
      cacheCreationTokens: acc.cacheCreationTokens + s.cacheCreationTokens,
    }),
    { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
  );

  // No modelUsage at all (an older CLI, a crashed startup result) leaves the
  // per-turn `usage` block as the only figure there is.
  const u = obj(usage);
  const fromTurn = {
    inputTokens: num(u.input_tokens),
    outputTokens: num(u.output_tokens),
    cacheReadTokens: num(u.cache_read_input_tokens),
    cacheCreationTokens: num(u.cache_creation_input_tokens),
  };

  return {
    ...(models.length > 0 ? totals : fromTurn),
    // "" is the contract's word for a mixed-model aggregate. It is also what a
    // result with no modelUsage gets: naming one model there would be a guess.
    model: models.length === 1 ? models[0].model : "",
    costUsd: null,
    // Omitted rather than sent empty when there is nothing to break down: the
    // contract reads an absent split as "this producer reported none" and
    // prices the aggregate by its single model, which is the right answer for a
    // single-model turn and the only one available with no modelUsage at all.
    ...(models.length > 0 ? { models } : {}),
  };
}

// Most file tools carry file_path / pattern / path; `skill` is the Skill tool's,
// and `description` is the last human-readable field a tool is likely to have.
// Reaching any of them beats the JSON dump, which rendered
// `$ Skill {"skill":"aep:aep"}` in a live run.
const SUMMARY_FIELDS = ["file_path", "path", "pattern", "glob", "url", "skill", "description"] as const;

interface ToolUseBlock {
  id: string;
  name: string;
  input: Record<string, unknown>;
}

// Best-effort pluck of tool_use content blocks from an assistant SDK message.
// The SDK exposes message.message.content[] where each block has a `type`;
// every other block kind (text, thinking) is developer material and reaches
// runtime.log, never the feed.
function assistantToolUseBlocks(message: Record<string, unknown>): ToolUseBlock[] {
  const content = obj(message.message).content;
  if (!Array.isArray(content)) return [];
  const out: ToolUseBlock[] = [];
  for (const block of content) {
    const b = obj(block);
    if (b.type !== "tool_use") continue;
    const name = str(b.name);
    if (!name) continue;
    out.push({ id: str(b.id), name, input: obj(b.input) });
  }
  return out;
}

interface ToolResultBlock {
  toolUseId: string;
  isError: boolean;
  content: unknown;
}

// The mirror of assistantToolUseBlocks: a `user` SDK message is how a tool's
// outcome comes back, one tool_result block per call it answers.
function userToolResultBlocks(message: Record<string, unknown>): ToolResultBlock[] {
  const content = obj(message.message).content;
  if (!Array.isArray(content)) return [];
  const out: ToolResultBlock[] = [];
  for (const block of content) {
    const b = obj(block);
    if (b.type !== "tool_result") continue;
    const id = str(b.tool_use_id);
    if (!id) continue;
    out.push({ toolUseId: id, isError: b.is_error === true, content: b.content });
  }
  return out;
}

// Tool output arrives either as a plain string or as an array of content
// blocks; only the text of it is any use in a one-line feed. Newlines are kept
// — the failure parse below reads this line by line, and collapsing whitespace
// first is what would leave a build failure reporting "Compiling source".
function resultText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  const parts: string[] = [];
  for (const block of content) {
    if (block && typeof block === "object") {
      const text = (block as Record<string, unknown>).text;
      if (typeof text === "string") parts.push(text);
    } else if (typeof block === "string") {
      parts.push(block);
    }
  }
  return parts.join("\n");
}

// A non-shell tool reports its failure wrapped like this, with no exit code.
const TOOL_USE_ERROR_TAG = /<\/?tool_use_error>/g;

/**
 * A tool result's output as plain text, this runtime's wrapper removed — the
 * form `failureDetail` / `failureText` (tool_rows.ts) read a diagnosis from.
 */
function toolOutputText(content: unknown): string {
  return resultText(content).replace(TOOL_USE_ERROR_TAG, "");
}

/**
 * How long a tool call ran before its Bash timeout severed it, when it was.
 *
 * A command that blows its Bash timeout is auto-backgrounded, and what comes
 * back is `code: 0`, empty output, and `is_error` false — indistinguishable
 * from a command that ran and succeeded. That cost a validation run its whole
 * cycle (#701): the final suite run was severed at the ceiling and the feed
 * recorded it as a success.
 *
 * `timedOutAfterMs` is the SDK's own flag for exactly that case. A
 * `backgroundTaskId` with no timeout is a deliberate `run_in_background`
 * launch — also not a completion, but nobody was misled about it, so it is
 * not reported here.
 */
function severedAfterMs(result: unknown): number | undefined {
  return num(obj(result).timedOutAfterMs) || undefined;
}

/**
 * The line deltas the runtime reports for a whole spawned agent, off its
 * fan-out call's `tool_result`.
 *
 * Kept because it is authoritative where it exists — the SDK's own
 * `totalDurationMs` of 209158 matched a hand-measured 3m29s exactly — but see
 * `AgentRecord.reportedLines` for why it is usually the counted figure that
 * ends up on the row.
 */
function toolStatsLines(result: unknown): { linesAdded: number; linesRemoved: number } | undefined {
  const stats = obj(obj(result).toolStats);
  if (!("linesAdded" in stats) && !("linesRemoved" in stats)) return undefined;
  return { linesAdded: num(stats.linesAdded), linesRemoved: num(stats.linesRemoved) };
}

// `parent_tool_use_id` is the id of the fan-out tool call a message was
// forwarded from, and null on the lead's own conversation. "" means the lead.
function parentOf(m: Record<string, unknown>): string {
  return str(m.parent_tool_use_id);
}

/** One live agent, as its `task_started` declared it. */
interface AgentRecord {
  /** The description its parent gave it — the only human name it has. */
  label: string;
  /** Wall-clock start, so a duration is real when the runtime reports none. */
  startedAt: number;
  /** Lines its own Write/Edit calls accounted for, when nothing better exists. */
  countedAdded: number;
  countedRemoved: number;
  /**
   * The runtime's own line deltas, if its fan-out `tool_result` carried
   * `toolStats`. Measured ordering: that result arrives one message AFTER the
   * `task_notification` that settles the agent (167 → 168 on the run ADR-0002
   * measured, 58 → 59 on probe 1), so for a real session this arrives too late
   * to reach the row and the counted figures are what get reported. It is read
   * anyway, and preferred when present, because the counted figures cannot see
   * an edit made through a shell heredoc — and because a future SDK that
   * reorders the two would then get the better number for free rather than
   * silently keeping the worse one.
   */
  reportedLines?: { linesAdded: number; linesRemoved: number };
  /** Set once its settle has been emitted, so nothing settles an agent twice. */
  settled: boolean;
}

/** One backgrounded shell command, which outlives the tool call that started it. */
interface BashTaskRecord {
  ownerAgentId: string;
  /**
   * The command, held so the SETTLE can name it too.
   *
   * The runtime's notification carries a status and an id and nothing else, so
   * without this the only name a finished background command has is its
   * `taskId` — and `background bql1cn6sh · failed` cannot be matched by eye to
   * any command on the feed. Live (2026-09-08) that was every one of 47
   * background settles. The contract states the rule at `summary`: a
   * `task_settled` carries the SAME summary its `task_started` did.
   */
  summary: string;
  /** The call that launched it, where the runtime named one. Carried onto both
   *  task events so a surface can draw ONE row for the command instead of an
   *  action row plus an unattached settle. */
  toolUseId?: string;
}

/** One tool call in flight. */
interface PendingCall {
  tool: string;
  /** For a Bash call, the command itself — what a backgrounded task reports. */
  command: string;
  /** Who made the call, so its outcome lands on the same row its action did. */
  agentId: string;
  startedAt: number;
  /** Lines this call would account for if it succeeds — see `lineDelta`. */
  added: number;
  removed: number;
}

/** A `TaskCreate` waiting for the id the runtime mints on its result. */
interface PendingPlanItem {
  title: string;
  agentId: string;
  ownerAgentId?: string;
}

export interface ClaudeAdapterOptions {
  /** Injected clock, so duration measurement is testable without sleeping. */
  now?: () => number;
  /**
   * What this run was dispatched to do, for `run_started`. The runner knows it
   * and the runtime does not, which is why it is passed in rather than read off
   * a message.
   */
  taskKind?: NonNullable<RunEvent["taskKind"]>;
  /** Heartbeat budget per agent; the default is the design's 10s. */
  heartbeatIntervalMs?: number;
}

export interface ClaudeAdapter {
  /** One SDK message → zero, one, or many run events, in order. */
  translate(message: unknown): RunEventInput[];
  /**
   * A spawned agent's transcript, as the `SubagentStop` hook reports it.
   *
   * Recorded and NOT emitted. A transcript is developer material — it holds
   * prompt text, and it is far larger than a feed line — and the design's
   * storage decision is explicit that transcripts stay on the local plane and
   * are never uploaded from a pod. What this buys is that the paths are known
   * in one place when `artifacts()` is built for the runtime port, instead of
   * being rediscovered by walking a session directory.
   */
  noteTranscript(agentId: string, path: string): void;
  /**
   * The transcripts this run produced — the translation half of the runtime
   * port's `artifacts()`, which adds the runtime's own developer files.
   */
  artifacts(): RuntimeArtifact[];
  /**
   * The newest `result`'s usage — cumulative, so the run's total so far
   * (`RuntimeSession.usage`).
   */
  usage(): RunEventUsage | undefined;
}

/**
 * Build the adapter for ONE run.
 *
 * Not reusable across runs: it accumulates that run's agent registry and
 * in-flight calls, and mixing two runs' state would mislabel lines rather than
 * merely lose detail.
 */
export function createClaudeAdapter(opts?: ClaudeAdapterOptions): ClaudeAdapter {
  const now = opts?.now ?? Date.now;
  const heartbeat = createHeartbeatLimiter({
    now,
    ...(opts?.heartbeatIntervalMs !== undefined ? { intervalMs: opts.heartbeatIntervalMs } : {}),
  });

  // --- the agent registry: the tree, declared -------------------------------
  //
  // Keyed by the runtime's `task_id`, which is the agent's identity for its
  // whole life and the id every one of its events carries.
  const agents = new Map<string, AgentRecord>();
  // The two joins that make the tree reconstructible. See the module header.
  const issuedBy = new Map<string, string>();
  const spawnedAgent = new Map<string, string>();
  // Fan-out calls, so their launch ack and their final result both stay off the
  // feed: the agent's own `agent_started` is its row and the notification is its
  // settle. Held apart from `pending`, which evicts under pressure.
  const fanOutCalls = new Map<string, { label: string; model: string }>();
  // Backgrounded shell commands, by the runtime's task id.
  const bashTasks = new Map<string, BashTaskRecord>();

  const pending = new Map<string, PendingCall>();
  const pendingPlanItems = new Map<string, PendingPlanItem>();
  const transcripts: RuntimeArtifact[] = [];
  let lastUsage: RunEventUsage | undefined;

  // Only the run's FIRST init opens the run. A notification-woken turn
  // announces another one (probe 2, message 28) and each spawned agent boots
  // its own session; a second `run_started` would restart the run for every
  // consumer that keys off it.
  let sawInit = false;

  // Where this run's project sits, so a row can say `specs/design/security.json`
  // instead of ~95 characters of mount point, environment, handle and UUID.
  //
  // Read off the runtime's `init` message rather than taken as an option,
  // because that message is the runtime DECLARING the cwd it is working in —
  // the same value the runner passed it as `policy.workspace`, echoed back by
  // the only layer that can confirm it took effect. An option would be a second
  // copy of one fact, and this adapter's whole rule is that the session's own
  // declaration wins over anything inferred beside it. Empty until the init
  // arrives, which leaves paths absolute rather than mis-relativised.
  let workspaceRoot = "";

  /**
   * Which agent a forwarded message belongs to.
   *
   * `parent_tool_use_id` names a tool CALL, not an agent, so it is resolved
   * through the two joins. `spawnedAgent` answers first: the call started an
   * agent, and the message came from inside it. `issuedBy` answers second: the
   * call started no agent, so the message is the runtime narrating that call to
   * whoever MADE it — which is the lead for a `TaskOutput` the lead is blocked
   * in, and a spawned agent for one of its own.
   *
   * **A tool_use_id is never an agent id.** This used to end `?? parent`, on the
   * reasoning that a parent with no `task_started` behind it still could not be
   * the lead. It can: the runtime stamps `parent_tool_use_id` on the
   * `tool_progress` frames of ANY call in flight, and a blocking `TaskOutput` is
   * a long one. Live (2026-09-08) that minted a fresh agent per wait — a run
   * with 3 agents reported 10, six of them named `toolu_…`, 84 events between
   * them — and because a minted id has no `agent_started` and can never settle,
   * the lead stayed classified `waiting` on it and the stall alarm went quiet.
   * The lead is therefore the last resort, which is the codebase's own
   * convention (`settleFanOutResult` writes `agentId ?? LEAD_AGENT_ID`).
   */
  function authorOf(m: Record<string, unknown>): string {
    const parent = parentOf(m);
    if (!parent) return LEAD_AGENT_ID;
    return spawnedAgent.get(parent) ?? issuedBy.get(parent) ?? LEAD_AGENT_ID;
  }

  function trackCall(id: string, call: Omit<PendingCall, "startedAt">): void {
    if (!id) return;
    if (pending.size >= MAX_PENDING_CALLS) {
      const oldest = pending.keys().next();
      if (!oldest.done) pending.delete(oldest.value);
    }
    pending.set(id, { ...call, startedAt: now() });
  }

  /**
   * How many lines a Write or an Edit accounts for.
   *
   * Counted rather than taken from the runtime because the runtime only reports
   * line deltas on a fan-out call's `tool_result`, which arrives after the
   * agent has already settled (see `AgentRecord.reportedLines`). Counting is
   * possible at all only because SDK 0.3.247 forwards a spawned agent's tool
   * calls with their inputs — on 0.3.220 it did not, which is why ADR-0002
   * recorded "+N/−N lines cannot be derived from this feed".
   *
   * `old_string`/`new_string` are the exact text an Edit replaces, so the two
   * counts are a real delta rather than a whole-file diff.
   */
  function lineDelta(tool: string, input: Record<string, unknown>): LineDelta {
    if (!AUTHORING_TOOLS.has(tool)) return { added: 0, removed: 0 };
    if (tool === "Write") return writeDelta(str(input.content));
    return editDelta(str(input.old_string), str(input.new_string));
  }

  // --- system messages ------------------------------------------------------

  function onInit(m: Record<string, unknown>): RunEventInput[] {
    // Before the `sawInit` gate: a spawned agent's own init carries the same
    // cwd, so learning it from whichever arrives first costs nothing and a run
    // whose first init was missed still shortens its paths. Trailing slashes
    // are stripped so the boundary test below is a single form.
    if (!workspaceRoot) workspaceRoot = str(m.cwd).replace(/\/+$/, "");
    if (sawInit) return [];
    sawInit = true;
    return [{
      kind: "run_started",
      agentId: LEAD_AGENT_ID,
      runtime: "claude-code",
      ...(str(m.model) ? { model: str(m.model) } : {}),
      ...(opts?.taskKind ? { taskKind: opts.taskKind } : {}),
    }];
  }

  function onTaskStarted(m: Record<string, unknown>): RunEventInput[] {
    const taskId = str(m.task_id);
    if (!taskId) return [];
    const toolUseId = str(m.tool_use_id);

    if (str(m.task_type) !== TASK_TYPE_AGENT) {
      // A command some agent put in the background. It outlives the tool call
      // that started it, which is why it needs its own `taskId`: the call
      // settles at once with "running in background", and the command's real
      // ending arrives as this task's own notification, possibly `stopped` at
      // session end — which is what names an orphan.
      //
      // The owner is resolved from `tool_use_id` where the runtime gives one:
      // that names the exact `Bash` call, and `issuedBy` names the agent that
      // made it. ADR-0002 recorded this channel as carrying no owner, and for
      // a `local_bash` task on SDK 0.3.247 it does (probe 2 message 21 —
      // `tool_use_id` plus `owned_by_subagent: true`). The fallback below is
      // the design's stated heuristic for a runtime that does not: the
      // in-flight Bash call whose command matches the description. Anything
      // else is the lead's, which is the common case and the safe default —
      // a guess between two spawned agents would be worse than none.
      const owner =
        (toolUseId ? issuedBy.get(toolUseId) : undefined) ??
        matchingBashCall(str(m.description))?.agentId ??
        LEAD_AGENT_ID;
      // `summary`, not `command`: the contract reserves `command` for the
      // `tool_use`/`tool_result`/`git_*` kinds and names `summary` as the one
      // line describing a task at both its ends. The tool call's own row
      // already carried the command; this is what will still be true when the
      // task settles minutes later.
      const summary = (toolUseId ? pending.get(toolUseId)?.command : "") || trimSummary(str(m.description));
      bashTasks.set(taskId, { ownerAgentId: owner, summary, ...(toolUseId ? { toolUseId } : {}) });
      return [{
        kind: "task_started",
        agentId: owner,
        taskId,
        ...(summary ? { summary } : {}),
        ...(toolUseId ? { toolUseId } : {}),
      }];
    }

    // A spawned agent's birth certificate — see the module header.
    const spawn = toolUseId ? fanOutCalls.get(toolUseId) : undefined;
    const parentAgentId = toolUseId ? issuedBy.get(toolUseId) : undefined;
    const depth = num(m.spawn_depth);
    agents.set(taskId, {
      label: trimSummary(str(m.description)),
      startedAt: now(),
      countedAdded: 0,
      countedRemoved: 0,
      settled: false,
    });
    if (toolUseId) spawnedAgent.set(toolUseId, taskId);
    return [{
      kind: "agent_started",
      agentId: taskId,
      // Absent means the lead, by the contract: a depth-1 agent's parent is the
      // lead by definition, and repeating it would make absence ambiguous.
      ...(parentAgentId && parentAgentId !== LEAD_AGENT_ID ? { parentAgentId } : {}),
      ...(str(m.description) ? { label: trimSummary(str(m.description)) } : {}),
      ...(str(m.subagent_type) ? { role: str(m.subagent_type) } : {}),
      ...(depth ? { depth } : {}),
      // Absence is NOT false here: it means the runtime did not say, and an
      // explicit `false` is a positive confirmation that a spawn ran inside its
      // parent's turn.
      ...(typeof m.is_backgrounded === "boolean" ? { background: m.is_backgrounded } : {}),
      // Only when the spawning call named one. Absence means "the run's model",
      // which is what `run_started` already said.
      ...(spawn?.model ? { model: spawn.model } : {}),
    }];
  }

  /**
   * The in-flight `Bash` call a backgrounded task's description points at.
   *
   * Only reached when the runtime gave no `tool_use_id` on the task. It matches
   * on the description the call was made with, which is the only thing the two
   * messages share — hence a match, not a certainty.
   */
  function matchingBashCall(description: string): PendingCall | undefined {
    if (!description) return undefined;
    for (const call of pending.values()) {
      if (call.tool === "Bash" && call.command === trimSummary(description)) return call;
    }
    return undefined;
  }

  function onTaskProgress(m: Record<string, unknown>): RunEventInput[] {
    const agentId = str(m.task_id);
    const agent = agents.get(agentId);
    if (!agent) return [];
    const usage = obj(m.usage);
    return [{
      kind: "agent_progress",
      agentId,
      ...(str(m.description) ? { phrase: trimSummary(str(m.description)) } : {}),
      ...(num(usage.tool_uses) ? { toolCount: num(usage.tool_uses) } : {}),
      ...(num(usage.total_tokens) ? { tokens: num(usage.total_tokens) } : {}),
    }];
  }

  function onTaskNotification(m: Record<string, unknown>): RunEventInput[] {
    const taskId = str(m.task_id);
    const status = agentStatus(m.status);

    const bash = bashTasks.get(taskId);
    if (bash) {
      bashTasks.delete(taskId);
      // No `outputBytes`: the notification names an `output_file` and never its
      // size, and stat-ing a path off a message would be the adapter reaching
      // into the filesystem to invent a figure the runtime declined to report.
      // The summary comes from the START — see BashTaskRecord.summary. It is the
      // adapter's to repeat because the runtime does not repeat it, and a
      // consumer joining the feed late has no start to fold onto.
      return [{
        kind: "task_settled",
        agentId: bash.ownerAgentId,
        taskId,
        ...(bash.summary ? { summary: bash.summary } : {}),
        // The launching call, so a surface can fold this onto the action row it
        // already drew rather than repeating the whole command line beside it.
        // Held from the start for the same reason `summary` is: the runtime's
        // notification carries an id and a status and nothing else.
        ...(bash.toolUseId ? { toolUseId: bash.toolUseId } : {}),
        ...(status ? { status } : {}),
      }];
    }

    const agent = agents.get(taskId);
    if (!agent || agent.settled) return [];
    agent.settled = true;
    const usage = obj(m.usage);
    const duration = num(usage.duration_ms) || Math.max(0, now() - agent.startedAt);
    const lines = agent.reportedLines ?? { linesAdded: agent.countedAdded, linesRemoved: agent.countedRemoved };
    const report = reportFrom(str(m.summary));
    return [{
      kind: "agent_settled",
      agentId: taskId,
      ...(status ? { status } : {}),
      // Treat this as the only copy: a spawned agent's transcript never reaches
      // this feed and dies with the pod, so an agent that settles without a
      // report leaves a reader nothing but counters.
      ...(report ? { report } : {}),
      ...(duration ? { durationMs: duration } : {}),
      ...(num(usage.tool_uses) ? { toolCount: num(usage.tool_uses) } : {}),
      ...(num(usage.total_tokens) ? { tokens: num(usage.total_tokens) } : {}),
      ...(lines.linesAdded ? { linesAdded: lines.linesAdded } : {}),
      ...(lines.linesRemoved ? { linesRemoved: lines.linesRemoved } : {}),
    }];
  }

  /** The runtime's own verdict word, accepted only where it is one of ours. */
  function agentStatus(v: unknown): AgentStatus | undefined {
    const s = str(v);
    return s === "running" || s === "completed" || s === "failed" || s === "stopped" ? s : undefined;
  }

  // --- assistant messages ---------------------------------------------------

  function onAssistant(m: Record<string, unknown>): RunEventInput[] {
    const agentId = authorOf(m);
    const events: RunEventInput[] = [];
    for (const tu of assistantToolUseBlocks(m)) {
      // Recorded BEFORE anything is emitted: this is the map that answers
      // "whose child is this" when the spawned agent's `task_started` arrives,
      // and for a depth-2 spawn that is the very next message.
      if (tu.id) issuedBy.set(tu.id, agentId);

      if (FANOUT_TOOLS.has(tu.name) && tu.id) {
        fanOutCalls.set(tu.id, { label: trimSummary(str(tu.input.description)), model: str(tu.input.model) });
        // No row of its own. The agent's `agent_started` IS the row — printing
        // the call too says the same sentence twice, once as a 200-character
        // dump of the prompt.
        continue;
      }

      if (tu.name === PLAN_CREATE_TOOL) {
        // The item's id is minted by the runtime and arrives on the RESULT, so
        // the two have to be paired before anything can be emitted: an item
        // with no id is a row a consumer cannot fold on.
        if (tu.id) {
          pendingPlanItems.set(tu.id, {
            title: trimSummary(str(tu.input.subject)),
            agentId,
            ...(planOwner(str(tu.input.owner), agentId) ? { ownerAgentId: planOwner(str(tu.input.owner), agentId) } : {}),
          });
        }
        continue;
      }

      if (tu.name === PLAN_UPDATE_TOOL) {
        const itemId = str(tu.input.taskId);
        if (!itemId) continue;
        const owner = planOwner(str(tu.input.owner), agentId);
        events.push({
          kind: "work_item",
          agentId,
          source: "plan",
          itemId,
          ...(str(tu.input.subject) ? { title: trimSummary(str(tu.input.subject)) } : {}),
          ...(planStatus(tu.input.status) ? { itemStatus: planStatus(tu.input.status) } : {}),
          ...(owner ? { ownerAgentId: owner } : {}),
        });
        continue;
      }

      // Reads of the plan change nothing a reader needs to see.
      if (PLAN_READ_TOOLS.has(tu.name)) continue;

      const delta = lineDelta(tu.name, tu.input);
      // NOT capped here: `shellEvents` collapses the workspace prefix, and a cap
      // applied first would have already eaten the tail it is trying to save —
      // the same mistake as truncating a path from the wrong end. Each branch
      // caps its own fields once it has composed them.
      const command = tu.name === "Bash" ? str(tu.input.command) : "";
      trackCall(tu.id, { tool: tu.name, command, agentId, added: delta.added, removed: delta.removed });

      const stamp = { agentId, ...(tu.id ? { toolUseId: tu.id } : {}) };
      if (tu.name === "Bash") {
        if (!command) {
          events.push({ kind: "tool_use", tool: "Bash", summary: "", ...stamp });
        } else {
          events.push(...shellEvents(command, workspaceRoot, "Bash").map((e) => ({ ...e, ...stamp })));
        }
        continue;
      }
      events.push({ kind: "tool_use", tool: tu.name, summary: summaryFromInput(tu.input, SUMMARY_FIELDS, workspaceRoot), ...stamp });
    }
    return events;
  }

  /**
   * Which agent owns a plan entry, or undefined when the lead does.
   *
   * The runtime's `owner` field is a free-form string, not an agent id, so it
   * is honoured only when it actually names an agent this run declared —
   * otherwise the owner is whoever made the call. Passing the raw string
   * through would put a value in an `agentId`-shaped field that resolves to no
   * agent, which is worse than the contract's own default of "absent = lead".
   */
  function planOwner(owner: string, issuer: string): string | undefined {
    if (owner && (agents.has(owner) || owner === LEAD_AGENT_ID)) {
      return owner === LEAD_AGENT_ID ? undefined : owner;
    }
    return issuer === LEAD_AGENT_ID ? undefined : issuer;
  }

  /** A plan entry's status, in the runtime's own task-list vocabulary. */
  // --- user messages (tool results) ----------------------------------------

  function onUser(m: Record<string, unknown>): RunEventInput[] {
    const author = authorOf(m);
    const events: RunEventInput[] = [];
    for (const tr of userToolResultBlocks(m)) {
      const call = pending.get(tr.toolUseId);
      pending.delete(tr.toolUseId);

      if (fanOutCalls.has(tr.toolUseId)) {
        events.push(...settleFanOutResult(tr, m));
        continue;
      }

      const plan = pendingPlanItems.get(tr.toolUseId);
      if (plan) {
        pendingPlanItems.delete(tr.toolUseId);
        events.push(...createdPlanItem(plan, m));
        continue;
      }

      const agentId = call?.agentId ?? author;
      // A successful authoring call is what the agent's line counters are made
      // of; a failed one wrote nothing.
      if (call && !tr.isError && (call.added || call.removed)) {
        const agent = agents.get(agentId);
        if (agent) {
          agent.countedAdded += call.added;
          agent.countedRemoved += call.removed;
        }
      }
      // The severed case is the one the feed actively lied about, so it is the
      // one overridden here. A deliberate background launch is left as it was:
      // nothing failed, and nobody has been misled about it.
      const severed = severedAfterMs(m.tool_use_result);
      events.push({
        kind: "tool_result",
        agentId,
        toolUseId: tr.toolUseId,
        ok: severed ? false : !tr.isError,
        ...(call ? { tool: call.tool, durationMs: Math.max(0, now() - call.startedAt) } : {}),
        ...(severed
          ? {
              // v1 named this `status: "severed"` so a renderer could say
              // "severed" rather than "failed". v2's `status` is the closed
              // AgentStatus set and belongs to a settle, so the distinction
              // lives in this sentence instead — which is the half a reader
              // needed anyway.
              summary:
                `the command hit its ${(severed / 1000).toFixed(1)}s timeout and was detached — ` +
                `it kept running in the background, so this call proved nothing`,
            }
          : // Only failures carry their text: successful output is bulky,
            // uninteresting, and the likelier place for a secret.
            tr.isError
            ? failureDetail(toolOutputText(tr.content))
            : {}),
      });
    }
    return events;
  }

  /**
   * A fan-out call's own result, which puts NOTHING on the feed.
   *
   * Both shapes it can take are a non-event here. A backgrounded spawn answers
   * in ~2ms with `async_launched`, which is the agent starting rather than
   * finishing — reported as an outcome it painted a section that went on to
   * succeed as a 0.0s failure. A foreground spawn answers with the agent's
   * closing report, one message AFTER the `task_notification` that has already
   * settled it. Either way the settle is the notification, which is the one
   * signal both shapes produce.
   *
   * Two things are still read off it. `toolStats` is stashed for the settle (see
   * `AgentRecord.reportedLines`), and a FAILED spawn's error text becomes a
   * notice — because that text is the last copy of the reason. Measured live:
   * without it a 22-minute agent arrived as a bare failure with the cause
   * recorded nowhere, its transcript not on the feed and `runtime.log` dying
   * with the pod (ADR-0002 decision 5).
   */
  function settleFanOutResult(tr: ToolResultBlock, m: Record<string, unknown>): RunEventInput[] {
    const agentId = spawnedAgent.get(tr.toolUseId);
    const lines = toolStatsLines(m.tool_use_result);
    if (agentId && lines) {
      const agent = agents.get(agentId);
      if (agent) agent.reportedLines = lines;
    }
    const status = str(obj(m.tool_use_result).status);
    const failed = tr.isError || (status !== "" && status !== "completed" && status !== "async_launched");
    if (!failed) return [];
    const said = failureText(toolOutputText(tr.content));
    const label = fanOutCalls.get(tr.toolUseId)?.label || "agent";
    return [{
      kind: "notice",
      agentId: agentId ?? LEAD_AGENT_ID,
      level: "error",
      detail: cap(
        `[fan-out] ${label} failed: ${said || `no error text on the tool result${status ? ` (status ${status})` : ""}`}`,
        MAX_REPORT,
      ),
    }];
  }

  /** The `TaskCreate` half that was waiting for its id. */
  function createdPlanItem(plan: PendingPlanItem, m: Record<string, unknown>): RunEventInput[] {
    const itemId = str(obj(obj(m.tool_use_result).task).id);
    // No id, no row: an item a consumer cannot fold on would print once and
    // then be repainted by nothing.
    if (!itemId) return [];
    return [{
      kind: "work_item",
      agentId: plan.agentId,
      source: "plan",
      itemId,
      ...(plan.title ? { title: plan.title } : {}),
      // A freshly created entry is pending in the runtime's own vocabulary.
      itemStatus: "pending",
      ...(plan.ownerAgentId ? { ownerAgentId: plan.ownerAgentId } : {}),
    }];
  }

  // --- the run's turns ------------------------------------------------------

  function onResult(m: Record<string, unknown>): RunEventInput[] {
    const subtype = str(m.subtype);
    const usage = usageFromResult(m);
    if (usage) lastUsage = usage;
    if (subtype === "success") {
      return [{ kind: "turn_ended", agentId: LEAD_AGENT_ID, outcome: "success", ...(usage ? { usage } : {}) }];
    }
    const errors = Array.isArray(m.errors) ? (m.errors as string[]).join(", ") : "";
    return [{
      kind: "turn_ended",
      agentId: LEAD_AGENT_ID,
      outcome: "failure",
      error: cap(errors || subtype, MAX_REPORT),
      ...(usage ? { usage } : {}),
    }];
  }

  // --- liveness -------------------------------------------------------------

  function onToolProgress(m: Record<string, unknown>): RunEventInput[] {
    const agentId = authorOf(m);
    const ref = str(m.tool_use_id);
    return heartbeat(agentId, {
      kind: "heartbeat",
      agentId,
      waitingOn: "tool",
      ...(ref ? { ref } : {}),
      ...(num(m.elapsed_time_seconds) ? { elapsedMs: Math.round(num(m.elapsed_time_seconds) * 1000) } : {}),
    });
  }

  function onModelWait(m: Record<string, unknown>): RunEventInput[] {
    const agentId = authorOf(m);
    // No `ref`: the wait is the emitting agent's own turn, and `agentId`
    // already names it.
    return heartbeat(agentId, { kind: "heartbeat", agentId, waitingOn: "model" });
  }

  function translate(message: unknown): RunEventInput[] {
    if (!message || typeof message !== "object") return [];
    const m = message as Record<string, unknown>;

    switch (m.type) {
      case "system":
        switch (str(m.subtype)) {
          case "init":
            return onInit(m);
          case "task_started":
            return onTaskStarted(m);
          case "task_progress":
            return onTaskProgress(m);
          case "task_notification":
            return onTaskNotification(m);
          case "thinking_tokens":
            return onModelWait(m);
          // Registry only. Its `patch.status` is how the run loop closes a
          // task it may have to stop, and the settle is the notification —
          // emitting both would settle every task twice.
          case "task_updated":
            return [];
          default:
            // `api_retry`, `compact_boundary`, the refusals, `permission_denied`
            // and `worker_shutting_down` are notices, and the run loop emits
            // them from its classifier (classify.ts): they explain a SILENCE, and
            // routing them through here would make each one count as the agent
            // making progress, which is the opposite of what they say.
            return [];
        }
      case "assistant":
        return onAssistant(m);
      case "user":
        return onUser(m);
      case "result":
        return onResult(m);
      case "tool_progress":
        return onToolProgress(m);
      // A streaming token frame, present only under the developer options.
      case "stream_event":
        return onModelWait(m);
      default:
        return [];
    }
  }

  return {
    translate,
    noteTranscript(agentId, path) {
      if (!agentId || !path) return;
      transcripts.push({ agentId, path, kind: "transcript" });
    },
    artifacts: () => [...transcripts],
    usage: () => lastUsage,
  };
}
