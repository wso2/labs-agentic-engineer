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
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { discoverCases, selectCases } from "../src/cases.js";
import { compare, stat, METRIC_KEYS } from "../src/report.js";
import { hostEnv } from "../src/preflight.js";
import { DEFAULTS, PATH_METRICS, PATHS, REPORTED_METRICS, SESSION } from "../src/config.js";

function casesTree(suites: Record<string, Record<string, string>>): string {
  const root = mkdtempSync(join(tmpdir(), "bal-evals-"));
  for (const [suite, files] of Object.entries(suites)) {
    mkdirSync(join(root, suite), { recursive: true });
    for (const [name, body] of Object.entries(files)) writeFileSync(join(root, suite, name), body);
  }
  return root;
}

test("cases: a folder IS a suite — a new directory needs no registration", () => {
  const root = casesTree({
    narrow: { "a.yaml": "prompt: do a\n" },
    // Nothing anywhere names "connectors". Discovering it is the whole contract.
    connectors: { "b.yaml": "prompt: do b\n" },
  });
  const found = discoverCases(root);
  assert.deepEqual(
    found.map((c) => `${c.suite}/${c.name}`),
    ["connectors/b", "narrow/a"],
  );
});

test("cases: dot-directories and non-YAML are not cases", () => {
  const root = casesTree({
    ".git": { "config.yaml": "prompt: nope\n" },
    narrow: { "a.yaml": "prompt: yes\n", "README.md": "not a case" },
  });
  const found = discoverCases(root);
  assert.equal(found.length, 1);
  assert.equal(found[0]?.name, "a");
});

test("cases: a case with no prompt is refused at load, not run as an empty session", () => {
  // An empty session scores a clean zero on every metric, which reads as a pass.
  const root = casesTree({ narrow: { "bad.yaml": "expect:\n  builds: true\n" } });
  assert.throws(() => discoverCases(root), /'prompt' is required/);
});

test("cases: selection is exact, so a typo selects nothing rather than something else", () => {
  const root = casesTree({ narrow: { "aws-s3-submodule.yaml": "prompt: p\n", "http.yaml": "prompt: p\n" } });
  const all = discoverCases(root);
  assert.equal(selectCases(all, undefined, "aws-s3-submodule").length, 1);
  assert.equal(selectCases(all, undefined, "aws-s3").length, 0, "a prefix must not match");
  assert.equal(selectCases(all, "narrow").length, 2);
  assert.equal(selectCases(all, "full").length, 0);
});

test("cases: expectations are read, including importsNot", () => {
  const root = casesTree({
    narrow: {
      "e.yaml": "prompt: p\nexpect:\n  builds: true\n  imports: [a/b]\n  importsNot: [c/d]\n",
    },
  });
  assert.deepEqual(discoverCases(root)[0]?.expect, { builds: true, imports: ["a/b"], importsNot: ["c/d"] });
});

test("cases: a fixture names a source file, resolved against the case's own directory", () => {
  const root = casesTree({
    service: {
      "s.yaml": "prompt: p\nfixtures:\n  openapi.yaml: openapi/s.yaml\n",
    },
  });
  mkdirSync(join(root, "service", "openapi"), { recursive: true });
  writeFileSync(join(root, "service", "openapi", "s.yaml"), "openapi: 3.0.3\n");
  assert.deepEqual(discoverCases(root)[0]?.fixtures, {
    "openapi.yaml": join(root, "service", "openapi", "s.yaml"),
  });
});

test("cases: a fixture with no source file is refused at load, not silently skipped", () => {
  // A case whose contract never arrived still RUNS — the agent writes the
  // service from the prompt alone, which is a different task scoring under the
  // same name, and no metric downstream can see it happened.
  const root = casesTree({ service: { "s.yaml": "prompt: p\nfixtures:\n  openapi.yaml: openapi/gone.yaml\n" } });
  assert.throws(() => discoverCases(root), /fixture 'openapi.yaml' has no source/);
});

test("cases: every shipped case's fixtures resolve", () => {
  // The authored tree, not a temp one: a spec renamed without its case file
  // updated is exactly the break the load-time refusal is for, and this is
  // where it gets caught before a sweep spends an hour discovering it.
  for (const shipped of discoverCases(PATHS.casesDir)) {
    for (const [destination, source] of Object.entries(shipped.fixtures ?? {})) {
      assert.ok(existsSync(source), `${shipped.suite}/${shipped.name}: ${destination} -> ${source}`);
    }
  }
});

