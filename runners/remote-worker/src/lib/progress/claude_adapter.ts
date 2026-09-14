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

import type { RuntimeArtifact } from "../../runtime/port.js";
import {
  LEAD_AGENT_ID,
  type AgentStatus,
  type RunEvent,
  type RunEventInput,
  type RunEventModelUsage,
  type RunEventUsage,
} from "./emitter.js";

/** `summary`, `phrase`, `label` and `detail` are all capped at 200 in the contract. */
const MAX_SUMMARY = 200;

/** `report` is capped at 500: a last line, not a transcript. */
const MAX_REPORT = 500;

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
 * How often one agent may say "still alive" while it is waiting.
 *
 * Bounded because the point of a heartbeat is that a row's age is honest, not
 * that every tick is on the record: a 55-minute run produces a few hundred of
 * these at this cadence, and one per `tool_progress` message would produce
 * thousands. It is the design's default (§13) and is a per-AGENT budget, so a
 * run with four agents waiting still says so about all four.
 */
const HEARTBEAT_MIN_INTERVAL_MS = 10_000;

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function obj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
}

function cap(s: string, max: number): string {
  const collapsed = s.replace(/\s+/g, " ").trim();
  if (collapsed.length <= max) return collapsed;
  return collapsed.slice(0, max - 1) + "…";
}

function trimSummary(s: string): string {
  return cap(s, MAX_SUMMARY);
}

/**
 * An agent's closing words: the runtime's own LAST line, bounded.
 *
 * The last line rather than the whole summary because that is where a report
 * lives — an agent told to "reply with exactly one line" produces exactly that
 * (`REPORT: alpha done, 1 file`), while one that narrates first puts its
 * conclusion at the end. Bounded because this is a row, not a transcript, and
 * because everything on this feed reaches a user-visible build log.
 */
function reportFrom(summary: string): string {
  const lines = summary.split("\n").map((l) => l.trim()).filter((l) => l !== "");
  return cap(lines[lines.length - 1] ?? "", MAX_REPORT);
}

/** Lines a piece of authored text accounts for. `""` is no lines, not one. */
function lineCount(text: string): number {
  return text === "" ? 0 : text.split("\n").length;
}

/**
 * The token usage of the run so far, off an SDK `result` message: the folded
 * aggregate AND the per-model split.
 *
 * The split is what makes the number usable. Cost is stamped per model, against
 * that model's own rate row, so an aggregate that folded two models — and
 * therefore reports `model: ""` — cannot be priced at all. Mixed-model runs are
 * not exotic: the runtime reaches for small-model helpers of its own, and a lead
 * is expected to pick the model for the job (a walk on the fast one, a build on
 * the default). Emitting only the total is how those runs became unpriceable.
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

/**
 * What one Bash command becomes on the feed.
 *
 * The three rewrites exist because a commit, a push and a `gh` call are the
 * run's EFFECTS — the things a reader scans a feed for — and they read as
 * effects rather than as shell only if the producer says which they are.
 *
 * A command's absolute paths are NOT shortened the way a file tool's argument
 * is (see `relativiseToWorkspace`). The contract calls this field "the command
 * line that ran", and a relative path in it would be a claim that resolves only
 * from the workspace root — which is not where the command ran once an agent has
 * `cd`-ed into a component. Editing the text of a command a reader may re-run
 * is the one place brevity is worth less than truth.
 */
/**
 * A shell command as a READER's line: the workspace root collapsed to `~ws`.
 *
 * `command` keeps the exact text — see `bashEvents` — and this is the other
 * field the contract defines: "one line describing the call, composed by the
 * producer for a reader". A live run (2026-09-08) put 66 of 379 rows through the
 * feed carrying the same ~95-character prefix, and `agent_progress` then
 * truncated them to `Running cat /home/aep/aep-workspace/default/hello-world-s…`
 * — a row that says nothing at all. Collapsing the prefix is what makes the tail
 * survive the width.
 *
 * `~ws` rather than deleting the prefix outright: a bare `hello-webapp && npm
 * run build` would read as a command someone could paste, and it is not one.
 */
