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
import { readFileSync } from "node:fs";
import {
  formatAgentReport,
  formatAgentStatus,
  formatEvent,
  formatHeartbeat,
  isSilentKind,
  type RunEventView,
} from "../src/index.js";

const LEAD = { agentId: "lead" };

test("every kind the contract declares renders — a blank row is the defect this file exists to catch", () => {
  // A kind with no case falls to the default arm and renders "", which on a log
  // surface is indistinguishable from a run that stopped talking. The contract's
  // RunEventKind is a closed set, so it can be walked.
  const kinds = [
    "run_started",
    "agent_started",
    "agent_progress",
    "agent_settled",
    "tool_use",
    "tool_result",
    "task_started",
    "task_settled",
    "git_commit",
    "git_push",
    "gh_action",
    "work_item",
    "heartbeat",
    "notice",
    "turn_ended",
    "run_settled",
  ];
  // Only the STATE kinds may be silent, plus `tool_result` on a fast success
  // (an outcome speaks only when it carries something the action did not) and
  // `turn_ended` on a turn that went fine. Everything else must earn its row
  // from a bare payload. `task_started` joined the silent set because it is a
  // DUPLICATE rather than a state — the `tool_use` that launched the background
  // command already printed it. See isSilentKind.
  const mayBeSilent = new Set([
    "agent_progress",
    "work_item",
    "heartbeat",
    "task_started",
    "tool_result",
    "turn_ended",
  ]);
  for (const kind of kinds) {
    const { text } = formatEvent({ kind, agentId: "lead" });
    if (mayBeSilent.has(kind)) continue;
    assert.notEqual(text, "", `${kind} renders a blank row`);
  }
});

test("run_started states what this attempt is, on which runtime and model", () => {
  assert.equal(
    formatEvent({
      ...LEAD,
      kind: "run_started",
      taskKind: "implementation",
      runtime: "claude-code",
      model: "claude-opus-4",
    }).text,
    "▸ implementation run · claude-code · claude-opus-4",
  );
});

test("agent_started is the section header, and names the agent the parent named it", () => {
  assert.equal(
    formatEvent({
      agentId: "ag_1",
      kind: "agent_started",
      label: "Implement todo-api service (issue #3)",
      role: "coder",
      depth: 1,
    }).text,
    "⑂ Implement todo-api service (issue #3)",
  );
  // No label: the reusable role is the next-best name, then the runtime's own id.
  assert.equal(formatEvent({ agentId: "ag_2", kind: "agent_started", role: "reviewer" }).text, "⑂ reviewer");
  assert.equal(formatEvent({ agentId: "ag_3", kind: "agent_started" }).text, "⑂ ag_3");
  // The lead has no parent to have labelled it, so it names itself.
  assert.equal(formatEvent({ ...LEAD, kind: "agent_started" }).text, "⑂ lead agent");
});

test("a BACKGROUND agent says so, because it changes what its parent is doing", () => {
  // Background is the ordinary case for a builder — fan-out is backgrounded by
  // default (ADR-0014) and the skill, not the platform, decides the shape — so
  // the header says which agents the lead is NOT blocked inside, which is what
  // makes an interleaved feed readable. (An older runtime forwarded none of a
  // backgrounded subagent's messages, so the platform forced fan-out into the
  // foreground. That hook is deleted and this word is no longer a fault report.)
  assert.equal(
    formatEvent({ agentId: "ag_1", kind: "agent_started", label: "todo-api", background: true }).text,
    "⑂ todo-api · background",
  );
  // An explicit false means the parent is blocked inside this one until it
  // returns — which its own row already shows as "waiting on", so the header
  // does not repeat it.
  assert.equal(
    formatEvent({ agentId: "ag_1", kind: "agent_started", label: "todo-api", background: false }).text,
    "⑂ todo-api",
  );
});

