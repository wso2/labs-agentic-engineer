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

// The loop is replayed against REAL recordings (test/fixtures/, SDK 0.3.247)
// rather than hand-written messages, because the rule under test is a claim
// about what the SDK actually does: that a `result` is one turn ending and not
// the run. A hand-made stream would assert our belief about the surface twice
// instead of once against the surface itself.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import {
  RUN_DEADLINE_ENV,
  consumeRun,
  createRunTerminator,
  runDeadlineFromEnv,
  type RunDeadline,
  type RunTermination,
} from "./run_loop.js";
import { createClaudeAdapter } from "./progress/claude_adapter.js";
import { createRunWatchdog } from "./progress/watchdog.js";
import type { RunEventInput } from "./progress/emitter.js";

// Where in the stream a line was emitted. `"closed"` is the marker that matters:
// it means the source had already ended, which is the whole settle rule.
type Cursor = number | "closed";

interface Emitted {
  at: Cursor;
  event: RunEventInput;
}

function fixture(name: string): unknown[] {
  const file = new URL(`../../test/fixtures/${name}`, import.meta.url);
  return fs
    .readFileSync(file, "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as unknown);
}

/**
 * Replay a recording, stamping every emitted line with the 1-indexed message
 * being read when it went out.
 *
 * The generator's `finally` is what makes `"closed"` honest: it runs when the
 * iterator completes, so a line stamped `"closed"` was emitted after the last
 * message and not merely while reading it.
 */
async function replay(messages: unknown[]): Promise<{ exitCode: number; error?: string; emitted: Emitted[] }> {
  let cursor: Cursor = 0;
  async function* source(): AsyncGenerator<unknown> {
    try {
      for (let i = 0; i < messages.length; i++) {
        cursor = i + 1;
        yield messages[i];
      }
    } finally {
      cursor = "closed";
    }
  }
  const emitted: Emitted[] = [];
  const result = await consumeRun(
    { messages: source(), stopTask: async () => {} },
    {
      translate: createClaudeAdapter({ taskKind: "implementation" }).translate,
      // A real watchdog, with its emit redirected: it is never started here, so
      // it reports nothing — but it still has to survive every event the feed
      // sees, which is a pin worth keeping for free.
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push({ at: cursor, event }),
    },
  );
  return { ...result, emitted };
}

function settlesOf(emitted: Emitted[]): Emitted[] {
  return emitted.filter((e) => e.event.kind === "run_settled");
}

function kindsOf(emitted: Emitted[], kind: RunEventInput["kind"]): RunEventInput[] {
  return emitted.map((e) => e.event).filter((e) => e.kind === kind);
}

/** Everything one agent produced, by the ONE field the whole feed is attributed by. */
function byAgent(emitted: Emitted[], agentId: string): RunEventInput[] {
  return emitted.map((e) => e.event).filter((e) => e.agentId === agentId);
}

// --- the rate-limit filter, as the loop actually wires it -------------------

/** One `rate_limit_event` at a given utilisation, the shape the SDK sends. */
function rateLimit(utilization: number): unknown {
  return {
    type: "rate_limit_event",
    rate_limit_info: { status: "allowed_warning", rateLimitType: "seven_day", utilization },
  };
}

// The filter itself is pinned in diagnostics.test.ts. What is pinned HERE is the
// wiring, and specifically that the reader is built per RUN: a module-level
// factory would remember the last sentence across runs, so a second pod on a
// throttled account would stay silent about a limit it had never reported.
test("consumeRun: an unchanged rate-limit sentence is said once per run", async () => {
  // 0.823 → 0.826 both round to 83%, so they are ONE sentence, not two.
  const stream = [rateLimit(0.82), rateLimit(0.823), rateLimit(0.826), rateLimit(0.834)];

  const first = await replay(stream);
  const said = kindsOf(first.emitted, "notice").filter((e) => e.code === "rate_limit");
  assert.deepEqual(
    said.map((e) => e.detail),
    [
      "[rate-limit] near the limit on the seven_day window at 82%",
      "[rate-limit] near the limit on the seven_day window at 83%",
    ],
    "four events, two distinct sentences — the repeat of 83% is dropped, the move to 83% is not",
  );

  // A fresh run says it again. This is the assertion that fails if the reader is
  // ever hoisted out of `consumeRun`.
  const second = await replay([rateLimit(0.82)]);
  assert.equal(
    kindsOf(second.emitted, "notice").filter((e) => e.code === "rate_limit").length,
    1,
    "a new run has said nothing yet, so its first warning must go out",
  );
});

// --- probe 2: the lead ends its turn while a subagent is still working -------

// The agent ids in probe 2, as the runtime declared them. They are the SDK's
// own task ids, not the tool-call ids the v1 feed used: an agent's identity is
// what `task_started` says it is.
const P2 = {
  agent: "a5730970b14906133",
  bashTask: "bo1ztqo2o",
};

// The G8 pin. Returning on the first `result` (message 16 of 40) killed the pod
// with the subagent mid-flight and 24 messages unread.
test("consumeRun: a lead that ends its turn early does not end the run", async () => {
  const { exitCode, emitted } = await replay(fixture("probe2-lead-ends-early.jsonl"));

  // The agent's own attributed steps arrive at messages 18-25, AFTER the lead's
  // first `result` at 16. They are the work this run existed to do.
  const steps = byAgent(emitted, P2.agent);
  assert.ok(
    steps.some((e) => e.kind === "tool_use" && e.tool === "Bash"),
    "the agent's forwarded tool_use must reach the feed",
  );
  assert.ok(steps.some((e) => e.kind === "tool_result"), "and so must its outcome");
  const afterFirstResult = emitted.filter((e) => e.at !== "closed" && e.at > 16);
  assert.ok(
    afterFirstResult.length >= 3,
    `the stream must keep being read past the first result (got ${afterFirstResult.length} later lines)`,
  );

  // The run's verdict is the LAST turn's, and the pod stayed alive for it.
  assert.equal(exitCode, 0);
});

// v2 puts exactly one settle on the feed and one `turn_ended` per SDK result.
// Two settles would settle the same run twice — the first, here, with a verdict
// the run went on to disprove.
test("consumeRun: two SDK results are two turn_ended and ONE run_settled, at the close", async () => {
  const { emitted } = await replay(fixture("probe2-lead-ends-early.jsonl"));

  assert.equal(kindsOf(emitted, "turn_ended").length, 2, "one per SDK result");
  const settles = settlesOf(emitted);
  assert.equal(settles.length, 1, "exactly one run_settled per run");
  assert.equal(
    settles[0].at,
    "closed",
    "the settle is emitted after the source closed — past the second result (message 37) and past the `stopped` task_notification (message 40)",
  );
  assert.equal(settles[0].event.outcome, "success");
  // Nothing at all is emitted after it: the settle is the terminal line.
  assert.equal(emitted[emitted.length - 1], settles[0]);
});

// The orphan pin. The subagent backgrounded its own `sleep 25`, reported
// "completed" with the command still running, and the session ended — so the
// command was stopped from outside. `stopped` at session end is the only thing
// on this feed that names that, and it needs the task's own id because the tool
// call that started it settled 20 seconds earlier.
test("consumeRun: the orphaned shell command is reported as stopped, owned by the agent that ran it", async () => {
  const { emitted } = await replay(fixture("probe2-lead-ends-early.jsonl"));

  const started = kindsOf(emitted, "task_started");
  const settled = kindsOf(emitted, "task_settled");
  assert.equal(started.length, 1);
  assert.equal(settled.length, 1);
  assert.deepEqual(
    { agentId: started[0].agentId, taskId: started[0].taskId, summary: started[0].summary },
    { agentId: P2.agent, taskId: P2.bashTask, summary: "sleep 25 && echo slow-done" },
  );
  assert.equal(settled[0].status, "stopped", "not `failed` — the work was taken away, it did not go wrong");
  assert.equal(settled[0].agentId, P2.agent);
  // The settle repeats the start's summary: the runtime's notification carries
  // only an id and a status, so without it the row reads `background <id> ·
  // stopped` and matches no command on the feed.
  assert.equal(settled[0].summary, "sleep 25 && echo slow-done");
});

// The agent settles from its notification, which is the only completion signal a
// backgrounded spawn ever gets.
test("consumeRun: the background agent settles with the report it ended on", async () => {
  const { emitted } = await replay(fixture("probe2-lead-ends-early.jsonl"));

  const settled = kindsOf(emitted, "agent_settled");
  assert.equal(settled.length, 1);
  assert.equal(settled[0].agentId, P2.agent);
  assert.equal(settled[0].status, "completed");
  assert.ok((settled[0].report ?? "").length > 0, "an agent that settles without a report leaves a reader nothing");
});

// The notification wakes the lead for a NEW turn, and the SDK announces that
// turn with a second `init`. A second `run_started` would restart the run for
// every consumer that keys off it.
test("consumeRun: a notification-woken turn does not restart the run", async () => {
  const { emitted } = await replay(fixture("probe2-lead-ends-early.jsonl"));
  assert.equal(kindsOf(emitted, "run_started").length, 1, "two inits, one run");
});

// --- probe 1: three background agents in one turn, one of them nesting -------

// Probe 1's agents, again as the runtime declared them.
const P1 = {
  alpha: "ac7f4459090cb6845",
  beta: "a8562a681e0fcee7f",
  gamma: "a6e8d6b9cd56b107f",
  // Spawned BY gamma from inside gamma's own turn — the depth-2 case.
  gammaChild: "a174ace70ca17fabb",
};

// The "background forwarding works now" pin, and the replacement for the
// deleted foreground hook. On SDK 0.3.220 a backgrounded fan-out forwarded
// nothing (ADR-0002 decision 13); on 0.3.247 every one of these agents is
// declared with `background: true` and every one of its steps arrives
// attributed to it.
test("consumeRun: a backgrounded spawn is declared as such and its steps stay attributed", async () => {
  const { emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const started = kindsOf(emitted, "agent_started");
  assert.equal(started.length, 4, "three agents in one turn, plus gamma's child");
  for (const id of [P1.alpha, P1.beta, P1.gamma]) {
    const agent = started.find((e) => e.agentId === id);
    assert.ok(agent, `${id} was never announced`);
    assert.equal(agent.background, true, "these were launched detached, and the feed says so");
    assert.equal(agent.depth, 1);
    assert.equal(agent.parentAgentId, undefined, "a depth-1 agent's parent is the lead by definition");
  }
  // Attributed steps for every backgrounded agent that ran a command.
  for (const id of [P1.alpha, P1.beta]) {
    assert.ok(
      byAgent(emitted, id).some((e) => e.kind === "tool_use" && e.tool === "Bash"),
      `${id}'s own commands are missing from the feed`,
    );
  }
});

// The tree, from the stream alone: gamma's child names gamma because the
// message carrying its spawning tool_use was itself forwarded from gamma.
test("consumeRun: a depth-2 agent names its parent, and is not flattened onto it", async () => {
  const { emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const child = kindsOf(emitted, "agent_started").find((e) => e.agentId === P1.gammaChild);
  assert.ok(child);
  assert.equal(child.depth, 2);
  assert.equal(child.parentAgentId, P1.gamma);
  assert.equal(child.background, false, "gamma ran this one inside its own turn");
  // Its own step is ITS step, not gamma's.
  assert.ok(byAgent(emitted, P1.gammaChild).some((e) => e.kind === "tool_use" && e.tool === "Bash"));
});

// Four agents, four reports. The report is the last thing an agent says and the
// only copy of it that reaches this feed.
test("consumeRun: every agent settles with the report it ended on", async () => {
  const { exitCode, emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const settled = kindsOf(emitted, "agent_settled");
  assert.equal(settled.length, 4);
  assert.deepEqual(
    Object.fromEntries(settled.map((e) => [e.agentId, e.report])),
    {
      [P1.alpha]: "REPORT: alpha done, 1 file",
      [P1.beta]: "REPORT: beta done, 1 file",
      [P1.gamma]: "REPORT: gamma done",
      [P1.gammaChild]: "DONE",
    },
  );
  for (const e of settled) assert.equal(e.status, "completed");

  assert.equal(kindsOf(emitted, "turn_ended").length, 1, "one per SDK result");
  const settles = settlesOf(emitted);
  assert.equal(settles.length, 1, "one settle");
  assert.equal(settles[0].at, "closed");
  assert.equal(exitCode, 0);
});

// alpha wrote one file. `+N/−N lines` was recorded as underivable from this feed
// in ADR-0002, because 0.3.220 forwarded no subagent steps; 0.3.247 does, so the
// Write's own input is on the wire and can be counted.
test("consumeRun: an agent's line deltas are counted from the edits it landed", async () => {
  const { emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const alpha = kindsOf(emitted, "agent_settled").find((e) => e.agentId === P1.alpha);
  assert.equal(alpha?.linesAdded, 1, "alpha.txt is one line");
  assert.equal(alpha?.linesRemoved, undefined);
});

// A fan-out call is not a row and its result is not a settle. Four agents means
// four launch acks and four closing results, and none of them may reach the feed
// as a tool call — the `agent_started` is the row and the notification is the
// settle.
test("consumeRun: a fan-out call and its result put no tool row on the feed", async () => {
  const { emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const fanOutIds = [
    "toolu_01URe6KoUyhHT5gpGZHz6jUF",
    "toolu_01FasL1KkPF6AVGpctAkZ41g",
    "toolu_01PqjNtjEAKRayPK7ELWCuW8",
    "toolu_014WaYc8iqufE5kChDQPNETo",
  ];
  const rows = emitted.map((e) => e.event).filter((e) => e.toolUseId && fanOutIds.includes(e.toolUseId));
  assert.deepEqual(rows, [], "the agent's own lifecycle events are how a fan-out appears");
  // …and no consumer has to know the runtime calls its fan-out tool `Agent`.
  assert.deepEqual(
    emitted.map((e) => e.event).filter((e) => e.tool === "Agent" || e.tool === "Task"),
    [],
  );
});

// The lead in this recording waits on its background agents with TaskOutput,
// which is why that tool is no longer in DISALLOWED_TOOLS. Recorded evidence,
// so a revert of the deny-list change fails here with the reason attached.
test("consumeRun: the lead waits on its backgrounded work with TaskOutput", async () => {
  const { emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const waits = emitted.filter((e) => e.event.kind === "tool_use" && e.event.tool === "TaskOutput");
  assert.equal(waits.length, 3, "one wait per backgrounded agent");
});

// The subscription window's state arrives on its own message type and used to
// be dropped with every unrecognised one.
test("consumeRun: a rate-limit event reaches the feed as a closed-code notice", async () => {
  const { emitted } = await replay(fixture("probe1-background-fanout.jsonl"));

  const notices = kindsOf(emitted, "notice").filter((e) => e.code === "rate_limit");
  assert.equal(notices.length, 1);
  assert.equal(notices[0].level, "info", "this window was merely being consumed");
});

// The v2 pin the design is emphatic about, checked where it actually matters:
// a heartbeat must not keep the watchdog quiet through the stall it describes.
// Neither recording contains a `tool_progress` message — both probes finish
// their calls far inside the interval that produces one — so the tool heartbeat
// is exercised against a synthetic message here and in the adapter's own suite.
test("consumeRun: heartbeats reach the feed and never reset the watchdog's idle clock", async () => {
  const emitted: RunEventInput[] = [];
  const reported: RunEventInput[] = [];
  let clock = 0;
  const watchdog = createRunWatchdog({ idleMs: 120_000, now: () => clock, emit: (e) => reported.push(e) });
  // The adapter shares the test's clock, so the rate limiter is exercised
  // against elapsed time rather than against how fast the loop happens to run.
  const adapter = createClaudeAdapter({ now: () => clock, heartbeatIntervalMs: 10_000 });
  async function* source(): AsyncGenerator<unknown> {
    yield { type: "system", subtype: "init", model: "m" };
    yield { type: "assistant", message: { content: [{ type: "tool_use", id: "t1", name: "Bash", input: { command: "bal build" } }] }, parent_tool_use_id: null };
    // Twelve minutes of "the model is thinking" over one still-running tool,
    // both of them the lead's — so they share the lead's single budget, which
    // is what keeps the cadence at one per ten seconds however many kinds of
    // waiting are stacked up.
    for (let i = 0; i < 36; i++) {
      clock += 10_000;
      yield { type: "system", subtype: "thinking_tokens", estimated_tokens: i * 10 };
      // Inside the same window, so it is dropped rather than doubling up.
      yield { type: "tool_progress", tool_use_id: "t1", tool_name: "Bash", parent_tool_use_id: null, elapsed_time_seconds: i * 20 };
      clock += 10_000;
      yield { type: "tool_progress", tool_use_id: "t1", tool_name: "Bash", parent_tool_use_id: null, elapsed_time_seconds: i * 20 + 10 };
      watchdog.check();
    }
    yield { type: "result", subtype: "success" };
  }

  await consumeRun(
    { messages: source(), stopTask: async () => {} },
    { translate: adapter.translate, watchdog, emit: (e) => emitted.push(e) },
  );

  // Bounded: at most one per agent per ten seconds, and both waits share the
  // lead's one budget, so twelve minutes buys 72 rather than 144.
  const beats = emitted.filter((e) => e.kind === "heartbeat");
  assert.equal(beats.length, 72, "one per ten seconds, not one per message");
  assert.ok(beats.some((e) => e.waitingOn === "model"));
  assert.ok(beats.some((e) => e.waitingOn === "tool" && e.ref === "t1"));
  // …and the stall was still reported, every idle window, through all of them.
  assert.equal(reported.length, 6, "twelve minutes of heartbeats is still twelve minutes without progress");
  assert.match(String(reported[0]?.detail), /waiting on Bash \(bal build\)/);
});

// --- the deadline guard ------------------------------------------------------

/** A deadline the test fires by hand, so no assertion depends on a timer. */
function manualDeadline(budgetMs: number): RunDeadline & { fire: () => void } {
  let fire: () => void = () => {};
  const expiry = new Promise<void>((resolve) => {
    fire = resolve;
  });
  return { expiry, budgetMs, cancel: () => {}, fire: () => fire() };
}

function taskStarted(taskId: string): unknown {
  return {
    type: "system",
    subtype: "task_started",
    task_id: taskId,
    tool_use_id: `toolu_${taskId}`,
    description: taskId,
    task_type: "local_agent",
    is_backgrounded: true,
  };
}

test("consumeRun: a run that never ends is terminated at its deadline", async () => {
  const deadline = manualDeadline(45 * 60_000);
  const stopped: string[] = [];
  const emitted: RunEventInput[] = [];
  // Two tasks start, then the source goes quiet for ever — the shape of a run
  // whose lead has walked off and left its subagents running.
  async function* source(): AsyncGenerator<unknown> {
    yield taskStarted("task-alpha");
    yield taskStarted("task-beta");
    deadline.fire();
    await new Promise<void>(() => {});
  }

  const result = await consumeRun(
    {
      messages: source(),
      stopTask: async (taskId) => {
        stopped.push(taskId);
      },
    },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
      deadline,
    },
  );

  assert.deepEqual(stopped.sort(), ["task-alpha", "task-beta"], "every live task is stopped");

  const errors = emitted.filter((e) => e.kind === "notice" && e.level === "error");
  assert.equal(errors.length, 1, "one user-facing line naming the termination");
  assert.equal(errors[0].code, "terminated", "a closed code, so a consumer can react without parsing prose");
  assert.match(
    String(errors[0].detail),
    /^\[deadline] terminated — the run hit its 45m time budget, stopping 2 running task\(s\) — /,
  );

  const settles = emitted.filter((e) => e.kind === "run_settled");
  assert.equal(settles.length, 1, "the termination is the run's one settle");
  assert.equal(settles[0].outcome, "failure");
  assert.equal(result.exitCode, 1);
  assert.match(result.error ?? "", /time budget/);
});

// A task that has already settled is not a task to stop. The set is closed by
// the task's own notification, which is what every settled task in both
// recordings produces.
test("consumeRun: the deadline stops only the tasks still running", async () => {
  const deadline = manualDeadline(60_000);
  const stopped: string[] = [];
  async function* source(): AsyncGenerator<unknown> {
    yield taskStarted("task-alpha");
    yield taskStarted("task-beta");
    yield {
      type: "system",
      subtype: "task_notification",
      task_id: "task-alpha",
      tool_use_id: "toolu_task-alpha",
      status: "completed",
    };
    deadline.fire();
    await new Promise<void>(() => {});
  }

  await consumeRun(
    { messages: source(), stopTask: async (taskId) => void stopped.push(taskId) },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: () => {},
      deadline,
    },
  );

  assert.deepEqual(stopped, ["task-beta"]);
});

// --- the termination seam ----------------------------------------------------

// The defect this seam removes: the MCP auth policy's `onFatal` emitted a
// `run_settled` of its own while the loop was still reading, so a fatal put TWO
// settles on one run's feed — and every consumer treats a settle as terminal
// (`buildCrew` settles every agent it never heard close on one). The counts here
// are the assertion, not the presence: one settle is what "exactly one" means.
const FATAL: RunTermination = {
  source: "mcp auth",
  why: "this run's platform credential can no longer be renewed: token expired",
  error: "mcp auth: token expired",
};

test("consumeRun: a fatal ends the run with exactly one settle, naming what ended it", async () => {
  const terminator = createRunTerminator();
  // A deadline is configured too and is never fired: with both arms present the
  // fatal still has to be the one that ends the run, and still only once.
  const deadline = manualDeadline(45 * 60_000);
  const stopped: string[] = [];
  const emitted: RunEventInput[] = [];
  // Two subagents running when the credential dies, and a source that then goes
  // quiet for ever — the shape a fatal actually has, since the CLI keeps waiting
  // on a tool call the proxy will never answer.
  async function* source(): AsyncGenerator<unknown> {
    yield taskStarted("task-alpha");
    yield taskStarted("task-beta");
    terminator.terminate(FATAL);
    await new Promise<void>(() => {});
  }

  const result = await consumeRun(
    { messages: source(), stopTask: async (taskId) => void stopped.push(taskId) },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
      deadline,
      terminator,
    },
  );

  const settles = emitted.filter((e) => e.kind === "run_settled");
  assert.equal(settles.length, 1, "one fatal, one settle — the defect was two");
  assert.equal(settles[0].outcome, "failure");
  assert.equal(settles[0].error, FATAL.error, "the settle carries the fatal's own reason");

  // …and a line saying what happened, under the closed code the contract
  // already carries for an ended run.
  const errors = emitted.filter((e) => e.kind === "notice" && e.level === "error");
  assert.equal(errors.length, 1, "one user-facing line naming the termination");
  assert.equal(errors[0].code, "terminated");
  assert.match(
    String(errors[0].detail),
    /^\[mcp auth] terminated — this run's platform credential can no longer be renewed: token expired, stopping 2 running task\(s\) — /,
  );

  // The same bounded stop phase a deadline gets: a fatal leaves the same
  // subagents running, and a pod about to exit must not leave them working.
  assert.deepEqual(stopped.sort(), ["task-alpha", "task-beta"]);
  // The exit code follows the settle, as it does on every other ending.
  assert.equal(result.exitCode, 1);
  assert.equal(result.error, FATAL.error);
});

// `runDeadlineFromEnv` returns undefined unless AEP_RUN_DEADLINE_SECONDS is set,
// which is every playground run and every dispatch made before the dispatcher
// stamped it. There was no race at all in that state, so a seam that only worked
// beside a deadline would have left exactly those runs unable to end early —
// which is the whole population the fatal path has to serve.
test("consumeRun: a fatal ends the run with no deadline configured", async () => {
  const terminator = createRunTerminator();
  const emitted: RunEventInput[] = [];
  async function* source(): AsyncGenerator<unknown> {
    yield { type: "system", subtype: "init", skills: [] };
    terminator.terminate(FATAL);
    await new Promise<void>(() => {});
  }

  const result = await consumeRun(
    { messages: source(), stopTask: async () => {} },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
      terminator,
    },
  );

  const settles = emitted.filter((e) => e.kind === "run_settled");
  assert.equal(settles.length, 1, "exactly one, with no deadline to have produced it");
  assert.equal(settles[0].outcome, "failure");
  assert.equal(settles[0].error, FATAL.error);
  const errors = emitted.filter((e) => e.kind === "notice" && e.level === "error");
  assert.equal(errors.length, 1);
  assert.equal(errors[0].code, "terminated");
  // Nothing was running, so the line says nothing about stopping anything.
  assert.ok(!String(errors[0].detail).includes("stopping"));
  assert.equal(result.exitCode, 1);
  assert.equal(result.error, FATAL.error);
});

// The MCP proxy can fail a last request while the session is closing, after the
// loop has already settled a healthy run. The seam is a promise for this reason:
// tripping one nobody is racing any more changes nothing, where the callback it
// replaced would have written a failure over a run that had ended green.
test("consumeRun: a fatal after the stream closed cannot re-settle the run", async () => {
  const terminator = createRunTerminator();
  const emitted: RunEventInput[] = [];
  async function* source(): AsyncGenerator<unknown> {
    yield { type: "system", subtype: "init", skills: [] };
    yield { type: "result", subtype: "success" };
  }

  const result = await consumeRun(
    { messages: source(), stopTask: async () => {} },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
      terminator,
    },
  );
  terminator.terminate(FATAL);
  // A tick, so a termination that WAS still being awaited would have run.
  await new Promise((resolve) => setImmediate(resolve));

  const settles = emitted.filter((e) => e.kind === "run_settled");
  assert.equal(settles.length, 1);
  assert.equal(settles[0].outcome, "success");
  assert.equal(result.exitCode, 0);
});

// --- the existing endings, unchanged ----------------------------------------

test("consumeRun: a stream that closes without any result is still a failure", async () => {
  const emitted: RunEventInput[] = [];
  async function* source(): AsyncGenerator<unknown> {
    yield { type: "system", subtype: "init", skills: [] };
  }

  const result = await consumeRun(
    { messages: source(), stopTask: async () => {} },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
    },
  );

  assert.equal(result.exitCode, 1);
  assert.equal(result.error, "agent stream ended without result");
  // v1 emitted no terminal line at all here. Under the recorder a run that
  // never settled is indistinguishable from a recording that was lost, so v2
  // settles and says it does not know.
  const settles = emitted.filter((e) => e.kind === "run_settled");
  assert.equal(settles.length, 1);
  assert.equal(settles[0].outcome, "failure");
  assert.equal(settles[0].error, "agent stream ended without result");
});

test("consumeRun: a stream that throws fails the run and says why", async () => {
  const emitted: RunEventInput[] = [];
  const recorded: unknown[] = [];
  async function* source(): AsyncGenerator<unknown> {
    yield { type: "system", subtype: "init", skills: [] };
    throw new Error("connection reset");
  }

  const result = await consumeRun(
    { messages: source(), stopTask: async () => {} },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
      record: (message) => recorded.push(message),
    },
  );

  assert.equal(result.exitCode, 1);
  assert.equal(result.error, "connection reset");
  const settles = emitted.filter((e) => e.kind === "run_settled");
  assert.equal(settles.length, 1);
  assert.equal(settles[0].error, "connection reset");
  assert.ok(recorded.some((m) => (m as Record<string, unknown>).type === "worker_error"));
});

