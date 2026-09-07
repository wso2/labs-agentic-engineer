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
import {
  formatLine,
  formatOutcome,
  formatSubagentReport,
  groupBySubagent,
  mergeOutcomes,
} from "../src/index.js";

test("tool_use: a bare argument keeps its verb, a whole sentence does not gain one", () => {
  // Two sources fill `summary` and they need opposite treatment. A tool call
  // carries an argument…
  assert.equal(formatLine({ kind: "tool_use", tool: "Write", summary: "src/App.tsx" }).text, "$ Write src/App.tsx");
  // …a subagent step carries the SDK's own sentence, which already has the
  // verb. Printing the tool here produced "$ Read Reading src/App.tsx" in a
  // live run — the defect this rule exists to prevent.
  assert.equal(formatLine({ kind: "tool_use", tool: "", summary: "Reading src/App.tsx" }).text, "$ Reading src/App.tsx");
  // Bash never gets its name printed: the `$` prompt already says "shell".
  assert.equal(formatLine({ kind: "tool_use", tool: "Bash", summary: "bal build" }).text, "$ bal build");
  // No summary at all still names something.
  assert.equal(formatLine({ kind: "tool_use", tool: "Glob", summary: "" }).text, "$ Glob");
});

test("activity is header material, never a row", () => {
  // The intent phrase belongs to a collapsed section's status line. Inline it is
  // status text rather than progress — "Running List project root contents"
  // beside `ls` says nothing the command didn't.
  assert.equal(formatLine({ kind: "activity", summary: "Writing todo-api/service.bal" }).text, "");
});

test("a progress_item is row state, never a row", () => {
  // It says an ITEM's status changed — the Validation page repaints that
  // criterion's existing row from it. Printed here as well, one criterion moving
  // through five statuses would be five lines interleaved with the tool calls
  // that caused them, all of it narrating what the row above already shows.
  assert.equal(formatLine({ kind: "progress_item", itemId: "AC-003-a", status: "authoring" }).text, "");
  // Explicit, not a fall-through: the default arm returns "" for this payload
  // only because it happens to carry neither `message` nor `summary`, which is
  // an accident of the shape rather than a decision.
  assert.equal(
    formatLine({ kind: "progress_item", itemId: "AC-003-a", status: "fail", summary: "boom" }).text,
    "",
  );
});

test("a severed call is not reported as a failure", () => {
  // A command that blew its timeout was detached, so it neither succeeded nor
  // failed — it has no outcome yet and may still be running. Calling it
  // "failed" names a defect that did not happen, on the one line a reader
  // consults to tell those apart.
  const severed = formatOutcome({
    kind: "tool_result",
    ok: false,
    status: "severed",
    durationMs: 600_000,
    summary: "the command hit its 600.0s timeout and was detached",
  });
  assert.equal(severed.detail, "severed · the command hit its 600.0s timeout and was detached");
  assert.equal(severed.tone, "warn");
  assert.doesNotMatch(severed.detail, /failed/);

  // A real failure with no exit code still says so.
  assert.equal(
    formatOutcome({ kind: "tool_result", ok: false, summary: "File does not exist" }).detail,
    "failed · File does not exist",
  );
});

test("an outcome speaks only when it carries something the action didn't", () => {
  // A fast success: nothing to add. The next action appearing is the evidence.
  assert.deepEqual(formatOutcome({ kind: "tool_result", ok: true, durationMs: 40 }), {
    detail: "",
    duration: "",
    tone: "muted",
  });
  // Slow enough that the number IS the point — it separates a slow build from a
  // wedged run.
  assert.deepEqual(formatOutcome({ kind: "tool_result", ok: true, durationMs: 10_600 }), {
    detail: "",
    duration: "10.6s",
    tone: "muted",
  });
  // A shell failure: the exit code names THIS command as what broke.
  assert.deepEqual(
    formatOutcome({ kind: "tool_result", ok: false, exitCode: 2, summary: "ls: cannot access 'todo-api/'" }),
    { detail: "exit 2 · ls: cannot access 'todo-api/'", duration: "", tone: "error" },
  );
  // A non-shell tool reports no code. "failed" is exactly as much as is known;
  // fabricating a code would be worse, and silence worse still.
  assert.deepEqual(formatOutcome({ kind: "tool_result", ok: false, summary: "File does not exist" }), {
    detail: "failed · File does not exist",
    duration: "",
    tone: "error",
  });
  // Absent `ok` means "not a tool result", never "succeeded" — and never a
  // failure either.
  assert.equal(formatOutcome({ kind: "tool_use" }).detail, "");
});

