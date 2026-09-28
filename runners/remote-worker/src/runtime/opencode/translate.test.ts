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

// The translator's rules that no recording exercises: the guard's marked
// refusal, a failing shell command, a permission-free failure, a stopped child,
// the plan's replace semantics, the adapter's tool clock. The recorded shapes
// are pinned by replay.test.ts; these are the same shapes, hand-built for the
// branches the spikes never hit.

import { test } from "node:test";
import assert from "node:assert/strict";
import { WORKSPACE_GUARD_MARKER } from "./plugin/protocol.js";
import { createOpencodeAdapter, type OpencodeAdapterOptions } from "./translate.js";

const WS = "/work/proj";
const ev = (type: string, properties: Record<string, unknown> = {}) => ({ type, properties });

function adapterAt(over: Partial<OpencodeAdapterOptions> = {}) {
  let clock = 1_000;
  const adapter = createOpencodeAdapter({ model: "claude-sonnet-5", now: () => clock, ...over });
  const run = (m: unknown) => adapter.translate(m);
  run(ev("session.created", { info: { id: "root", directory: WS } }));
  return { adapter, run, tick: (ms: number) => (clock += ms) };
}

const tool = (callID: string, name: string, state: Record<string, unknown>, sessionID = "root") =>
  ev("message.part.updated", { part: { type: "tool", tool: name, callID, sessionID, state } });

test("translate: the guard's marked refusal becomes the run's own workspace line, not a row diagnosis", () => {
  const denied: string[] = [];
  const { run } = adapterAt({ onWorkspaceDenied: (r) => denied.push(r) });
  const input = { filePath: "/elsewhere/x.ts", content: "x" };
  assert.deepEqual(run(tool("c1", "write", { status: "running", input, time: { start: 10 } })), [
    { kind: "tool_use", tool: "write", summary: "/elsewhere/x.ts", agentId: "lead", toolUseId: "c1" },
  ]);
  const out = run(
    tool("c1", "write", {
      status: "error",
      input,
      error: `${WORKSPACE_GUARD_MARKER} Refusing to write outside the project. write named /elsewhere/x.ts, …`,
      time: { start: 10, end: 13 },
    }),
  );
  assert.deepEqual(out, [{ kind: "tool_result", agentId: "lead", toolUseId: "c1", ok: false, tool: "write", durationMs: 3 }]);
  assert.deepEqual(denied, ["Refusing to write outside the project. write named /elsewhere/x.ts, …"]);
});

test("translate: a shell call that ran to a non-zero exit is a failed call, with its code and diagnosis", () => {
  const { run } = adapterAt();
  run(tool("b1", "bash", { status: "running", input: { command: `cd ${WS}/api && npm test` }, time: { start: 0 } }));
  const out = run(
    tool("b1", "bash", {
      status: "completed",
      input: { command: `cd ${WS}/api && npm test` },
      output: "> api@1 test\n> node --test\nnot ok 1 - adds\nError: expected 3, got 4\n",
      metadata: { exit: 1 },
      time: { start: 0, end: 900 },
    }),
  );
  assert.deepEqual(out, [
    {
      kind: "tool_result",
      agentId: "lead",
      toolUseId: "b1",
      ok: false,
      tool: "bash",
      durationMs: 900,
      summary: "Error: expected 3, got 4",
      exitCode: 1,
    },
  ]);
});

test("translate: a commit through the shell is a git_commit row, via the shared rewrite", () => {
  const { run } = adapterAt();
  const out = run(tool("g1", "bash", { status: "running", input: { command: 'git commit -m "feat: add api"' } }));
  assert.deepEqual(out, [{ kind: "git_commit", summary: "feat: add api", agentId: "lead", toolUseId: "g1" }]);
});

