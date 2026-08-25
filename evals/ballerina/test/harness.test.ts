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
import { spawnSync } from "node:child_process";
import { readAgentBuilds } from "../src/metrics/build.js";
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { discoverCases, selectCases } from "../src/cases.js";
import { archive, prepareScratch, readSources } from "../src/scratch.js";
import { compare, pickBaseline, stat, METRIC_KEYS, type Summary } from "../src/report.js";
import { hostEnv, workingTreeToolJars } from "../src/preflight.js";
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

test("cases: a source assertion is parsed and its regex compiled at load", () => {
  const root = casesTree({
    narrow: {
      "a.yaml":
        "prompt: p\nexpect:\n  mustContain:\n    - 'kafkaProducer->send'\n  mustNotContain:\n    - 'return true'\n",
    },
  });
  const found = discoverCases(root);
  assert.deepEqual(found[0]?.expect?.mustContain, ["kafkaProducer->send"]);
  assert.deepEqual(found[0]?.expect?.mustNotContain, ["return true"]);
});

test("cases: a malformed assertion regex fails the run at load, not an hour into a sweep", () => {
  // A bad pattern found mid-sweep surfaces as a harness error on one attempt after
  // the lanes are already spent; found at load it costs nothing.
  const root = casesTree({ narrow: { "a.yaml": "prompt: p\nexpect:\n  mustContain:\n    - '('\n" } });
  assert.throws(() => discoverCases(root), /is not a valid regex/);
});

test("cases: an unknown expectation key is refused rather than silently ignored", () => {
  // The failure being defended against: `mustContian:` asserts nothing, reports a
  // pass, and reads identically to a case that genuinely passed.
  const root = casesTree({ narrow: { "a.yaml": "prompt: p\nexpect:\n  mustContian:\n    - x\n" } });
  assert.throws(() => discoverCases(root), /unknown expectation 'mustContian'/);
});

test("cases: selection is exact, so a typo selects nothing rather than something else", () => {
  const root = casesTree({ narrow: { "aws-s3-submodule.yaml": "prompt: p\n", "http.yaml": "prompt: p\n" } });
  const all = discoverCases(root);
  assert.equal(selectCases(all, undefined, "aws-s3-submodule").length, 1);
  assert.equal(selectCases(all, undefined, "aws-s3").length, 0, "a prefix must not match");
  assert.equal(selectCases(all, "narrow").length, 2);
  assert.equal(selectCases(all, "full").length, 0);
});

test("cases: a comma-separated selection picks exactly those cases", () => {
  // The tuning loop runs a handful of cases, not one and not a whole suite. Without
  // this it is several sweeps, each with its own report and its own baseline diff.
  const root = casesTree({
    narrow: { "a.yaml": "prompt: p\n", "b.yaml": "prompt: p\n" },
    service: { "c.yaml": "prompt: p\n" },
  });
  const all = discoverCases(root);
  assert.deepEqual(
    selectCases(all, undefined, "a,c").map((c) => c.name),
    ["a", "c"],
  );
  assert.deepEqual(
    selectCases(all, "narrow,service", "c").map((c) => c.name),
    ["c"],
    "suite and case still intersect",
  );
  assert.equal(selectCases(all, undefined, "a, b").length, 2, "surrounding space is trimmed");
  assert.equal(selectCases(all, undefined, "a,nope").length, 1, "an unknown member selects nothing of its own");
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
  // Every one of these was written by a real run. A selector, `-s` and `-r` are
  // arguments, not filters, and must NOT count.
  for (const piped of ["bal library overview x/y | head -200", "bal library api x/y 2>&1 | grep -n Auth"]) {
    assert.ok(PATH_METRICS.filters.test(piped), piped);
  }
  for (const clean of [
    "bal library client ballerina/http Client",
    "bal library client ballerinax/github -s \"cache\"",
    "bal library type x/y Z -r",
  ]) {
    assert.ok(!PATH_METRICS.filters.test(clean), clean);
  }
});

/**
 * H4: a `bal build` in a scratch directory is not a build cycle of the case.
 *
 * The test used to be `/\bbal build\b/` against the command and nothing else, so
 * a probe an agent ran to check one signature counted as a compiler round trip
 * of the project under test — and "agent build cycles" is one of the three
 * headline numbers. Conservative on purpose: a command this cannot classify
 * counts as a project build, because under-counting the number the tool is meant
 * to drive down would flatter the tool.
 */
