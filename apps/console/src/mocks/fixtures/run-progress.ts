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

import type { components } from "../../generated/aep-api";

type RunCycleView = components["schemas"]["RunCycleView"];
type RunEvent = components["schemas"]["RunEvent"];
type RunProgressLine = components["schemas"]["RunProgressLine"];

// Per-cycle agent output for the run progress stream, in the v2 envelope.
//
// The point of the fixture is to exercise every shape the feed has to render, in
// mock mode, without a cluster:
//   - a lead agent that fans out, with its own steps interleaved between the
//     agents it spawned;
//   - a BACKGROUND agent, so the header that says so is on screen;
//   - a DEPTH-2 agent — one spawned agent fanning out again — which the v1
//     envelope could not describe at all;
//   - a REPORT on every settle, which no v1 surface could show;
//   - the silent kinds (agent_progress, heartbeat, work_item), which must paint
//     state and never a blank row — including the LEAD'S OWN PLAN, whose entries
//     fold onto the agents that own them rather than printing five rows apiece.
//
// The version (build) feed below still replays the v1 RunProgressLine envelope:
// it has not moved, and one fixture emitting both is what keeps that honest.

// When this fixture's run "started", and how fast its clock runs.
//
// Both are anchored to the REPLAY rather than to a date in the past, because the
// timestamps are read and not merely displayed: the crew view measures how long
// an agent has been quiet against them, and the timeline draws its lanes from
// them. A fixed date made every mock run read as months stale, and a fixture
// clock running faster than the replay made a healthy agent read as stalled
// while its next line was still queued.
//
// So: the run starts when the mock loads, and one event is one second — which is
// exactly the pace the handler sends them at (MOCK_LINE_MS).
const T0 = Date.now();
const iso = (offsetSec: number) => new Date(T0 + offsetSec * 1000).toISOString();

/** Terminal run states — only a terminal run settles the progress stream. */
export function isTerminalRunState(state: string): boolean {
  return ["succeeded", "failed", "cancelled"].includes(state);
}

const LEAD = "lead";

// One validation attempt, criterion by criterion, in the order a run produces it.
// The ids are the mock oracle's (fixtures/validation.ts CATALOGUE) — a status for
// an id the oracle does not carry would paint no row at all.
//
// It covers every shape the page has to render: the plan moving the whole board at
// once, a criterion explored and authored and passed, one that FAILS and is healed
// (the branch a reader most needs to recognise), and one that stays failed. The
// manual criterion (AC-003-b) is deliberately absent — a run never touches it.
const CRITERION_LIFECYCLE: [string, string][] = [
  ["AC-001-a", "planned"],
  ["AC-001-b", "planned"],
  ["AC-002-a", "planned"],
  ["AC-002-b", "planned"],
  ["AC-003-a", "planned"],

  ["AC-001-a", "exploring"],
  ["AC-001-a", "authoring"],
  ["AC-001-a", "running"],
  ["AC-001-a", "pass"],

  ["AC-001-b", "exploring"],
  ["AC-001-b", "authoring"],
  ["AC-001-b", "running"],
  ["AC-001-b", "fail"],
  ["AC-001-b", "healing"],
  ["AC-001-b", "running"],
  ["AC-001-b", "pass"],

  ["AC-002-a", "exploring"],
  ["AC-002-a", "authoring"],
  ["AC-002-a", "running"],
  ["AC-002-a", "pass"],

  ["AC-002-b", "exploring"],
  ["AC-002-b", "authoring"],
  ["AC-002-b", "running"],
  ["AC-002-b", "pass"],

  ["AC-003-a", "exploring"],
  ["AC-003-a", "authoring"],
  ["AC-003-a", "running"],
  ["AC-003-a", "fail"],
];

/**
 * One cycle's replayed v2 feed. Every event is stamped with the agent that
 * produced it; `seq` is monotonic from the attempt's first event, which is what
 * the console dedupes replays on.
 */