test("stat: median and spread over an even and an odd sample", () => {
  assert.deepEqual(stat([5, 1, 3]), { median: 3, min: 1, max: 5, spread: 4 });
  assert.deepEqual(stat([4, 2]), { median: 3, min: 2, max: 4, spread: 2 });
  assert.deepEqual(stat([]), { median: 0, min: 0, max: 0, spread: 0 });
});

test("compare: a delta inside the spread is reported as inconclusive", () => {
  // The rule that exists because a 32% token drop once accompanied a WORSE
  // answer. A move smaller than the run-to-run variation has shown nothing.
  const now = stat([10, 14, 12]); // median 12, spread 4
  const before = stat([13, 15, 14]); // median 14, spread 2
  assert.match(compare(now, before), /inconclusive — spread is 4/);

  // Clear of both spreads: a real move, reported with where it came from.
  const moved = stat([2, 3, 2]); // median 2, spread 1
  assert.equal(compare(moved, before), "-12 (was 14)");
});

test("compare: no baseline and no change each say so plainly", () => {
  assert.equal(compare(stat([1]), undefined), "—");
  assert.equal(compare(stat([5, 5]), stat([5, 5])), "unchanged");
});

test("config: the reported columns and their accessors are the same list", () => {
  // config.ts names the columns, report.ts reads them off an attempt. Split
  // across two files, one can gain a column the other has not — and the failure
  // mode is a metric silently missing from every report, which is invisible.
  assert.deepEqual(METRIC_KEYS, [...REPORTED_METRICS]);
});

test("config: the tool-denial list keeps a case answering from the LIBRARY", () => {
  // Not hygiene. WebSearch/WebFetch would let a run answer from a blog post,
  // which is evading the question this harness asks rather than answering it;
  // Agent would scatter one package's lookups across streams.
  for (const denied of ["WebSearch", "WebFetch", "Agent"]) {
    assert.ok(SESSION.disallowedTools.includes(denied as never), `${denied} must stay denied`);
  }
  assert.ok(!SESSION.allowedTools.includes("Agent" as never));
});

test("config: env vars override defaults, and a malformed one falls back", async () => {
  // A sweep that silently ran once because BAL_EVAL_REPEATS=three is worse than
  // one that ignored the variable.
  const before = process.env.BAL_EVAL_REPEATS;
  try {
    process.env.BAL_EVAL_REPEATS = "5";
    const { DEFAULTS: raised } = await import(`../src/config.js?raised=${Date.now()}`);
    assert.equal(raised.repeats, 5);

    process.env.BAL_EVAL_REPEATS = "three";
    const { DEFAULTS: bad } = await import(`../src/config.js?bad=${Date.now()}`);
    assert.equal(bad.repeats, 1, "a malformed value falls back to the default");
  } finally {
    if (before === undefined) delete process.env.BAL_EVAL_REPEATS;
    else process.env.BAL_EVAL_REPEATS = before;
  }
});

test("config: the filter regex catches every way a run has narrowed a document", () => {
  // Every one of these was written by a real run. `--client`/`--sigs`/a name are
  // flags, not filters, and must NOT count.
  for (const piped of ["bal library overview x/y | head -200", "bal library api x/y 2>&1 | grep -n Auth"]) {
    assert.ok(PATH_METRICS.filters.test(piped), piped);
  }
  for (const clean of ["bal library overview ballerina/http --client Client", "bal library type x/y Z --deps"]) {
    assert.ok(!PATH_METRICS.filters.test(clean), clean);
  }
});

test("config: defaults are sane without any environment", () => {
  assert.ok(DEFAULTS.concurrency >= 1);
  assert.ok(DEFAULTS.repeats >= 1);
  assert.ok(DEFAULTS.timeoutMinutes >= 1);
});

test("hostEnv: both credentials are stripped, everything else survives", () => {
  // Deleting rather than never-adding is the point: deployments/.env is already
  // in process.env by the time a session is built, and Claude Code ranks
  // ANTHROPIC_API_KEY above the keychain.
  const env = hostEnv({
    ANTHROPIC_API_KEY: "sk-ant-api-xxx",
    CLAUDE_CODE_OAUTH_TOKEN: "sk-ant-oat-xxx",
    PATH: "/usr/bin",
    HOME: "/home/dev",
  });
  assert.equal(env.ANTHROPIC_API_KEY, undefined);
  assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, undefined);
  assert.equal(env.PATH, "/usr/bin");
  assert.equal(env.HOME, "/home/dev");
});