function commandForReader(cmd: string, root: string): string {
  if (!root) return cmd;
  return cmd.split(root).join("~ws");
}

/**
 * The second field, and ONLY when it earns its place.
 *
 * `command` and `summary` say different things — one is the line that ran, one
 * is the line a reader scans — but on a command with no workspace path in it
 * they are the same string, and sending both would put a duplicate on every row
 * of the feed. Every renderer reads `summary ?? command`, so one field is
 * enough whenever the collapse changed nothing.
 */
function readerLine(cmd: string, shown: string): { summary?: string } {
  return shown === cmd ? {} : { summary: trimSummary(shown) };
}

/**
 * A shell row's fields. `summary` alone while it says everything; `command`
 * joins it only once the two differ, and then carries the exact line — uncapped,
 * because the contract puts no ceiling on `command` and its whole purpose is to
 * be the text somebody can re-run.
 */
function shellFields(cmd: string, shown: string): { summary: string; command?: string } {
  return shown === cmd
    ? { summary: trimSummary(cmd) }
    : { summary: trimSummary(shown), command: cmd };
}

function bashEvents(command: string, workspaceRoot: string): RunEventInput[] {
  const cmd = command.trim();
  const shown = commandForReader(cmd, workspaceRoot);
  // git commit -m "..." or -F file
  if (/^git\s+commit\b/.test(cmd)) {
    const msgMatch = cmd.match(/-m\s+(['"])(.+?)\1/);
    return [{ kind: "git_commit", summary: msgMatch ? trimSummary(msgMatch[2]) : trimSummary(cmd) }];
  }
  // git push origin <branch> / git push -u origin <branch>
  if (/^git\s+push\b/.test(cmd)) {
    const tokens = cmd.split(/\s+/);
    const branch = tokens[tokens.length - 1];
    return [{
      kind: "git_push",
      ...(branch && branch !== "push" ? { branch } : {}),
      summary: trimSummary(shown),
    }];
  }
  // gh anything
  if (/^gh\s+/.test(cmd)) {
    return [{ kind: "gh_action", command: cmd, ...readerLine(cmd, shown) }];
  }
  return [{ kind: "tool_use", tool: "Bash", ...shellFields(cmd, shown) }];
}

/**
 * A path inside the workspace, said the way a reader of the workspace would say
 * it.
 *
 * The workspace root is ~95 characters of nothing — a mount point, an
 * environment name, a project handle and a UUID — and it is identical on every
 * row of a run. Live (2026-09-08) that pushed the only meaningful part of a
 * `Read` off the right edge and left ten consecutive reads looking like one
 * repeated line: `$ Read /home/aep/aep-workspace/default/employees-submit-…
 * /07f229c4-4e80-4168-9609-e5a4c4ff7f66/specs/design/security.json`.
 *
 * Two cases are deliberately left alone. A path OUTSIDE the workspace stays
 * absolute, because "outside" is the interesting half of that fact — the
 * runtime's own session directory under `$HOME/.claude/projects/…` reads as a
 * detour precisely because it does not start where the project does. And a path
 * under the workspace's own `.claude/` needs nothing special: relativised it
 * becomes `.claude/skills/aep/references/component-contract.md`, which is both
 * short and unambiguous about being tooling rather than the product.
 *
 * Matching is on a path boundary, so a sibling checkout at `<root>-old` is not
 * mistaken for a child of the root.
 */
function relativiseToWorkspace(path: string, root: string): string {
  if (!root || !path.startsWith("/")) return path;
  if (path === root) return ".";
  return path.startsWith(`${root}/`) ? path.slice(root.length + 1) : path;
}

function summaryFromInput(input: unknown, workspaceRoot: string): string {
  if (input && typeof input === "object") {
    const o = input as Record<string, unknown>;
    // Most file tools carry file_path / pattern / path; `skill` is the Skill
    // tool's, and `description` is the last human-readable field a tool is
    // likely to have. Reaching any of them beats the JSON dump below, which
    // rendered `$ Skill {"skill":"aep:aep"}` in a live run.
    const candidate = o.file_path ?? o.path ?? o.pattern ?? o.glob ?? o.url ?? o.skill ?? o.description;
    // Relativised BEFORE the cap, so the cap spends its 200 characters on the
    // part that identifies the file rather than on the mount point.
    if (typeof candidate === "string") return trimSummary(relativiseToWorkspace(candidate, workspaceRoot));
    // Fall back to a compact JSON dump (truncated). Helps surface unknown tools.
    try {
      return trimSummary(JSON.stringify(o));
    } catch {
      return "";
    }
  }
  return "";
}

interface ToolUseBlock {
  id: string;
  name: string;
  input: Record<string, unknown>;
}

// Best-effort pluck of tool_use content blocks from an assistant SDK message.
// The SDK exposes message.message.content[] where each block has a `type`;
// every other block kind (text, thinking) is developer material and reaches
// claude.log, never the feed.
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

// The SDK's own first line on a failed shell call. Parsing it is not a guess at
// a format we do not own: this prefix is what the SDK writes, and it is the
// only place the process status appears.
const EXIT_CODE_LINE = /^Exit code (\d+)\b/;

// A non-shell tool reports its failure wrapped like this, with no exit code.
const TOOL_USE_ERROR_TAG = /<\/?tool_use_error>/g;

// A line that announces the fault, as opposed to the build chatter above it.
// `bal build` prints nine lines of dependency pulls before the first ERROR, so
// "the line after the exit code" would report "Compiling source" — technically
// the first line and useless as a diagnosis.
//
// Matched ANYWHERE in the line, not anchored at its start. Anchoring looked
// like the conservative choice and it excluded the commonest compiler-error
// shape there is — `src/main.tsx(13,44): error TS2307: …` puts the file first,
// so `^error` never fires and such a line only ever read well when it happened
// to also be the first line of output. Every diagnostic family this feed sees
// (tsc, javac, ballerina, gcc, a test runner's failure line) leads with a
// location. The cost of unanchoring is a prose line that merely mentions the
// word winning over the real one; the fallback below makes that the mild
// failure, since the tail of the output is where a diagnosis lives anyway.
const ANNOUNCES_FAULT = /\b(?:error|fatal|panic|exception|failed)\b/i;

/**
 * A trailing line that TELLS THE READER WHAT TO DO NEXT rather than saying what
 * went wrong. Skipped when walking back from the end for a diagnosis.
 *
 * This is the other half of the last-line rule, and it was learned the hard way.
 * Taking the last line fixed a live run's `--- expense-webapp dir ---`, and then
 * the very next run reported `Learn about accessibility experiences using
 * `gh help accessibility`` as the reason a `gh issue view` failed — gh prints
 * that footer after every error. `git`, `cargo` and `npm` all end the same way
 * ("See 'git help'", "For more information about this error…"), so the fault is
 * the LAST line that is not guidance, not the last line.
 */
const OFFERS_GUIDANCE =
  /^\s*(?:learn (?:more|about)\b|see\b|try\b|usage:|hint:|note:|for more info(?:rmation)?\b|run ['"`]|use ['"`])/i;

// How much of a failed spawned agent's error text reaches the feed. See the
// notice in `settleFanOutResult` for why that case is not held to the one-line
// rule below.
const MAX_FAILURE_LINES = 10;
const MAX_FAILURE_CHARS = 2000;

/** Output split into the lines a diagnosis can be chosen from. */
function outputLines(content: unknown): string[] {
  return resultText(content)
    .replace(TOOL_USE_ERROR_TAG, "")
    .split("\n")
    // A progress bar rewrites its own line with \r; only the final state of it
    // is text. Without this, one `bal build` line arrives 40 columns wide with
    // eight stale copies of itself in front.
    .map((l) => (l.split("\r").pop() ?? "").trim())
    .filter((l) => l !== "");
}

/** The failed call's own words, flattened to one line and bounded. */
function failureText(content: unknown): string {
  const lines = outputLines(content);
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
function failureDetail(content: unknown): { exitCode?: number; summary: string } {
  const lines = outputLines(content);
  if (lines.length === 0) return { summary: "" };

  const code = EXIT_CODE_LINE.exec(lines[0]);
  const rest = code ? lines.slice(1) : lines;
  let at = rest.findIndex((l) => ANNOUNCES_FAULT.test(l));
  // Nothing announced a fault, so fall back to the LAST line rather than the
  // first. Shell output is a banner then a diagnosis: a compiler names its
  // sources, npm prints its version notice, a test runner lists what it ran,
  // and the sentence that explains the exit code comes last. Taking line 0 put
  // `--- expense-webapp dir ---` and `import React from 'react';` on the feed
  // as two of four failure diagnoses in one live run (2026-09-08) — an echoed
  // heading and the first line of a file the command had cat'd.
  if (at < 0) {
    // Walk back past the tool's closing advice to the last line that states
    // something. If a command printed nothing BUT guidance, the last line is
    // still better than nothing.
    let end = rest.length - 1;
    while (end > 0 && (rest[end] === "" || OFFERS_GUIDANCE.test(rest[end]))) end--;
    at = end;
  }
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
  const r = obj(result);
  const severed = num(r.timedOutAfterMs);
  return {
    ...(severed ? { severedAfterMs: severed } : {}),
    backgrounded: str(r.backgroundTaskId) !== "",
  };
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
  /**
   * Called when a plain tool call settles, with the same `ok` this adapter puts
   * on the feed. A seam, not a second feature: whether a call succeeded is
   * already decided here from the SDK's own `is_error`, and re-deriving it
   * anywhere else would give two answers to one question.
   *
   * Fan-out results are deliberately excluded — such a call settles a whole
   * agent, which is a different kind of fact from one command's exit. Today's
   * only consumer is the validation progress tracker, which settles per-spec
   * `npm test` calls.
   */
  onToolOutcome?: (toolUseId: string, ok: boolean) => void;
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
  const onToolOutcome = opts?.onToolOutcome;
  const heartbeatIntervalMs = opts?.heartbeatIntervalMs ?? HEARTBEAT_MIN_INTERVAL_MS;

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
  const lastHeartbeatAt = new Map<string, number>();
  const transcripts: RuntimeArtifact[] = [];

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
  function lineDelta(tool: string, input: Record<string, unknown>): { added: number; removed: number } {
    if (!AUTHORING_TOOLS.has(tool)) return { added: 0, removed: 0 };
    if (tool === "Write") return { added: lineCount(str(input.content)), removed: 0 };
    return { added: lineCount(str(input.new_string)), removed: lineCount(str(input.old_string)) };
  }

  /** At most one heartbeat per agent per interval, and never as progress. */
  function heartbeat(agentId: string, event: RunEventInput): RunEventInput[] {
    const at = now();
    const last = lastHeartbeatAt.get(agentId);
    if (last !== undefined && at - last < heartbeatIntervalMs) return [];
    lastHeartbeatAt.set(agentId, at);
    return [event];
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
      // NOT capped here: `bashEvents` collapses the workspace prefix, and a cap
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
          events.push(...bashEvents(command, workspaceRoot).map((e) => ({ ...e, ...stamp })));
        }
        continue;
      }
      events.push({ kind: "tool_use", tool: tu.name, summary: summaryFromInput(tu.input, workspaceRoot), ...stamp });
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
  function planStatus(v: unknown): NonNullable<RunEvent["itemStatus"]> | undefined {
    const s = str(v);
    return s === "pending" || s === "in_progress" || s === "completed" || s === "deleted" ? s : undefined;
  }

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
      const incomplete = incompleteCall(m.tool_use_result);
      // A call that has not finished settles nothing. Reporting its outcome
      // would be inventing one: the command is still running, and a consumer
      // that folds outcomes into state would record a result no run produced.
      if (!incomplete.severedAfterMs && !incomplete.backgrounded) {
        onToolOutcome?.(tr.toolUseId, !tr.isError);
      }
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
      const severed = incomplete.severedAfterMs;
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
            ? failureDetail(tr.content)
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
   * recorded nowhere, its transcript not on the feed and `claude.log` dying
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
    const said = failureText(tr.content);
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
            // them from progress/diagnostics.ts: they explain a SILENCE, and
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
  };
}