test("translate: a stopped child settles `stopped`, and its abort error is not a failure line", () => {
  const { adapter, run, tick } = adapterAt();
  run(tool("t1", "task", { status: "running", input: { description: "build api", subagent_type: "general" }, metadata: { sessionId: "kid", model: { modelID: "claude-sonnet-5" } } }));
  const started = run(ev("session.created", { info: { id: "kid", parentID: "root", title: "build api (@general subagent)" } }));
  assert.equal(started[0].kind, "agent_started");
  adapter.markStopped("kid");
  assert.deepEqual(run(ev("session.error", { sessionID: "kid", error: { name: "MessageAbortedError", data: { message: "aborted" } } })), []);
  tick(5_000);
  const [settled] = run(ev("session.idle", { sessionID: "kid" }));
  assert.equal(settled.kind, "agent_settled");
  assert.equal(settled.status, "stopped");
  assert.equal(settled.durationMs, 5_000);
});

test("translate: a root abort the runner did not ask for is an error line and a failed turn", () => {
  const { run } = adapterAt();
  assert.deepEqual(run(ev("session.error", { sessionID: "root", error: { name: "MessageAbortedError", data: { message: "aborted" } } })), [
    { kind: "notice", agentId: "lead", level: "error", detail: "[opencode] MessageAbortedError: aborted" },
  ]);
  const [turn] = run(ev("session.idle", { sessionID: "root" }));
  assert.equal(turn.kind, "turn_ended");
  assert.equal(turn.outcome, "failure");
  assert.equal(turn.error, "MessageAbortedError: aborted");
});

test("translate: a root abort the runner asked for (close) is silent", () => {
  const { adapter, run } = adapterAt();
  adapter.markStopped("root");
  assert.deepEqual(run(ev("session.error", { sessionID: "root", error: { name: "MessageAbortedError" } })), []);
  const [turn] = run(ev("session.idle", { sessionID: "root" }));
  assert.equal(turn.outcome, "success");
});

test("translate: a child abort nobody asked for is a failed settle, not a stop", () => {
  const { run } = adapterAt();
  run(tool("t1", "task", { status: "running", input: { description: "walk" }, metadata: { sessionId: "kid" } }));
  run(ev("session.created", { info: { id: "kid", parentID: "root" } }));
  const [line] = run(ev("session.error", { sessionID: "kid", error: { name: "MessageAbortedError" } }));
  assert.equal(line.kind, "notice");
  assert.equal(line.level, "error");
  const [settled] = run(ev("session.idle", { sessionID: "kid" }));
  assert.equal(settled.status, "failed");
});

test("translate: a child's provider error is an error line and a failed settle", () => {
  const { run } = adapterAt();
  run(tool("t1", "task", { status: "running", input: { description: "walk" }, metadata: { sessionId: "kid" } }));
  run(ev("session.created", { info: { id: "kid", parentID: "root" } }));
  assert.deepEqual(run(ev("session.error", { sessionID: "kid", error: { name: "ProviderAuthError", data: { message: "invalid x-api-key" } } })), [
    { kind: "notice", agentId: "kid", level: "error", detail: "[opencode] ProviderAuthError: invalid x-api-key" },
  ]);
  const [settled] = run(ev("session.idle", { sessionID: "kid" }));
  assert.equal(settled.status, "failed");
});

test("translate: a busy session beats as a model wait, within the same per-agent budget as the deltas", () => {
  const { run, tick } = adapterAt();
  run(tool("t1", "task", { status: "running", input: { description: "walk" }, metadata: { sessionId: "kid" } }));
  run(ev("session.created", { info: { id: "kid", parentID: "root" } }));
  const busy = (sessionID: string) => ev("session.status", { sessionID, status: { type: "busy" } });
  assert.deepEqual(run(busy("root")), [{ kind: "heartbeat", agentId: "lead", waitingOn: "model" }]);
  assert.deepEqual(run(busy("kid")), [{ kind: "heartbeat", agentId: "kid", waitingOn: "model" }]);
  assert.deepEqual(run(ev("message.part.delta", { sessionID: "root" })), []);
  tick(10_000);
  assert.deepEqual(run(busy("root")), [{ kind: "heartbeat", agentId: "lead", waitingOn: "model" }]);
  assert.deepEqual(run(ev("session.status", { sessionID: "root", status: { type: "idle" } })), []);
});

