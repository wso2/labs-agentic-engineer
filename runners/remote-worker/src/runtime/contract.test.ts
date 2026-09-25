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

// The ADAPTER CONTRACT, held against both adapters (ADR-0015; the test
// ADR-0012 left owed): the same kind of session, recorded from each runtime and
// replayed through each adapter and the real run loop, must produce the same
// run-event SHAPE — modulo tool spelling, ids and timings.
//
// The two recordings are the closest pair the spikes produced: Claude Code's
// probe 1 and OpenCode's S2b both fan out THREE builders from the lead in one
// turn, one of which fans out ONE child of its own. They were not driven by
// one script, so this is a SHAPE contract, not an event-for-event one:
//
//   held here — the agent tree (depth, parent), exactly one start and one
//   settle per agent with a status and a report, every step inside its agent's
//   life, every call paired with its outcome on the same agent, the fan-out
//   never a row, one run_started and one run_settled (last), per-model usage
//   on the settle in the platform's spelling.
//
//   different by design — `background` (true on Claude Code, false on
//   OpenCode: ADR-0015), the wait tool's rows on Claude Code's lead (OpenCode
//   has no wait tool; the `task` call is the wait), the runtime's tool names.
//
//   not yet recordable — ADR-0015 owes one scripted session with a
//   denied write and a commit as well. Neither probe has them; the rows those
//   produce are pinned per adapter instead (translate.test.ts on each side),
//   and a pair of recordings from one script is the missing fixture.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { consumeRun } from "../lib/run_loop.js";
import type { RunWatchdog } from "../lib/progress/watchdog.js";
import type { RunEventInput } from "../lib/progress/emitter.js";
import type { MessageClassifier } from "./port.js";
import { createClaudeClassifier } from "./claude/classify.js";
import { createClaudeAdapter } from "./claude/translate.js";
import { createOpencodeClassifier } from "./opencode/classify.js";
import { createStreamCloser, sessionStream } from "./opencode/settle.js";
import { createOpencodeAdapter } from "./opencode/translate.js";

const FIXTURES = new URL("../../test/fixtures/", import.meta.url);

