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
import { createSdkTranslator } from "./from-sdk.js";

// A fresh translator per test: it is per-run state by design, so sharing one
// would let an earlier test's subagent labels and pending calls leak forward.
const progressFromSdkMessage = (m: unknown) => createSdkTranslator()(m);

test("from-sdk: system init → agent_started phase", () => {
  const events = progressFromSdkMessage({ type: "system", subtype: "init" });
  assert.equal(events.length, 1);
  assert.deepEqual(events[0], { kind: "phase", phase: "agent_started" });
});

test("from-sdk: result success → success result", () => {
  const events = progressFromSdkMessage({ type: "result", subtype: "success" });
  assert.equal(events.length, 1);
  assert.deepEqual(events[0], { kind: "result", status: "success" });
});

test("from-sdk: result success with usage → result carries token usage + model", () => {
  const events = progressFromSdkMessage({
    type: "result",
    subtype: "success",
    usage: {
      input_tokens: 12000,
      output_tokens: 3400,
      cache_read_input_tokens: 500000,
      cache_creation_input_tokens: 80000,
    },
    modelUsage: { "claude-sonnet-5": { input_tokens: 12000 } },
  });
  assert.equal(events.length, 1);
  assert.deepEqual(events[0], {
    kind: "result",
    status: "success",
    usage: {
      inputTokens: 12000,
      outputTokens: 3400,
      cacheReadTokens: 500000,
      cacheCreationTokens: 80000,
      model: "claude-sonnet-5",
    },
  });
});

test("from-sdk: result usage with multiple models → model '' (mixed)", () => {
  const events = progressFromSdkMessage({
    type: "result",
    subtype: "success",
    usage: { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
    modelUsage: { "claude-sonnet-5": {}, "claude-opus-4-8": {} },
  });
  assert.equal((events[0] as { usage: { model: string } }).usage.model, "");
});

test("from-sdk: result usage carries per-model slices for aep-api to price (#291)", () => {
  const events = progressFromSdkMessage({
    type: "result",
    subtype: "success",
    usage: {
      input_tokens: 110,
      output_tokens: 55,
      cache_read_input_tokens: 1000,
      cache_creation_input_tokens: 200,
    },
    modelUsage: {
      "claude-sonnet-5": {
        inputTokens: 100, outputTokens: 50,
        cacheReadInputTokens: 1000, cacheCreationInputTokens: 200,
        canonicalModel: "claude-sonnet-5",
      },
      "claude-haiku-4-5-20251001": {
        inputTokens: 10, outputTokens: 5,
        cacheReadInputTokens: 0, cacheCreationInputTokens: 0,
        canonicalModel: "claude-haiku-4-5",
      },
    },
  });
  const usage = (events[0] as unknown as { usage: Record<string, unknown> }).usage;
  // Mixed models: the single aggregate model stays "", but the split survives.
  assert.equal(usage.model, "");
  assert.deepEqual(usage.models, [
    {
      model: "claude-sonnet-5",
      inputTokens: 100, outputTokens: 50,
      cacheReadTokens: 1000, cacheCreationTokens: 200,
    },
    {
      model: "claude-haiku-4-5",
      inputTokens: 10, outputTokens: 5,
      cacheReadTokens: 0, cacheCreationTokens: 0,
    },
  ]);
});

test("from-sdk: versioned modelUsage keys collapse onto their canonical model", () => {
  const events = progressFromSdkMessage({
    type: "result",
    subtype: "success",
    usage: { input_tokens: 30, output_tokens: 3, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
    modelUsage: {
      "claude-sonnet-5-20260101": { inputTokens: 10, outputTokens: 1, canonicalModel: "claude-sonnet-5" },
      "claude-sonnet-5-20260201": { inputTokens: 20, outputTokens: 2, canonicalModel: "claude-sonnet-5" },
    },
  });
  const usage = (events[0] as unknown as { usage: Record<string, unknown> }).usage;
  // Two dated releases of ONE model are still a single-model run.
  assert.equal(usage.model, "claude-sonnet-5");
  assert.deepEqual(usage.models, [{
    model: "claude-sonnet-5",
    inputTokens: 30, outputTokens: 3,
    cacheReadTokens: 0, cacheCreationTokens: 0,
  }]);
});

test("from-sdk: zero-token modelUsage entries produce no slices and keep the single model", () => {
  const events = progressFromSdkMessage({
    type: "result",
    subtype: "success",
    usage: { input_tokens: 5, output_tokens: 1, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
    modelUsage: { "claude-sonnet-5": {} },
  });
  const usage = (events[0] as unknown as { usage: Record<string, unknown> }).usage;
  assert.equal(usage.model, "claude-sonnet-5");
  assert.equal("models" in usage, false);
});

test("from-sdk: result error → failure result with errors joined", () => {
  const events = progressFromSdkMessage({
    type: "result",
    subtype: "error_during_execution",
    errors: ["failed to push", "auth denied"],
  });
  assert.equal(events.length, 1);
  assert.deepEqual(events[0], {
    kind: "result",
    status: "failure",
    error: "failed to push, auth denied",
  });
});

test("from-sdk: assistant Bash git commit -m → git_commit event with message", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      role: "assistant",
      content: [
        {
          type: "tool_use",
          name: "Bash",
          input: { command: "git commit -m \"Add JWT validation\"" },
        },
      ],
    },
  });
  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "git_commit");
  assert.equal((events[0] as { summary: string }).summary, "Add JWT validation");
});