test("agent_settled carries the report — the one thing the flat form could not show before", () => {
  const settled = formatEvent({
    agentId: "ag_1",
    kind: "agent_settled",
    label: "todo-api",
    status: "completed",
    durationMs: 209_158,
    toolCount: 19,
    linesAdded: 553,
    linesRemoved: 4,
    tokens: 41_200,
    report: "Implemented the service, its types and a smoke test. `bal build` is clean.",
  });
  assert.equal(settled.text, "▪ todo-api completed · 3m29s · 19 tools · +553/−4 lines · 41.2k tokens");
  // The prose is a separate field, not appended: it is a paragraph and the row
  // is a line. A surface renders it under the header.
  assert.equal(
    settled.report,
    "Implemented the service, its types and a smoke test. `bal build` is clean.",
  );
  assert.equal(settled.tone, "success");
});

test("a stopped agent is a cancellation, never a failure", () => {
  // The work was taken away rather than going wrong. Toning it as an error would
  // report a run someone stopped on purpose as broken.
  assert.equal(formatEvent({ agentId: "a", kind: "agent_settled", label: "x", status: "stopped" }).tone, "warn");
  assert.equal(formatEvent({ agentId: "a", kind: "agent_settled", label: "x", status: "failed" }).tone, "error");
});

test("a report omits figures the runtime did not give, rather than showing zeroes", () => {
  assert.equal(
    formatAgentReport({ id: "a1", label: "todo-webapp", status: "failed", durationMs: 353_000, toolCount: 31 }),
    "todo-webapp failed · 5m53s · 31 tools",
  );
  // Running: the step count and the phrase, which is the whole point of a
  // collapsed section.
  assert.equal(
    formatAgentStatus({ id: "a1", label: "todo-api", status: "running", toolCount: 12, activity: "Writing todo-api/service.bal" }),
    "running · 12 tools · Writing todo-api/service.bal",
  );
  assert.equal(formatAgentReport({ id: "a1", label: "todo-api", status: "running" }), "todo-api running");
});

test("the three state kinds are silent, and say so out loud", () => {
  // Each repaints something a surface already drew. A `work_item` printed as a
  // row would narrate one criterion moving through five statuses as five lines
  // interleaved with the tool calls that caused them.
  for (const kind of ["agent_progress", "work_item", "heartbeat"]) {
    assert.ok(isSilentKind(kind), `${kind} should be silent`);
    assert.equal(formatEvent({ ...LEAD, kind, phrase: "x", itemId: "AC-001-a", itemStatus: "pass" }).text, "");
  }
  assert.ok(!isSilentKind("tool_use"));
});

test("a heartbeat explains the silence for the ONE surface that shows it", () => {
  // Never a row — a quiet run would become a wall of "still waiting". It is the
  // agent's live status instead.
  assert.equal(
    formatHeartbeat({ ...LEAD, kind: "heartbeat", waitingOn: "tool", ref: "t1", elapsedMs: 130_000 }),
    "waiting on a tool call for 2m10s",
  );
  assert.equal(formatHeartbeat({ ...LEAD, kind: "heartbeat", waitingOn: "model" }), "waiting on the model");
});

test("a fan-out is an agent, not a tool call — and this renderer knows no tool names", () => {
  // The contract's rule: "Runtime names never appear … no consumer branches on
  // it: fan-out is `agent_started`, not a `tool_result` whose tool is called
  // Agent." A spawn's whole representation is the started/settled pair, which
  // carries the label, role, depth, background and the agent's own report — all
  // of it absent from a tool call.
  const started = formatEvent({
    agentId: "a1",
    kind: "agent_started",
    label: "todo-api",
    role: "coder",
    depth: 1,
    background: true,
  });
  assert.match(started.text, /todo-api/);

  // No producer emits a call for a spawn: the claude adapter emits none (the
  // started event IS the row) and the v1 lift turns a fan-out result into
  // `agent_settled`. So this module does not special-case one — an event whose
  // tool happens to be named `Agent` is rendered as the ordinary call it claims
  // to be, rather than being silently swallowed on the strength of its name.
  assert.equal(
    formatEvent({ ...LEAD, kind: "tool_use", tool: "Agent", summary: "todo-api" }).text,
    "$ Agent todo-api",
  );

  // The structural half of the same rule, and the one that catches a
  // reintroduction: a runtime's tool names have no business in this file.
  const src = readFileSync(new URL("../src/event.ts", import.meta.url), "utf8");
  assert.doesNotMatch(src, /"Agent"|"Task"/);
});

