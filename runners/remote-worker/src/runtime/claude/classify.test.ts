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

import { test } from "node:test";
import assert from "node:assert/strict";
import { apiRetryLine } from "../../lib/run_loop.js";
import { createClaudeClassifier, createStallSignalReader, readApiRetry, readStallSignal } from "./classify.js";

/** One message through a fresh classifier — for the classes with no memory. */
function classOf(message: unknown): string {
  return createClaudeClassifier()(message).kind;
}

// The literal wire shape, copied from a live SDK message rather than from the
// type declaration — the reader's job is to survive what actually arrives.
const RETRY = {
  type: "system",
  subtype: "api_retry",
  attempt: 3,
  max_retries: 10,
  retry_delay_ms: 4_200,
  error_status: 529,
  error: "overloaded",
  uuid: "u1",
  session_id: "s1",
};

test("readApiRetry: reads the real wire shape", () => {
  assert.deepEqual(readApiRetry(RETRY), {
    attempt: 3,
    maxRetries: 10,
    retryDelayMs: 4_200,
    errorStatus: 529,
    error: "overloaded",
  });
});

test("readApiRetry: a connection error keeps its null status rather than inventing one", () => {
  // error_status is null for a timeout or a refused connection — the case the
  // dead-endpoint probe produced, and the one most likely to strand a run.
  const info = readApiRetry({ ...RETRY, error_status: null, error: "unknown" });
  assert.equal(info?.errorStatus, null);
  assert.match(apiRetryLine(info!), /no response/);
  assert.doesNotMatch(apiRetryLine(info!), /HTTP/);
});

test("readApiRetry: every other message is not a retry", () => {
  // The run loop asks this of EVERY message, so a false positive would divert
  // real work away from the translator.
  for (const m of [
    { type: "system", subtype: "init" },
    { type: "system", subtype: "task_progress", task_id: "t1" },
    { type: "assistant", message: { content: [] } },
    { type: "result", subtype: "success" },
    { type: "stream_event", event: {} },
    null,
    undefined,
    "api_retry",
    42,
  ]) {
    assert.equal(readApiRetry(m), undefined, `treated as a retry: ${JSON.stringify(m)}`);
  }
});

test("readApiRetry: a renamed field degrades to a usable line, never to NaN", () => {
  // The image ships whatever SDK version it ships. A retry we can only half
  // read is still worth reporting; a line reading "retry NaN/undefined" is not.
  const info = readApiRetry({ type: "system", subtype: "api_retry" });
  assert.deepEqual(info, {
    attempt: 0,
    maxRetries: 0,
    retryDelayMs: 0,
    errorStatus: null,
    error: "unknown",
  });
  assert.doesNotMatch(apiRetryLine(info!), /NaN|undefined/);
});

test("apiRetryLine: names the attempt, the cause and the wait", () => {
  const line = apiRetryLine(readApiRetry(RETRY)!);
  assert.match(line, /retry 3\/10/);
  assert.match(line, /overloaded \(HTTP 529\)/);
  assert.match(line, /next attempt in 4s/);
});

// A streaming frame is the one model wait that never reaches runtime.log, so the
// flag is what the loop's record decision turns on.
test("classify: only the streaming frames are marked streaming", () => {
  const classify = createClaudeClassifier();
  assert.deepEqual(classify({ type: "stream_event", event: {} }), { kind: "model_wait", streaming: true });
  assert.deepEqual(classify({ type: "system", subtype: "thinking_tokens" }), { kind: "model_wait", streaming: false });
  assert.equal(classOf({ type: "assistant", message: { content: [] } }), "activity");
  assert.equal(classOf(RETRY), "retry");
  assert.equal(classOf(null), "activity");
});

// --- The system messages that explain a silence, or an ending ---------------