test("from-sdk: assistant Bash git push → git_push event with branch", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      content: [
        {
          type: "tool_use",
          name: "Bash",
          input: { command: "git push origin task/jwt-9a3" },
        },
      ],
    },
  });
  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "git_push");
  assert.equal((events[0] as { branch: string }).branch, "task/jwt-9a3");
});

test("from-sdk: assistant Bash gh → gh_action event", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      content: [
        {
          type: "tool_use",
          name: "Bash",
          input: { command: "gh pr ready 18" },
        },
      ],
    },
  });
  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "gh_action");
  assert.equal((events[0] as { command: string }).command, "gh pr ready 18");
});

test("from-sdk: assistant Bash other → generic tool_use", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      content: [
        {
          type: "tool_use",
          name: "Bash",
          input: { command: "go test ./..." },
        },
      ],
    },
  });
  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "tool_use");
  assert.equal((events[0] as { tool: string }).tool, "Bash");
  assert.equal((events[0] as { summary: string }).summary, "go test ./...");
});

test("from-sdk: assistant Edit → tool_use with file_path summary", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      content: [
        {
          type: "tool_use",
          name: "Edit",
          input: { file_path: "services/auth/jwt.go" },
        },
      ],
    },
  });
  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "tool_use");
  assert.equal((events[0] as { summary: string }).summary, "services/auth/jwt.go");
});

test("from-sdk: assistant with multiple tool_use blocks emits in order", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      content: [
        { type: "text", text: "first I'll read..." },
        { type: "tool_use", name: "Read", input: { file_path: "a.go" } },
        { type: "tool_use", name: "Edit", input: { file_path: "b.go" } },
      ],
    },
  });
  assert.equal(events.length, 2);
  assert.equal((events[0] as { summary: string }).summary, "a.go");
  assert.equal((events[1] as { summary: string }).summary, "b.go");
});

test("from-sdk: a main-agent message carries no emitter attribution", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_use", name: "Read", input: { file_path: "a.go" } }] },
  });
  assert.equal(events.length, 1);
  assert.equal((events[0] as { emitter?: string }).emitter, undefined);
});

test("from-sdk: a forwarded subagent message is attributed to the subagent", () => {
  const events = progressFromSdkMessage({
    type: "assistant",
    parent_tool_use_id: "toolu_01Task",
    message: {
      content: [
        { type: "tool_use", name: "Read", input: { file_path: "a.go" } },
        { type: "tool_use", name: "Bash", input: { command: "git commit -m \"x\"" } },
      ],
    },
  });
  assert.equal(events.length, 2);
  // Every event derived from the message inherits the attribution, including
  // the ones bashEvents rewrites into a different kind.
  assert.equal((events[0] as { emitter?: string }).emitter, "subagent");
  assert.equal(events[1].kind, "git_commit");
  assert.equal((events[1] as { emitter?: string }).emitter, "subagent");
});

// --- Per-subagent identity + tool outcomes (one translator across messages) ---

/** The fan-out call the main agent makes, as the SDK reports it. */
function fanOut(id: string, description: string, tool = "Agent"): unknown {
  return {
    type: "assistant",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_use", id, name: tool, input: { description, prompt: "…" } }] },
  };
}

function subagentCall(parent: string, id: string, file: string): unknown {
  return {
    type: "assistant",
    parent_tool_use_id: parent,
    message: { content: [{ type: "tool_use", id, name: "Write", input: { file_path: file } }] },
  };
}

function toolResult(parent: string | null, id: string, opts?: { error?: string }): unknown {
  return {
    type: "user",
    parent_tool_use_id: parent,
    message: {
      content: [{
        type: "tool_result",
        tool_use_id: id,
        ...(opts?.error ? { is_error: true, content: opts.error } : { content: "ok" }),
      }],
    },
  };
}

test("from-sdk: concurrent subagents are told apart by id and by the label the main agent gave them", () => {
  const translate = createSdkTranslator();
  translate(fanOut("toolu_api", "Implement todo-api Ballerina service (issue #3)"));
  translate(fanOut("toolu_web", "Implement todo-webapp React SPA (issue #4)"));

  // Interleaved, exactly as a real fan-out arrives.
  const api = translate(subagentCall("toolu_api", "toolu_w1", "todo-api/service.bal"))[0];
  const web = translate(subagentCall("toolu_web", "toolu_w2", "todo-webapp/src/App.tsx"))[0];

  assert.equal(api.emitter, "subagent");
  assert.equal(api.emitterId, "toolu_api");
  assert.equal(api.emitterLabel, "Implement todo-api Ballerina service (issue #3)");
  assert.equal(web.emitterId, "toolu_web");
  assert.equal(web.emitterLabel, "Implement todo-webapp React SPA (issue #4)");
});

test("from-sdk: `Task` names the same fan-out tool as `Agent`", () => {
  const translate = createSdkTranslator();
  translate(fanOut("toolu_t", "Implement the user service", "Task"));
  assert.equal(translate(subagentCall("toolu_t", "toolu_w1", "a.go"))[0].emitterLabel, "Implement the user service");
});