function recording(name: string): unknown[] {
  return fs
    .readFileSync(new URL(name, FIXTURES), "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as unknown);
}

const quietWatchdog: RunWatchdog = {
  observe: () => {},
  observeRetry: () => {},
  observeStream: () => {},
  check: () => {},
  describe: () => "",
  start: () => () => {},
};

async function feed(
  messages: AsyncIterable<unknown>,
  translate: (m: unknown) => RunEventInput[],
  classify: MessageClassifier,
): Promise<RunEventInput[]> {
  const events: RunEventInput[] = [];
  await consumeRun(
    { messages, stopTask: async () => {} },
    { translate, classify, watchdog: quietWatchdog, emit: (e) => events.push(e), inputGraceMs: 5 },
  );
  return events;
}

async function* each(messages: unknown[]): AsyncGenerator<unknown> {
  yield* messages;
}

async function claudeFeed(): Promise<RunEventInput[]> {
  let clock = 0;
  const adapter = createClaudeAdapter({ taskKind: "implementation", now: () => (clock += 1000) });
  return feed(each(recording("probe1-background-fanout.jsonl")), adapter.translate, createClaudeClassifier());
}

async function opencodeFeed(): Promise<RunEventInput[]> {
  let clock = 0;
  const adapter = createOpencodeAdapter({ model: "claude-haiku-4-5", taskKind: "implementation", now: () => (clock += 1000) });
  const stream = sessionStream(each(recording("opencode-s2b-foreground-fanout.jsonl")), createStreamCloser(), { skills: [] });
  return feed(stream, adapter.translate, createOpencodeClassifier());
}

/** The feed with everything a runtime is allowed to spell its own way taken out. */
interface Shape {
  runStarted: number;
  runSettled: { count: number; last: boolean; outcome?: string; models: string[] };
  /** Each agent as `depth:parentDepth`, sorted — the tree without its ids. */
  tree: string[];
  /** Per agent: started once, settled once, with a status and a report. */
  agentsSettledOnceWithReports: boolean;
  /** No step of an agent falls outside its own start → settle. */
  stepsInsideTheirAgent: boolean;
  /** Every tool_use has exactly one tool_result, on the same agent. */
  callsPaired: boolean;
  /** The fan-out never reaches a row. */
  fanOutIsNotARow: boolean;
}

function shapeOf(events: RunEventInput[]): Shape {
  const started = events.filter((e) => e.kind === "agent_started");
  const depthOf = new Map<string, number>([["lead", 0]]);
  for (const a of started) depthOf.set(a.agentId ?? "", a.depth ?? 1);
  const tree = started.map((a) => `${a.depth ?? 1}:${depthOf.get(a.parentAgentId ?? "lead") ?? "?"}`).sort();

  const agentsSettledOnceWithReports = started.every((a) => {
    const settles = events.filter((e) => e.kind === "agent_settled" && e.agentId === a.agentId);
    return (
      started.filter((b) => b.agentId === a.agentId).length === 1 &&
      settles.length === 1 &&
      settles[0].status === "completed" &&
      (settles[0].report ?? "") !== ""
    );
  });

  const stepsInsideTheirAgent = started.every((a) => {
    const from = events.indexOf(a);
    const to = events.findIndex((e) => e.kind === "agent_settled" && e.agentId === a.agentId);
    return events.every(
      (e, i) => e.agentId !== a.agentId || !["tool_use", "tool_result"].includes(e.kind) || (i > from && i < to),
    );
  });

  const uses = events.filter((e) => e.kind === "tool_use");
  const results = events.filter((e) => e.kind === "tool_result");
  const callsPaired =
    uses.length === results.length &&
    uses.every((u) => {
      const r = results.filter((x) => x.toolUseId === u.toolUseId);
      return r.length === 1 && r[0].agentId === u.agentId;
    });

  const settles = events.filter((e) => e.kind === "run_settled");
  return {
    runStarted: events.filter((e) => e.kind === "run_started").length,
    runSettled: {
      count: settles.length,
      last: events.at(-1)?.kind === "run_settled",
      outcome: settles[0]?.outcome,
      models: (settles[0]?.usage?.models ?? []).map((m) => m.model.replace(/-\d{8}$/, "")),
    },
    tree,
    agentsSettledOnceWithReports,
    stepsInsideTheirAgent,
    callsPaired,
    fanOutIsNotARow: !uses.some((u) => ["Agent", "Task", "task"].includes(u.tool ?? "")),
  };
}

test("adapter contract: Claude Code and OpenCode produce the same run-event shape for a 3 + 1 fan-out", async () => {
  const claude = shapeOf(await claudeFeed());
  const opencode = shapeOf(await opencodeFeed());

  const expected: Shape = {
    runStarted: 1,
    runSettled: { count: 1, last: true, outcome: "success", models: ["claude-haiku-4-5"] },
    // Three children of the lead, and one child of a child.
    tree: ["1:0", "1:0", "1:0", "2:1"],
    agentsSettledOnceWithReports: true,
    stepsInsideTheirAgent: true,
    callsPaired: true,
    fanOutIsNotARow: true,
  };
  assert.deepEqual(claude, expected, "Claude Code's feed");
  assert.deepEqual(opencode, expected, "OpenCode's feed");
});

test("adapter contract: the differences are the recorded ones and no others", async () => {
  const claude = await claudeFeed();
  const opencode = await opencodeFeed();
  const background = (evs: RunEventInput[]) =>
    evs.filter((e) => e.kind === "agent_started").map((a) => a.background);
  // Claude Code's builders are backgrounded (its depth-2 child is foreground);
  // every OpenCode agent is stated foreground (ADR-0015).
  assert.deepEqual(background(claude).sort(), [false, true, true, true]);
  assert.deepEqual(background(opencode), [false, false, false, false]);
  // The wait tool is Claude Code's lead's only extra rows.
  const leadTools = (evs: RunEventInput[]) =>
    [...new Set(evs.filter((e) => e.kind === "tool_use" && e.agentId === "lead").map((e) => e.tool))].sort();
  assert.deepEqual(leadTools(claude), ["TaskOutput", "ToolSearch"]);
  assert.deepEqual(leadTools(opencode), []);
  // Each names its own runtime, and only there.
  assert.equal(claude.find((e) => e.kind === "run_started")?.runtime, "claude-code");
  assert.equal(opencode.find((e) => e.kind === "run_started")?.runtime, "opencode");
});
