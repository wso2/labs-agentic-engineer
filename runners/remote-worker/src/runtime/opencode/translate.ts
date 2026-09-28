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

// OpenCode bus events → RUN EVENTS v2. The translation half of the second
// adapter, and the mirror of `runtime/claude/translate.ts`: different messages
// in, the same contract out, sharing everything that is not a runtime's — the
// shell rewrite, a failed call's diagnosis, the line deltas (tool_rows.ts), the
// field caps and the heartbeat budget (adapter_common.ts).
//
// The table it implements (measured on test/fixtures/opencode-*):
//
//   session.created, no parent        → run_started {runtime: "opencode"}; its id is `lead` after
//   session.created, parent + spawn   → agent_started {background: false}
//   tool part → running (not task)    → tool_use (bash → git_commit / git_push / gh_action)
//   tool part → completed | error     → tool_result
//   tool part, tool = task            → nothing on a row: the agent_started IS its row
//   running tool part with a title    → agent_progress (children only)
//   session.idle, child               → agent_settled
//   session.idle, root                → turn_ended {usage: cumulative}
//   session.error                     → notice {level: error}, the agent marked failed
//   todo.updated                      → work_item {source: plan} per CHANGED entry
//   message.part.delta                → heartbeat {waitingOn: model}
//   session.status {busy}             → heartbeat {waitingOn: model}
//   aep.tick                          → heartbeat {waitingOn: tool} per call older than 10s
//
// **Identity is declared, never inferred.** An agent's id is its OpenCode
// session id, its parent is `Session.parentID`, its depth is the chain. The
// spawning `task` part's `running` update (carrying `metadata.sessionId`) and
// the child's `session.created` arrive close together in either order (the
// committed recordings have the child first; an earlier spike had the part
// first), so a spawn is keyed by that id and `agent_started` goes out when the
// second of the two arrives.
//
// **Every agent is `background: false`.** The platform does not set OpenCode's
// experimental background flag (ADR-0015), so every `task` call blocks its
// caller until the child idles; the field is STATED so a reader is told rather
// than left to infer it.
//
// **Line counts are counted, not reported.** Every `session.updated` carries a
// summary of `{additions: 0, deletions: 0, files: 0}` and every `session.diff`
// an empty list, even after four files were written (S2b) — so the counts come
// from the successful write/edit calls' own inputs, exactly as the Claude
// adapter counts them.
//
// Per run, like its Claude twin: the registries describe ONE run.

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
import { WORKSPACE_GUARD_MARKER } from "./plugin/protocol.js";
import { num, obj, str } from "../fields.js";
import { readEvent, TOOL_TICK } from "./messages.js";
import { FANOUT_TOOL, PLAN_TOOL, platformModel, SHELL_TOOL } from "./tools.js";

/** A call running this long with nothing on the wire gets a tool heartbeat on the next tick. */
const TOOL_HEARTBEAT_AFTER_MS = 10_000;

/** OpenCode's own suffix on a subagent session's title — `Create alpha file (@general subagent)`. */
const SUBAGENT_TITLE_SUFFIX = /\s*\(@[^)]*\bsubagent\)\s*$/;

interface SessionRecord {
  parentSessionId?: string;
  depth: number;
  createdAt: number;
  /** Its session title, the only name it has until its spawn is seen. */
  title: string;
  /** agent_started emitted. */
  started: boolean;
  settled: boolean;
  toolCount: number;
  linesAdded: number;
  linesRemoved: number;
  /** The latest assistant text it produced — its report when it settles. */
  lastText: string;
  /** Why it failed, when it did: an errored last message or a session.error. */
  failure?: string;
  /** Its newest assistant message, for the turn outcome. */
  lastAssistant?: string;
}

interface Spawn {
  label: string;
  role: string;
  model: string;
  parentSessionId: string;
  callId: string;
}

interface MessageRecord {
  role: string;
  sessionId: string;
  modelId: string;
  input: number;
  output: number;
  reasoning: number;
  cacheRead: number;
  cacheWrite: number;
  total: number;
  error?: string;
}

interface CallRecord {
  tool: string;
  sessionId: string;
  startedAt: number;
  used: boolean;
  settled: boolean;
  progressed: boolean;
}