test("from-sdk: an unlabelled fan-out still attributes by id", () => {
  const translate = createSdkTranslator();
  // No description in the input — the id is all there is, and it must survive.
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_x", name: "Agent", input: { prompt: "…" } }] },
  });
  const e = translate(subagentCall("toolu_x", "toolu_w1", "a.go"))[0];
  assert.equal(e.emitterId, "toolu_x");
  assert.equal(e.emitterLabel, undefined);
});

test("from-sdk: main-agent lines carry no identity fields at all", () => {
  const translate = createSdkTranslator();
  const e = translate({
    type: "assistant",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_use", id: "toolu_r", name: "Read", input: { file_path: "a.go" } }] },
  })[0];
  assert.equal(e.emitter, undefined);
  assert.equal(e.emitterId, undefined);
  assert.equal(e.emitterLabel, undefined);
  // The call id is still stamped — that is what pairs it with its outcome.
  assert.equal(e.toolUseId, "toolu_r");
});

test("from-sdk: a tool_result pairs with its call, carrying tool name and measured duration", () => {
  let clock = 1_000;
  const translate = createSdkTranslator({ now: () => clock });
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_b1", name: "Bash", input: { command: "bal build" } }] },
  });
  clock += 42_000; // a cold `bal build` — the invisible time this event exists to show
  const [res] = translate(toolResult(null, "toolu_b1"));

  assert.equal(res.kind, "tool_result");
  assert.equal((res as { ok: boolean }).ok, true);
  assert.equal(res.toolUseId, "toolu_b1");
  assert.equal((res as { tool?: string }).tool, "Bash");
  assert.equal((res as { durationMs?: number }).durationMs, 42_000);
  // A success carries no output — see ToolResultEvent.
  assert.equal((res as { summary?: string }).summary, undefined);
});

test("from-sdk: a failed tool call is reported as such, with its error text", () => {
  const translate = createSdkTranslator();
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_r1", name: "Read", input: { file_path: "issues/4.md" } }] },
  });
  const [res] = translate(
    toolResult(null, "toolu_r1", { error: "<tool_use_error>File does not exist.</tool_use_error>" }),
  );
  assert.equal((res as { ok: boolean }).ok, false);
  assert.equal((res as { summary?: string }).summary, "File does not exist.");
  // A non-shell tool reports no exit code. Absence must stay absence — a
  // fabricated 1 would read as a process status the SDK never gave us.
  assert.equal((res as { exitCode?: number }).exitCode, undefined);
});

test("from-sdk: a shell failure carries its exit code, parsed from the SDK's own first line", () => {
  const translate = createSdkTranslator();
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_b1", name: "Bash", input: { command: "ls todo-api/" } }] },
  });
  const [res] = translate(
    toolResult(null, "toolu_b1", { error: "Exit code 2\nls: cannot access 'todo-api/': No such file or directory" }),
  );
  assert.equal((res as { exitCode?: number }).exitCode, 2);
  assert.equal((res as { summary?: string }).summary, "ls: cannot access 'todo-api/': No such file or directory");
});

test("from-sdk: a diagnosis that ends in a colon takes the line it was introducing", () => {
  // Real payload from a live run. On its own the first line is a heading, and
  // reporting a heading as the cause explains nothing.
  const translate = createSdkTranslator();
  const [res] = translate(
    toolResult(null, "toolu_r1", {
      error:
        "<tool_use_error>InputValidationError: Read failed due to the following issue:\n" +
        "The parameter `offset` type is expected as `number` but provided as `string`</tool_use_error>",
    }),
  );
  assert.equal(
    (res as { summary?: string }).summary,
    "InputValidationError: Read failed due to the following issue: The parameter `offset` type is expected as `number` but provided as `string`",
  );
});

test("from-sdk: a build failure reports the compiler's first ERROR, not the chatter above it", () => {
  // Real `bal build` output: nine lines of dependency pulls before the first
  // ERROR, with progress bars that rewrite their own line via \r. "The line
  // after the exit code" would report "Compiling source", which diagnoses
  // nothing. Captured from .aep-playground/runs/2026-08-01T09-01-46Z.
  const translate = createSdkTranslator();
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_b1", name: "Bash", input: { command: "bal build" } }] },
  });
  const output = [
    "Exit code 1",
    "Compiling source",
    "\taep/todo_api:0.1.0",
    "ballerinax/cdc:1.4.0 [central…]   0% [>\rballerinax/cdc:1.4.0 [central…] 100% [>",
    "\tballerinax/cdc:1.4.0 pulled from central successfully",
    "ERROR [db.bal:(2:1,2:42)] cannot resolve module 'ballerinax/postgresql.driver as _'",
    "ERROR [db.bal:(14:9,14:58)] undefined parameter 'user'",
    "error: compilation contains errors",
  ].join("\n");
  const [res] = translate(toolResult(null, "toolu_b1", { error: output }));
  assert.equal((res as { exitCode?: number }).exitCode, 1);
  assert.equal(
    (res as { summary?: string }).summary,
    "ERROR [db.bal:(2:1,2:42)] cannot resolve module 'ballerinax/postgresql.driver as _'",
  );
});