// The preload check is a feed line, so it is the loop's, and it still fires: a
// skill the SDK did not resolve is dropped in silence otherwise.
test("consumeRun: a requested skill the session did not resolve warns", async () => {
  const emitted: RunEventInput[] = [];
  async function* source(): AsyncGenerator<unknown> {
    yield { type: "system", subtype: "init", skills: ["aep"] };
    yield { type: "result", subtype: "success" };
  }

  await consumeRun(
    { messages: source(), stopTask: async () => {} },
    {
      translate: createClaudeAdapter().translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push(event),
      requestedSkills: ["aep", "ballerina"],
    },
  );

  // A notice with no `code`: the closed set names conditions a consumer
  // branches on, and "a skill did not resolve" is prose for a reader.
  const warnings = emitted.filter((e) => e.kind === "notice" && e.level === "warn");
  assert.equal(warnings.length, 1);
  assert.equal(warnings[0].code, undefined);
  assert.match(String(warnings[0].detail), /ballerina/);
});

// --- runDeadlineFromEnv ------------------------------------------------------

// Unset is the state every caller is in today: the dispatcher's Job spec does
// not pass this yet, and nothing about those runs may change because of it.
test("runDeadlineFromEnv: no deadline unless one is asked for", () => {
  assert.equal(runDeadlineFromEnv({}), undefined);
  assert.equal(runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "" }), undefined);
  assert.equal(runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "0" }), undefined);
  assert.equal(runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "-30" }), undefined);
  assert.equal(runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "soon" }), undefined);
});