test("readStallSignal: an auto-compaction is minutes of silence with a name", () => {
  // The stall shape api_retry cannot explain: no retries, no tool, no tokens —
  // the run is compacting, and until now that reached the feed as nothing.
  const line = readStallSignal({
    type: "system",
    subtype: "compact_boundary",
    compact_metadata: { trigger: "auto", pre_tokens: 152_331, post_tokens: 38_120, duration_ms: 47_000 },
    uuid: "u1",
    session_id: "s1",
  });
  assert.equal(line?.level, "info", "compaction is healthy — it is reported, not warned about");
  assert.equal(line?.code, "compaction", "the code is what a consumer branches on");
  assert.equal(line?.detail, "[compact] auto compaction 152k → 38k tokens in 47s");
});

test("readStallSignal: a refusal with no fallback is the end of the turn, and says so", () => {
  const line = readStallSignal({
    type: "system",
    subtype: "model_refusal_no_fallback",
    original_model: "claude-sonnet-5",
    request_id: "req_1",
    api_refusal_category: "cyber",
    api_refusal_explanation: "The request was declined.",
    content: "",
    uuid: "u1",
    session_id: "s1",
  });
  assert.equal(line?.level, "error");
  assert.equal(line?.code, "refusal");
  assert.equal(
    line?.detail,
    "[model] claude-sonnet-5 refused (cyber) and no fallback ran: The request was declined.",
  );
});

test("readStallSignal: a denied tool call is named — the agent is working around a wall", () => {
  // On the stream only since SDK 0.3.223. A DISALLOWED_TOOLS denial used to
  // reach the feed as a tool call with a puzzling result and no reason.
  const line = readStallSignal({
    type: "system",
    subtype: "permission_denied",
    tool_name: "ScheduleWakeup",
    tool_use_id: "toolu_1",
    decision_reason_type: "rule",
    message: "This tool is not available in this session",
    uuid: "u1",
    session_id: "s1",
  });
  assert.equal(line?.level, "warn");
  assert.equal(line?.code, "permission_denied");
  assert.match(line?.detail ?? "", /^\[permission\] ScheduleWakeup denied by rule: /);
});

test("readStallSignal: prose from elsewhere is bounded", () => {
  const line = readStallSignal({
    type: "system",
    subtype: "model_refusal_no_fallback",
    original_model: "claude-sonnet-5",
    api_refusal_explanation: "x".repeat(5_000),
  });
  assert.ok((line?.detail.length ?? 0) < 300, "an unbounded field cannot flood the feed");
  assert.match(line?.detail ?? "", /…$/);
});

test("readStallSignal: a missing field degrades to a usable line, never to undefined text", () => {
  // Same rule as readApiRetry: the image ships whatever SDK it ships.
  const line = readStallSignal({ type: "system", subtype: "worker_shutting_down" });
  assert.equal(line?.detail, "[worker] shutting down: unknown");
  assert.equal(readStallSignal({ type: "system", subtype: "compact_boundary" })?.detail, "[compact] auto compaction");
});

// The one entry that is not a system subtype. It arrives on every window
// change, including the healthy ones, which is why the level moves with the
// status rather than being fixed at `warn`.
test("readStallSignal: a rate-limit window is reported at the volume its status earns", () => {
  const allowed = readStallSignal({
    type: "rate_limit_event",
    rate_limit_info: { status: "allowed", rateLimitType: "five_hour", utilization: 0.26 },
  });
  assert.equal(allowed?.level, "info");
  assert.equal(allowed?.code, "rate_limit");
  assert.equal(allowed?.detail, "[rate-limit] allowed on the five_hour window at 26%");

  const rejected = readStallSignal({
    type: "rate_limit_event",
    rate_limit_info: { status: "rejected", rateLimitType: "seven_day" },
  });
  assert.equal(rejected?.level, "error", "a refusal is the whole explanation for a run going nowhere");
  assert.equal(rejected?.detail, "[rate-limit] rejected on the seven_day window");

  assert.equal(readStallSignal({ type: "rate_limit_event" }), undefined, "a shape we do not recognise claims nothing");
});