test("from-sdk: a result reports its OWN subagent, and array-shaped output is flattened", () => {
  const translate = createSdkTranslator();
  translate(fanOut("toolu_api", "Implement todo-api"));
  translate(subagentCall("toolu_api", "toolu_w1", "a.bal"));
  const [res] = translate({
    type: "user",
    parent_tool_use_id: "toolu_api",
    message: {
      content: [{
        type: "tool_result",
        tool_use_id: "toolu_w1",
        is_error: true,
        content: [{ type: "text", text: "compilation failed" }],
      }],
    },
  });
  assert.equal(res.emitterId, "toolu_api");
  assert.equal(res.emitterLabel, "Implement todo-api");
  assert.equal((res as { summary?: string }).summary, "compilation failed");
});

test("from-sdk: a result for a call this translator never saw still reports its outcome", () => {
  // Can happen after the pending-call cap evicts, or on a resumed stream.
  const [res] = createSdkTranslator()(toolResult(null, "toolu_unknown", { error: "boom" }));
  assert.equal(res.kind, "tool_result");
  assert.equal((res as { ok: boolean }).ok, false);
  assert.equal((res as { tool?: string }).tool, undefined);
  assert.equal((res as { durationMs?: number }).durationMs, undefined);
});

test("from-sdk: a tool call is paired once — a duplicate result does not re-measure it", () => {
  let clock = 0;
  const translate = createSdkTranslator({ now: () => clock });
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_b1", name: "Bash", input: { command: "ls" } }] },
  });
  clock += 5_000;
  assert.equal((translate(toolResult(null, "toolu_b1"))[0] as { durationMs?: number }).durationMs, 5_000);
  clock += 60_000;
  assert.equal((translate(toolResult(null, "toolu_b1"))[0] as { durationMs?: number }).durationMs, undefined);
});

test("from-sdk: a Bash call rewritten to git_commit keeps its call id and attribution", () => {
  const translate = createSdkTranslator();
  translate(fanOut("toolu_a", "Implement it"));
  const [e] = translate({
    type: "assistant",
    parent_tool_use_id: "toolu_a",
    message: {
      content: [{ type: "tool_use", id: "toolu_c1", name: "Bash", input: { command: "git commit -m \"x\"" } }],
    },
  });
  assert.equal(e.kind, "git_commit");
  assert.equal(e.toolUseId, "toolu_c1");
  assert.equal(e.emitterLabel, "Implement it");
});

// --- The task_* subagent surface -------------------------------------------
// Current SDK builds leave parent_tool_use_id null and report subagent work as
// system/task_* messages instead. Captured from a real run
// (.aep-playground/runs/2026-08-01T12-29-43Z).

// `tool_use_id` is the call that spawned the task and is the subagent's real
// identity — distinct per task, exactly as a real run reports it.
function taskStarted(taskId: string, description: string, taskType = "local_agent", spawnId?: string): unknown {
  return {
    type: "system",
    subtype: "task_started",
    task_id: taskId,
    tool_use_id: spawnId ?? `toolu_${taskId}`,
    description,
    task_type: taskType,
  };
}

function taskProgress(taskId: string, tool: string, description: string, steps?: number): unknown {
  return {
    type: "system",
    subtype: "task_progress",
    task_id: taskId,
    last_tool_name: tool,
    description,
    ...(steps ? { usage: { tool_uses: steps } } : {}),
  };
}

function taskNotification(taskId: string, status: string, summary?: string): unknown {
  return { type: "system", subtype: "task_notification", task_id: taskId, status, summary };
}

/** The SDK's own closing report for a fanned-out subagent, shape as measured. */
function agentResult(
  spawnId: string,
  status: string,
  totals?: { totalDurationMs?: number; totalToolUseCount?: number; linesAdded?: number; linesRemoved?: number },
): unknown {
  return {
    type: "user",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_result", tool_use_id: spawnId, content: "…" }] },
    tool_use_result: {
      status,
      totalDurationMs: totals?.totalDurationMs,
      totalToolUseCount: totals?.totalToolUseCount,
      toolStats: { linesAdded: totals?.linesAdded, linesRemoved: totals?.linesRemoved },
    },
  };
}