test("runDeadlineFromEnv: the budget quoted on the feed is the Job's, not the margin's", () => {
  const deadline = runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "2700" }, 0);
  assert.ok(deadline);
  assert.equal(deadline.budgetMs, 2_700_000);
  deadline.cancel();
});

// The margin is capped at half the budget, so a short deadline still leaves a
// window instead of firing the instant the loop starts.
test("runDeadlineFromEnv: a short budget is not swallowed whole by the margin", async () => {
  const deadline = runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "10" }, 0);
  assert.ok(deadline);
  let fired = false;
  deadline.expiry.then(() => {
    fired = true;
  });
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.equal(fired, false, "a 10s budget must not fire immediately");
  deadline.cancel();
});

// Time already spent provisioning is time off the budget: the guard defends the
// POD's deadline, and cloning plus the skills mirror happen before the first
// SDK message.
test("runDeadlineFromEnv: a budget already spent fires at once", async () => {
  const deadline = runDeadlineFromEnv({ [RUN_DEADLINE_ENV]: "60" }, 120_000);
  assert.ok(deadline);
  // The timer is unref'd on purpose — a guard must never be the reason a
  // finished run's process stays alive — so the test holds the event loop open
  // itself, the way the pending `messages.next()` does in a real run.
  const holdOpen = setInterval(() => {}, 10);
  try {
    await deadline.expiry;
  } finally {
    clearInterval(holdOpen);
    deadline.cancel();
  }
});


