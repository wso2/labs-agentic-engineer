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

// Translate Claude Agent SDK messages into runner progress events.
// One SDK message can yield zero, one, or many events (an assistant
// message often carries multiple tool_use content blocks). The caller
// emits each returned event in order.
//
// The translator is a factory, not a free function, because two of the things
// the feed has to answer span messages: which subagent a line came from (the
// fan-out call's description arrives on the main agent's message, the work
// arrives on later forwarded ones) and how long a call took (its start and its
// outcome are two different messages). Building that state per run keeps it out
// of module scope, so tests and concurrent runs cannot see each other's.

import type { ProgressAttribution, ProgressEventInput, TurnModelUsage, TurnUsage } from "./schema.js";

const MAX_SUMMARY = 200;

// The SDK's fan-out tool has gone by both names across versions — `Agent` is
// what the current one emits, `Task` is what the allow-list in runner.ts and
// the `aep` skill still call it. Recognising both means a rename does not
// silently drop every subagent label back to anonymous.
const FANOUT_TOOLS = new Set(["Task", "Agent"]);

// A fanned-out subagent, as opposed to a backgrounded shell command. The SDK
// reports both through the same task_* messages and tells them apart only by
// this field: `local_agent` is another agent doing work on our behalf and its
// lines belong to IT; `local_bash` is the main agent's own long-running command
// and its lines belong to the main agent.
const TASK_TYPE_AGENT = "local_agent";

// The status a fan-out call returns when it was launched with
// `run_in_background` — an acknowledgement that the subagent STARTED, arriving
// ~2ms in. The runner forces fan-out to the foreground (see
// fanout_foreground.ts) so this should not occur, but a cluster and a console
// can be on different images and the feed must not report a launch as an
// outcome if it does.
const ASYNC_LAUNCHED = "async_launched";

// A run can outlive far more tool calls than it ever gets results for
// (interrupted calls, a stream that ends mid-flight), so the pending map is
// capped and evicted oldest-first. Losing the oldest entry costs a duration and
// a tool name on a line whose call is long past; leaking is unbounded memory in
// a pod. Map preserves insertion order, which is what makes this one line.
const MAX_PENDING_CALLS = 2000;

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

// usageFromResult reads the SDK result message's token usage (#291). Tokens
// come from `usage` (snake_case SDK fields); the per-model split comes from
// `modelUsage`, a record keyed by model id whose entries carry camelCase token
// counts and (on current SDKs) the `canonicalModel` alias. Versioned release
// ids collapse onto their canonical model — two dated releases of one model
// are still a single-model run. `model` names the run's one model, or "" when
// several genuinely mixed (matching contracts.TokenUsage.Add); `models` keeps
// the split either way, because aep-api prices each slice against its own rate
// row — a real coding run regularly touches a second model (the SDK's small-
// model helpers), and collapsing to "" made every such run unpriceable.
// Returns undefined when the result carried no usage at all (older SDKs /
// nothing to stamp).
function usageFromResult(m: Record<string, unknown>): TurnUsage | undefined {
  const u = m.usage;
  if (!u || typeof u !== "object") return undefined;
  const usage = u as Record<string, unknown>;

  const byModel = new Map<string, TurnModelUsage>();
  const mu = m.modelUsage;
  if (mu && typeof mu === "object") {
    for (const [key, val] of Object.entries(mu as Record<string, unknown>)) {
      const entry = val && typeof val === "object" ? (val as Record<string, unknown>) : {};
      const canonical = typeof entry.canonicalModel === "string" && entry.canonicalModel !== "" ? entry.canonicalModel : key;
      const slice = byModel.get(canonical) ?? {
        model: canonical, inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0,
      };
      slice.inputTokens += num(entry.inputTokens);
      slice.outputTokens += num(entry.outputTokens);
      slice.cacheReadTokens += num(entry.cacheReadInputTokens);
      slice.cacheCreationTokens += num(entry.cacheCreationInputTokens);
      byModel.set(canonical, slice);
    }
  }
  // The aggregate model considers every entry (a zero-token entry still names
  // the run's model); the slices carry only entries that spent something —
  // aep-api's pricing has nothing to do with an all-zero slice.
  const model = byModel.size === 1 ? [...byModel.keys()][0] : "";
  const models = [...byModel.values()].filter(
    (s) => s.inputTokens + s.outputTokens + s.cacheReadTokens + s.cacheCreationTokens > 0,
  );

  return {
    inputTokens: num(usage.input_tokens),
    outputTokens: num(usage.output_tokens),
    cacheReadTokens: num(usage.cache_read_input_tokens),
    cacheCreationTokens: num(usage.cache_creation_input_tokens),
    model,
    ...(models.length > 0 ? { models } : {}),
  };
}