test("from-sdk: a subagent's narration is attributed to it, and is not a row", () => {
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement todo-api Ballerina service (issue #3)"));
  translate(taskStarted("a35", "Implement todo-webapp React SPA (issue #4)"));

  const api = translate(taskProgress("a67", "Bash", "Running bal build", 7))[0];
  const web = translate(taskProgress("a35", "Write", "Writing src/App.tsx"))[0];

  // `activity`, not `tool_use`: the phrase is the live status of a collapsed
  // section, and inline beside the raw command it is status text rather than
  // progress. Renderers format it to nothing.
  assert.equal(api.kind, "activity");
  assert.equal((api as { summary: string }).summary, "Running bal build");
  // The SDK's own step count, so a collapsed section reports what the subagent
  // says it has done rather than what this feed happened to see.
  assert.equal((api as { toolCount?: number }).toolCount, 7);
  assert.equal(api.emitter, "subagent");
  assert.equal(api.emitterId, "toolu_a67");
  assert.equal(api.emitterLabel, "Implement todo-api Ballerina service (issue #3)");
  assert.equal(web.emitterId, "toolu_a35");
  assert.equal(web.emitterLabel, "Implement todo-webapp React SPA (issue #4)");
});

test("from-sdk: a backgrounded command settles on its own result, keeping its owner", () => {
  // A long command the agent puts in the background is reported through the same
  // task_* messages as a fanned-out subagent, and told apart only by task_type.
  // Its OUTCOME, though, comes back as an ordinary tool_result — and measured on
  // a real run that result carries the exit code, the compiler's own error
  // lines, and a parent_tool_use_id naming the subagent that ran it. The
  // notification carries none of that, which is why it is not the settle.
  let clock = 0;
  const translate = createSdkTranslator({ now: () => clock });
  translate(fanOut("toolu_spawn", "Implement todo-api"));
  translate({
    type: "assistant",
    parent_tool_use_id: "toolu_spawn",
    message: { content: [{ type: "tool_use", id: "toolu_b1", name: "Bash", input: { command: "bal build" } }] },
  });
  // task_started emits nothing — the Bash call that backgrounded it already
  // produced a line, and a second one would double every long command. What it
  // does is start the clock.
  assert.deepEqual(translate(taskStarted("b1", "Build the Ballerina project", "local_bash", "toolu_b1")), []);
  clock += 130_000;
  // …and neither does the notification, which would settle it a second time
  // with strictly less to say.
  assert.deepEqual(translate(taskNotification("b1", "failed", "Build the Ballerina project")), []);

  const [done] = translate(
    toolResult("toolu_spawn", "toolu_b1", { error: "Exit code 1\nerror: compilation contains errors" }),
  );
  assert.equal(done.kind, "tool_result");
  assert.equal((done as { ok: boolean }).ok, false);
  assert.equal((done as { exitCode?: number }).exitCode, 1);
  assert.equal((done as { summary?: string }).summary, "error: compilation contains errors");
  assert.equal((done as { durationMs?: number }).durationMs, 130_000, "timed from the task start, not the launch");
  // It belongs to the subagent that ran it, so it merges onto that section's own
  // action row instead of drifting into the main agent's stream.
  assert.equal(done.emitterId, "toolu_spawn");
});

test("from-sdk: a fan-out call's result is the subagent's closing report, with the SDK's own totals", () => {
  // This was once suppressed as a launch acknowledgement. Measured on a real
  // run it is not: the Agent call is message 44 and its result is message 168,
  // carrying totalDurationMs. Suppressing it threw away the only figures a
  // settled section can report.
  let clock = 0;
  const translate = createSdkTranslator({ now: () => clock });
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_spawn", name: "Agent", input: { description: "Implement todo-api" } }] },
  });
  translate(taskStarted("a67", "Implement todo-api", "local_agent", "toolu_spawn"));

  // The notification arrives one message ahead of the result and carries none
  // of its totals, so it is not the settle for a subagent.
  clock += 209_158;
  assert.deepEqual(translate(taskNotification("a67", "completed")), []);

  const [done] = translate(
    agentResult("toolu_spawn", "completed", {
      totalDurationMs: 209_158,
      totalToolUseCount: 19,
      linesAdded: 553,
      linesRemoved: 4,
    }),
  );
  assert.equal(done.kind, "tool_result");
  assert.equal((done as { ok: boolean }).ok, true);
  assert.equal((done as { status?: string }).status, "completed");
  assert.equal((done as { tool?: string }).tool, "Agent");
  assert.equal((done as { durationMs?: number }).durationMs, 209_158);
  assert.equal((done as { toolCount?: number }).toolCount, 19);
  assert.equal((done as { linesAdded?: number }).linesAdded, 553);
  assert.equal((done as { linesRemoved?: number }).linesRemoved, 4);
  // Attributed to the SUBAGENT, so the figures land in its own section rather
  // than beside it as the main agent's.
  assert.equal(done.emitterId, "toolu_spawn");
  assert.equal(done.emitterLabel, "Implement todo-api");
  assert.equal((done as { summary?: string }).summary, "Implement todo-api");
});

test("from-sdk: a subagent that dies is reported as a failure, not merely as silence", () => {
  const translate = createSdkTranslator();
  translate(fanOut("toolu_spawn", "Implement todo-webapp"));
  translate(taskStarted("a67", "Implement todo-webapp", "local_agent", "toolu_spawn"));
  const [done] = translate(agentResult("toolu_spawn", "error_during_execution", { totalToolUseCount: 31 }));
  assert.equal((done as { ok: boolean }).ok, false, "the SDK's own verdict, not our inference from silence");
  assert.equal((done as { status?: string }).status, "error_during_execution");
});

/** A fan-out call that came back as an error block rather than a report. */
function agentFailure(spawnId: string, text: string): unknown {
  return {
    type: "user",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_result", tool_use_id: spawnId, is_error: true, content: text }] },
  };
}