// Both shapes mean "the model is still working", which is the one thing that
// must never reset the idle clock — see the run loop.
test("classify: a thinking-token delta and a streaming frame are model waits", () => {
  assert.equal(classOf({ type: "system", subtype: "thinking_tokens", estimated_tokens: 40 }), "model_wait");
  assert.equal(classOf({ type: "stream_event" }), "model_wait");
  assert.equal(classOf({ type: "system", subtype: "task_progress" }), "task_bookkeeping");
  assert.equal(classOf({ type: "assistant" }), "activity");
  assert.equal(classOf(null), "activity");
});

// The half that is easy to get wrong: once the rate limiter drops a heartbeat
// this message produces NOTHING, and a loop that fell through to observe() on
// an empty event list would reset the idle clock for as long as the tool kept
// ticking — silencing the watchdog through exactly the stall it reports.
test("classify: a still-running tool call is liveness, not work", () => {
  assert.equal(classOf({ type: "tool_progress", tool_use_id: "t1", elapsed_time_seconds: 42 }), "tool_progress");
  assert.notEqual(classOf({ type: "system", subtype: "task_progress" }), "tool_progress", "that one IS work");
  assert.equal(classOf(null), "activity");
});

test("readStallSignal: every other message is not a signal", () => {
  assert.equal(readStallSignal(RETRY), undefined, "retries have their own line");
  assert.equal(readStallSignal({ type: "system", subtype: "task_started" }), undefined);
  assert.equal(readStallSignal({ type: "assistant", message: { content: [] } }), undefined);
  assert.equal(readStallSignal(null), undefined);
});

// The live run of 2026-09-08 carried 17 of these, gaps as short as 8 seconds,
// and exactly three distinct sentences between them. A warning that repeats
// every minute is one a reader learns to skip past — including the minute it
// finally says `rejected`.
test("stall reader: a rate-limit warning is said once per thing it has to say", () => {
  const read = createStallSignalReader();
  const window = (utilization: number, status = "allowed_warning"): unknown => ({
    type: "rate_limit_event",
    rate_limit_info: { status, rateLimitType: "seven_day", utilization },
  });

  // The live utilisations, in the order the run saw them: 82% four times,
  // 83% eleven times, 84% twice.
  const run = [0.822, 0.8241, 0.8249, 0.8203, 0.8251, 0.8288, 0.834, 0.8299, 0.8312, 0.8349, 0.8302, 0.8281,
    0.8266, 0.8341, 0.8337, 0.8449, 0.8351];
  const said = run.map((u) => read(window(u))).filter((s) => s !== undefined);
  assert.deepEqual(said.map((s) => s!.detail), [
    "[rate-limit] near the limit on the seven_day window at 82%",
    "[rate-limit] near the limit on the seven_day window at 83%",
    "[rate-limit] near the limit on the seven_day window at 84%",
  ], "three sentences, not seventeen — one per whole percent the reader can see change");
});

test("stall reader: the line that MATTERS is never swallowed by the ones before it", () => {
  const read = createStallSignalReader();
  const at = (status: string, utilization: number): unknown => ({
    type: "rate_limit_event",
    rate_limit_info: { status, rateLimitType: "seven_day", utilization },
  });
  assert.ok(read(at("allowed_warning", 0.83)), "the first crossing always speaks");
  assert.equal(read(at("allowed_warning", 0.834)), undefined, "the same sentence again does not");
  // A status change is a different sentence, so it is a different fact: the
  // provider is now refusing, which is the whole explanation for a run that is
  // about to go nowhere.
  const rejected = read(at("rejected", 0.834));
  assert.equal(rejected?.level, "error");
  assert.equal(rejected?.detail, "[rate-limit] rejected on the seven_day window at 83%");
  // And a window that comes back down says so, rather than being remembered as
  // "already said": only the LAST line is held, so this suppresses repetition
  // and never a change.
  assert.ok(read(at("allowed_warning", 0.82)));
});

// Only the rate limit repeats itself. Everything else here is a discrete
// occurrence, and a second one is a second fact.
test("stall reader: a second compaction is a second event, not a repeat", () => {
  const read = createStallSignalReader();
  const compaction = { type: "system", subtype: "compact_boundary" };
  assert.ok(read(compaction));
  assert.ok(read(compaction), "two compactions are two silences to explain");
});