export interface OpencodeAdapterOptions {
  /** The org's model, platform spelling, for `run_started` — the config the adapter wrote. */
  model: string;
  taskKind?: NonNullable<RunEvent["taskKind"]>;
  /** Injected clock, so durations and heartbeat budgets are testable without sleeping. */
  now?: () => number;
  /**
   * The platform's workspace guard refused a write — `RuntimePolicy.write.onDenied`.
   * The plugin cannot reach the feed, so it marks its refusal in the tool's
   * error text and this is where the marked text becomes the run's own line.
   */
  onWorkspaceDenied?: (reason: string) => void;
  /** Heartbeat budget per agent; the default is the design's 10s. */
  heartbeatIntervalMs?: number;
}

export interface OpencodeAdapter {
  translate(message: unknown): RunEventInput[];
  /**
   * The runner is aborting this session (`stopTask`, or `close` for whatever is
   * still busy, the root included). Call BEFORE the abort: its
   * `MessageAbortedError` is then expected and silent, and a child settles
   * `stopped`. An abort nobody asked for stays an error.
   */
  markStopped(sessionId: string): void;
  /** The run's cumulative usage so far — `RuntimeSession.usage`. */
  usage(): RunEventUsage | undefined;
}

/** OpenCode's argument spellings, most identifying first — see `summaryFromInput`. */
const SUMMARY_FIELDS = ["filePath", "path", "pattern", "url", "query", "name", "description"] as const;

/** A successful authoring call's line delta, by OpenCode's argument spelling. */
function lineDelta(tool: string, input: Record<string, unknown>): LineDelta {
  if (tool === "write") return writeDelta(str(input.content));
  if (tool === "edit") return editDelta(str(input.oldString), str(input.newString));
  return { added: 0, removed: 0 };
}

/** An error object as a line: `ProviderAuthError: invalid x-api-key`. */
function errorLine(error: unknown): string {
  const e = obj(error);
  const name = str(e.name);
  const message = str(obj(e.data).message) || str(e.message);
  return [name, message].filter(Boolean).join(": ");
}