// --- the input rule: when the loop lets the CLI's stdin close -----------------
//
// Synthetic messages, because the recordings above were all made with a string
// prompt — the SDK had already closed stdin at their first result, so nothing
// in them can show the loop deciding. The shapes are the fixtures', trimmed to
// the fields the adapter and the live-task tracker read.

const INIT = { type: "system", subtype: "init", session_id: "s", tools: [], skills: [] };
const RESULT = { type: "result", subtype: "success", is_error: false, num_turns: 1, usage: { input_tokens: 1, output_tokens: 1 } };
const TASK_STARTED = {
  type: "system", subtype: "task_started", task_id: "t1", tool_use_id: "toolu_1",
  description: "build a component", is_backgrounded: true, spawn_depth: 1, task_type: "local_agent",
};
const TASK_DONE = { type: "system", subtype: "task_notification", task_id: "t1", tool_use_id: "toolu_1", status: "completed", summary: "DONE" };
const LEAD_SPEAKS = {
  type: "assistant", uuid: "u-2", session_id: "s",
  message: { id: "m-2", role: "assistant", model: "m", content: [{ type: "text", text: "the subagent is done" }] },
};

/**
 * Like `replay`, but with an `endInput` spy, and — when `holdOpen` — a source
 * that yields its last message and then stays open until input is ended, which
 * is what a real CLI does: it exits, and the stream closes, only once stdin has.
 */
