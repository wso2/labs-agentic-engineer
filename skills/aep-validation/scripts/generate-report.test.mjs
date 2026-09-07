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

// The merge is the one part of this script that can produce a WRONG verdict
// rather than a loud failure, so it is pinned here against fixtures.
//
// Driven as a subprocess, because the script owns its exit code — exit 2 is
// half its contract and cannot be observed by importing it.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";

const SCRIPT = path.join(import.meta.dirname, "generate-report.mjs");
const ROOT_DIR = "/repo/tests/e2e/specs";

/** One Playwright JSON reporter document covering the given specs. */
function pwRun(specs, { rootDir = ROOT_DIR, version = "1.61.1" } = {}) {
  return {
    config: { rootDir, version },
    suites: specs.map(({ id, status, file = `${id}.spec.ts` }) => ({
      title: file,
      file,
      specs: [
        {
          title: `${id}: does the thing`,
          file,
          line: 3,
          column: 1,
          tests: [
            {
              status,
              results: [
                {
                  duration: 10,
                  ...(status === "unexpected" ? { error: { message: "boom" } } : {}),
                },
              ],
            },
          ],
        },
      ],
    })),
  };
}

/**
 * A throwaway project tree. `runs` is an ordered list, written under names that
 * sort in the order given — which is what the merge reads as time order.
 */
function fixture({ criteria, runs = [], specFiles = {}, resultsAsFile = false }) {
  const dir = mkdtempSync(path.join(tmpdir(), "genreport-"));
  mkdirSync(path.join(dir, "specs/validation"), { recursive: true });
  mkdirSync(path.join(dir, "tests/e2e/specs"), { recursive: true });
  writeFileSync(
    path.join(dir, "specs/validation/validation-criteria.json"),
    JSON.stringify({
      requirements: [{ id: "REQ-001", statement: "It works", criteria }],
    }),
  );
  for (const [name, body] of Object.entries(specFiles)) {
    writeFileSync(path.join(dir, "tests/e2e/specs", name), body);
  }
  let results;
  if (resultsAsFile) {
    results = "tests/e2e/test-results/results.json";
    mkdirSync(path.join(dir, "tests/e2e/test-results"), { recursive: true });
    writeFileSync(path.join(dir, results), JSON.stringify(runs[0]));
  } else {
    results = "tests/e2e/test-results/runs";
    mkdirSync(path.join(dir, results), { recursive: true });
    runs.forEach((doc, i) => {
      writeFileSync(path.join(dir, results, `2026-09-02T00-00-0${i}-000Z.json`), JSON.stringify(doc));
    });
  }
  return { dir, results };
}

function run({ dir, results }) {
  const r = spawnSync(
    process.execPath,
    [SCRIPT, "--issue", "30", "--commit", "deadbeef", "--results", results, "--specs", "tests/e2e/specs"],
    { cwd: dir, encoding: "utf8" },
  );
  let report = null;
  try {
    report = JSON.parse(readFileSync(path.join(dir, "tests/validation/report.json"), "utf8"));
  } catch {
    // exit 2 writes nothing, which is the point of several tests below
  }
  return { code: r.status, stderr: r.stderr, report };
}

const spec = (id) => `// spec: tests/validation/test-plan.md § ${id}\ntest('${id}: x', () => {});\n`;
const e2e = (id) => ({ id, must: `${id} holds`, method: "e2e" });
const statusOf = (report, id) => report.criteria.find((c) => c.id === id)?.status;

// ---- the contract between the config and this script ---------------------