// --- the rest of the loop's questions -------------------------------------------

test("classify: a result is a turn ending, and nothing else is", () => {
  assert.equal(classOf({ type: "result", subtype: "success" }), "turn_end");
  assert.equal(classOf({ type: "result", subtype: "error_max_turns" }), "turn_end");
  assert.equal(classOf({ type: "system", subtype: "task_notification", task_id: "t1" }), "task_bookkeeping");
});

test("classify: an init carries the skills the session resolved", () => {
  const classify = createClaudeClassifier();
  assert.deepEqual(classify({ type: "system", subtype: "init", skills: ["aep", "go"] }), {
    kind: "init",
    resolvedSkills: ["aep", "go"],
  });
  assert.deepEqual(classify({ type: "system", subtype: "init" }), { kind: "init", resolvedSkills: [] }, "absent is none");
});

// The live-task set is what a deadline or a fatal has to stop, so each id has
// to enter and leave it on exactly the messages that mean so.
test("classify: task bookkeeping names the task that started or ended", () => {
  const classify = createClaudeClassifier();
  const task = (subtype: string, extra: Record<string, unknown> = {}): unknown => ({
    type: "system",
    subtype,
    task_id: "t1",
    ...extra,
  });
  assert.deepEqual(classify(task("task_started")), { kind: "task_bookkeeping", started: "t1" });
  assert.deepEqual(classify(task("task_notification", { status: "completed" })), { kind: "task_bookkeeping", ended: "t1" });
  for (const status of ["completed", "failed", "killed", "stopped", "cancelled", "error"]) {
    assert.deepEqual(classify(task("task_updated", { patch: { status } })), { kind: "task_bookkeeping", ended: "t1" }, status);
  }
  // Anything not known to be terminal leaves the task live: stopping a task
  // that already finished is a no-op, dropping one still running is not.
  assert.deepEqual(classify(task("task_updated", { patch: { status: "running" } })), { kind: "task_bookkeeping" });
  assert.deepEqual(classify(task("task_updated")), { kind: "task_bookkeeping" });
  assert.deepEqual(classify(task("task_progress")), { kind: "task_bookkeeping" });
  assert.deepEqual(classify(task("task_started", { task_id: "" })), { kind: "task_bookkeeping" }, "no id, nothing to stop");
});

// The roster looks authoritative and is not: it lists only BACKGROUNDED tasks,
// so reading ids off it would evict a foreground subagent (probe 1's depth-2
// child) and leave it running past a deadline.
test("classify: the background roster is bookkeeping that moves no task", () => {
  assert.deepEqual(createClaudeClassifier()({ type: "system", subtype: "background_tasks_changed", tasks: [] }), {
    kind: "task_bookkeeping",
  });
});

// The one place the order of the questions matters: a repeated rate-limit
// sentence is not a stall signal, and it is then plain activity.
test("classify: a rate-limit sentence already said falls through to activity", () => {
  const classify = createClaudeClassifier();
  const window = { type: "rate_limit_event", rate_limit_info: { status: "allowed_warning", rateLimitType: "seven_day", utilization: 0.83 } };
  assert.equal(classify(window).kind, "stall_signal");
  assert.equal(classify(window).kind, "activity");
  // Per session: a second session has said nothing yet.
  assert.equal(createClaudeClassifier()(window).kind, "stall_signal");
});

test("classify: every health message is its own class, not activity", () => {
  assert.deepEqual(createClaudeClassifier()(RETRY), { kind: "retry", info: readApiRetry(RETRY) });
  for (const subtype of ["compact_boundary", "model_refusal_fallback", "model_refusal_no_fallback", "permission_denied", "worker_shutting_down"]) {
    assert.equal(classOf({ type: "system", subtype }), "stall_signal", subtype);
  }
  assert.equal(classOf({ type: "system", subtype: "something_new" }), "activity", "an unknown subtype claims nothing");
});