test("translate: the root's errored last message makes its turn a failure", () => {
  const { run } = adapterAt();
  run(ev("message.updated", { info: { id: "m1", role: "assistant", sessionID: "root", modelID: "claude-sonnet-5", tokens: { input: 5, output: 1 }, error: { name: "APIError", data: { message: "boom" } } } }));
  const [turn] = run(ev("session.idle", { sessionID: "root" }));
  assert.equal(turn.kind, "turn_ended");
  assert.equal(turn.outcome, "failure");
  assert.equal(turn.error, "APIError: boom");
});

test("translate: the plan is replaced on the wire and diffed onto the feed; cancelled is deleted", () => {
  const { run } = adapterAt();
  const todos = (list: [string, string][]) =>
    ev("todo.updated", { sessionID: "root", todos: list.map(([content, status]) => ({ content, status, priority: "high" })) });
  assert.equal(run(todos([["api", "pending"], ["web", "pending"]])).length, 2);
  assert.deepEqual(run(todos([["api", "in_progress"], ["web", "pending"]])), [
    { kind: "work_item", agentId: "lead", source: "plan", itemId: "api", title: "api", itemStatus: "in_progress" },
  ]);
  assert.deepEqual(run(todos([["api", "completed"], ["web", "cancelled"]])), [
    { kind: "work_item", agentId: "lead", source: "plan", itemId: "api", title: "api", itemStatus: "completed" },
    { kind: "work_item", agentId: "lead", source: "plan", itemId: "web", title: "web", itemStatus: "deleted" },
  ]);
  // An entry dropped from the list is deleted too — the list IS the plan.
  assert.deepEqual(run(todos([["api", "completed"]])), []);
});

test("translate: the adapter's clock beats for a call that runs silently, once per agent per budget", () => {
  const { run, tick } = adapterAt();
  run(tool("b1", "bash", { status: "running", input: { command: "bal build" }, time: { start: 1_000 } }));
  assert.deepEqual(run({ type: "aep.tick" }), [], "a young call gets no beat");
  tick(12_000);
  assert.deepEqual(run({ type: "aep.tick" }), [
    { kind: "heartbeat", agentId: "lead", waitingOn: "tool", ref: "b1", elapsedMs: 12_000 },
  ]);
  tick(1_000);
  assert.deepEqual(run({ type: "aep.tick" }), [], "the per-agent budget holds");
  run(tool("b1", "bash", { status: "completed", input: { command: "bal build" }, metadata: { exit: 0 }, time: { start: 1_000, end: 14_000 } }));
  tick(20_000);
  assert.deepEqual(run({ type: "aep.tick" }), [], "a settled call is not waited on");
});

test("translate: edit lines are counted from the call's own input, and only when it succeeds", () => {
  const { run } = adapterAt();
  run(tool("t1", "task", { status: "running", input: { description: "fix" }, metadata: { sessionId: "kid" } }));
  run(ev("session.created", { info: { id: "kid", parentID: "root" } }));
  const edit = { filePath: `${WS}/a.ts`, oldString: "a\nb", newString: "a\nb\nc" };
  run(tool("e1", "edit", { status: "running", input: edit }, "kid"));
  run(tool("e1", "edit", { status: "completed", input: edit, time: { start: 1, end: 2 } }, "kid"));
  run(tool("e2", "edit", { status: "running", input: edit }, "kid"));
  run(tool("e2", "edit", { status: "error", input: edit, error: "oldString not found", time: { start: 1, end: 2 } }, "kid"));
  const [settled] = run(ev("session.idle", { sessionID: "kid" }));
  assert.equal(settled.linesAdded, 3);
  assert.equal(settled.linesRemoved, 2);
  assert.equal(settled.toolCount, 2);
});

test("translate: a fan-out call that fails is an error line naming the agent", () => {
  const { run } = adapterAt();
  run(tool("t1", "task", { status: "running", input: { description: "walk the app", subagent_type: "general" } }));
  assert.deepEqual(
    run(tool("t1", "task", { status: "error", input: { description: "walk the app" }, error: "Unknown agent type: scout is not a valid agent type" })),
    [{ kind: "notice", agentId: "lead", level: "error", detail: "[fan-out] walk the app failed: Unknown agent type: scout is not a valid agent type" }],
  );
});