test("tool_use and tool_result read exactly as they do on the v1 feed", () => {
  // The two envelopes are the same run watched through different pipes; a wording
  // difference here is a defect a reader would blame on the agent.
  assert.equal(formatEvent({ ...LEAD, kind: "tool_use", tool: "Write", summary: "src/App.tsx" }).text, "$ Write src/App.tsx");
  assert.equal(formatEvent({ ...LEAD, kind: "tool_use", tool: "Bash", summary: "bal build" }).text, "$ bal build");
  assert.equal(formatEvent({ ...LEAD, kind: "tool_use", tool: "Bash", command: "bal test" }).text, "$ bal test");
  assert.equal(
    formatEvent({ ...LEAD, kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, summary: "compilation failed", durationMs: 25_100 }).text,
    "✗ Bash exit 1 · compilation failed 25.1s",
  );
  // A fast success is deliberately silent — an action always earns a line, an
  // outcome only when it carries something the action didn't.
  assert.equal(formatEvent({ ...LEAD, kind: "tool_result", tool: "Read", ok: true, durationMs: 40 }).text, "");
});

test("one backgrounded command is ONE action row and one outcome row, never three", () => {
  // The wire carries three events for one launch: the `tool_use` with the
  // command, a `task_started` with the same command plus the runtime's id, and
  // a `task_settled`. Printing all three made 47 launches 141 rows in one live
  // run (2026-09-08), two of them the same text. The start is folded away — the
  // action was already announced by the call that made it.
  assert.equal(
    formatEvent({ ...LEAD, kind: "task_started", taskId: "bg1", summary: "bal build --offline" }).text,
    "",
  );
  assert.equal(isSilentKind("task_started"), true, "a folded row must be dropped, not printed blank");

  // The settle is the one row, and it names the COMMAND rather than the id: the
  // runtime's notification carries neither, so the producer repeats the start's
  // summary. Without it the row reads `background bg1 · failed`, which cannot be
  // matched by eye to anything else on the feed.
  const settled = formatEvent({ ...LEAD, kind: "task_settled", taskId: "bg1", summary: "bal build --offline", status: "failed", outputBytes: 20_480 });
  assert.equal(settled.text, "✗ background bal build --offline · failed · 20.0 KB output");
  assert.equal(settled.tone, "error");
  // A producer that gave no summary still leaves the id rather than nothing.
  assert.equal(
    formatEvent({ ...LEAD, kind: "task_settled", taskId: "bg1", status: "completed" }).text,
    "↳ background bg1 · completed",
  );
});

test("notice names WHICH condition in words, and never renders a bare code as nothing", () => {
  assert.equal(
    formatEvent({ ...LEAD, kind: "notice", level: "warn", code: "api_retry", detail: "overloaded_error, retrying in 4s" }).text,
    "⚠ retrying after a model error · overloaded_error, retrying in 4s",
  );
  assert.equal(formatEvent({ ...LEAD, kind: "notice", level: "info", code: "compaction" }).text, "ℹ context compacted");
  // A code from a producer one version ahead still prints, as itself.
  assert.equal(formatEvent({ ...LEAD, kind: "notice", level: "error", code: "quota_exhausted" }).text, "✗ quota_exhausted");
});

test("notice: a code-less notice is its own prose, with nothing prefixed", () => {
  // The other notice shape, and it stays: a scrubbed line the run itself
  // printed, or the watchdog's sentence. No closed set could name it, so the
  // prose stands alone — announcing "notice" first would narrate the envelope
  // rather than the news.
  assert.equal(
    formatEvent({ ...LEAD, kind: "notice", level: "info", detail: "installing playwright browsers" }).text,
    "ℹ installing playwright browsers",
  );
  // Neither half is still a row: an empty one is indistinguishable from a run
  // that stopped talking.
  assert.equal(formatEvent({ ...LEAD, kind: "notice", level: "warn" }).text, "⚠ notice");
});