export function runCycleEvents(cycle: RunCycleView, startSeq: number): RunEvent[] {
  let seq = startSeq;
  const at = (
    agentId: string,
    rest: Omit<RunEvent, "v" | "seq" | "ts" | "agentId">,
  ): RunEvent => ({
    v: 2,
    seq: seq++,
    ts: iso(seq),
    agentId,
    ...rest,
  });
  const lead = (rest: Omit<RunEvent, "v" | "seq" | "ts" | "agentId">) => at(LEAD, rest);

  if (cycle.kind === "validation") {
    const settled = Boolean(cycle.mergeSha || cycle.endedAt);
    return [
      lead({ kind: "run_started", taskKind: "validation", runtime: "claude-code", model: "claude-opus-4" }),
      lead({ kind: "agent_started", role: "validator", model: "claude-opus-4" }),
      lead({ kind: "tool_use", tool: "Read", summary: "specs/validation/validation-criteria.json", toolUseId: "v0" }),
      lead({ kind: "tool_result", tool: "Read", ok: true, durationMs: 120, toolUseId: "v0" }),
      // The per-criterion story, which is what the Validation page's rows are
      // painted from — and which renders as NO row here, on purpose. Only for an
      // OPEN cycle: the fold ignores a closed one, and a settled attempt's
      // per-criterion truth is its committed report's to tell.
      ...(settled
        ? []
        : CRITERION_LIFECYCLE.map(([itemId, itemStatus]) =>
            lead({ kind: "work_item", source: "criterion", itemId, itemStatus: itemStatus as NonNullable<RunEvent["itemStatus"]> }),
          )),
      lead({ kind: "tool_use", tool: "Bash", summary: "pnpm playwright test", toolUseId: "v1" }),
      lead({ kind: "heartbeat", waitingOn: "tool", ref: "v1", elapsedMs: 42_000 }),
      lead({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 61_400, toolUseId: "v1" }),
      lead({ kind: "git_commit", sha: "7ab41c90ee31d5f0", files: 4 }),
      lead({
        kind: "agent_settled",
        status: "completed",
        durationMs: 184_000,
        toolCount: 22,
        tokens: 96_400,
        report:
          "Checked 5 automated criteria against the deployed system. AC-001-b failed on first run (missing CORS header), was healed, and passed on re-check. AC-003-a is still failing: the delete endpoint returns 200 for an unknown id.",
      }),
      lead({ kind: "run_settled", outcome: "success" }),
    ];
  }

  // A coding cycle: the lead plans, fans out to two component agents at once,
  // and keeps working between their events — the interleaving is the thing the
  // grouping has to survive.
  const api = "ag_todo_api";
  const web = "ag_todo_webapp";
  const contract = "ag_openapi";
  return [
    lead({ kind: "run_started", taskKind: "implementation", runtime: "claude-code", model: "claude-opus-4" }),
    lead({ kind: "agent_started", role: "lead", model: "claude-opus-4" }),

    // The LEAD'S OWN PLAN, which reaches the feed as `work_item {source:
    // "plan"}` rows the crew folds by item. Every shape the fold has to survive
    // is here: an entry handed to an agent that has not been spawned yet, an
    // update carrying a status and no title, and one the lead thought better of.
    lead({ kind: "work_item", source: "plan", itemId: "p1", title: "Read the design and the contract", itemStatus: "in_progress" }),
    lead({ kind: "work_item", source: "plan", itemId: "p2", title: "Implement the shortener API", itemStatus: "pending", ownerAgentId: api }),
    lead({ kind: "work_item", source: "plan", itemId: "p3", title: "Implement the web front end", itemStatus: "pending", ownerAgentId: web }),
    lead({ kind: "work_item", source: "plan", itemId: "p4", title: "Rewrite the deployment script", itemStatus: "pending" }),
    lead({ kind: "work_item", source: "plan", itemId: "p5", title: "Open the pull request", itemStatus: "pending" }),

    lead({ kind: "tool_use", tool: "Bash", summary: "git status", toolUseId: "m1" }),
    lead({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 240, toolUseId: "m1" }),
    // A status-only update: the title has to stick, or the row blanks the moment
    // the lead ticks it off.
    lead({ kind: "work_item", source: "plan", itemId: "p1", itemStatus: "completed" }),
    // Thought better of — a deleted entry is not work any more, so it is not a
    // row, and nothing below it may shift because of it.
    lead({ kind: "work_item", source: "plan", itemId: "p4", itemStatus: "deleted" }),

    // Fan-out. A fan-out call is NOT a tool_use on the feed — `agent_started` is
    // its row, and it says more (label, role, depth, background) than a call
    // ever could. Nothing emits one, so nothing fabricates one here either.
    // `background: false` means the lead is blocked inside this agent until it
    // returns; the webapp agent below carries `true`, the ordinary case.
    at(api, { kind: "agent_started", label: "Implement the shortener API (issue #3)", role: "coder", depth: 1, background: false }),
    at(web, { kind: "agent_started", label: "Implement the web front end (issue #4)", role: "coder", depth: 1, background: true }),

    lead({ kind: "work_item", source: "plan", itemId: "p2", itemStatus: "in_progress", ownerAgentId: api }),
    lead({ kind: "work_item", source: "plan", itemId: "p3", itemStatus: "in_progress", ownerAgentId: web }),
    at(api, { kind: "agent_progress", phrase: "Reading the existing handler" }),
    at(api, { kind: "tool_use", tool: "Write", summary: "src/api/shorten.ts", toolUseId: "a1" }),
    at(web, { kind: "tool_use", tool: "Write", summary: "src/ui/App.tsx", toolUseId: "w1" }),
    at(api, { kind: "tool_result", tool: "Write", ok: true, durationMs: 90, toolUseId: "a1" }),

    // Depth 2: the API agent fans out again for the contract. v1 could not
    // describe this at all — its one level of attribution filed a grandchild's
    // work under its parent.
    at(contract, {
      kind: "agent_started",
      label: "Write the OpenAPI contract",
      role: "contract-author",
      parentAgentId: api,
      depth: 2,
    }),
    at(contract, { kind: "tool_use", tool: "Write", summary: "contracts/shortener.yaml", toolUseId: "c1" }),
    at(contract, { kind: "tool_result", tool: "Write", ok: true, durationMs: 70, toolUseId: "c1" }),
    at(contract, {
      kind: "agent_settled",
      status: "completed",
      durationMs: 41_200,
      toolCount: 6,
      linesAdded: 118,
      tokens: 18_300,
      report: "Wrote contracts/shortener.yaml: POST /links, GET /{code}, and the 404 shape the redirect handler needs.",
    }),

    at(api, { kind: "tool_use", tool: "Bash", summary: "go test ./...", toolUseId: "a2" }),
    lead({ kind: "tool_use", tool: "Read", summary: "README.md", toolUseId: "m2" }),
    at(api, { kind: "tool_result", tool: "Bash", ok: true, summary: "ok  github.com/acme/shortener  0.412s", durationMs: 9_800, toolUseId: "a2" }),
    // Every action gets its outcome. An unanswered call is not a stylistic
    // omission any more: the crew view reads one as a tool still in flight, and
    // a healthy mock run went amber because this line was missing.
    lead({ kind: "tool_result", tool: "Read", ok: true, durationMs: 60, toolUseId: "m2" }),
    at(api, {
      kind: "agent_settled",
      status: "completed",
      durationMs: 209_158,
      toolCount: 19,
      linesAdded: 553,
      linesRemoved: 4,
      tokens: 214_000,
      report: "Implemented POST /links and GET /{code} over the existing store, with the OpenAPI contract written by a sub-agent. `go test ./...` is clean.",
    }),

    // A backgrounded agent forwards none of its own messages, so its section is
    // thin — which is exactly why its header says `background`.
    at(web, { kind: "notice", level: "warn", code: "api_retry", detail: "overloaded_error, retrying in 4s" }),
    at(web, {
      kind: "agent_settled",
      status: "failed",
      durationMs: 353_000,
      toolCount: 31,
      linesAdded: 210,
      linesRemoved: 12,
      tokens: 188_500,
      report: "Scaffolded the front end but could not get `vite build` to pass: the generated client imports a type the contract does not export yet.",
    }),
    lead({ kind: "work_item", source: "plan", itemId: "p2", itemStatus: "completed", ownerAgentId: api }),

    lead({ kind: "task_started", taskId: "bg-build", summary: "pnpm build --filter web" }),
    lead({ kind: "task_settled", taskId: "bg-build", summary: "pnpm build --filter web", status: "completed", outputBytes: 20_480 }),

    // A cycle that ENDED without merging anything was stopped. Without this the
    // feed simply went quiet, and a surface that reads liveness off the events
    // showed a cancelled run's lead as still working — which is the one reading
    // the crew view exists to prevent. The only such cycle in these fixtures is
    // the cancelled earlier session, so `ended and never merged` is exactly
    // "the run was taken away".
    ...(cycle.endedAt && !cycle.mergeSha
      ? [
          lead({ kind: "notice", level: "warn", code: "terminated", detail: "cancelled from the console" }),
          lead({ kind: "agent_settled", status: "stopped", durationMs: 158_000, toolCount: 11 }),
          lead({ kind: "run_settled", outcome: "cancelled" }),
        ]
      : []),
    ...(cycle.mergeSha
      ? [
          lead({ kind: "work_item", source: "plan", itemId: "p5", itemStatus: "completed" }),
          lead({ kind: "git_commit", sha: cycle.mergeSha, files: 6, toolUseId: "m9" }),
          lead({ kind: "git_push", branch: cycle.branch ?? "" }),
          lead({ kind: "gh_action", summary: `pr create — #${String(cycle.prNumber ?? 0)}` }),
          lead({
            kind: "agent_settled",
            status: "completed",
            durationMs: 612_000,
            toolCount: 48,
            tokens: 812_000,
            report: `Opened PR #${String(cycle.prNumber ?? 0)} with the shortener API and the front-end scaffold. The front end does not build yet and is called out in the description.`,
          }),
          lead({ kind: "run_settled", outcome: "success" }),
        ]
      : []),
  ];
}

