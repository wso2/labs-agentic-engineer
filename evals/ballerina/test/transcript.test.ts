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
  assert.equal(m.byVerb.client, undefined, "never reached for a container verb in this run");
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

/**
 * H2, which inverted the metric this harness exists to produce.
 *
 * The check used to be `body.trim().startsWith("{")`, which only holds when the
 * caller REDIRECTED stderr. An unpiped failure arrives with the runner's own
 * `Exit code 1` prefix in front of the JSON, so every failure from a
 * well-behaved unpiped call was scored as a successful document — and the
 * failure rate therefore rewarded the piping the design is trying to make
 * unnecessary.
 */
test("readPathMetrics: an unpiped failure is a failure, prefix and all", () => {
  const failure = '{"kind":"symbol-not-found","qualified":"ballerinax/kafka:4.6.5"}';
  const cases: { label: string; body: string; kind: string | undefined }[] = [
    { label: "redirected", body: `${failure}\n`, kind: "symbol-not-found" },
    { label: "unpiped, with the runner's prefix", body: `Exit code 1\n\n${failure}\n`, kind: "symbol-not-found" },
    { label: "a document is not a failure", body: "# ballerinax/kafka 4.6.5\n\n| Clients | 3 |\n", kind: undefined },
  ];
  for (const { label, body, kind } of cases) {
    const m = readPathMetrics(transcript("bal library type ballerinax/kafka NoSuchType", body));
    assert.equal(m.lookups, 1, label);
    assert.equal(m.failed, kind === undefined ? 0 : 1, label);
  }
  // A record body has braces too, and it is not a failure object.
  const record = "public type X record {\n    string a;\n};\n";
  assert.equal(readPathMetrics(transcript("bal library type x/y X", record)).failed, 0);
});

/**
 * A pipe that CUT is a different finding from a pipe that was merely typed.
 *
 * The 2026-08-17 sweep counted claims-fhir at 6 piped of 6 and telemetry-kafka at
 * 5 of 5, against a skill that asks for zero — a number that reads as a crisis. But
 * every document is now bounded, and `overview ballerina/crypto` is 42 lines, so
 * `| head -250` over it removes nothing at all. Without this split the habit and
 * the damage report as one figure, and only the damage is evidence about the tool.
 */
test("truncation: a window the document never reached cut nothing", () => {
  const short = `${"line\n".repeat(40)}## Next\n`;
  const piped = readPathMetrics(transcript("bal library overview ballerina/crypto | head -250", short));
  assert.equal(piped.piped, 1, "still the habit");
  assert.equal(piped.truncated, 0, "but 41 lines through a 250-line window lost nothing");

  const long = "line\n".repeat(250);
  const cut = readPathMetrics(transcript("bal library api ballerinax/github | head -250", long));
  assert.equal(cut.truncated, 1, "the window was reached, so the rest is gone");
});

test("truncation: a content filter always cuts, and a byte window is measured in bytes", () => {
  const body = `${"line\n".repeat(40)}## Next\n`;
  // grep/sed/awk/cut/wc drop lines or columns by construction — there is no
  // window to fall short of, and `grep` is what drops a trailing note while
  // keeping the signature it belongs to.
  for (const filter of ["grep -n 'function'", "sed -n '1,20p'", "awk '{print $1}'", "cut -c1-80", "wc -l"]) {
    const m = readPathMetrics(transcript(`bal library api x/y | ${filter}`, body));
    assert.equal(m.truncated, 1, filter);
  }
  assert.equal(readPathMetrics(transcript("bal library api x/y | head -c 100", body)).truncated, 1);
  assert.equal(readPathMetrics(transcript("bal library api x/y | head -c 40000", body)).truncated, 0);
  // A bare `head` is ten lines, which is the shape that hides the most.
  assert.equal(readPathMetrics(transcript("bal library api x/y | head", body)).truncated, 1);
  // Unpiped is neither.
  const clean = readPathMetrics(transcript("bal library overview x/y", body));
  assert.equal(clean.piped, 0);
  assert.equal(clean.truncated, 0);
});

