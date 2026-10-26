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

// The run's three scripts as the agent uses them, end to end: capture each
// scenario, assemble the report, check it. Each one is right on its own and the
// pipeline can still be wrong, which is how a scoped run came to fail at
// assembly: the checker knew the version's scope and the assembler did not.

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const SCRIPTS = dirname(fileURLToPath(import.meta.url));
const CAPTURE = join(SCRIPTS, "capture-scenario.mjs");
const ASSEMBLE = join(SCRIPTS, "assemble-report.mjs");
const CHECKER = join(SCRIPTS, "check-report.mjs");

const CLAIMS = `Feature: F1 Claims

  @story-F1.1
  Rule: A claim needs a receipt

    Scenario: Submitting with a receipt
      When Ann submits a claim
      Then it is listed
`;
const APPROVALS = `Feature: F2 Approvals

  @story-F2.1
  Rule: A manager sees pending claims

    Scenario: The queue
      When Sam opens the queue
      Then the claim is there

  @story-F2.4
  Rule: A deputy approves in the manager's place

    Scenario: Deputy approves
      When Dee approves
      Then the claim is approved
`;

const WHEN = { keyword: "When", text: "Ann submits a claim", command: "agent-browser click @e3", exit: 0 };
const THEN = { keyword: "Then", text: "it is listed", command: 'agent-browser wait --text "Taxi"', exit: 0 };

const CLAIM = {
  feature: "F1 Claims",
  featureFile: "specs/validation/acceptance/F1-claims.feature",
  line: 6,
  rule: "A claim needs a receipt",
  scenario: "Submitting with a receipt",
  outcome: "passed",
  steps: [WHEN, THEN],
};
const QUEUE = {
  feature: "F2 Approvals",
  featureFile: "specs/validation/acceptance/F2-approvals.feature",
  line: 6,
  rule: "A manager sees pending claims",
  scenario: "The queue",
  outcome: "failed",
  steps: [WHEN, { ...THEN, exit: 1, observed: "the queue was empty" }],
  reproduced: "yes",
  note: "nothing further",
  evidence: {
    network: [{ method: "GET", url: "/api/claims", status: 500 }],
    console: [],
    snapshot: '- table "Pending claims"\n  - rowgroup with no data rows',
  },
};
const DEPUTY = {
  feature: "F2 Approvals",
  featureFile: "specs/validation/acceptance/F2-approvals.feature",
  line: 13,
  rule: "A deputy approves in the manager's place",
  scenario: "Deputy approves",
  outcome: "blocked",
  steps: [{ keyword: "When", text: "Dee approves", observed: "no deputy login was provisioned" }],
  reproduced: "unattempted",
  note: "nothing further",
  evidence: { network: [], console: [] },
};