test("tool_result renders standalone for a surface that cannot rewrite a printed row", () => {
  assert.equal(
    formatLine({ kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, summary: "compilation failed", durationMs: 25_100 }).text,
    "✗ Bash exit 1 · compilation failed 25.1s",
  );
  assert.equal(formatLine({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 42_000 }).text, "↳ Bash 42.0s");
  assert.equal(formatLine({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 185_000 }).text, "↳ Bash 3m5s");
  // A fast success is deliberately silent — an empty text, which renderers drop.
  assert.equal(formatLine({ kind: "tool_result", tool: "Read", ok: true, durationMs: 40 }).text, "");
});

test("a fan-out call's result reads as a whole subagent's report, not one call's outcome", () => {
  const { text, tone } = formatLine({
    kind: "tool_result",
    tool: "Agent",
    ok: true,
    status: "completed",
    summary: "todo-api",
    durationMs: 209_158,
    toolCount: 19,
    linesAdded: 553,
    linesRemoved: 4,
  });
  assert.equal(text, "▪ todo-api completed · 3m29s · 19 tools · +553/−4 lines");
  assert.equal(tone, "success");
});

test("a subagent report omits figures the SDK did not give, rather than showing zeroes", () => {
  assert.equal(formatSubagentReport({ label: "todo-webapp", status: "failed", durationMs: 353_000, toolCount: 31 }), "todo-webapp failed · 5m53s · 31 tools");
  // Running: the step count and the phrase, which is the whole point of a
  // collapsed section.
  assert.equal(
    formatSubagentReport({ label: "todo-api", status: "running", toolCount: 12, activity: "Writing todo-api/service.bal" }),
    "todo-api running · 12 tools · Writing todo-api/service.bal",
  );
  assert.equal(formatSubagentReport({ label: "todo-api", status: "running" }), "todo-api running");
});

test("tones are semantic, never a theme token — a TUI imports this too", () => {
  for (const line of [
    { kind: "tool_use", tool: "Bash", summary: "ls" },
    { kind: "result", status: "success" },
    { kind: "log", level: "warn", summary: "careful" },
  ]) {
    const { tone } = formatLine(line);
    assert.ok(
      ["default", "muted", "info", "success", "warn", "error"].includes(tone),
      `tone ${tone} is not a semantic weight`,
    );
  }
});

test("phase ids render through the friendly-label map, with a raw-id fallback", () => {
  assert.equal(formatLine({ kind: "phase", phase: "workspace_ready" }).text, "▸ Workspace ready");
  assert.equal(formatLine({ kind: "phase", phase: "agent_started" }).text, "▸ agent_started");
  // BFF bootstrap summaries (capacity detail) beat the phase-id label.
  assert.equal(
    formatLine({
      kind: "phase",
      phase: "runner_unschedulable",
      summary: "No capacity to schedule the runner: 0/5 nodes are available: 5 Too many pods.",
    }).text,
    "▸ No capacity to schedule the runner: 0/5 nodes are available: 5 Too many pods.",
  );
});

