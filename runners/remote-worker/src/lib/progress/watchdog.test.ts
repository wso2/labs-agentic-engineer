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
import { createRunWatchdog } from "./watchdog.js";
import type { RunEventInput } from "./emitter.js";
import type { ApiRetryInfo } from "./diagnostics.js";

const OVERLOADED: ApiRetryInfo = {
  attempt: 3,
  maxRetries: 10,
  retryDelayMs: 4_200,
  errorStatus: 529,
  error: "overloaded",
};

const IDLE = 120_000;

function harness() {
  let clock = 0;
  const emitted: RunEventInput[] = [];
  const watchdog = createRunWatchdog({
    idleMs: IDLE,
    now: () => clock,
    emit: (e) => emitted.push(e),
  });
  return {
    watchdog,
    emitted,
    summaries: () => emitted.map((e) => String(e.detail ?? "")),
    advance: (ms: number) => {
      clock += ms;
    },
  };
}

test("watchdog: silence with a tool in flight names the tool — the run is not dead, that call is slow", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "bal tool pull openapi", toolUseId: "t1" }]);

  h.advance(IDLE - 1);
  h.watchdog.check();
  assert.equal(h.emitted.length, 0, "a build under the threshold is not reported");

  h.advance(2);
  h.watchdog.check();
  assert.equal(h.emitted.length, 1);
  assert.equal(h.emitted[0]?.kind, "notice");
  assert.equal((h.emitted[0] as { level?: string }).level, "warn", "never error — a long pull is legitimate");
  assert.match(h.summaries()[0] ?? "", /waiting on Bash \(bal tool pull openapi\) for 2m0s/);
});

test("watchdog: silence with NOTHING in flight blames the model, not a tool", () => {
  // The other half of the diagnosis: same symptom, different fault.
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "ls", toolUseId: "t1" }]);
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /no tool in flight — waiting on the model/);
});

test("watchdog: continued silence repeats every idle window, so a dead zone becomes a trail", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1" }]);

  // The real run that motivated this went 8m49s without a line.
  for (let i = 0; i < 4; i++) {
    h.advance(IDLE);
    h.watchdog.check();
  }
  assert.equal(h.emitted.length, 4);
  // Each one reports a LONGER wait — that progression is what says "still
  // stuck" rather than "just started".
  assert.match(h.summaries()[0] ?? "", /for 2m0s/);
  assert.match(h.summaries()[3] ?? "", /for 8m0s/);
});

test("watchdog: activity resets the clock — a working run is never reported", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Read", summary: "a.go", toolUseId: "t1" }]);
  for (let i = 0; i < 5; i++) {
    h.advance(IDLE - 1_000);
    h.watchdog.check();
    h.watchdog.observe([{ kind: "tool_use", tool: "Read", summary: `f${i}.go`, toolUseId: `t${i + 2}` }]);
  }
  assert.deepEqual(h.emitted, []);
});

test("watchdog: with several calls open it reports the OLDEST — the one actually stuck", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "old" }]);
  h.advance(60_000);
  h.watchdog.observe([{ kind: "tool_use", tool: "Read", summary: "a.bal", toolUseId: "new" }]);

  h.advance(IDLE);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /waiting on Bash \(bal build\) for 3m0s/);

  // Once the slow one lands, the remaining call is what is waited on.
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "old" }]);
  h.advance(IDLE);
  h.watchdog.check();
  assert.match(h.summaries()[1] ?? "", /waiting on Read \(a\.bal\)/);
});

test("watchdog: a Bash call rewritten to git_commit is still tracked as in flight", () => {
  // bashEvents changes the KIND; forgetting these would leave a git push that
  // hangs on auth looking like an idle model.
  const h = harness();
  h.watchdog.observe([{ kind: "git_push", branch: "main", summary: "git push origin main", toolUseId: "g1" }]);
  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /waiting on git_push \(git push origin main\)/);
});

test("watchdog: describe() is usable before anything has happened", () => {
  // The SIGTERM path calls it at arbitrary times, including during startup.
  assert.match(createRunWatchdog().describe(), /no tool in flight/);
});

