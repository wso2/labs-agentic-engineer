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
 * The extractors, against transcripts nobody wrote for them.
 *
 * `__fixtures__/` holds two REAL playground runs of the same project — one
 * before the skill's "unpiped" line and one after — trimmed to the messages
 * these metrics read. They are the runs the analysis in
 * `docs/design/draft/bal-library-run-analysis-2026-08-14.md` was written from,
 * so every number asserted below is one a human already checked by hand. That
 * is what makes this a test of the extractor rather than a snapshot of it.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { readInvocation, readPathMetrics, worstDetour, type Lookup } from "../src/metrics/transcript.js";
import { isSignatureError, readBuildAttempt, summarize } from "../src/metrics/build.js";

const FIXTURES = join(import.meta.dirname, "__fixtures__");

function fixture(name: string): string {
  return readFileSync(join(FIXTURES, name), "utf8");
}

test("path metrics: the 2026-08-14 run, as analysed by hand", () => {
  const m = readPathMetrics(fixture("run-2026-08-14.ndjson"));
  assert.equal(m.lookups, 19);
  // EVERY call was filtered, in the run that predates the skill's "unpiped"
  // line as well as the one after it. The first hand count of this said 16 and
  // was simply wrong; the extractor is why that got caught.
  assert.equal(m.piped, 19);
  // `--help` and `overview ballerina/mime`. Every other document was cut short
  // of its last four lines. `--help` counts because the metric is "did a `##
  // Next` block survive the filter", and its own does.
  assert.equal(m.sawNext, 2);
  // Reasoning capture did not exist yet: the blocks were signed and empty, and
  // an extractor that counted them would have reported this run as observable.
  assert.equal(m.thinkingBlocks, 0);
  assert.equal(m.subagentThinkingBlocks, 0);
  // 19 tool calls, 20 invocations: one Bash call held `type ballerina/log
  // --help; overview ballerina/log`. The census counts invocations, `lookups`
  // counts turns, and the two differing here is the point.
  assert.equal(m.byVerb.overview, 8);
  assert.equal(m.byVerb.type, 9);
  assert.equal(m.byVerb.api, 2);
  assert.equal(m.byVerb.ops, undefined, "never reached for `ops` in this run");
  const invocations = Object.values(m.byVerb).reduce((a, b) => a + b, 0);
  assert.equal(invocations, 20);
});

test("path metrics: the 2026-08-15 run piped everything and captured reasoning", () => {
  const m = readPathMetrics(fixture("run-2026-08-15.ndjson"));
  assert.equal(m.lookups, 19);
  // The finding the skill change bought: the instruction landed and changed
  // nothing. 19/19 before, 19/19 after.
  assert.equal(m.piped, 19);
  // ADR-0002 decision 16 landed for this run, and most of it is the subagents'.
  assert.ok(m.thinkingBlocks >= 50, `expected captured reasoning, got ${m.thinkingBlocks}`);
  assert.ok(m.subagentThinkingBlocks >= 40, `expected subagent reasoning, got ${m.subagentThinkingBlocks}`);
  // It reached for `ops` for the first time across three runs.
  assert.equal(m.byVerb.ops, 1);
});

test("path metrics: the aws detour is found, and it grew between the two runs", () => {
  const before = readPathMetrics(fixture("run-2026-08-14.ndjson"));
  const after = readPathMetrics(fixture("run-2026-08-15.ndjson"));
  // The metric that matters and that the skill line did not touch.
  assert.ok(
    after.worstDetour.calls > before.worstDetour.calls,
    `detour should have grown: ${before.worstDetour.calls} → ${after.worstDetour.calls}`,
  );
  assert.match(after.worstDetour.target, /aws/);
});

test("worstDetour: a streak breaks on a call that YIELDED, not on a change of target", () => {
  // The measured shape: one question, spelled three ways, answered by none of
  // them. Counting per-target would report three detours of one.
  const lookups: Lookup[] = [
    fake({ target: "ballerinax/aws.auth", failed: true }),
    fake({ target: "ballerinax/aws.s3", failed: true }),
    fake({ target: "ballerinax/aws.auth", failed: true }),
    fake({ target: "ballerinax/aws.auth", chars: 5000 }),
    fake({ target: "ballerina/http", failed: true }),
  ];
  const detour = worstDetour(lookups);
  assert.equal(detour.calls, 3);
  assert.equal(detour.target, "ballerinax/aws.auth");
});