async function replayWithInput(
  messages: unknown[],
  opts: { holdOpen?: boolean; inputGraceMs?: number } = {},
): Promise<{ exitCode: number; endedAt: Cursor[]; settledAt: Cursor | undefined }> {
  let cursor: Cursor = 0;
  let releaseSource!: () => void;
  const released = new Promise<void>((resolve) => {
    releaseSource = resolve;
  });
  const endedAt: Cursor[] = [];
  async function* source(): AsyncGenerator<unknown> {
    try {
      for (let i = 0; i < messages.length; i++) {
        cursor = i + 1;
        yield messages[i];
      }
      if (opts.holdOpen) await released;
    } finally {
      cursor = "closed";
    }
  }
  const emitted: Emitted[] = [];
  const result = await consumeRun(
    {
      messages: source(),
      stopTask: async () => {},
      endInput: () => {
        endedAt.push(cursor);
        releaseSource();
      },
    },
    {
      translate: createClaudeAdapter({ taskKind: "implementation" }).translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: (event) => emitted.push({ at: cursor, event }),
      ...(opts.inputGraceMs !== undefined ? { inputGraceMs: opts.inputGraceMs } : {}),
    },
  );
  return { exitCode: result.exitCode, endedAt, settledAt: settlesOf(emitted)[0]?.at };
}