test("from-sdk: a failed subagent's error text is printed, because nothing else carries it", () => {
  // The live regression: a 22-minute subagent failed and the feed said `ok:false`
  // and nothing else. Its transcript is not on this feed and claude.log dies with
  // the pod, so the tool result's own text is the last copy of the reason.
  const translate = createSdkTranslator();
  translate(fanOut("toolu_spawn", "Continue expense-webapp React SPA implementation"));
  const events = translate(
    agentFailure("toolu_spawn", "Error: API request failed after 600s\nrequest timed out\n"),
  );

  assert.equal(events.length, 2, "the row that settles the section, then the reason");
  assert.equal((events[0] as { ok: boolean }).ok, false);
  // The row keeps the LABEL — the console renders it as the section heading.
  assert.equal((events[0] as { summary?: string }).summary, "Continue expense-webapp React SPA implementation");

  const why = events[1];
  assert.equal(why.kind, "log");
  assert.equal((why as { level?: string }).level, "error");
  const line = (why as { summary: string }).summary;
  assert.match(line, /^\[fan-out\] Continue expense-webapp React SPA implementation failed: /);
  assert.match(line, /Error: API request failed after 600s \| request timed out/, "every line, not one sentence");
  // Attributed to the subagent, so the reason lands in its own section.
  assert.equal(why.emitterId, "toolu_spawn");
});

test("from-sdk: a failed subagent with no error text says so, rather than saying nothing", () => {
  // "The SDK gave no reason" is itself the finding — it is what a reader would
  // otherwise spend the next run establishing.
  const translate = createSdkTranslator();
  translate(fanOut("toolu_spawn", "Implement todo-webapp"));
  const events = translate({
    type: "user",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_result", tool_use_id: "toolu_spawn", content: "" }] },
    tool_use_result: { status: "error_during_execution" },
  });

  assert.equal(events.length, 2);
  assert.match(
    (events[1] as { summary: string }).summary,
    /failed: no error text on the tool result \(status error_during_execution\)/,
  );
});

test("from-sdk: a successful subagent adds no failure line", () => {
  const translate = createSdkTranslator();
  translate(fanOut("toolu_spawn", "Implement todo-api"));
  const events = translate(agentResult("toolu_spawn", "completed"));
  assert.equal(events.length, 1);
});

test("from-sdk: a subagent's totals fall back to a measured duration when the SDK reports none", () => {
  let clock = 0;
  const translate = createSdkTranslator({ now: () => clock });
  translate(fanOut("toolu_spawn", "Implement todo-api"));
  translate(taskStarted("a67", "Implement todo-api", "local_agent", "toolu_spawn"));
  clock += 432_000;
  const [done] = translate(agentResult("toolu_spawn", "completed"));
  assert.equal((done as { durationMs?: number }).durationMs, 432_000);
});

test("from-sdk: a subagent narrated on BOTH surfaces is ONE subagent, and the command is the row", () => {
  // Current SDK builds emit task_progress AND forward the same tool_use. Both
  // are kept, in different roles: the forwarded call is the row (it carries the
  // actual command and is what a tool_result can pair with), the narration is
  // header material. Keying both to the SPAWNING CALL id is what stops them
  // reading as two subagents — a live run once showed "[#1] Running Pull the
  // OpenAPI tool" alongside "[#2] bal tool pull openapi", same call.
  const translate = createSdkTranslator();
  translate({
    type: "assistant",
    message: { content: [{ type: "tool_use", id: "toolu_spawn", name: "Agent", input: { description: "Implement todo-api" } }] },
  });
  translate(taskStarted("a67", "Implement todo-api", "local_agent", "toolu_spawn"));

  const viaTask = translate(taskProgress("a67", "Bash", "Pull the OpenAPI tool"));
  const viaForward = translate({
    type: "assistant",
    parent_tool_use_id: "toolu_spawn",
    message: { content: [{ type: "tool_use", id: "toolu_b1", name: "Bash", input: { command: "bal tool pull openapi" } }] },
  });

  assert.equal(viaTask[0]?.kind, "activity");
  assert.equal(viaForward[0]?.kind, "tool_use");
  assert.equal((viaForward[0] as { summary: string }).summary, "bal tool pull openapi");
  assert.equal(viaTask[0]?.emitterId, "toolu_spawn", "both surfaces resolve to ONE identity");
  assert.equal(viaForward[0]?.emitterId, "toolu_spawn");

  // The forwarded call keeps its outcome, name and timing.
  const [res] = translate(toolResult("toolu_spawn", "toolu_b1", { error: "Exit code 1" }));
  assert.equal((res as { ok: boolean }).ok, false);
  assert.equal((res as { tool?: string }).tool, "Bash");
  assert.equal((res as { exitCode?: number }).exitCode, 1);
  assert.equal(res?.emitterId, "toolu_spawn");
});

test("from-sdk: only the run's FIRST init starts the run — a subagent's session does not", () => {
  const translate = createSdkTranslator();
  const init = { type: "system", subtype: "init" };
  assert.deepEqual(translate(init), [{ kind: "phase", phase: "agent_started" }]);
  // Each fanned-out subagent boots its own session; a second "agent started"
  // reads as the run restarting.
  assert.deepEqual(translate(init), []);
  assert.deepEqual(translate(init), []);
});

test("from-sdk: task messages for an unknown task are ignored, not mis-attributed", () => {
  const translate = createSdkTranslator();
  assert.deepEqual(translate(taskProgress("ghost", "Bash", "x")), []);
  assert.deepEqual(translate(taskNotification("ghost", "completed")), []);
  assert.deepEqual(translate({ type: "system", subtype: "task_updated", task_id: "a67" }), []);
});

test("from-sdk: unknown message type → empty", () => {
  assert.deepEqual(progressFromSdkMessage({ type: "user", message: { content: [] } }), []);
  assert.deepEqual(progressFromSdkMessage(null), []);
  assert.deepEqual(progressFromSdkMessage("garbage"), []);
});