/** Build the adapter for ONE run. */
export function createOpencodeAdapter(opts: OpencodeAdapterOptions): OpencodeAdapter {
  const now = opts.now ?? Date.now;
  const heartbeat = createHeartbeatLimiter({
    now,
    ...(opts.heartbeatIntervalMs !== undefined ? { intervalMs: opts.heartbeatIntervalMs } : {}),
  });

  let rootId = "";
  let workspaceRoot = "";
  // The root's newest assistant message and a session.error it raised: the
  // two ways a turn ends in failure. Cleared when a turn reports it.
  let rootLastAssistant: string | undefined;
  let rootFailure: string | undefined;
  const sessions = new Map<string, SessionRecord>();
  // Sessions the runner asked to abort — any id, the root's too, and possibly
  // before the child's session.created arrives.
  const stopRequested = new Set<string>();
  const spawns = new Map<string, Spawn>();
  const messages = new Map<string, MessageRecord>();
  const calls = new Map<string, CallRecord>();
  const plans = new Map<string, Map<string, { title: string; status?: NonNullable<RunEvent["itemStatus"]> }>>();

  function agentOf(sessionId: string): string {
    if (!sessionId || sessionId === rootId || !sessions.has(sessionId)) return LEAD_AGENT_ID;
    return sessionId;
  }

  function depthOf(sessionId: string | undefined): number {
    if (!sessionId || sessionId === rootId) return 0;
    return sessions.get(sessionId)?.depth ?? 0;
  }

  /**
   * `agent_started`, once both halves of the birth certificate are in: the
   * child's `session.created` (id, parent) and its spawning part (label, role,
   * model). `force` starts it with what is known when the child speaks before
   * its spawn was seen — a name from its title is better than a row with none.
   */
  function start(childId: string, force = false): RunEventInput[] {
    const session = sessions.get(childId);
    if (!session || session.started) return [];
    const spawn = spawns.get(childId);
    if (!spawn && !force) return [];
    session.started = true;
    const parentAgentId = agentOf(spawn?.parentSessionId ?? session.parentSessionId ?? "");
    const label = spawn?.label || trimSummary(session.title.replace(SUBAGENT_TITLE_SUFFIX, ""));
    return [{
      kind: "agent_started",
      agentId: childId,
      ...(parentAgentId !== LEAD_AGENT_ID ? { parentAgentId } : {}),
      ...(label ? { label } : {}),
      ...(spawn?.role ? { role: spawn.role } : {}),
      depth: session.depth,
      background: false,
      ...(spawn?.model ? { model: spawn.model } : {}),
    }];
  }

  function onSessionCreated(p: Record<string, unknown>): RunEventInput[] {
    const info = obj(p.info);
    const id = str(info.id);
    if (!id) return [];
    const parent = str(info.parentID);
    if (!parent) {
      if (rootId) return [];
      rootId = id;
      workspaceRoot = str(info.directory).replace(/\/+$/, "");
      return [{
        kind: "run_started",
        agentId: LEAD_AGENT_ID,
        runtime: "opencode",
        ...(opts.model ? { model: opts.model } : {}),
        ...(opts.taskKind ? { taskKind: opts.taskKind } : {}),
      }];
    }
    if (sessions.has(id)) return [];
    sessions.set(id, {
      parentSessionId: parent,
      depth: depthOf(parent) + 1,
      createdAt: now(),
      title: str(info.title),
      started: false,
      settled: false,
      toolCount: 0,
      linesAdded: 0,
      linesRemoved: 0,
      lastText: "",
    });
    return start(id);
  }

  function onMessageUpdated(p: Record<string, unknown>): RunEventInput[] {
    const info = obj(p.info);
    const id = str(info.id);
    if (!id) return [];
    const tokens = obj(info.tokens);
    const cache = obj(tokens.cache);
    const error = info.error ? errorLine(info.error) : undefined;
    const record: MessageRecord = {
      role: str(info.role),
      sessionId: str(info.sessionID) || str(p.sessionID),
      modelId: str(info.modelID),
      input: num(tokens.input),
      output: num(tokens.output),
      reasoning: num(tokens.reasoning),
      cacheRead: num(cache.read),
      cacheWrite: num(cache.write),
      total: num(tokens.total),
      ...(error ? { error } : {}),
    };
    // One message is re-announced as it grows; the LATEST snapshot is its usage.
    messages.set(id, record);
    if (record.role === "assistant") {
      const session = sessions.get(record.sessionId);
      if (session) session.lastAssistant = id;
      else if (record.sessionId === rootId) rootLastAssistant = id;
    }
    return [];
  }

  /**
   * The run's usage so far: every assistant message of EVERY session, summed
   * per model. Cumulative, so the last `turn_ended` is the run's total and
   * `run_settled` can carry it (run_loop.ts) — the same contract Claude Code's
   * `modelUsage` meets.
   *
   * `outputTokens` is OpenCode's `output + reasoning`: the two are reported
   * apart (a step's `total` is input + output + reasoning + cache, measured on
   * S1c), and the provider bills reasoning as output — S1c's first Sonnet step
   * prices to its reported `cost` only when its 12 reasoning tokens are counted
   * as output. `costUsd` is null for the reason the Claude adapter gives: the
   * platform stamps cost from its own rate table.
   */
  function usage(): RunEventUsage | undefined {
    const perModel = new Map<string, RunEventModelUsage>();
    for (const m of messages.values()) {
      if (m.role !== "assistant" || !m.modelId) continue;
      if (m.input + m.output + m.reasoning + m.cacheRead + m.cacheWrite === 0) continue;
      const model = platformModel(m.modelId);
      const slice = perModel.get(model) ?? {
        inputTokens: 0,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
        model,
        costUsd: null,
      };
      slice.inputTokens += m.input;
      slice.outputTokens += m.output + m.reasoning;
      slice.cacheReadTokens += m.cacheRead;
      slice.cacheCreationTokens += m.cacheWrite;
      perModel.set(model, slice);
    }
    const models = [...perModel.values()];
    if (models.length === 0) return undefined;
    const totals = models.reduce(
      (acc, s) => ({
        inputTokens: acc.inputTokens + s.inputTokens,
        outputTokens: acc.outputTokens + s.outputTokens,
        cacheReadTokens: acc.cacheReadTokens + s.cacheReadTokens,
        cacheCreationTokens: acc.cacheCreationTokens + s.cacheCreationTokens,
      }),
      { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
    );
    return { ...totals, model: models.length === 1 ? models[0].model : "", costUsd: null, models };
  }

  /** Tokens one session spent — its own assistant messages, latest snapshot each. */
  function sessionTokens(sessionId: string): number {
    let total = 0;
    for (const m of messages.values()) {
      if (m.sessionId !== sessionId || m.role !== "assistant") continue;
      total += m.total || m.input + m.output + m.reasoning + m.cacheRead + m.cacheWrite;
    }
    return total;
  }

  function onTextPart(part: Record<string, unknown>): RunEventInput[] {
    const text = str(part.text).trim();
    if (!text || part.synthetic === true) return [];
    const message = messages.get(str(part.messageID));
    if (message?.role !== "assistant") return [];
    const session = sessions.get(str(part.sessionID));
    if (session) session.lastText = text;
    return [];
  }

  /** The fan-out call: registers the spawn; confirms or fails it. No row of its own. */
  function onTaskPart(part: Record<string, unknown>, state: Record<string, unknown>, status: string): RunEventInput[] {
    const callId = str(part.callID);
    const parentSessionId = str(part.sessionID);
    const input = obj(state.input);
    const metadata = obj(state.metadata);
    const childId = str(metadata.sessionId);
    const events: RunEventInput[] = [];
    countCall(callId, FANOUT_TOOL, parentSessionId, state);
    if (childId && !spawns.has(childId)) {
      spawns.set(childId, {
        label: trimSummary(str(input.description)),
        role: str(input.subagent_type),
        model: platformModel(str(obj(metadata.model).modelID)),
        parentSessionId,
        callId,
      });
      events.push(...start(childId));
    }
    if (status === "error") {
      const call = calls.get(callId);
      if (call && !call.settled) {
        call.settled = true;
        const said = failureText(str(state.error));
        const label = spawns.get(childId)?.label || trimSummary(str(input.description)) || "agent";
        const child = childId ? sessions.get(childId) : undefined;
        if (child && !child.failure) child.failure = said || "the fan-out call failed";
        events.push({
          kind: "notice",
          agentId: childId && sessions.has(childId) ? childId : agentOf(parentSessionId),
          level: "error",
          detail: cap(`[fan-out] ${label} failed: ${said || "no error text on the task call"}`, MAX_REPORT),
        });
      }
    }
    return events;
  }

  /** A call seen for the first time, counted toward its session's tools. */
  function countCall(callId: string, tool: string, sessionId: string, state: Record<string, unknown>): CallRecord {
    let call = calls.get(callId);
    if (!call) {
      const startedAt = num(obj(state.time).start) || now();
      call = { tool, sessionId, startedAt, used: false, settled: false, progressed: false };
      calls.set(callId, call);
      const session = sessions.get(sessionId);
      if (session) session.toolCount += 1;
    }
    return call;
  }

  function onToolPart(part: Record<string, unknown>): RunEventInput[] {
    const tool = str(part.tool);
    const callId = str(part.callID);
    const sessionId = str(part.sessionID);
    const state = obj(part.state);
    const status = str(state.status);
    if (!tool || !callId || status === "pending" || status === "") return [];
    if (tool === FANOUT_TOOL) return onTaskPart(part, state, status);

    const agentId = agentOf(sessionId);
    const events: RunEventInput[] = [];
    // A child that speaks before its spawn was seen is still an agent.
    if (agentId !== LEAD_AGENT_ID) events.push(...start(sessionId, true));
    const input = obj(state.input);
    const call = countCall(callId, tool, sessionId, state);
    const stamp = { agentId, toolUseId: callId };

    // The task list reaches the feed as `todo.updated`'s rows, not as a call.
    if (tool === PLAN_TOOL) return events;

    if (!call.used) {
      call.used = true;
      if (tool === SHELL_TOOL) {
        const command = str(input.command);
        events.push(
          ...(command ? shellEvents(command, workspaceRoot, tool) : [{ kind: "tool_use" as const, tool, summary: "" }]).map(
            (e) => ({ ...e, ...stamp }),
          ),
        );
      } else {
        events.push({ kind: "tool_use", tool, summary: summaryFromInput(input, SUMMARY_FIELDS, workspaceRoot), ...stamp });
      }
    }

    // A child's running call with a title is what it is doing now.
    const title = str(state.title);
    if (status === "running" && title && agentId !== LEAD_AGENT_ID && !call.progressed) {
      call.progressed = true;
      const session = sessions.get(sessionId);
      events.push({
        kind: "agent_progress",
        agentId,
        phrase: trimSummary(title),
        ...(session?.toolCount ? { toolCount: session.toolCount } : {}),
      });
    }

    if ((status === "completed" || status === "error") && !call.settled) {
      call.settled = true;
      events.push(...settleCall(tool, callId, agentId, sessionId, state, status, input, call));
    }
    return events;
  }

  function settleCall(
    tool: string,
    callId: string,
    agentId: string,
    sessionId: string,
    state: Record<string, unknown>,
    status: string,
    input: Record<string, unknown>,
    call: CallRecord,
  ): RunEventInput[] {
    const time = obj(state.time);
    const metadata = obj(state.metadata);
    const exit = metadata.exit;
    const exitCode = tool === SHELL_TOOL && typeof exit === "number" ? exit : undefined;
    // A shell call that ran to its end is `completed` whatever its exit code;
    // the feed's `ok` means the command succeeded, as it does for Claude Code.
    const ok = status === "completed" && (exitCode === undefined || exitCode === 0);
    // The part's own clock when it states both ends; the adapter's otherwise.
    const durationMs =
      typeof time.end === "number" && typeof time.start === "number"
        ? Math.max(0, time.end - time.start)
        : Math.max(0, now() - call.startedAt);

    let failure: { exitCode?: number; summary?: string } = {};
    if (status === "error") {
      const text = str(state.error);
      if (text.startsWith(WORKSPACE_GUARD_MARKER)) {
        // The platform's own refusal: the run says so in its own words, and the
        // row carries no second copy of the sentence.
        opts.onWorkspaceDenied?.(text.slice(WORKSPACE_GUARD_MARKER.length).trim());
      } else {
        failure = failureDetail(text);
      }
    } else if (!ok) {
      failure = failureDetail(str(state.output) || str(metadata.output));
    }

    if (ok) {
      const delta = lineDelta(tool, input);
      const session = sessions.get(sessionId);
      if (session) {
        session.linesAdded += delta.added;
        session.linesRemoved += delta.removed;
      }
    }
    return [{
      kind: "tool_result",
      agentId,
      toolUseId: callId,
      ok,
      tool,
      durationMs,
      ...(failure.summary ? { summary: failure.summary } : {}),
      // Only a failed call carries its exit code, as on Claude Code: a zero on
      // every successful shell row would be noise the row does not print.
      ...(!ok && exitCode !== undefined ? { exitCode } : !ok && failure.exitCode !== undefined ? { exitCode: failure.exitCode } : {}),
    }];
  }

  function settleChild(sessionId: string): RunEventInput[] {
    const session = sessions.get(sessionId);
    if (!session || session.settled) return [];
    const events = start(sessionId, true);
    session.settled = true;
    const lastError = session.lastAssistant ? messages.get(session.lastAssistant)?.error : undefined;
    const failure = session.failure ?? lastError;
    const status: AgentStatus = stopRequested.has(sessionId) ? "stopped" : failure ? "failed" : "completed";
    const report = reportFrom(session.lastText);
    const tokens = sessionTokens(sessionId);
    const durationMs = Math.max(0, now() - session.createdAt);
    events.push({
      kind: "agent_settled",
      agentId: sessionId,
      status,
      ...(report ? { report } : {}),
      ...(durationMs ? { durationMs } : {}),
      ...(session.toolCount ? { toolCount: session.toolCount } : {}),
      ...(tokens ? { tokens } : {}),
      ...(session.linesAdded ? { linesAdded: session.linesAdded } : {}),
      ...(session.linesRemoved ? { linesRemoved: session.linesRemoved } : {}),
    });
    return events;
  }

  function onSessionIdle(p: Record<string, unknown>): RunEventInput[] {
    const id = str(p.sessionID);
    if (!id) return [];
    if (id !== rootId) return settleChild(id);
    const lastError = rootLastAssistant ? messages.get(rootLastAssistant)?.error : undefined;
    const error = rootFailure ?? lastError;
    rootFailure = undefined;
    const u = usage();
    return [{
      kind: "turn_ended",
      agentId: LEAD_AGENT_ID,
      outcome: error ? "failure" : "success",
      ...(error ? { error: cap(error, MAX_REPORT) } : {}),
      ...(u ? { usage: u } : {}),
    }];
  }

  function onSessionError(p: Record<string, unknown>): RunEventInput[] {
    const id = str(p.sessionID);
    const error = obj(p.error);
    const name = str(error.name);
    const session = sessions.get(id);
    // The stop the runner asked for — expected, and already said by the loop.
    if (name === "MessageAbortedError" && stopRequested.has(id)) return [];
    const line = errorLine(error) || "unknown error";
    if (session) session.failure = line;
    else if (id === rootId || !id) rootFailure = line;
    return [{
      kind: "notice",
      agentId: agentOf(id),
      level: "error",
      detail: cap(`[opencode] ${line}`, MAX_REPORT),
    }];
  }

  /** `todo.updated` replaces the list; the feed gets only what changed. */
  function onTodoUpdated(p: Record<string, unknown>): RunEventInput[] {
    const sessionId = str(p.sessionID);
    const agentId = agentOf(sessionId);
    const todos = Array.isArray(p.todos) ? (p.todos as unknown[]).map(obj) : [];
    const before = plans.get(sessionId) ?? new Map();
    const after = new Map<string, { title: string; status?: NonNullable<RunEvent["itemStatus"]> }>();
    const events: RunEventInput[] = [];
    for (const todo of todos) {
      const title = trimSummary(str(todo.content));
      // Measured: 1.18.32's todos carry no id (S2b), so the entry's own text is
      // its identity — a status change keeps it, a rewording is a new item.
      const itemId = str(todo.id) || title;
      if (!itemId) continue;
      const status = planStatus(todo.status);
      after.set(itemId, { title, ...(status ? { status } : {}) });
      const prior = before.get(itemId);
      if (prior && prior.title === title && prior.status === status) continue;
      events.push({
        kind: "work_item",
        agentId,
        source: "plan",
        itemId,
        ...(title ? { title } : {}),
        ...(status ? { itemStatus: status } : {}),
        ...(agentId !== LEAD_AGENT_ID ? { ownerAgentId: agentId } : {}),
      });
    }
    for (const [itemId, prior] of before) {
      if (after.has(itemId) || prior.status === "deleted") continue;
      events.push({ kind: "work_item", agentId, source: "plan", itemId, itemStatus: "deleted" });
    }
    plans.set(sessionId, after);
    return events;
  }

  function modelHeartbeat(sessionId: string): RunEventInput[] {
    const agentId = agentOf(sessionId);
    return heartbeat(agentId, { kind: "heartbeat", agentId, waitingOn: "model" });
  }

  function onTick(): RunEventInput[] {
    const at = now();
    const events: RunEventInput[] = [];
    for (const [callId, call] of calls) {
      if (call.settled || call.tool === FANOUT_TOOL || at - call.startedAt < TOOL_HEARTBEAT_AFTER_MS) continue;
      const agentId = agentOf(call.sessionId);
      events.push(
        ...heartbeat(agentId, { kind: "heartbeat", agentId, waitingOn: "tool", ref: callId, elapsedMs: at - call.startedAt }),
      );
    }
    return events;
  }

  function translate(message: unknown): RunEventInput[] {
    const ev = readEvent(message);
    if (!ev) return [];
    const p = ev.properties;
    switch (ev.type) {
      case "session.created":
        return onSessionCreated(p);
      case "session.idle":
        return onSessionIdle(p);
      case "session.error":
        return onSessionError(p);
      case "message.updated":
        return onMessageUpdated(p);
      case "message.part.updated": {
        const part = obj(p.part);
        if (part.type === "tool") return onToolPart(part);
        if (part.type === "text") return onTextPart(part);
        return [];
      }
      case "session.status":
        if (str(obj(p.status).type) !== "busy") return [];
        return modelHeartbeat(str(p.sessionID));
      case "message.part.delta":
        return modelHeartbeat(str(p.sessionID));
      case "todo.updated":
        return onTodoUpdated(p);
      case TOOL_TICK:
        return onTick();
      default:
        return [];
    }
  }

  return {
    translate,
    markStopped(sessionId) {
      stopRequested.add(sessionId);
    },
    usage,
  };
}