test("watchdog: a retry does NOT reset the idle clock — otherwise a retry storm reports nothing", () => {
  // The regression that matters most. Retries arrive as SDK messages, so
  // routing them through observe() would keep the clock alive and the watchdog
  // silent through exactly the stall it exists to report. The measured backoff
  // climbs 0.2s → 33.6s, all of it inside the idle window.
  const h = harness();
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);

  for (let i = 0; i < 8; i++) {
    h.advance(30_000);
    h.watchdog.observeRetry({ ...OVERLOADED, attempt: i + 1 });
    h.watchdog.check();
  }
  assert.equal(h.emitted.length, 2, "4 minutes of retries produce two reports, not zero");
  assert.match(h.summaries()[0] ?? "", /waiting on the model for 2m0s/);
});

test("watchdog: the report names the retry — the diagnosis, not just the symptom", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);
  h.watchdog.observeRetry(OVERLOADED);

  h.advance(IDLE + 1);
  h.watchdog.check();
  const line = h.summaries()[0] ?? "";
  assert.match(line, /no tool in flight — waiting on the model for 2m0s/);
  assert.match(line, /\(API retry 3\/10, overloaded, last 2m0s ago\)/);
});

test("watchdog: real activity clears the retry, so a later stall is not blamed on a stale cause", () => {
  const h = harness();
  h.watchdog.observeRetry(OVERLOADED);
  // The retry was followed by a call that worked — that retry is history.
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1" }]);
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.doesNotMatch(h.summaries()[0] ?? "", /API retry/);
});

// An agent's life is DECLARED in v2. These two events are what the adapter
// emits for a spawned agent, and the watchdog's clock now starts and stops on
// them rather than on the first and last line the agent happened to produce.
const AGENT_STARTED: RunEventInput = {
  kind: "agent_started",
  agentId: "a1",
  label: "implement checkout",
  depth: 1,
};
const AGENT_SETTLED: RunEventInput = { kind: "agent_settled", agentId: "a1", status: "completed" };

/** One line of work done INSIDE that agent. */
function agentLine(e: RunEventInput): RunEventInput {
  return { ...e, agentId: "a1" };
}