test("from-sdk: assistant with no content array → empty", () => {
  assert.deepEqual(
    progressFromSdkMessage({ type: "assistant", message: { content: null } }),
    [],
  );
});

test("from-sdk: long Bash command summary is truncated", () => {
  const longCmd = "echo " + "x".repeat(500);
  const events = progressFromSdkMessage({
    type: "assistant",
    message: {
      content: [{ type: "tool_use", name: "Bash", input: { command: longCmd } }],
    },
  });
  assert.equal(events.length, 1);
  const summary = (events[0] as { summary: string }).summary;
  assert.ok(summary.length <= 200);
  assert.ok(summary.endsWith("…"));
});

// ── Backgrounded fan-out ────────────────────────────────────────────────────
//
// The runner forces fan-out to the foreground (fanout_foreground.ts), so these
// exercise the path a mismatched image can still take. Shapes are copied from a
// measured run: the launch answers in ~2ms with `async_launched` and no totals,
// and the real figures arrive on the notification six minutes later.

/** The acknowledgement a `run_in_background: true` fan-out answers with. */
function asyncLaunchAck(spawnId: string, description: string): unknown {
  return {
    type: "user",
    parent_tool_use_id: null,
    message: { content: [{ type: "tool_result", tool_use_id: spawnId, content: "Async agent launched successfully." }] },
    tool_use_result: { isAsync: true, status: "async_launched", agentId: "acd2f8", description },
  };
}

function agentNotification(taskId: string, spawnId: string, status: string, usage?: Record<string, number>): unknown {
  return {
    type: "system",
    subtype: "task_notification",
    task_id: taskId,
    tool_use_id: spawnId,
    status,
    summary: `Agent "Implement todo-api" ${status}`,
    ...(usage ? { usage } : {}),
  };
}