/**
 * How a run that somebody CANCELLED ends, as the last thing on its feed.
 *
 * A cancellation is a fact about the run, not about a cycle, so the handler
 * appends it after the cycle it interrupted rather than the fixture baking it
 * in. Without it a cancelled run simply went quiet, and a surface that reads
 * liveness off the events showed its lead as still working — which is the one
 * reading the crew view exists to prevent.
 */
export function runCancelledEvents(startSeq: number): RunEvent[] {
  let seq = startSeq;
  const lead = (rest: Omit<RunEvent, "v" | "seq" | "ts" | "agentId">): RunEvent => ({
    v: 2,
    seq: seq++,
    ts: new Date().toISOString(),
    agentId: LEAD,
    ...rest,
  });
  return [
    lead({ kind: "notice", level: "warn", code: "terminated", detail: "cancelled from the console" }),
    // `stopped` is a cancellation and NOT a failure: the work did not go wrong,
    // it was taken away.
    lead({ kind: "agent_settled", status: "stopped", durationMs: 158_000, toolCount: 11 }),
    lead({ kind: "run_settled", outcome: "cancelled" }),
  ];
}

/** A heartbeat for a live run's newest cycle — the silence, explained. */
export function runHeartbeatEvent(seq: number, tick: number): RunEvent {
  return {
    v: 2,
    seq,
    ts: new Date().toISOString(),
    agentId: LEAD,
    kind: "heartbeat",
    waitingOn: "model",
    elapsedMs: tick * 4000,
  };
}