function trimSummary(s: string): string {
  const collapsed = s.replace(/\s+/g, " ").trim();
  if (collapsed.length <= MAX_SUMMARY) return collapsed;
  return collapsed.slice(0, MAX_SUMMARY - 1) + "…";
}

function bashEvents(command: string): ProgressEventInput[] {
  const cmd = command.trim();
  // git commit -m "..." or -F file
  if (/^git\s+commit\b/.test(cmd)) {
    const msgMatch = cmd.match(/-m\s+(['"])(.+?)\1/);
    return [{
      kind: "git_commit",
      summary: msgMatch ? trimSummary(msgMatch[2]) : trimSummary(cmd),
    }];
  }
  // git push origin <branch> / git push -u origin <branch>
  if (/^git\s+push\b/.test(cmd)) {
    const tokens = cmd.split(/\s+/);
    const branch = tokens[tokens.length - 1];
    return [{
      kind: "git_push",
      branch: branch && branch !== "push" ? branch : undefined,
      summary: trimSummary(cmd),
    }];
  }
  // gh anything
  if (/^gh\s+/.test(cmd)) {
    return [{
      kind: "gh_action",
      command: trimSummary(cmd),
    }];
  }
  return [{
    kind: "tool_use",
    tool: "Bash",
    summary: trimSummary(cmd),
  }];
}

function summaryFromInput(input: unknown): string {
  if (input && typeof input === "object") {
    const obj = input as Record<string, unknown>;
    // Most file tools carry file_path / pattern / path; `skill` is the Skill
    // tool's, and `description` is the last human-readable field a tool is
    // likely to have. Reaching any of them beats the JSON dump below, which
    // rendered `$ Skill {"skill":"aep:aep"}` in a live run.
    const candidate = obj.file_path ?? obj.path ?? obj.pattern ?? obj.glob ?? obj.url ?? obj.skill ?? obj.description;
    if (typeof candidate === "string") return trimSummary(candidate);
    // Fall back to a compact JSON dump (truncated). Helps surface unknown tools.
    try {
      return trimSummary(JSON.stringify(obj));
    } catch {
      return "";
    }
  }
  return "";
}

interface ToolUseBlock {
  id: string;
  name: string;
  input: unknown;
}

// Best-effort pluck of tool_use content blocks from an assistant SDK message.
// The SDK exposes message.message.content[] where each block has a `type`.
function assistantToolUseBlocks(message: unknown): ToolUseBlock[] {
  if (!message || typeof message !== "object") return [];
  const m = message as Record<string, unknown>;
  const inner = m.message;
  if (!inner || typeof inner !== "object") return [];
  const content = (inner as Record<string, unknown>).content;
  if (!Array.isArray(content)) return [];
  const out: ToolUseBlock[] = [];
  for (const block of content) {
    if (!block || typeof block !== "object") continue;
    const b = block as Record<string, unknown>;
    if (b.type !== "tool_use") continue;
    const name = typeof b.name === "string" ? b.name : "";
    if (!name) continue;
    out.push({ id: typeof b.id === "string" ? b.id : "", name, input: b.input });
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
function userToolResultBlocks(message: unknown): ToolResultBlock[] {
  if (!message || typeof message !== "object") return [];
  const m = message as Record<string, unknown>;
  const inner = m.message;
  if (!inner || typeof inner !== "object") return [];
  const content = (inner as Record<string, unknown>).content;
  if (!Array.isArray(content)) return [];
  const out: ToolResultBlock[] = [];
  for (const block of content) {
    if (!block || typeof block !== "object") continue;
    const b = block as Record<string, unknown>;
    if (b.type !== "tool_result") continue;
    const id = typeof b.tool_use_id === "string" ? b.tool_use_id : "";
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

// The SDK's own first line on a failed shell call. Parsing it is not a guess at
// a format we do not own: this prefix is what the SDK writes, and it is the
// only place the process status appears.
const EXIT_CODE_LINE = /^Exit code (\d+)\b/;

// A non-shell tool reports its failure wrapped like this, with no exit code.
const TOOL_USE_ERROR_TAG = /<\/?tool_use_error>/g;

// A line that announces the fault, as opposed to the build chatter above it.
// `bal build` prints nine lines of dependency pulls before the first ERROR, so
// "the line after the exit code" would report "Compiling source" — technically
// the first line and useless as a diagnosis. Anchored at the start so a line
// merely CONTAINING the word (a path, a prose sentence) does not win over the
// compiler's own.
const ANNOUNCES_FAULT = /^(?:error|fatal|panic|exception|failed)\b/i;

interface FailureDetail {
  exitCode?: number;
  summary: string;
}

// How much of a failed fan-out's error text reaches the feed.
//
// `failureDetail` below is the right shape for a tool ROW — the command is
// printed above it and the whole output is in claude.log. A failed fan-out has
// neither: the subagent's transcript is not on this feed at all, and claude.log
// is inside a pod that is about to be deleted. Measured on a live run, that
// left a 22-minute subagent arriving as a bare `ok:false` with no reason on
// record anywhere. So the fan-out branch prints what the SDK actually said,
// bounded instead of reduced to one sentence.
const MAX_FAILURE_LINES = 10;
const MAX_FAILURE_CHARS = 2000;

/** The failed call's own words, flattened to one line and bounded. */
function failureText(content: unknown): string {
  const lines = resultText(content)
    .replace(TOOL_USE_ERROR_TAG, "")
    .split("\n")
    .map((l) => (l.split("\r").pop() ?? "").trim())
    .filter((l) => l !== "");
  if (lines.length === 0) return "";
  const kept = lines.slice(0, MAX_FAILURE_LINES);
  const dropped = lines.length - kept.length;
  const text = kept.join(" | ") + (dropped > 0 ? ` (+${dropped} more line${dropped === 1 ? "" : "s"})` : "");
  return text.length <= MAX_FAILURE_CHARS ? text : text.slice(0, MAX_FAILURE_CHARS - 1) + "…";
}

/**
 * The one-line diagnosis of a failed call, plus its exit code when the tool was
 * a shell. Everything else in the output is developer material and belongs in
 * claude.log, not in a progress feed.
 */
function failureDetail(content: unknown): FailureDetail {
  const lines = resultText(content)
    .replace(TOOL_USE_ERROR_TAG, "")
    .split("\n")
    // A progress bar rewrites its own line with \r; only the final state of it
    // is text. Without this, one `bal build` line arrives 40 columns wide with
    // eight stale copies of itself in front.
    .map((l) => (l.split("\r").pop() ?? "").trim())
    .filter((l) => l !== "");
  if (lines.length === 0) return { summary: "" };

  const code = EXIT_CODE_LINE.exec(lines[0]);
  const rest = code ? lines.slice(1) : lines;
  let at = rest.findIndex((l) => ANNOUNCES_FAULT.test(l));
  if (at < 0) at = 0;
  let diagnosis = rest[at] ?? "";
  // A line ending in a colon is introducing the next one, not stating anything
  // ("InputValidationError: Read failed due to the following issue:" — the issue
  // itself is below). Taking one without the other reports a heading.
  if (diagnosis.endsWith(":") && rest[at + 1]) diagnosis = `${diagnosis} ${rest[at + 1]}`;
  return {
    ...(code ? { exitCode: Number(code[1]) } : {}),
    summary: trimSummary(diagnosis),
  };
}

/**
 * The authoritative totals for one fanned-out subagent, off its Agent call's
 * result. Measured: the SDK's `totalDurationMs` of 209158 matched a
 * hand-measured 3m29s exactly, so these figures are reported rather than
 * re-derived. `toolStats` carries the line deltas of the subagent's edits,
 * which nothing in our own feed can reconstruct.
 */
function subagentTotals(result: unknown): {
  status?: string;
  durationMs?: number;
  toolCount?: number;
  linesAdded?: number;
  linesRemoved?: number;
} {
  if (!result || typeof result !== "object") return {};
  const r = result as Record<string, unknown>;
  const stats = r.toolStats && typeof r.toolStats === "object" ? (r.toolStats as Record<string, unknown>) : {};
  const status = str(r.status);
  const duration = num(r.totalDurationMs);
  const tools = num(r.totalToolUseCount);
  const added = num(stats.linesAdded);
  const removed = num(stats.linesRemoved);
  return {
    ...(status ? { status } : {}),
    ...(duration ? { durationMs: duration } : {}),
    ...(tools ? { toolCount: tools } : {}),
    ...(added ? { linesAdded: added } : {}),
    ...(removed ? { linesRemoved: removed } : {}),
  };
}

/**
 * Whether a tool call came back WITHOUT having finished, and why.
 *
 * A command that blows its Bash timeout is auto-backgrounded, and what comes
 * back is `code: 0`, empty output, and `is_error` false — indistinguishable
 * from a command that ran and succeeded. That cost a validation run its whole
 * cycle (#701): the final suite run was severed at the ceiling, the feed
 * recorded it as a success, and the per-criterion rows painted `Passed` on
 * criteria whose tests never completed.
 *
 * `timedOutAfterMs` is the SDK's own flag for exactly that case. A
 * `backgroundTaskId` with no timeout is a deliberate `run_in_background`
 * launch — also not a completion, but nobody was misled about it.
 */
function incompleteCall(result: unknown): { severedAfterMs?: number; backgrounded: boolean } {
  if (!result || typeof result !== "object") return { backgrounded: false };
  const r = result as Record<string, unknown>;
  const severed = num(r.timedOutAfterMs);
  return {
    ...(severed ? { severedAfterMs: severed } : {}),
    backgrounded: str(r.backgroundTaskId) !== "",
  };
}

// parentOf reads the SDK's subagent discriminator: `parent_tool_use_id` is the
// id of the fan-out tool call a message was forwarded from, and null on the
// main conversation. "" means the main agent.
function parentOf(m: Record<string, unknown>): string {
  const parent = m.parent_tool_use_id;
  return typeof parent === "string" ? parent : "";
}

interface PendingCall {
  tool: string;
  startedAt: number;
}

// One entry per fanned-out subagent or backgrounded command, keyed by the id of
// the tool call that spawned it. That id is the ONE identity: it is what the
// SDK puts in `parent_tool_use_id` on forwarded messages and in
// `task_started.tool_use_id`, so both surfaces resolve to the same subagent
// instead of each inventing one.
interface TaskInfo {
  label: string;
  isAgent: boolean;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

export type SdkTranslator = (message: unknown) => ProgressEventInput[];

export interface SdkTranslatorOptions {
  /** Injected clock, so duration measurement is testable without sleeping. */
  now?: () => number;
  /**
   * Called when a plain tool call settles, with the same `ok` this translator
   * puts on the feed. A seam, not a second feature: whether a call succeeded is
   * already decided here from the SDK's own `is_error`, and re-deriving it
   * anywhere else would give two answers to one question.
   *
   * Fan-out results are deliberately excluded — an `Agent`/`Task` call settles a
   * whole subagent, which is a different kind of fact from one command's exit,
   * and nothing consumes it. Today's only consumer is the validation progress
   * tracker, which settles per-spec `npm test` calls.
   */
  onToolOutcome?: (toolUseId: string, ok: boolean) => void;
}

/**
 * Build a translator for ONE run. Not reusable across runs: it accumulates the
 * run's subagent labels and in-flight tool calls, and mixing two runs' state
 * would mislabel lines rather than merely lose detail.
 */
export function createSdkTranslator(opts?: SdkTranslatorOptions): SdkTranslator {
  const now = opts?.now ?? Date.now;
  const onToolOutcome = opts?.onToolOutcome;
  // Spawning tool-call id → that subagent / backgrounded command.
  const subagents = new Map<string, TaskInfo>();
  // SDK task id → the spawning tool-call id above, so a task_* message and a
  // forwarded message about the SAME subagent resolve to one identity.
  const taskCanonical = new Map<string, string>();
  // Nothing is suppressed here any more, and that took two corrections to get
  // right. A tool call's own `tool_result` is ALWAYS its outcome:
  //
  //   - For a fan-out call it is the subagent's closing report, not a launch
  //     acknowledgement. Measured: the Agent call is message 44 and its result
  //     is message 168, carrying totalDurationMs. Suppressing it threw away the
  //     only figures a settled section can report.
  //   - For a BACKGROUNDED command it is the diagnosis. Measured: a failed
  //     `bal build`'s result carries `Exit code 1` and the compiler's own ERROR
  //     lines, and its `parent_tool_use_id` names the subagent that ran it. Its
  //     task_notification carries neither — only the description it was already
  //     launched with. Suppressing the result lost the exit code of the single
  //     most important failure a coding run has.
  //
  // task_started still starts the clock, which is what makes the duration real.
  // Tool call id → what it was and when we saw it go out.
  const pending = new Map<string, PendingCall>();
  // Fan-out calls whose result was a launch acknowledgement rather than an
  // outcome, so the settle has to come from the task_notification instead. Held
  // apart from `pending` because that map evicts its oldest entry under
  // pressure, and the entry that must survive a whole run is exactly the oldest
  // one — an evicted fan-out would settle nowhere and leave its section running
  // for ever. There is one entry per concurrent subagent, so it needs no cap.
  const awaitingNotification = new Map<string, number>();
  // Only the run's FIRST init is the run starting; each fanned-out subagent
  // boots its own session and announces a second one.
  let sawInit = false;

  // Attribution for a subagent, by its canonical (spawning-call) id. A
  // backgrounded command is the main agent's own work and gets none.
  //
  // An id we have no record of is still a subagent: the SDK only sets a parent
  // on messages it forwarded from inside a fan-out call, so the id's existence
  // IS the proof. Only a task we know to be a backgrounded command is excluded.
  // Getting this the other way round would silently un-attribute a whole
  // subagent whenever its spawning call went unseen.
  function attribution(id: string): ProgressAttribution {
    if (!id) return {};
    const info = subagents.get(id);
    if (info && !info.isAgent) return {};
    return { emitter: "subagent", emitterId: id, ...(info?.label ? { emitterLabel: info.label } : {}) };
  }

  function remember(id: string, next: Partial<TaskInfo>): TaskInfo {
    const info = subagents.get(id) ?? { label: "", isAgent: false };
    const merged = { ...info, ...next, label: next.label || info.label };
    subagents.set(id, merged);
    return merged;
  }

  function trackCall(id: string, tool: string): void {
    if (!id) return;
    if (pending.size >= MAX_PENDING_CALLS) {
      const oldest = pending.keys().next();
      if (!oldest.done) pending.delete(oldest.value);
    }
    pending.set(id, { tool, startedAt: now() });
  }

  return function progressFromSdkMessage(message: unknown): ProgressEventInput[] {
    if (!message || typeof message !== "object") return [];
    const m = message as Record<string, unknown>;
    const type = m.type;

    // The SDK reports subagent work on TWO channels at once, and both are
    // handled: `parent_tool_use_id` on forwarded assistant/user messages, and
    // system task_started / task_progress / task_notification keyed by task id.
    // Neither can be dropped — the cluster and the playground can be running
    // different images, and a run whose subagents went unattributed is exactly
    // the failure this surface exists to prevent. They are keyed to ONE identity
    // (the spawning tool call) and carry different things: the forwarded channel
    // has the commands and their outcomes, the task channel has the subagent's
    // own narration and step count.
    if (type === "system") {
      const subtype = str(m.subtype);

      if (subtype === "init") {
        if (sawInit) return [];
        sawInit = true;
        return [{ kind: "phase", phase: "agent_started" }];
      }

      // A task begins: a fanned-out subagent, or a command put in the
      // background. Record it, because every later line about it carries only
      // the id.
      if (subtype === "task_started") {
        const taskId = str(m.task_id);
        if (!taskId) return [];
        const isAgent = str(m.task_type) === TASK_TYPE_AGENT;
        // The spawning call is the identity; the task id is an alias for it.
        const id = str(m.tool_use_id) || taskId;
        taskCanonical.set(taskId, id);
        remember(id, { label: trimSummary(str(m.description)), isAgent });
        // Re-started from here rather than from the spawning call, because a
        // task is dispatched some way into that call's own handling. This is the
        // clock the task's tool_result measures against, and it is the ONLY
        // duration a backgrounded command has when the SDK reports none.
        trackCall(id, isAgent ? "Agent" : "Bash");
        // No line of its own: the tool call that launched it is where this
        // belongs on the feed — the section heading for a subagent, the Bash row
        // for a backgrounded command — and a second line would double every
        // long-running command. What this message adds is the clock.
        return [];
      }

      // One per tool call a subagent makes. Its `description` is the subagent's
      // own phrasing of what it is doing ("Delete main.bal — a service package
      // cannot have a main function") and `usage.tool_uses` is its own count of
      // the steps so far — the two things a collapsed section reports live.
      //
      // Not a line of its own: the subagent's forwarded tool_use carries the
      // actual command, which is what a progress list wants. See ActivityEvent.
      if (subtype === "task_progress") {
        const id = taskCanonical.get(str(m.task_id));
        if (!id || !subagents.get(id)?.isAgent) return [];
        const usage = m.usage && typeof m.usage === "object" ? (m.usage as Record<string, unknown>) : {};
        const steps = num(usage.tool_uses);
        return [{
          kind: "activity",
          summary: trimSummary(str(m.description)),
          ...(steps ? { toolCount: steps } : {}),
          ...attribution(id),
        }];
      }

      // A task settles. For almost everything this is NOT where that gets
      // reported: measured, it arrives one message ahead of the task's own
      // tool_result and carries strictly less — no exit code, no output, no
      // subagent totals, and a `summary` that is only the description the task
      // was launched with. The tool_result carries all of it, and its
      // parent_tool_use_id even names the subagent that owns a backgrounded
      // command. Emitting both would settle every task twice, the first time
      // with the poorer of the two reports.
      //
      // The ONE exception is a fan-out that was launched in the background. Its
      // tool_result already came and went as an acknowledgement, so this message
      // is the only completion signal that will ever arrive — six minutes later,
      // carrying `usage.duration_ms` and `usage.tool_uses`. Dropping it left the
      // section reading "async_launched · 0.0s" for the rest of the run.
      // Verified against a foreground run: a foreground fan-out emits no agent
      // task_notification at all, so this cannot double-settle one.
      if (subtype === "task_notification") {
        const id = taskCanonical.get(str(m.task_id)) || str(m.tool_use_id);
        const startedAt = id ? awaitingNotification.get(id) : undefined;
        if (!id || startedAt === undefined) return [];
        awaitingNotification.delete(id);
        pending.delete(id);
        const info = subagents.get(id);
        const usage = m.usage && typeof m.usage === "object" ? (m.usage as Record<string, unknown>) : {};
        const status = str(m.status) || "completed";
        // The SDK's own figure where it gave one; ours is the fallback, and it
        // is honest here because the clock started at the launch we saw.
        const duration = num(usage.duration_ms) || Math.max(0, now() - startedAt);
        const tools = num(usage.tool_uses);
        return [{
          kind: "tool_result",
          ok: status === "completed",
          toolUseId: id,
          tool: "Agent",
          summary: info?.label ?? trimSummary(str(m.summary)),
          status,
          ...(duration ? { durationMs: duration } : {}),
          ...(tools ? { toolCount: tools } : {}),
          // No line deltas: `toolStats` rides the tool_result, which a
          // backgrounded fan-out never produces. A section settled this way
          // reports its duration and step count and omits "+N/−N lines" rather
          // than claiming zero.
          ...attribution(id),
        }];
      }

      return [];
    }

    if (type === "assistant") {
      const parent = parentOf(m);
      const who = attribution(parent);
      // Current SDK builds narrate a subagent through BOTH surfaces at once —
      // the same `bal tool pull` arrives as a task_progress AND as a forwarded
      // tool_use. The forwarded block is the one that becomes a row: it carries
      // the actual command, and its tool_result is the only per-call outcome and
      // duration a subagent produces. The task_progress copy becomes an
      // `activity`, which is header material and never a row, so the two no
      // longer double-count. Measured: both channels cover the same window —
      // after one catch-up burst of ~9 steps the forwarded gaps fall to 19.9s,
      // 2.6s, 5.3s — so this does not trade liveness for detail.
      const events: ProgressEventInput[] = [];
      for (const tu of assistantToolUseBlocks(m)) {
        const input = tu.input && typeof tu.input === "object" ? (tu.input as Record<string, unknown>) : {};
        // A fan-out call names the subagent every later line belongs to, so
        // record it BEFORE emitting: the label has to be in hand by the time
        // that subagent's first message arrives.
        if (FANOUT_TOOLS.has(tu.name) && tu.id) {
          const description = typeof input.description === "string" ? input.description : "";
          remember(tu.id, { label: trimSummary(description), isAgent: true });
        }
        trackCall(tu.id, tu.name);

        const stamp = { ...who, ...(tu.id ? { toolUseId: tu.id } : {}) };
        if (tu.name === "Bash") {
          const cmd = String(input.command ?? "");
          if (!cmd) {
            events.push({ kind: "tool_use", tool: "Bash", summary: "", ...stamp });
          } else {
            events.push(...bashEvents(cmd).map((e) => ({ ...e, ...stamp })));
          }
          continue;
        }
        // A fan-out call gets no row of its own. Its whole content is the
        // description, and that description is already the heading of the
        // section the subagent's lines land in — printing both says the same
        // sentence twice, once as a 200-character JSON prompt dump.
        if (FANOUT_TOOLS.has(tu.name)) continue;
        events.push({
          kind: "tool_use",
          tool: tu.name,
          summary: summaryFromInput(tu.input),
          ...stamp,
        });
      }
      return events;
    }

    if (type === "user") {
      const who = attribution(parentOf(m));
      const events: ProgressEventInput[] = [];
      for (const tr of userToolResultBlocks(m)) {
        const call = pending.get(tr.toolUseId);
        pending.delete(tr.toolUseId);
        const info = subagents.get(tr.toolUseId);
        // A fan-out call's result settles the WHOLE subagent, and the SDK's own
        // totals for it are better than anything measured out here. Attributed
        // to the subagent itself (not to the main agent that launched it) so the
        // figures land in that subagent's section rather than beside it.
        if (info?.isAgent) {
          const totals = subagentTotals(m.tool_use_result);
          // A BACKGROUNDED fan-out answers in ~2ms with `async_launched`, which
          // is the subagent starting, not finishing. Reported as an outcome it
          // painted a section that went on to succeed as a 0.0s failure — the
          // status is not "completed", which is what `ok` is derived from. So
          // it produces no line, and the section stays live on its narration
          // until the task_notification settles it above.
          if (totals.status === ASYNC_LAUNCHED) {
            awaitingNotification.set(tr.toolUseId, call?.startedAt ?? now());
            continue;
          }
          const ok = !tr.isError && (totals.status ?? "completed") === "completed";
          events.push({
            kind: "tool_result",
            ok,
            toolUseId: tr.toolUseId,
            tool: "Agent",
            // Named even on success: this is the line reporting how long a
            // whole subagent took, and "Agent (3m29s)" without saying which
            // one answers nothing.
            summary: info.label,
            ...(call ? { durationMs: Math.max(0, now() - call.startedAt) } : {}),
            ...totals,
            ...attribution(tr.toolUseId),
          });
          // A second line, not a richer row: `summary` on the row above is
          // contractually the subagent's LABEL (the console renders it as the
          // section heading), so the reason has to arrive beside it. Emitted
          // even when the tool result carried no text, because "the SDK gave no
          // reason" is itself the finding — it is what a reader would otherwise
          // spend the next run trying to establish.
          if (!ok) {
            const said = failureText(tr.content);
            const why = said || `no error text on the tool result${totals.status ? ` (status ${totals.status})` : ""}`;
            events.push({
              kind: "log",
              level: "error",
              summary: `[fan-out] ${info.label || "subagent"} failed: ${why}`,
              ...attribution(tr.toolUseId),
            });
          }
          continue;
        }
        const incomplete = incompleteCall(m.tool_use_result);
        // A call that has not finished settles nothing. Reporting its outcome
        // would be inventing one: the command is still running, and a consumer
        // that folds outcomes into state would record a result no run produced.
        if (!incomplete.severedAfterMs && !incomplete.backgrounded) {
          onToolOutcome?.(tr.toolUseId, !tr.isError);
        }
        // The severed case is the one the feed actively lied about, so it is the
        // one overridden here. A deliberate background launch is left as it was:
        // nothing failed, and nobody has been misled about it.
        const severed = incomplete.severedAfterMs;
        events.push({
          kind: "tool_result",
          ok: severed ? false : !tr.isError,
          toolUseId: tr.toolUseId,
          ...(call ? { tool: call.tool, durationMs: Math.max(0, now() - call.startedAt) } : {}),
          ...(severed
            ? {
                // `status` is the SDK's verdict word and already on the wire, so
                // naming this one costs no schema move. @aep/progress-view reads
                // it to say "severed" where it would otherwise say "failed" —
                // which would name a defect that did not happen.
                status: "severed",
                summary:
                  `the command hit its ${(severed / 1000).toFixed(1)}s timeout and was detached — ` +
                  `it kept running in the background, so this call proved nothing`,
              }
            : // Only failures carry their text — see ToolResultEvent's doc comment.
              tr.isError
              ? failureDetail(tr.content)
              : {}),
          ...who,
        });
      }
      return events;
    }

    if (type === "result") {
      const subtype = String(m.subtype ?? "");
      const usage = usageFromResult(m);
      // A run that ends with fan-outs still in `awaitingNotification` ended while
      // its own subagents were working. That is not a success however the SDK
      // labels it: the session stopped, the pod is about to go away, and whatever
      // those subagents were mid-way through writing is what the run shipped.
      // Observed exactly once and it looked like a fast green run — 159s,
      // `result: success`, one component left as a generated stub and the second
      // never created. `fanout_foreground.ts` is what stops a fan-out detaching in
      // the first place; this is the backstop for when it cannot, which decision 4
      // of ADR-0002 requires precisely because a mismatched image can bypass a
      // hook. Reported as a failure, not a warning, because the artefact is
      // incomplete and a green run is the one thing nobody re-reads.
      if (awaitingNotification.size > 0) {
        const orphans = [...awaitingNotification.keys()]
          .map((id) => subagents.get(id)?.label || "subagent")
          .join(", ");
        return [{
          kind: "result",
          status: "failure",
          error:
            `run ended with ${awaitingNotification.size} detached subagent(s) still running (${orphans}) — ` +
            `their work is unfinished and unattributed. A fan-out was backgrounded despite the foreground hook.`,
          ...(usage ? { usage } : {}),
        }];
      }
      if (subtype === "success") {
        return [{ kind: "result", status: "success", ...(usage ? { usage } : {}) }];
      }
      const errors = Array.isArray(m.errors) ? (m.errors as string[]).join(", ") : "";
      return [{
        kind: "result",
        status: "failure",
        error: errors || subtype,
      }];
    }

    return [];
  };
}