test("from-sdk: a backgrounded fan-out's launch is not an outcome", () => {
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement todo-api Ballerina service (issue #3)"));

  // Reported as an outcome this said "async_launched · 0.0s" AND, because the
  // status is not "completed", painted a section that went on to succeed red.
  assert.deepEqual(translate(asyncLaunchAck("toolu_a67", "Implement todo-api Ballerina service (issue #3)")), []);
});

test("from-sdk: a backgrounded fan-out settles on its notification", () => {
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement todo-api Ballerina service (issue #3)"));
  translate(asyncLaunchAck("toolu_a67", "Implement todo-api Ballerina service (issue #3)"));

  const events = translate(
    agentNotification("a67", "toolu_a67", "completed", { tool_uses: 33, duration_ms: 381665, total_tokens: 46088 }),
  );

  assert.equal(events.length, 1);
  const settle = events[0] as unknown as Record<string, unknown>;
  assert.equal(settle.kind, "tool_result");
  assert.equal(settle.ok, true);
  assert.equal(settle.status, "completed");
  // Keyed to the SPAWNING call, which is the id the section is grouped under —
  // settling under the task id would create a second, empty section.
  assert.equal(settle.toolUseId, "toolu_a67");
  assert.equal(settle.emitterId, "toolu_a67");
  assert.equal(settle.summary, "Implement todo-api Ballerina service (issue #3)");
  // The SDK's own figures, not ours.
  assert.equal(settle.durationMs, 381665);
  assert.equal(settle.toolCount, 33);
  // `toolStats` rides the tool_result a backgrounded fan-out never produces, so
  // the line deltas are absent rather than reported as zero.
  assert.equal(settle.linesAdded, undefined);
  assert.equal(settle.linesRemoved, undefined);
});

test("from-sdk: a failed backgrounded fan-out settles as failed", () => {
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement todo-api"));
  translate(asyncLaunchAck("toolu_a67", "Implement todo-api"));
  const settle = translate(agentNotification("a67", "toolu_a67", "failed"))[0] as unknown as Record<string, unknown>;
  assert.equal(settle.ok, false);
  assert.equal(settle.status, "failed");
});

test("from-sdk: a FOREGROUND fan-out is not settled twice", () => {
  // The reason the notification is read at all is that a backgrounded fan-out
  // has no other completion signal. A foreground one settles on its own result,
  // and measured it emits no agent notification — but if a build ever sent both,
  // the second must not append a duplicate section report.
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement todo-api"));
  const settled = translate(agentResult("toolu_a67", "completed", { totalDurationMs: 296000, totalToolUseCount: 26 }));
  assert.equal(settled.length, 1);
  assert.equal((settled[0] as { toolCount?: number }).toolCount, 26);

  assert.deepEqual(translate(agentNotification("a67", "toolu_a67", "completed", { tool_uses: 26 })), []);
});

test("from-sdk: a backgrounded COMMAND's notification is still dropped", () => {
  // Unchanged, and the reason is measured: a backgrounded command's own
  // tool_result carries the exit code and the compiler's error lines, and its
  // notification carries neither. Reading notifications for agents must not
  // quietly start double-settling commands.
  const translate = createSdkTranslator();
  translate(taskStarted("b97", "Build the Ballerina project", "local_bash"));
  assert.deepEqual(translate(taskNotification("b97", "failed", "Build the Ballerina project")), []);
});

test("from-sdk: a run that ends with a detached subagent still running is a FAILURE", () => {
  // The regression this exists for: the session finished while both its fan-outs
  // were mid-flight, and the SDK's own `result` said success. On disk that run
  // left one component as a `bal openapi` stub and never created the second, in
  // 159s — indistinguishable from a fast green run unless the feed says otherwise.
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement hello-api"));
  translate(asyncLaunchAck("toolu_a67", "Implement hello-api"));

  const events = translate({ type: "result", subtype: "success" });
  assert.equal(events.length, 1);
  const res = events[0] as { kind: string; status: string; error?: string };
  assert.equal(res.kind, "result");
  assert.equal(res.status, "failure");
  assert.match(String(res.error), /detached subagent/);
  // Names who was abandoned — with the feed dark for a detached subagent, the
  // label off its spawning call is the only identity left to report.
  assert.match(String(res.error), /Implement hello-api/);
});

test("from-sdk: a settled backgrounded fan-out does not poison the run's result", () => {
  // The backstop must key on subagents still UNSETTLED at the end, not on the
  // run having backgrounded one at some point — otherwise a detached-but-finished
  // fan-out would fail an otherwise complete run.
  const translate = createSdkTranslator();
  translate(taskStarted("a67", "Implement hello-api"));
  translate(asyncLaunchAck("toolu_a67", "Implement hello-api"));
  translate(agentNotification("a67", "toolu_a67", "completed"));

  assert.deepEqual(translate({ type: "result", subtype: "success" }), [{ kind: "result", status: "success" }]);
});

// A command that blows its Bash timeout is auto-backgrounded and answers with
// code 0, empty output and is_error false — indistinguishable, from here, from
// a command that ran and succeeded. That cost a validation run its whole cycle
// (#701): the feed called it a success and the per-criterion rows painted
// `Passed` on criteria whose tests never finished.
function bashCall(id: string, command: string): unknown {
  return {
    type: "assistant",
    message: { content: [{ type: "tool_use", id, name: "Bash", input: { command } }] },
  };
}

function bashResult(id: string, result: Record<string, unknown>): unknown {
  return {
    type: "user",
    message: { content: [{ type: "tool_result", tool_use_id: id, content: "" }] },
    tool_use_result: { stdout: "", stderr: "", interrupted: false, ...result },
  };
}

test("from-sdk: a severed command is reported as severed, not as a success", () => {
  const translate = createSdkTranslator();
  translate(bashCall("toolu_run", "npm test --prefix tests/e2e"));
  const out = translate(
    bashResult("toolu_run", { timedOutAfterMs: 600_000, backgroundTaskId: "b3gnow81g" }),
  );
  const result = out.find((e) => e.kind === "tool_result");
  assert.ok(result && result.kind === "tool_result");
  assert.equal(result.ok, false);
  assert.equal(result.status, "severed");
  assert.match(result.summary ?? "", /hit its 600\.0s timeout and was detached/);
  // It says the command is still going, because it is — the point is that this
  // call proved nothing, not that anything failed. @aep/progress-view reads the
  // `severed` status so the feed does not print the word "failed".
  assert.match(result.summary ?? "", /kept running in the background/);
});

test("from-sdk: a severed command settles no outcome", () => {
  // The false-green: whatever folds outcomes into state must not be told this
  // call succeeded. Validation's per-criterion rows are the consumer that was
  // painting `Passed` from it.
  const settled: [string, boolean][] = [];
  const translate = createSdkTranslator({
    onToolOutcome: (id, ok) => settled.push([id, ok]),
  });
  translate(bashCall("toolu_run", "npm test --prefix tests/e2e -- specs/AC-003-a.spec.ts"));
  translate(bashResult("toolu_run", { timedOutAfterMs: 597_462, backgroundTaskId: "b3gnow81g" }));
  assert.deepEqual(settled, []);
});

test("from-sdk: a deliberately backgrounded command settles no outcome either", () => {
  // run_in_background is a launch, not a completion. Nothing was misled about
  // it, so the feed line is left alone — but it cannot settle a result.
  const settled: [string, boolean][] = [];
  const translate = createSdkTranslator({
    onToolOutcome: (id, ok) => settled.push([id, ok]),
  });
  translate(bashCall("toolu_bg", "npm test --prefix tests/e2e"));
  const out = translate(bashResult("toolu_bg", { backgroundTaskId: "b3gnow81g" }));
  assert.deepEqual(settled, []);
  const result = out.find((e) => e.kind === "tool_result");
  assert.ok(result && result.kind === "tool_result");
  assert.equal(result.ok, true, "a launch did not fail");
});

test("from-sdk: an ordinary command still settles its outcome both ways", () => {
  const settled: [string, boolean][] = [];
  const translate = createSdkTranslator({
    onToolOutcome: (id, ok) => settled.push([id, ok]),
  });
  translate(bashCall("toolu_ok", "npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts"));
  translate(bashResult("toolu_ok", { code: 0 }));
  translate(bashCall("toolu_bad", "npm test --prefix tests/e2e -- specs/AC-002-a.spec.ts"));
  translate({
    type: "user",
    message: { content: [{ type: "tool_result", tool_use_id: "toolu_bad", is_error: true, content: "Exit code 1" }] },
  });
  assert.deepEqual(settled, [["toolu_ok", true], ["toolu_bad", false]]);
});