test("notice: the dark-zone codes keep the exact wording v1's phase labels had", () => {
  // The regression this pins. v1 had a `phase` kind whose eight ids were worded
  // HERE; v2 dropped the kind and the copy briefly moved into the producers —
  // the runner wrote "[workspace] provisioning" itself, the BFF wrote its six
  // sentences in Go. Two producers wording one fact is exactly what this package
  // exists to prevent, so the eight came back as `notice` codes and the strings
  // below are carried over verbatim: a reader must not see the copy change just
  // because the envelope did.
  const expected: Array<[string, string]> = [
    ["runner_scheduling", "Waiting for a runner to be scheduled…"],
    ["runner_unschedulable", "No capacity to schedule the runner on the cluster…"],
    ["runner_pulling_image", "Pulling the agent image…"],
    ["runner_image_pull_backoff", "Still pulling the agent image (retrying)…"],
    ["runner_config_error", "Waiting on runner configuration and secrets…"],
    ["runner_starting", "Starting the agent…"],
    ["workspace_provisioning", "Setting up the workspace…"],
    ["workspace_ready", "Workspace ready"],
  ];
  for (const [code, label] of expected) {
    assert.equal(formatEvent({ ...LEAD, kind: "notice", level: "info", code }).text, `ℹ ${label}`);
  }
});

test("notice: a dark-zone code carrying detail says both, condition first", () => {
  // The platform adds the scheduler's own explanation to an unschedulable
  // notice. The label says WHICH condition and the detail says why this time;
  // dropping either loses half the answer.
  assert.equal(
    formatEvent({
      ...LEAD,
      kind: "notice",
      level: "warn",
      code: "runner_unschedulable",
      detail: "0/3 nodes are available: Insufficient cpu",
    }).text,
    "⚠ No capacity to schedule the runner on the cluster… · 0/3 nodes are available: Insufficient cpu",
  );
});

test("turn_ended speaks only when the turn went badly", () => {
  // A turn ending is not news — the next action appearing proves it. A run can
  // lose several turns and still succeed, and those are the rows explaining why
  // it took so long.
  assert.equal(formatEvent({ ...LEAD, kind: "turn_ended", outcome: "success" }).text, "");
  assert.equal(
    formatEvent({ ...LEAD, kind: "turn_ended", outcome: "failure", error: "context length exceeded" }).text,
    "✗ turn failure — context length exceeded",
  );
});

test("run_settled is the run's terminal line, and cancelled is not failed", () => {
  assert.deepEqual(formatEvent({ ...LEAD, kind: "run_settled", outcome: "success" }), {
    text: "■ run success",
    tone: "success",
  });
  assert.equal(formatEvent({ ...LEAD, kind: "run_settled", outcome: "failure", error: "agent exited 1" }).text, "■ run failure — agent exited 1");
  assert.equal(formatEvent({ ...LEAD, kind: "run_settled", outcome: "cancelled" }).tone, "warn");
});

test("git and gh effects read as the effects they are", () => {
  assert.equal(formatEvent({ ...LEAD, kind: "git_commit", sha: "9f3a2c1de", files: 6 }).text, "✓ commit 9f3a2c1 · 6 files");
  assert.equal(formatEvent({ ...LEAD, kind: "git_push", branch: "aep/m1-c1" }).text, "↑ push aep/m1-c1");
  assert.equal(formatEvent({ ...LEAD, kind: "gh_action", summary: "pr create" }).text, "⚙ pr create");
  assert.equal(formatEvent({ ...LEAD, kind: "gh_action", command: "gh pr merge", ok: false }).tone, "error");
});

test("tones stay semantic, never a theme token — a TUI imports this too", () => {
  const events: RunEventView[] = [
    { ...LEAD, kind: "run_started" },
    { ...LEAD, kind: "agent_settled", status: "failed" },
    { ...LEAD, kind: "notice", level: "warn", code: "rate_limit" },
    { ...LEAD, kind: "run_settled", outcome: "cancelled" },
  ];
  for (const e of events) {
    const { tone } = formatEvent(e);
    assert.ok(
      ["default", "muted", "info", "success", "warn", "error"].includes(tone),
      `tone ${tone} is not a semantic weight`,
    );
  }
});