test("build: a probe build somewhere else is not a cycle of the case", () => {
  const green = "Compiling source\n\tacme/app:0.1.0\n\nGenerating executable\n";
  const counted = ["bal build", "bal build --offline", "cd myproject && bal build"];
  const ignored = ["cd /tmp/probe && bal build", "bal build /tmp/probe", "cd $TMPDIR/x && bal build"];
  for (const command of counted) {
    assert.equal(readAgentBuilds([{ command, output: green }]).length, 1, command);
  }
  for (const command of ignored) {
    assert.equal(readAgentBuilds([{ command, output: green }]).length, 0, command);
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

test("baseline: a one-case debug run does not become the baseline for a full sweep", () => {
  // The measured shape: two full sweeps with a single-case re-run between them.
  // Newest-directory-wins picked the re-run, which would have dashed six cases
  // and diffed the seventh against different conditions.
  const runs: Record<string, Summary[]> = {
    "2026-08-15T12-00-00Z": [summaryFor("narrow/a"), summaryFor("service/b")],
    "2026-08-16T03-35-00Z": [summaryFor("service/b")],
  };
  const read = (dir: string): Summary[] | undefined => runs[dir];
  const dirs = Object.keys(runs);

  assert.deepEqual(
    pickBaseline(dirs, ["narrow/a", "service/b"], read)?.map((s) => s.key),
    ["narrow/a", "service/b"],
    "walks past the newer run that covers only one of the two cases",
  );
  // A narrower sweep still baselines against a wider run that contains it.
  assert.ok(pickBaseline(dirs, ["service/b"], read));
  // Nothing covers it: no column beats a misleading one.
  assert.equal(pickBaseline(dirs, ["service/c"], read), undefined);
});

test("baseline: a sweep that ran without the tool is never the baseline", () => {
  // Measured, and it inverted three rows at once. The 2026-08-17 sweep whose sessions
  // dropped the `library` registration reported telemetry-kafka at 16 lookup tokens
  // and 1 turn — a run with no tool in it. The next, clean sweep then diffed 6,500
  // tokens against that 16 and printed "+6484", which reads as a 400x regression when
  // it is the tool coming back. A baseline is a claim about conditions, and those
  // conditions did not include the thing under test.
  const runs: Record<string, Summary[]> = {
    "2026-08-17T10-00-00Z": [withMissing("service/a", 0), withMissing("service/b", 0)],
    "2026-08-17T11-00-00Z": [withMissing("service/a", 0), withMissing("service/b", 4)],
  };
  const read = (dir: string): Summary[] | undefined => runs[dir];
  const dirs = Object.keys(runs);

  const picked = pickBaseline(dirs, ["service/a", "service/b"], read);
  assert.ok(picked, "a clean older sweep is still available");
  assert.equal(picked?.[1]?.stats.toolMissing?.max, 0, "walked past the contaminated newer sweep");

  // One spoiled case spoils the sweep as a baseline: the attempts ran concurrently
  // and shared one `bal-tools.toml`, so a neighbour's clean row is not proof.
  const onlyDirty: Record<string, Summary[]> = { "2026-08-17T11-00-00Z": runs["2026-08-17T11-00-00Z"] ?? [] };
  assert.equal(
    pickBaseline(Object.keys(onlyDirty), ["service/a"], (d) => onlyDirty[d]),
    undefined,
    "no column beats a misleading one",
  );
});

function summaryFor(key: string): Summary {
  return { key, suite: "", case: "", attempts: 1, green: 1, clean: 1, stats: {}, violations: {} };
}

/** A summary carrying a `toolMissing` stat, which is what disqualifies a baseline. */
function withMissing(key: string, missing: number): Summary {
  return {
    ...summaryFor(key),
    stats: { toolMissing: { median: missing, min: missing, max: missing, spread: 0 } },
  };
}

/**
 * Why one test here is conditional, when nothing else in this file is.
 *
 * `prepareScratch` runs a REAL `bal new` — that is the point of it, since the
 * scaffold is what an attempt starts from. This package is a HOST-ONLY harness
 * (evals/ballerina/AGENTS.md): it measures the developer's own toolchain, and
 * the repo deliberately keeps Ballerina out of CI — even the tool's own job
 * notes its tests need no distribution. So on a runner this test cannot run.
 *
 * ANNOUNCED rather than silent. node:test prints `# SKIP <reason>` into the TAP
 * output, so a run that skipped it says so and names why; the alternative was a
 * red build on every push, which is what sent me here.
 */
const BAL_MISSING =
  spawnSync("bal", ["version"], { stdio: "ignore" }).error !== undefined
    ? "needs `bal` on PATH — host-only eval harness, and CI runners carry no Ballerina distribution"
    : undefined;

test("scratch: an attempt runs outside the repo and is archived back afterwards", { skip: BAL_MISSING }, () => {
  // B3. When the workspace lived under `.runs/`, `cases/` was a few `..` from the
  // session's cwd — and the claims-fhir attempt of the 2026-08-16 sweep read its own
  // expect.imports and reversed a design decision on the strength of it. What the
  // session can reach is the assertion here; the archive copy is what keeps the
  // README's promise that you can cd into the result and build it by hand.
  const runs = mkdtempSync(join(tmpdir(), "bal-evals-runs-"));
  const skills = mkdtempSync(join(tmpdir(), "bal-evals-skills-"));
  mkdirSync(join(skills, "ballerina"), { recursive: true });
  writeFileSync(join(skills, "ballerina", "SKILL.md"), "# skill\n");

  const evalCase = { name: "smoke", suite: "narrow", file: "", prompt: "p" };
  const scratch = prepareScratch(runs, evalCase, 1, skills);

  assert.ok(
    !scratch.workspace.startsWith(runs),
    `the session's cwd must not be inside the runs tree, got ${scratch.workspace}`,
  );
  assert.ok(existsSync(join(scratch.workspace, "Ballerina.toml")), "a real bal new package");
  assert.ok(existsSync(join(scratch.workspace, ".claude", "skills", "ballerina", "SKILL.md")));

  archive(scratch);
  assert.ok(existsSync(join(scratch.archiveDir, "Ballerina.toml")), "archived for a human to open");
  assert.ok(scratch.archiveDir.startsWith(runs), "the archive lands under .runs/");
  assert.ok(!existsSync(scratch.workspace), "staging is dropped once copied");
});

test("scratch: source assertions read the package's own .bal, not the mirrored skill", () => {
  // The skill carries worked examples, so a pattern like `http:RetryConfig` appears
  // in `code-rules.md` whether or not the agent wrote it. Matching the mirror would
  // let every case assert itself. `target/` is build output for the same reason.
  const workspace = mkdtempSync(join(tmpdir(), "bal-eval-sources-"));
  writeFileSync(join(workspace, "service.bal"), "import ballerina/http;\nhttp:RetryConfig cfg = {};\n");
  mkdirSync(join(workspace, ".claude", "skills", "ballerina"), { recursive: true });
  writeFileSync(join(workspace, ".claude", "skills", "ballerina", "leak.bal"), "SKILL_EXAMPLE_MARKER");
  mkdirSync(join(workspace, "target"), { recursive: true });
  writeFileSync(join(workspace, "target", "generated.bal"), "BUILD_OUTPUT_MARKER");

  const sources = readSources(workspace);
  assert.match(sources, /http:RetryConfig/);
  assert.doesNotMatch(sources, /SKILL_EXAMPLE_MARKER/);
  assert.doesNotMatch(sources, /BUILD_OUTPUT_MARKER/);
});

test("preflight: the built jar is found by looking, so a version bump cannot silence the stale check", () => {
  // The filename carries the tool's version. Spelling it here (or in config.ts)
  // is a second copy of a value whose only home is the tool's gradle.properties:
  // a bump would make the lookup miss, and a miss disables the stale-jar refusal
  // WITHOUT SAYING SO — the sweep would quietly measure the previous CLI.
  const libs = mkdtempSync(join(tmpdir(), "bal-eval-libs-"));
  assert.deepEqual(workingTreeToolJars(libs), [], "an empty output directory is not a build");

  writeFileSync(join(libs, "native-9.9.9-SNAPSHOT.jar"), "jar");
  assert.deepEqual(
    workingTreeToolJars(libs),
    [join(libs, "native-9.9.9-SNAPSHOT.jar")],
    "a version this repo has never pinned is still found",
  );
});

test("preflight: a stale jar from a previous version is REPORTED, not silently skipped", () => {
  // `gradlew :native:jar` does not clean, so a version bump leaves both jars
  // behind. Answering "no working-tree build" there would drop the stale check
  // without a word — the silent outcome preflight exists to prevent. The caller
  // turns a length > 1 into a blocker, so both have to reach it.
  const libs = mkdtempSync(join(tmpdir(), "bal-eval-bumped-"));
  writeFileSync(join(libs, "native-0.1.0-SNAPSHOT.jar"), "old");
  writeFileSync(join(libs, "native-0.2.0-SNAPSHOT.jar"), "new");

  const found = workingTreeToolJars(libs);
  assert.equal(found.length, 2, "both jars are reported so the caller can refuse");
  assert.deepEqual(found.map((jar) => basename(jar)), [
    "native-0.1.0-SNAPSHOT.jar",
    "native-0.2.0-SNAPSHOT.jar",
  ]);
});

test("preflight: a non-existent output directory is not a build", () => {
  assert.deepEqual(workingTreeToolJars(join(tmpdir(), `bal-eval-absent-${process.pid}`)), []);
});