test("grouping: interleaved subagents each get ONE section, placed where they first spoke", () => {
  const rows = groupBySubagent([
    { kind: "tool_use", emitter: "main" },
    { kind: "tool_use", emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api" },
    { kind: "tool_use", emitter: "subagent", emitterId: "a2", emitterLabel: "todo-webapp" },
    { kind: "tool_use", emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api" },
  ]);

  assert.deepEqual(rows.map((r) => r.kind), ["line", "group", "group"]);
  const first = rows[1];
  assert.ok(first?.kind === "group");
  assert.equal(first.group.id, "a1");
  // Both a1 lines land together despite a2 interleaving between them.
  assert.equal(first.group.lines.length, 2);
});

test("grouping: a subagent line with no id stays ungrouped rather than being filed under a guess", () => {
  const rows = groupBySubagent([{ kind: "tool_use", emitter: "subagent" }]);
  assert.deepEqual(rows.map((r) => r.kind), ["line"]);
});

test("grouping: a label arriving after the first line is still adopted", () => {
  const rows = groupBySubagent([
    { kind: "tool_use", emitter: "subagent", emitterId: "a1" },
    { kind: "tool_use", emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api" },
  ]);
  const g = rows[0];
  assert.ok(g?.kind === "group");
  assert.equal(g.group.label, "todo-api");
  assert.equal(g.group.report.label, "todo-api");
});

test("grouping: a section's own report is its header, and its narration feeds it", () => {
  const rows = groupBySubagent([
    { kind: "activity", summary: "Writing todo-api/service.bal", toolCount: 12, emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api" },
    { kind: "tool_use", tool: "Write", summary: "todo-api/service.bal", toolUseId: "t1", emitter: "subagent", emitterId: "a1" },
    { kind: "tool_result", tool: "Agent", ok: true, status: "completed", summary: "ignored", toolUseId: "a1", durationMs: 209_158, toolCount: 19, emitter: "subagent", emitterId: "a1" },
  ]);
  const g = rows[0];
  assert.ok(g?.kind === "group");
  // Only the STEP is a row: the activity and the closing report are header.
  assert.equal(g.group.lines.length, 1);
  assert.equal(g.group.lines[0]?.tool, "Write");
  // The label stays the group's — the report's own summary field is the
  // runner's copy of it and must not overwrite a better one seen earlier.
  assert.equal(formatSubagentReport(g.group.report), "todo-api completed · 3m29s · 19 tools");
});

test("grouping: a running section reports its latest phrase and step count", () => {
  const rows = groupBySubagent([
    { kind: "activity", summary: "Reading todo-api/types.bal", toolCount: 3, emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api" },
    { kind: "activity", summary: "Writing todo-api/service.bal", toolCount: 12, emitter: "subagent", emitterId: "a1" },
  ]);
  const g = rows[0];
  assert.ok(g?.kind === "group");
  assert.equal(formatSubagentReport(g.group.report), "todo-api running · 12 tools · Writing todo-api/service.bal");
});

test("merging: an outcome folds onto the action it answers, wherever it arrived", () => {
  const rows = mergeOutcomes([
    { kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1" },
    { kind: "tool_use", tool: "Read", summary: "db.bal", toolUseId: "t2" },
    { kind: "tool_result", tool: "Read", ok: true, toolUseId: "t2" },
    { kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, toolUseId: "t1" },
  ]);
  assert.equal(rows.length, 2);
  assert.equal(rows[0]?.outcome?.exitCode, 1);
  assert.equal(rows[1]?.outcome?.ok, true);
});

test("merging: an orphan outcome keeps its own row — a failure must never vanish", () => {
  const rows = mergeOutcomes([{ kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, toolUseId: "gone" }]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0]?.outcome, undefined);
  // …and having kept its row, it does not then swallow a later result.
  const twice = mergeOutcomes([
    { kind: "tool_result", tool: "Bash", ok: false, toolUseId: "x" },
    { kind: "tool_result", tool: "Bash", ok: true, toolUseId: "x" },
  ]);
  assert.equal(twice.length, 2);
});

test("merging: a Bash call rewritten by kind still takes its own outcome", () => {
  // git_commit / git_push / gh_action are the same tool call under a different
  // kind, so they are actions and claim their id like one.
  const rows = mergeOutcomes([
    { kind: "git_commit", sha: "9f3a2c1", toolUseId: "t1" },
    { kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, toolUseId: "t1" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0]?.outcome?.exitCode, 1);
});
