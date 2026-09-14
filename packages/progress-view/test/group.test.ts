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
  formatAgentReport,
  groupByAgent,
  mergeOutcomes,
  type AgentSection,
  type RunEventView,
} from "../src/index.js";

/** The one child section of `parent`, asserted to exist. */
function child<E>(parent: AgentSection<E>, at = 0): AgentSection<E> {
  const sections = parent.rows.flatMap((r) => (r.kind === "section" ? [r.section] : []));
  const found = sections[at];
  assert.ok(found, `no section at ${String(at)} under ${parent.id}`);
  return found;
}

const started = (agentId: string, label: string, rest: Partial<RunEventView> = {}): RunEventView => ({
  kind: "agent_started",
  agentId,
  label,
  ...rest,
});

test("grouping: concurrent agents each get ONE section, placed where they first spoke", () => {
  // Their events arrive INTERLEAVED. Read flat, three components' work reads as
  // one agent contradicting itself.
  const lead = groupByAgent([
    { kind: "tool_use", agentId: "lead", tool: "Bash", summary: "git status" },
    started("a1", "todo-api", { depth: 1 }),
    started("a2", "todo-webapp", { depth: 1 }),
    { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal build" },
    { kind: "tool_use", agentId: "a2", tool: "Bash", summary: "npm install" },
    { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal test" },
  ]);

  assert.deepEqual(lead.rows.map((r) => r.kind), ["event", "section", "section"]);
  const api = child(lead, 0);
  assert.equal(api.id, "a1");
  assert.equal(api.agent.label, "todo-api");
  // Both a1 events land together despite a2 interleaving between them.
  assert.equal(api.rows.length, 2);
  assert.equal(child(lead, 1).id, "a2");
});

test("grouping: the tree is DECLARED — parentAgentId nests, depth says how far", () => {
  // v1 inferred one level from the tool call that spawned a subagent, which
  // could not describe this at all: an agent that fans out again.
  const lead = groupByAgent([
    started("a1", "todo-api", { depth: 1 }),
    started("a2", "write the OpenAPI contract", { parentAgentId: "a1", depth: 2 }),
    { kind: "tool_use", agentId: "a2", tool: "Write", summary: "openapi.yaml" },
  ]);

  assert.equal(lead.depth, 0);
  const api = child(lead, 0);
  assert.equal(api.depth, 1);
  assert.equal(api.parentId, "lead");
  // The depth-2 agent is a section INSIDE a1, not a sibling of it.
  assert.equal(lead.rows.filter((r) => r.kind === "section").length, 1);
  const contract = child(api, 0);
  assert.equal(contract.id, "a2");
  assert.equal(contract.depth, 2);
  assert.equal(contract.parentId, "a1");
  assert.equal(contract.rows.length, 1);
});

test("grouping: depth is taken from the event, not counted — a reader who joined late never saw the parents", () => {
  // The whole reason the contract records it rather than deriving it.
  const lead = groupByAgent([started("a9", "deep worker", { parentAgentId: "gone", depth: 3 })]);
  const orphanParent = child(lead, 0);
  assert.equal(orphanParent.id, "gone");
  assert.equal(child(orphanParent, 0).depth, 3);
});

test("grouping: an agent that never announced itself is named by its id, not dropped", () => {
  // A feed joined mid-run, or one the platform admits has gaps. Filing its work
  // under a guessed owner would be worse than a poor name.
  const lead = groupByAgent([{ kind: "tool_use", agentId: "ag_7", tool: "Bash", summary: "ls" }]);
  const unknown = child(lead, 0);
  assert.equal(unknown.id, "ag_7");
  assert.equal(unknown.agent.label, "ag_7");
  assert.equal(unknown.agent.status, "running");
  assert.equal(unknown.rows.length, 1);
});

test("grouping: an agent_started arriving late adopts the label without reviving a settled agent", () => {
  const lead = groupByAgent([
    { kind: "agent_settled", agentId: "a1", status: "completed", toolCount: 19 },
    started("a1", "todo-api"),
  ]);
  const api = child(lead, 0);
  assert.equal(api.agent.label, "todo-api");
  assert.equal(api.agent.status, "completed");
  assert.equal(api.agent.toolCount, 19);
});

test("grouping: a section's header is its own start and settle, never rows inside it", () => {
  const lead = groupByAgent([
    started("a1", "todo-api", { depth: 1, role: "coder" }),
    { kind: "agent_progress", agentId: "a1", phrase: "Writing todo-api/service.bal" },
    { kind: "tool_use", agentId: "a1", tool: "Write", summary: "todo-api/service.bal", toolUseId: "t1" },
    {
      kind: "agent_settled",
      agentId: "a1",
      status: "completed",
      durationMs: 209_158,
      toolCount: 19,
      report: "Service, types and a smoke test. bal build is clean.",
    },
  ]);
  const api = child(lead, 0);
  // Only the STEP is a row: the start, the phrase and the closing report are header.
  assert.equal(api.rows.length, 1);
  assert.equal(api.rows[0]?.kind === "event" && api.rows[0].event.tool, "Write");
  assert.equal(formatAgentReport(api.agent), "todo-api completed · 3m29s · 19 tools");
  assert.equal(api.agent.role, "coder");
  // The report is the point: a spawned agent's transcript dies with the pod, so
  // this is the only copy of what it says it did.
  assert.equal(api.agent.report, "Service, types and a smoke test. bal build is clean.");
  // …and the live phrase is gone, so a settled agent does not claim to still be
  // writing a file.
  assert.equal(api.agent.activity, undefined);
});

test("grouping: a running section reports its latest phrase, and a heartbeat displaces a stale one", () => {
  const lead = groupByAgent([
    started("a1", "todo-api"),
    { kind: "agent_progress", agentId: "a1", phrase: "Reading todo-api/types.bal" },
    { kind: "agent_progress", agentId: "a1", phrase: "Writing todo-api/service.bal" },
  ]);
  assert.equal(
    formatAgentReport(child(lead, 0).agent),
    "todo-api running · Writing todo-api/service.bal",
  );

  // A heartbeat only fires when nothing else is happening, so it is fresher than
  // the phrase it displaces — and bare silence is what it exists to explain.
  const waiting = groupByAgent([
    started("a1", "todo-api"),
    { kind: "agent_progress", agentId: "a1", phrase: "Writing todo-api/service.bal" },
    { kind: "heartbeat", agentId: "a1", waitingOn: "tool", ref: "t1", elapsedMs: 130_000 },
  ]);
  assert.equal(
    formatAgentReport(child(waiting, 0).agent),
    "todo-api running · waiting on a tool call for 2m10s",
  );
});

test("grouping: work_item never becomes a row — the surface holding the item repaints it", () => {
  const lead = groupByAgent([
    { kind: "work_item", agentId: "lead", itemId: "AC-001-a", itemStatus: "running", source: "criterion" },
    { kind: "work_item", agentId: "lead", itemId: "AC-001-a", itemStatus: "pass", source: "criterion" },
  ]);
  assert.equal(lead.rows.length, 0);
});

test("grouping: the lead's own life is the top level, header and all", () => {
  const lead = groupByAgent([
    { kind: "run_started", agentId: "lead", taskKind: "implementation" },
    started("lead", "", { role: "" }),
    { kind: "agent_settled", agentId: "lead", status: "completed", tokens: 812_000, report: "Opened PR #12." },
  ]);
  // The lead is not a section inside anything — everything the run did is its.
  assert.equal(lead.depth, 0);
  assert.equal(lead.parentId, undefined);
  assert.equal(lead.agent.label, "lead agent");
  assert.equal(lead.agent.report, "Opened PR #12.");
  // run_started is a row; the lead's start and settle are its header.
  assert.equal(lead.rows.length, 1);
});

test("merging: an outcome folds onto the action it answers, wherever it arrived", () => {
  const rows = mergeOutcomes([
    { kind: "tool_use", toolUseId: "t1" },
    { kind: "tool_use", toolUseId: "t2" },
    { kind: "tool_result", toolUseId: "t2" },
    { kind: "tool_result", toolUseId: "t1" },
  ]);
  assert.equal(rows.length, 2);
  assert.equal(rows[0]?.line.toolUseId, "t1");
  assert.equal(rows[0]?.outcome?.toolUseId, "t1");
  assert.equal(rows[1]?.outcome?.toolUseId, "t2");
});

test("merging: an orphan outcome keeps its own row — a failure must never vanish", () => {
  const rows = mergeOutcomes([{ kind: "tool_result", toolUseId: "gone" }]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0]?.outcome, undefined);
  // …and having kept its row, it does not then swallow a later result.
  const twice = mergeOutcomes([
    { kind: "tool_result", toolUseId: "x" },
    { kind: "tool_result", toolUseId: "x" },
  ]);
  assert.equal(twice.length, 2);
});

test("merging: a Bash call rewritten by kind still takes its own outcome", () => {
  // git_commit / git_push / gh_action are the same tool call under a different
  // kind, so they are actions and claim their id like one.
  const rows = mergeOutcomes([
    { kind: "git_commit", toolUseId: "t1" },
    { kind: "tool_result", toolUseId: "t1" },
  ]);
  assert.equal(rows.length, 1);
  assert.ok(rows[0]?.outcome);
});

test("grouping: an agent that claims itself as its parent still renders, at the top level", () => {
  // A producer this build does not understand must not be able to wedge the
  // surface reading it: resolving the ancestry naively recurses forever here,
  // and then draws a section inside itself.
  const lead = groupByAgent([
    started("a1", "self-parenting", { parentAgentId: "a1", depth: 1 }),
    { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "ls" },
  ]);
  const self = child(lead, 0);
  assert.equal(self.parentId, "lead");
  assert.equal(self.rows.length, 1);

  // …and a two-agent loop is broken at the point it closes, rather than making
  // two sections each contain the other.
  const looped = groupByAgent([
    started("a1", "one", { parentAgentId: "a2" }),
    started("a2", "two", { parentAgentId: "a1" }),
  ]);
  assert.equal(looped.rows.filter((r) => r.kind === "section").length, 1);
});

// The reported bug: one backgrounded `gh issue comment` drew TWO rows on a live
// feed — the action, and a settle repeating the same 130-character command line
// beside it. `task_started` is silent and shares the action's `toolUseId`, so the
// risk in folding is that it claims the action's place and the settle lands on a
// row nobody can see.
test("mergeOutcomes: a backgrounded command is ONE row that gains its outcome", () => {
  const rows = mergeOutcomes([
    { kind: "gh_action", toolUseId: "t1", command: "gh issue comment 3 --body \"Starting\"" },
    { kind: "task_started", toolUseId: "t1", taskId: "bg1", summary: "gh issue comment 3 --body \"Starting\"" },
    { kind: "task_settled", toolUseId: "t1", taskId: "bg1", summary: "gh issue comment 3 --body \"Starting\"", status: "completed" },
  ]);

  assert.equal(rows.length, 1, "one command, one row");
  assert.equal(rows[0]!.line.kind, "gh_action", "and it is the ACTION that survives, not the settle");
  assert.equal(rows[0]!.outcome?.kind, "task_settled", "with the ending folded onto it");
  assert.equal(rows[0]!.backgrounded, true, "marked async, so a row with no outcome yet reads as detached");
});

// Before the settle arrives the row still has to say it is detached — otherwise
// a four-minute build is indistinguishable from a call being waited on.
test("mergeOutcomes: a still-running background command is marked before it settles", () => {
  const rows = mergeOutcomes([
    { kind: "tool_use", toolUseId: "t1", summary: "bal build" },
    { kind: "task_started", toolUseId: "t1", taskId: "bg1", summary: "bal build" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0]!.backgrounded, true);
  assert.equal(rows[0]!.outcome, undefined, "nothing has come back yet, and the row must not pretend otherwise");
});

// A settle whose action never reached this surface still has to draw: dropping
// it would hide the ending of a command a reader can see nothing else about.
test("mergeOutcomes: an orphaned settle is still a row of its own", () => {
  const rows = mergeOutcomes([
    { kind: "task_settled", toolUseId: "gone", taskId: "bg1", summary: "pnpm dev:mock", status: "stopped" },
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0]!.line.kind, "task_settled");
});