test("worstDetour: an empty-bodied result is a dead end, same as a failure", () => {
  // `api ballerinax/aws | grep AuthConfig` returned "(Bash completed with no
  // output)" — 31 chars, exit 0, and worth exactly nothing.
  const detour = worstDetour([fake({ target: "ballerinax/aws", chars: 31 })]);
  assert.equal(detour.calls, 1);
});

test("worstDetour: no wasted calls is a zero, not a one", () => {
  const detour = worstDetour([fake({ chars: 5000 }), fake({ chars: 8000 })]);
  assert.deepEqual(detour, { target: "", calls: 0, chars: 0 });
});

test("readInvocation: verb and target, including the shapes that have no target", () => {
  assert.deepEqual(readInvocation("bal library overview ballerinax/aws.s3 2>&1 | head -200"), {
    verb: "overview",
    target: "ballerinax/aws.s3",
  });
  assert.deepEqual(readInvocation("bal library type ballerina/mime Entity --deps"), {
    verb: "type",
    target: "ballerina/mime",
  });
  // `search` takes keywords; reporting "kafka" as a package would invent
  // detours between unrelated searches.
  assert.deepEqual(readInvocation("bal library search kafka messaging"), { verb: "search", target: "" });
  assert.deepEqual(readInvocation("bal library --help | head -50"), { verb: "help", target: "" });
});

test("build: a signature error is told apart from the agent's own slip", () => {
  // The class `bal library` exists to prevent.
  assert.ok(isSignatureError("undefined field 'accessKeyId' in record 'ballerinax/aws.s3:4.0.0:ConnectionConfig'"));
  assert.ok(isSignatureError("undefined method 'createObject' in object 'ballerinax/aws.s3:4.0.0:Client'"));
  assert.ok(isSignatureError("missing non-defaultable required record field 'auth'"));
  // The agent's own arithmetic — a tuple destructuring slip, not an API claim.
  assert.ok(!isSignatureError("missing comma token"));
  assert.ok(!isSignatureError("unused module prefix 'regexp'"));
  assert.ok(!isSignatureError("unreachable code"));
});

test("build: attempts summarize to cycles, greenness and the two error counts", () => {
  const red = readBuildAttempt(
    [
      "Compiling source",
      "ERROR [auth_util.bal:(1:23,1:29)] unused module prefix 'regexp'",
      "ERROR [s3.bal:(4:1,4:9)] undefined field 'accessKeyId' in record 'ballerinax/aws.s3:4.0.0:ConnectionConfig'",
      "HINT [service.bal:(76:5,76:5)] concurrent calls will not be made to this method",
      "error: compilation contains errors",
    ].join("\n"),
    1,
  );
  assert.equal(red.errors.length, 2);
  assert.equal(red.signatureErrors.length, 1, "the HINT must not count, the prefix slip must not count");

  const green = readBuildAttempt("Compiling source\n\tGenerating executable\n", 0);
  const m = summarize([red, green]);
  assert.deepEqual(m, {
    cycles: 2,
    green: true,
    firstAttemptErrors: 2,
    firstAttemptSignatureErrors: 1,
    totalErrors: 2,
    totalSignatureErrors: 1,
  });
});

test("build: a session that never built is NOT green", () => {
  // Unverified code reported as a pass is the one outcome this harness must
  // never produce.
  assert.equal(summarize([]).green, false);
});

test("parse: a truncated final line does not lose the run", () => {
  const ndjson = '{"type":"result","total_cost_usd":1.5,"duration_ms":10,"num_turns":2}\n{"type":"assist';
  const m = readPathMetrics(ndjson);
  assert.equal(m.costUsd, 1.5);
  assert.equal(m.turns, 2);
});

function fake(over: Partial<Lookup>): Lookup {
  return {
    command: "bal library type x/y Z",
    verb: "type",
    target: "x/y",
    piped: false,
    failed: false,
    sawNext: false,
    chars: 2000,
    verbs: ["type"],
    ...over,
  };
}
