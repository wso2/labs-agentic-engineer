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
import type { ProgressEventInput } from "./schema.js";
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
  const emitted: ProgressEventInput[] = [];
  const watchdog = createRunWatchdog({
    idleMs: IDLE,
    now: () => clock,
    emit: (e) => emitted.push(e),
  });
  return {
    watchdog,
    emitted,
    summaries: () => emitted.map((e) => ("summary" in e ? String(e.summary) : "")),
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
  assert.equal(h.emitted[0]?.kind, "log");
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

// One line as the translator really emits it for work done inside a subagent:
// the fan-out call gets NO tool_use event of its own, so `emitterId` is the only
// evidence on this stream that it exists. Every fan-out test below builds its
// events through here, because a hand-made `{tool: "Agent"}` tool_use — which is
// what this suite used to assert against — is a shape production never produces.
function subagentLine(e: ProgressEventInput): ProgressEventInput {
  return { ...e, emitter: "subagent", emitterId: "a1", emitterLabel: "implement checkout" };
}

test("watchdog: a silent subagent is named, not reported as an idle model", () => {
  // The live regression. A fan-out went quiet for ten minutes and every report
  // said "no tool in flight — waiting on the model", pointing at the lead while
  // a 22-minute Agent call was the thing being waited on.
  const h = harness();
  h.watchdog.observe([subagentLine({ kind: "tool_use", tool: "Edit", summary: "src/api.ts", toolUseId: "t9" })]);
  h.watchdog.observe([subagentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  const line = h.summaries()[0] ?? "";
  assert.match(line, /no tool in flight inside Agent \(implement checkout\), running 2m0s/);
  assert.match(line, /waiting on its model for 2m0s/);
});

test("watchdog: a tool in flight inside a subagent names both the call and the subagent", () => {
  // The inner call is the diagnosis; the subagent is where to look for it. The
  // fan-out must not outrank its own tool — it is always the older of the two.
  const h = harness();
  h.watchdog.observe([subagentLine({ kind: "tool_use", tool: "Bash", summary: "npm ci", toolUseId: "t9" })]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /waiting on Bash \(npm ci\) in subagent \(implement checkout\) for 2m0s/);
});

test("watchdog: a subagent's retry surfaces under the fan-out it belongs to", () => {
  // Where a retry inside a subagent shows up, and where the cause is hardest to
  // guess from outside.
  const h = harness();
  h.watchdog.observe([subagentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);
  h.watchdog.observeRetry({ ...OVERLOADED, error: "rate_limit", errorStatus: 429 });

  h.advance(IDLE + 1);
  h.watchdog.check();
  const line = h.summaries()[0] ?? "";
  assert.match(line, /inside Agent \(implement checkout\)/);
  assert.match(line, /API retry 3\/10, rate_limit/);
});

test("watchdog: the fan-out's own result settles it — the lead is idle again, not a subagent", () => {
  // That result carries the subagent id in BOTH toolUseId and emitterId, so the
  // registration and the deletion race inside one event. The deletion has to win
  // or a finished subagent is reported as running for the rest of the run.
  const h = harness();
  h.watchdog.observe([subagentLine({ kind: "tool_use", tool: "Edit", summary: "src/api.ts", toolUseId: "t9" })]);
  h.watchdog.observe([subagentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);
  h.watchdog.observe([
    { kind: "tool_result", ok: false, tool: "Agent", toolUseId: "a1", emitter: "subagent", emitterId: "a1" },
  ]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /^\[watchdog\] no tool in flight — waiting on the model for 2m0s$/);
});

test("watchdog: a settled subagent is not resurrected by a late line about it", () => {
  // Its id is registered from the lines it produces, so a stray line after the
  // settle would otherwise register a phantom that nothing ever closes.
  const h = harness();
  h.watchdog.observe([subagentLine({ kind: "tool_result", ok: true, toolUseId: "t9" })]);
  h.watchdog.observe([
    { kind: "tool_result", ok: false, tool: "Agent", toolUseId: "a1", emitter: "subagent", emitterId: "a1" },
  ]);
  h.watchdog.observe([subagentLine({ kind: "activity", summary: "a late narration" })]);

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /^\[watchdog\] no tool in flight — waiting on the model for 2m0s$/);
});

test("watchdog: several subagents at once are counted, not guessed between", () => {
  // A milestone cycle runs two or three concurrently and their lines interleave,
  // so naming one of them would be a coin flip.
  const h = harness();
  for (const id of ["a1", "a2"]) {
    h.watchdog.observe([
      { kind: "tool_result", ok: true, toolUseId: `t-${id}`, emitter: "subagent", emitterId: id, emitterLabel: id },
    ]);
  }

  h.advance(IDLE + 1);
  h.watchdog.check();
  assert.match(h.summaries()[0] ?? "", /no tool in flight in any of 2 running subagents/);
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