/**
 * One cycle's replayed log in the V1 envelope, for the version (build) feed —
 * which has not moved to v2. Deliberately small: it exists to keep that stream
 * exercised, not to be a second copy of the story above.
 */
export function runCycleLines(
  cycle: RunCycleView,
  index: number,
  startSeq: number,
): RunProgressLine[] {
  let seq = startSeq;
  const line = (
    rest: Omit<RunProgressLine, "cycleId" | "cycleKind" | "cycleIndex" | "schemaVersion" | "emitter" | "seq" | "ts">,
  ): RunProgressLine => ({
    cycleId: cycle.id,
    cycleKind: cycle.kind,
    cycleIndex: index + 1,
    schemaVersion: 1,
    emitter: "main",
    seq: seq++,
    ts: iso(seq * 10),
    ...rest,
  });

  return [
    line({ kind: "phase", phase: "planning" }),
    line({ kind: "log", summary: `Working milestone ${String(index + 1)}'s open issues` }),
    line({ kind: "tool_use", tool: "Bash", summary: "go test ./...", toolUseId: "t1" }),
    line({ kind: "tool_result", tool: "Bash", ok: true, durationMs: 412, toolUseId: "t1" }),
    ...(cycle.mergeSha
      ? [
          line({ kind: "git_commit", sha: cycle.mergeSha, files: 6 }),
          line({ kind: "git_push", branch: cycle.branch ?? "" }),
          line({
            kind: "result",
            status: "succeeded",
            summary: `PR #${String(cycle.prNumber ?? 0)} opened with Resolves`,
          }),
        ]
      : []),
  ];
}