test("consumeRun: a result with no task live ends input right there", async () => {
  const { exitCode, endedAt } = await replayWithInput([INIT, RESULT]);
  assert.deepEqual(endedAt, [2], "ended once, at the result");
  assert.equal(exitCode, 0);
});

test("consumeRun: a result while a task is live keeps input open until the turn after it", async () => {
  const { endedAt } = await replayWithInput([INIT, TASK_STARTED, RESULT, TASK_DONE, LEAD_SPEAKS, RESULT]);
  assert.deepEqual(endedAt, [6], "not at the first result (message 3), only at the second");
});

test("consumeRun: the last task settling after a result ends input after the grace, when nothing wakes the lead", async () => {
  const { exitCode, endedAt, settledAt } = await replayWithInput([INIT, TASK_STARTED, RESULT, TASK_DONE], {
    holdOpen: true,
    inputGraceMs: 20,
  });
  assert.deepEqual(endedAt, [4], "ended while still holding the last message — by the timer, not by a message");
  assert.equal(settledAt, "closed", "and the run settled only once the source closed");
  assert.equal(exitCode, 0);
});

test("consumeRun: a woken lead disarms the grace", async () => {
  // The lead speaks 5 ms after the task settles; the grace is 40 ms. Input must
  // end at the second result, not by the timer.
  let cursor = 0;
  const endedAt: number[] = [];
  async function* source(): AsyncGenerator<unknown> {
    for (const m of [INIT, TASK_STARTED, RESULT, TASK_DONE]) {
      cursor++;
      yield m;
    }
    await new Promise((r) => setTimeout(r, 5));
    for (const m of [LEAD_SPEAKS, RESULT]) {
      cursor++;
      yield m;
    }
    await new Promise((r) => setTimeout(r, 60));
  }
  await consumeRun(
    { messages: source(), stopTask: async () => {}, endInput: () => endedAt.push(cursor) },
    {
      translate: createClaudeAdapter({ taskKind: "implementation" }).translate,
      watchdog: createRunWatchdog({ emit: () => {} }),
      emit: () => {},
      inputGraceMs: 40,
    },
  );
  assert.deepEqual(endedAt, [6]);
});

test("consumeRun: on the probe 2 recording, input ends by the grace after the orphaned shell task is stopped", async () => {
  // At the second result (message 37) the lead's backgrounded `sleep` is still
  // live, so input stays open; it is reported stopped at message 40, and with
  // nothing waking the lead the grace ends input while the source still holds.
  const { endedAt, settledAt, exitCode } = await replayWithInput(fixture("probe2-lead-ends-early.jsonl"), {
    holdOpen: true,
    inputGraceMs: 20,
  });
  assert.deepEqual(endedAt, [40]);
  assert.equal(settledAt, "closed");
  assert.equal(exitCode, 0);
});
