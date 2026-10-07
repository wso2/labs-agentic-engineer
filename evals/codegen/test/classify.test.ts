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

/**
 * Whose failure each phase was — the app's (a hard fail, scored 0) or the
 * environment's (a harness error, excluded) — and the one line `wire` and this
 * harness agree on. A false zero in the statistics is the defect these guard
 * against.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { failedLine } from "@aep/playground/src/engine/wire/failure.js";
import { codingFailure, wireFailure, type CodingFacts } from "../src/classify.js";
import { readRunSettled, sawRunStarted } from "../src/metrics.js";
import { parseFailed } from "../src/play.js";
import { BrowserWatchdog } from "../src/walker.js";

// --- the wire contract ------------------------------------------------------

test("contract: what wire prints, the harness parses — cause and reason intact", () => {
  for (const failure of [
    { cause: "app" as const, reason: "api started and exited with code 1" },
    { cause: "environment" as const, reason: "api could not be started: Bind for 0.0.0.0:19090 failed: port is already allocated" },
  ]) {
    assert.deepEqual(parseFailed(failedLine(failure)), failure);
  }
  assert.equal(parseFailed("FAILED maybe something"), null, "an unknown cause is not the contract");
  assert.equal(parseFailed("✗ wire: the plan does not hold"), null);
});

// --- wire -------------------------------------------------------------------

test("wire: its own classification stands; an unclassified end is never the app's", () => {
  const base = { exitCode: 4, limitMinutes: 20, tail: "…" };
  const said = { cause: "app" as const, reason: "a Dockerfile step failed" };
  assert.deepEqual(wireFailure({ ...base, ended: "exited", failed: said }), said);
  assert.equal(wireFailure({ ...base, ended: "exited", failed: null }).cause, "environment");
  assert.equal(wireFailure({ ...base, ended: "timeout", failed: null }).cause, "environment");
});

// --- coding -----------------------------------------------------------------

const coding = (over: Partial<CodingFacts>): CodingFacts => ({
  timedOut: false,
  limitMinutes: 90,
  exitCode: 0,
  events: 400,
  agentStarted: true,
  settled: { outcome: "success", tokens: { input: 0, output: 0, cacheRead: 0, cacheCreation: 0 } },
  builtAnything: true,
  ...over,
});
const settled = (outcome: string, extra: { code?: string; error?: string } = {}): CodingFacts["settled"] => ({
  outcome,
  ...extra,
  tokens: { input: 0, output: 0, cacheRead: 0, cacheCreation: 0 },
});

test("coding: a run that succeeded and built something is no failure", () => {
  assert.equal(codingFailure(coding({})), null);
});

test("coding: the agent never getting a turn is the environment's — no events, or a runner that settled first", () => {
  assert.equal(codingFailure(coding({ events: 0, agentStarted: false, settled: null }))?.cause, "environment");
  const provisioning = codingFailure(
    coding({ events: 2, agentStarted: false, settled: settled("failure", { error: "workspace_provisioning: no such dir" }) }),
  );
  assert.equal(provisioning?.cause, "environment");
  assert.match(provisioning?.reason ?? "", /workspace_provisioning/);
  // Even a timeout: a run stuck before the agent started is not the agent's.
  assert.equal(codingFailure(coding({ agentStarted: false, timedOut: true, settled: null }))?.cause, "environment");
});

test("coding: a provider limit, a run that died unsettled, and a cancel are the environment's", () => {
  assert.equal(codingFailure(coding({ settled: settled("failure", { code: "provider_limit" }) }))?.cause, "environment");
  assert.equal(codingFailure(coding({ settled: null, exitCode: 137 }))?.cause, "environment");
  assert.equal(codingFailure(coding({ settled: settled("cancelled") }))?.cause, "environment");
});

test("coding: an agent that failed, ran out of time, or built nothing is the app's", () => {
  assert.equal(codingFailure(coding({ settled: settled("failure", { error: "gave up" }) }))?.cause, "app");
  assert.equal(codingFailure(coding({ timedOut: true, settled: null }))?.cause, "app");
  assert.equal(codingFailure(coding({ builtAnything: false }))?.cause, "app");
});

test("metrics: run_started marks the agent's start; run_settled carries its code and error", () => {
  const feed = [
    JSON.stringify({ kind: "notice", code: "workspace_provisioning" }),
    JSON.stringify({ kind: "run_settled", outcome: "failure", code: "provider_limit", error: "429" }),
  ].join("\n");
  assert.equal(sawRunStarted(feed), false);
  assert.equal(sawRunStarted(`${JSON.stringify({ kind: "run_started" })}\n${feed}`), true);
  assert.deepEqual(readRunSettled(feed), {
    outcome: "failure",
    code: "provider_limit",
    error: "429",
    tokens: { input: 0, output: 0, cacheRead: 0, cacheCreation: 0 },
  });
});

// --- the walker's browser watchdog --------------------------------------------

let ids = 0;
/** One Bash call and its result, in the shape the walk transcript records them. */
function bashCall(result: Record<string, unknown>, isError = false): SDKMessage[] {
  ids += 1;
  const id = `toolu_${String(ids)}`;
  return [
    { type: "assistant", message: { content: [{ type: "tool_use", id, name: "Bash", input: { command: "agent-browser snapshot -i" } }] } },
    { type: "user", message: { role: "user", content: [{ type: "tool_result", tool_use_id: id, content: "…", is_error: isError }] }, tool_use_result: result },
  ] as unknown as SDKMessage[];
}
const timedOut = (ms = 120_000): SDKMessage[] => bashCall({ stdout: "", stderr: "", interrupted: false, backgroundTaskId: "b1", timedOutAfterMs: ms });
const answered = (): SDKMessage[] => bashCall({ stdout: "✓ Done", stderr: "", interrupted: false });

function feed(dog: BrowserWatchdog, messages: SDKMessage[]): string[] {
  return messages.flatMap((message) => {
    const reason = dog.observe(message);
    return reason === undefined ? [] : [reason];
  });
}

test("watchdog: three commands in a row that ran to their timeout stop the walk, once", () => {
  const dog = new BrowserWatchdog(3);
  assert.deepEqual(feed(dog, [...answered(), ...timedOut(), ...timedOut()]), []);
  const stops = feed(dog, [...timedOut(30_000), ...timedOut()]);
  assert.equal(stops.length, 1, "it trips on the third and says so once");
  assert.match(stops[0] ?? "", /^browser unresponsive: 3 agent-browser commands in a row .*after 30s/);
});

test("watchdog: a command that completes in between resets the count — one slow page is not a dead browser", () => {
  const dog = new BrowserWatchdog(3);
  assert.deepEqual(feed(dog, [...timedOut(), ...timedOut(), ...answered(), ...timedOut(), ...timedOut()]), []);
});

test("watchdog: a refused command proves nothing either way; a Read or Write is not a browser command", () => {
  const dog = new BrowserWatchdog(2);
  const read = [
    { type: "assistant", message: { content: [{ type: "tool_use", id: "r1", name: "Read", input: {} }] } },
    { type: "user", message: { role: "user", content: [{ type: "tool_result", tool_use_id: "r1", content: "…" }] }, tool_use_result: { type: "image" } },
  ] as unknown as SDKMessage[];
  assert.deepEqual(feed(dog, [...timedOut(), ...read, ...bashCall({ stdout: "" }, true)]), []);
  assert.equal(feed(dog, timedOut()).length, 1);
});