/** A project clone with both feature files and one commit, plus its own capture directory. */
function withProject(fn) {
  const root = mkdtempSync(join(tmpdir(), "acc-pipeline-"));
  const p = { dir: join(root, "repo"), captures: join(root, "captures"), run: join(root, "run.json") };
  try {
    mkdirSync(join(p.dir, "specs/validation/acceptance"), { recursive: true });
    writeFileSync(join(p.dir, "specs/validation/acceptance/F1-claims.feature"), CLAIMS);
    writeFileSync(join(p.dir, "specs/validation/acceptance/F2-approvals.feature"), APPROVALS);
    writeFileSync(p.run, JSON.stringify({ isolation: "each scenario submits its own claim", baseUrl: "https://claims.test" }));
    const git = (...args) => execFileSync("git", ["-C", p.dir, ...args], { stdio: "ignore" });
    git("init", "-q");
    git("-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "init");
    fn(p);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

/** Runs one script against the project's capture directory and returns { code, out }. */
function run(p, script, args, input) {
  try {
    const out = execFileSync("node", [script, ...args], {
      encoding: "utf8",
      input,
      env: { ...process.env, AEP_CAPTURE_DIR: p.captures },
      stdio: ["pipe", "pipe", "pipe"],
    });
    return { code: 0, out };
  } catch (e) {
    return { code: e.status, out: `${e.stdout ?? ""}${e.stderr ?? ""}` };
  }
}
const capture = (p, entry) => run(p, CAPTURE, [p.dir], JSON.stringify(entry));
const assemble = (p, ...scope) => run(p, ASSEMBLE, [p.dir, p.run, ...scope]);
const check = (p, ...scope) => run(p, CHECKER, [p.dir, ...scope]);

test("the captures assemble into a report the checker accepts", () => {
  withProject((p) => {
    for (const entry of [DEPUTY, CLAIM, QUEUE]) assert.equal(capture(p, entry).code, 0);
    mkdirSync(join(p.dir, "tests/acceptance/snapshots"), { recursive: true });
    writeFileSync(join(p.dir, "tests/acceptance/snapshots/left-by-an-earlier-attempt.txt"), "stale\n");

    const built = assemble(p);
    assert.equal(built.code, 0, built.out);
    const checked = check(p);
    assert.equal(checked.code, 0, checked.out);

    const report = JSON.parse(readFileSync(join(p.dir, "tests/acceptance/report.json"), "utf8"));
    assert.deepEqual(
      report.scenarios.map((s) => s.scenario),
      ["Submitting with a receipt", "The queue", "Deputy approves"],
      "feature-file order, not capture order",
    );
    const queue = report.scenarios[1];
    assert.equal(queue.evidence.snapshot, undefined, "the tree leaves the report");
    assert.equal(readFileSync(join(p.dir, queue.evidence.snapshotFile), "utf8"), `${QUEUE.evidence.snapshot}\n`);
    assert.ok(report.captureWindow?.first && report.captureWindow?.last);
    assert.ok(report.scenarios.every((s) => s.capturedAt === undefined), "stamps are reported once, per run");
    assert.ok(!existsSync(join(p.dir, "tests/acceptance/snapshots/left-by-an-earlier-attempt.txt")));
  });
});

// The name is derived, never chosen, so a re-driven scenario replaces its own
// earlier capture instead of leaving two entries that claim it.
test("capturing a scenario again replaces its earlier capture", () => {
  withProject((p) => {
    capture(p, QUEUE);
    capture(p, { ...QUEUE, note: "the second pass saw the same empty table" });
    assert.equal(readdirSync(p.captures).length, 1);
  });
});

// A mistyped title used to be written and only refused at assembly, after the
// page was gone; capturing again under the right title then left the bad file
// behind to fail every later assembly.
test("a capture naming a scenario no feature file declares is refused", () => {
  withProject((p) => {
    const { code, out } = capture(p, { ...CLAIM, scenario: "Submiting with a receipt" });
    assert.equal(code, 2, out);
    assert.match(out, /no such scenario/);
    assert.ok(!existsSync(p.captures) || readdirSync(p.captures).length === 0, "nothing is written");
  });
});

// The validation issue names a version's scope once, and the agent passes it to
// both scripts. Built from one definition, they agree on what is owed.
test("a scoped run assembles and checks with the issue's arguments", () => {
  withProject((p) => {
    for (const entry of [CLAIM, QUEUE]) capture(p, entry);
    const scope = ["--features", "F1,F2", "--held-back", "F2.4"];

    const built = assemble(p, ...scope);
    assert.equal(built.code, 0, built.out);
    const checked = check(p, ...scope);
    assert.equal(checked.code, 0, checked.out);

    const unscoped = assemble(p);
    assert.equal(unscoped.code, 2, "without the scope, the held-back rule is owed a capture");
    assert.match(unscoped.out, /no capture for .*Deputy approves/);
    assert.match(unscoped.out, /pass them here/);
  });
});

test("a capture outside the scope is refused by name", () => {
  withProject((p) => {
    for (const entry of [CLAIM, QUEUE]) capture(p, entry);
    const { code, out } = assemble(p, "--features", "F1");
    assert.equal(code, 2, out);
    assert.match(out, /The queue.*outside this version's scope — remove .*\.json/);
  });
});