test("truncation: even the runs that piped everything only cut about a third", () => {
  // Measured, against the expectation that wrote this test. "19 of 19 piped" was
  // read for two sweeps as 19 damaged documents; it was 9, and 5 in the next run.
  // The other ten were windows the document never reached. So the headline number
  // overstated its own finding by 2-4x, in the direction that invites a fix to a
  // problem half that size — which is the whole reason for keeping them apart.
  const before = readPathMetrics(fixture("run-2026-08-14.ndjson"));
  assert.equal(before.piped, 19);
  assert.equal(before.truncated, 9);

  const after = readPathMetrics(fixture("run-2026-08-15.ndjson"));
  assert.equal(after.piped, 19);
  assert.equal(after.truncated, 5);
});

/**
 * A lookup that found no `bal library` AT ALL invalidates the attempt.
 *
 * Measured on 2026-08-17, and it is not hypothetical: three sessions in one sweep ran
 * `bal tool pull openapi`, which rewrites `~/.ballerina/.config/bal-tools.toml` and
 * DROPS the locally installed `library` entry, because that entry carries
 * `repository = "local"` and the regeneration does not preserve it. `catalog-redis`
 * then answered `unknown command 'library'` four times, and one session went on to
 * `bal tool pull library` — installing the PUBLISHED tool over the working-tree build.
 *
 * From that point the sweep measured a different tool under the name of the one being
 * tested. `preflight` cannot see it: it checks once, before the first session, and the
 * clobber happens inside them. So it has to be caught on the way out, per attempt, and
 * it is not a bad score — it is the absence of evidence, which must never average in
 * with runs that had a tool.
 */
test("integrity: a lookup that found no bal library at all is counted, not scored", () => {
  const missing = "bal: unknown command 'library'\nRun 'bal help' for usage.\n";
  const m = readPathMetrics(transcript("bal library overview ballerinax/redis 2>&1", missing));
  assert.equal(m.toolMissing, 1);
  // And it is NOT silently a successful document: the body carried no answer.
  assert.equal(m.lookups, 1);

  const fine = readPathMetrics(transcript("bal library overview x/y", "<!-- bal library overview v1 -->\n"));
  assert.equal(fine.toolMissing, 0);
});

/** One assistant turn holding one Bash call, and the result the runner returned for it. */
function transcript(command: string, body: string): string {
  const id = "toolu_1";
  return [
    JSON.stringify({
      type: "assistant",
      message: { content: [{ type: "tool_use", id, name: "Bash", input: { command } }] },
    }),
    JSON.stringify({
      type: "user",
      message: { content: [{ type: "tool_result", tool_use_id: id, content: body }] },
    }),
  ].join("\n");
}

test("readInvocation: verb and target, including the shapes that have no target", () => {
  assert.deepEqual(readInvocation("bal library overview ballerinax/aws.s3 2>&1 | head -200"), {
    verb: "overview",
    target: "ballerinax/aws.s3",
  });
  assert.deepEqual(readInvocation("bal library type ballerina/mime Entity -r"), {
    verb: "type",
    target: "ballerina/mime",
  });
  // `find` takes keywords; reporting "kafka" as a package would invent detours
  // between unrelated searches.
  assert.deepEqual(readInvocation("bal library find kafka messaging"), { verb: "find", target: "" });
  // The verbs the 2026-08-17 kind split introduced. A verb missing from the
  // config list keeps its own name but LOSES its target, which silently removes
  // the call from detour attribution — so each one is pinned with its target.
  assert.deepEqual(readInvocation("bal library client ballerinax/github Client repos"), {
    verb: "client",
    target: "ballerinax/github",
  });
  assert.deepEqual(readInvocation("bal library class ballerina/http Cookie"), {
    verb: "class",
    target: "ballerina/http",
  });
  assert.deepEqual(readInvocation("bal library funcs ballerina/uuid"), {
    verb: "funcs",
    target: "ballerina/uuid",
  });
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
    truncated: false,
    toolMissing: false,
    failed: false,
    sawNext: false,
    chars: 2000,
    verbs: ["type"],
    ...over,
  };
}