test("watchdog: a silent agent is named, not reported as an idle model", () => {
  // The live regression. A fan-out went quiet for ten minutes and every report
  // said "no tool in flight — waiting on the model", pointing at the lead while
  // a 22-minute Agent call was the thing being waited on.
  const h = harness();
  h.watchdog.observe([AGENT_STARTED]);
  h.watchdog.observe([agentLine({ kind: "tool_use", tool: "Edit", summary: "src/api.ts", toolUseId: "t9" })]);
  h.watchdog.observe([agentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  const line = h.summaries()[0] ?? "";
  assert.match(line, /no tool in flight inside agent \(implement checkout\), running 2m0s/);
  assert.match(line, /waiting on its model for 2m0s/);
});

test("watchdog: a tool in flight inside an agent names both the call and the agent", () => {
  // The inner call is the diagnosis; the agent is where to look for it. The
  // agent must not outrank its own tool — it is always the older of the two.
  const h = harness();
  h.watchdog.observe([AGENT_STARTED]);
  h.watchdog.observe([agentLine({ kind: "tool_use", tool: "Bash", summary: "npm ci", toolUseId: "t9" })]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /waiting on Bash \(npm ci\) in agent \(implement checkout\) for 2m0s/);
});

test("watchdog: an agent's retry surfaces under the agent it belongs to", () => {
  // Where a retry inside a spawned agent shows up, and where the cause is
  // hardest to guess from outside.
  const h = harness();
  h.watchdog.observe([AGENT_STARTED]);
  h.watchdog.observe([agentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);
  h.watchdog.observeRetry({ ...OVERLOADED, error: "rate_limit", errorStatus: 429 });

  h.advance(IDLE + 1);
  h.watchdog.check();
  const line = h.summaries()[0] ?? "";
  assert.match(line, /inside agent \(implement checkout\)/);
  assert.match(line, /API retry 3\/10, rate_limit/);
});

test("watchdog: agent_settled closes the agent — the lead is idle again, not an agent", () => {
  const h = harness();
  h.watchdog.observe([AGENT_STARTED]);
  h.watchdog.observe([agentLine({ kind: "tool_use", tool: "Edit", summary: "src/api.ts", toolUseId: "t9" })]);
  h.watchdog.observe([agentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);
  h.watchdog.observe([AGENT_SETTLED]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /^\[watchdog\] no tool in flight — waiting on the model for 2m0s$/);
});

test("watchdog: a settled agent is not resurrected by a late line about it", () => {
  // A stray line after the settle would otherwise register a phantom that
  // nothing ever closes, and the run would report it as running for ever.
  const h = harness();
  h.watchdog.observe([AGENT_STARTED]);
  h.watchdog.observe([AGENT_SETTLED]);
  h.watchdog.observe([AGENT_STARTED]);
  h.watchdog.observe([agentLine({ kind: "agent_progress", phrase: "a late narration" })]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /^\[watchdog\] no tool in flight — waiting on the model for 2m0s$/);
});

test("watchdog: several agents at once are counted, not guessed between", () => {
  // A milestone cycle runs two or three concurrently and their lines interleave,
  // so naming one of them would be a coin flip.
  const h = harness();
  for (const id of ["a1", "a2"]) {
    h.watchdog.observe([{ kind: "agent_started", agentId: id, label: id, depth: 1 }]);
  }

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /no tool in flight in any of 2 running agents/);
});

// The v2 pin the design is emphatic about: a heartbeat says the run is ALIVE,
// not that anything happened. One every ten seconds would keep the idle clock
// permanently reset and the watchdog silent through exactly the stall the
// heartbeats are describing.
test("watchdog: a heartbeat does NOT reset the idle clock", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1" }]);

  // Two minutes of heartbeats, one every ten seconds, exactly as the adapter
  // rate-limits them.
  for (let i = 0; i < 12; i++) {
    h.advance(10_000);
    h.watchdog.observe([{ kind: "heartbeat", agentId: "lead", waitingOn: "tool", ref: "t1", elapsedMs: i * 10_000 }]);
  }
  h.watchdog.check();
  assert.equal(h.emitted.length, 1, "the stall is still reported through the heartbeats");
  assert.match(h.summaries()[0] ?? "", /waiting on Bash \(bal build\) for 2m0s/);
});

// …and the other half: a message that produced real work alongside a heartbeat
// is still work.
test("watchdog: work alongside a heartbeat still counts as activity", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1" }]);
  h.advance(IDLE - 1_000);
  h.watchdog.observe([
    { kind: "heartbeat", agentId: "lead", waitingOn: "model" },
    { kind: "tool_result", ok: true, toolUseId: "t1" },
  ]);
  h.advance(1_002);
  h.watchdog.check();
  assert.deepEqual(h.emitted, [], "the call landed, so the window starts again");
});

test("watchdog: streaming frames do not reset the clock — the report fires on the same schedule either way", () => {
  // includePartialMessages is developer-only, and a diagnostic that changes when
  // the report fires would be useless for reproducing what the cluster saw.
  const h = harness();
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);
  for (let i = 0; i < 60; i++) {
    h.advance(2_000);
    h.watchdog.observeStream();
  }
  h.watchdog.check();
  assert.equal(h.emitted.length, 1, "two minutes of tokens is still two minutes without a tool");
  assert.match(h.summaries()[0] ?? "", /model streaming, last token 0s ago/);
});

test("watchdog: with no streaming measured, nothing is claimed about tokens", () => {
  // Absence of frames means "not measured", not "no tokens" — the normal case
  // in the cluster, where the option is off.
  const h = harness();
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);
  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.doesNotMatch(h.summaries()[0] ?? "", /streaming/);
});

test("watchdog: a retry outranks the streaming clock as the reported cause", () => {
  const h = harness();
  h.watchdog.observe([{ kind: "tool_result", ok: true, toolUseId: "t1" }]);
  h.watchdog.observeStream();
  h.watchdog.observeRetry(OVERLOADED);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /API retry/);
  assert.doesNotMatch(h.summaries()[0] ?? "", /streaming/);
});