test("generate-report: the config writes where this script reads", () => {
  // Two files on opposite sides of a copy: the config template is scaffolded
  // into the project repo and writes results; this script is invoked from
  // $AEP_SKILLS_DIR and reads them. Nothing imports across that gap, so a
  // renamed directory would break every validation run silently — the report
  // would find an empty directory and say no run had written results.
  //
  // Pinned the same way services/aep-api pins ReportFilePath across its own
  // split. If this fails, the two sides disagree about where results live.
  const template = readFileSync(
    path.join(import.meta.dirname, "../assets/playwright.config.template.ts"),
    "utf8",
  );
  const outputFile = /outputFile:\s*`([^`]+)`/.exec(template)?.[1];
  assert.ok(outputFile, "the template must configure a json reporter outputFile");
  assert.match(outputFile, /^test-results\/runs\/\$\{STAMP\}\.json$/);

  const script = readFileSync(SCRIPT, "utf8");
  const defaultResults = /results:\s*"([^"]+)"/.exec(script)?.[1];
  assert.equal(defaultResults, "tests/e2e/test-results/runs");

  // The template's path is relative to tests/e2e (npm --prefix runs it there);
  // this script's default is relative to the repo root. Same directory.
  assert.equal(path.posix.join("tests/e2e", outputFile.replace("/${STAMP}.json", "")), defaultResults);
});

test("generate-report: the stamp sorts chronologically", () => {
  // Merge order IS filename order, so the stamp must sort in time order or a
  // heal's re-run stops superseding the failure it repaired. ISO-8601 with the
  // colons and dot swapped for hyphens keeps that property.
  const stamp = (iso) => iso.replace(/[:.]/g, "-");
  const earlier = stamp("2026-09-02T08:10:59.240Z");
  const later = stamp("2026-09-02T08:11:00.235Z");
  assert.ok(earlier < later, `${earlier} must sort before ${later}`);
  assert.ok(stamp("2026-09-02T09:00:00.000Z") > later);
  // Across a midnight and a year boundary too.
  assert.ok(stamp("2026-09-02T23:59:59.999Z") < stamp("2026-09-03T00:00:00.000Z"));
  assert.ok(stamp("2026-12-31T23:59:59.999Z") < stamp("2027-01-01T00:00:00.000Z"));
});

// ---- the merge -----------------------------------------------------------

test("generate-report: a later pass supersedes an earlier failure", () => {
  // The heal loop's whole shape: a spec fails, is repaired, is re-run alone.
  // Folding both runs' spec.tests[] together instead would keep the failure,
  // because specOutcome is fail-dominant and has no notion of time.
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "unexpected" }]), pwRun([{ id: "AC-001-a", status: "expected" }])],
  });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.equal(statusOf(report, "AC-001-a"), "pass");
  assert.equal(report.totals.e2e.fail, 0);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: a later failure supersedes an earlier pass", () => {
  // Newest-wins runs both ways: a whole-suite run that fails a spec which
  // passed in isolation is a real finding, not noise to be outvoted.
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }]), pwRun([{ id: "AC-001-a", status: "unexpected" }])],
  });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.equal(statusOf(report, "AC-001-a"), "fail");
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: the same criterion across batches is not a duplicate", () => {
  // It used to be exit 2. Under merging it is a spec that ran more than once,
  // which is the entire point.
  const f = fixture({
    criteria: [e2e("AC-001-a"), e2e("AC-002-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a"), "AC-002-a.spec.ts": spec("AC-002-a") },
    runs: [
      pwRun([{ id: "AC-001-a", status: "expected" }]),
      pwRun([{ id: "AC-001-a", status: "expected" }, { id: "AC-002-a", status: "expected" }]),
    ],
  });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.equal(report.totals.e2e.pass, 2);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: two spec files claiming one criterion in ONE run still fails", () => {
  // The authoring bug the old check existed for, which merging must not lose.
  const doc = pwRun([{ id: "AC-001-a", status: "expected" }]);
  doc.suites.push({
    title: "copy.spec.ts",
    file: "copy.spec.ts",
    specs: [{ title: "AC-001-a: a second file", file: "copy.spec.ts", line: 3, column: 1, tests: [{ status: "expected", results: [{ duration: 1 }] }] }],
  });
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [doc],
  });
  const { code, stderr } = run(f);
  assert.equal(code, 2);
  assert.match(stderr, /duplicate spec title prefix AC-001-a within/);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: batch provenance names which run answered each criterion", () => {
  const f = fixture({
    criteria: [e2e("AC-001-a"), e2e("AC-002-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a"), "AC-002-a.spec.ts": spec("AC-002-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }]), pwRun([{ id: "AC-002-a", status: "expected" }])],
  });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.equal(report.batches.length, 2);
  assert.deepEqual(report.batches.map((b) => b.specs), [1, 1]);
  assert.notEqual(
    report.criteria.find((c) => c.id === "AC-001-a").batch,
    report.criteria.find((c) => c.id === "AC-002-a").batch,
  );
  rmSync(f.dir, { recursive: true, force: true });
});

// ---- the coverage invariant ---------------------------------------------

test("generate-report: a spec on disk with no result anywhere is a hard error", () => {
  // This is what replaces "one run must cover everything". A severed run
  // writes nothing at all, so its specs land here — named, and re-runnable.
  const f = fixture({
    criteria: [e2e("AC-001-a"), e2e("AC-002-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a"), "AC-002-a.spec.ts": spec("AC-002-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }])],
  });
  const { code, stderr, report } = run(f);
  assert.equal(code, 2);
  assert.match(stderr, /no test result for 1 spec\(s\) that exist on disk: AC-002-a\.spec\.ts/);
  assert.equal(report, null, "a refused report must not be written");
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: a result for the same criterion under a DIFFERENT file does not cover it", () => {
  // Results accumulate across the whole run, so a batch recorded before a spec
  // was moved lingers. Keying coverage on the criterion id alone would let that
  // stale result vouch for a file that has never been executed.
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "expected", file: "old-layout/AC-001-a.spec.ts" }])],
  });
  const { code, stderr } = run(f);
  assert.equal(code, 2);
  assert.match(stderr, /no test result for 1 spec\(s\) that exist on disk: AC-001-a\.spec\.ts/);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: a criterion with no spec file at all is a legitimate not_run", () => {
  // The other half of the split: nobody wrote a test for it, which is honest
  // and must not be confused with a run that went missing.
  const f = fixture({
    criteria: [e2e("AC-001-a"), e2e("AC-009-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }])],
  });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.equal(statusOf(report, "AC-009-a"), "not_run");
  assert.equal(report.criteria.find((c) => c.id === "AC-009-a").batch, null);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: the header gate covers a spec absent from every result", () => {
  // Scoped to the results, a severed run's specs vanished from the tree and
  // their missing headers went unreported — the report exited 0 having
  // skipped a check on exactly the specs whose run had failed.
  const f = fixture({
    criteria: [e2e("AC-001-a"), e2e("AC-002-a")],
    specFiles: {
      "AC-001-a.spec.ts": spec("AC-001-a"),
      "AC-002-a.spec.ts": "test('AC-002-a: x', () => {});\n",
    },
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }])],
  });
  const { code, stderr } = run(f);
  assert.equal(code, 2);
  assert.match(stderr, /AC-002-a\.spec\.ts is missing its "\/\/ spec:" header/);
  rmSync(f.dir, { recursive: true, force: true });
});

// ---- config collapse and back-compat ------------------------------------

test("generate-report: batches recorded under different rootDirs are refused", () => {
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [
      pwRun([{ id: "AC-001-a", status: "expected" }]),
      pwRun([{ id: "AC-001-a", status: "expected" }], { rootDir: "/elsewhere/specs" }),
    ],
  });
  const { code, stderr } = run(f);
  assert.equal(code, 2);
  assert.match(stderr, /different rootDir values/);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: a single results FILE still works", () => {
  // What one complete run looks like, and what every report written before
  // batching read.
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }])],
    resultsAsFile: true,
  });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.equal(statusOf(report, "AC-001-a"), "pass");
  assert.equal(report.batches.length, 1);
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: a missing specs directory degrades visibly, not silently", () => {
  // Without the warning the coverage invariant, the header gate and the locator
  // lint all pass over an empty list and the report looks complete having
  // checked nothing — the same failure the heal check's `healCheckSkipped`
  // warning exists to prevent, one hunk above it in the same file.
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: {},
    runs: [pwRun([{ id: "AC-001-a", status: "expected" }])],
  });
  rmSync(path.join(f.dir, "tests/e2e/specs"), { recursive: true, force: true });
  const { code, report } = run(f);
  assert.equal(code, 0);
  assert.ok(
    (report.warnings ?? []).some((w) => /no spec directory at/.test(w)),
    `expected a skipped-checks warning, got ${JSON.stringify(report.warnings)}`,
  );
  rmSync(f.dir, { recursive: true, force: true });
});

test("generate-report: an empty runs directory says no run has written results", () => {
  const f = fixture({
    criteria: [e2e("AC-001-a")],
    specFiles: { "AC-001-a.spec.ts": spec("AC-001-a") },
    runs: [],
  });
  const { code, stderr } = run(f);
  assert.equal(code, 2);
  assert.match(stderr, /holds no \.json files/);
  rmSync(f.dir, { recursive: true, force: true });
});
