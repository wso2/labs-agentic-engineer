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

// The checker is the only thing holding the graded party to its own report, so
// what it FAILS TO ASK is the interesting case — a rule it never applies reads
// exactly like a report that satisfies it.

import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const CHECKER = join(dirname(fileURLToPath(import.meta.url)), "check-report.mjs");

const FEATURE = `Feature: Lists

  Rule: A duplicate is rejected

    Scenario: Adding a duplicate
      When Dan adds it again
      Then the row appears
      And it shows today's date
`;

/** Runs the checker over one report and returns { code, out }. */
/** What a non-passed outcome owes beyond its steps, so a case can drop one. */
const NON_PASSED = {
  reproduced: "yes",
  note: "nothing further",
  evidence: { network: [], console: [], snapshotFile: "tests/acceptance/snapshots/dup.txt" },
};

function check(steps, outcome = "passed", overrides = {}) {
  const dir = mkdtempSync(join(tmpdir(), "acc-check-"));
  try {
    mkdirSync(join(dir, "specs/validation/acceptance"), { recursive: true });
    mkdirSync(join(dir, "tests/acceptance/snapshots"), { recursive: true });
    writeFileSync(join(dir, "specs/validation/acceptance/lists.feature"), FEATURE);
    writeFileSync(join(dir, "tests/acceptance/snapshots/dup.txt"), '- listitem "Milk"\n');
    writeFileSync(
      join(dir, "tests/acceptance/report.json"),
      JSON.stringify({
        schemaVersion: 3,
        commit: "abc123",
        isolation: "each scenario owns its own list",
        scenarios: [
          {
            feature: "Lists",
            featureFile: "specs/validation/acceptance/lists.feature",
            line: 5,
            rule: "A duplicate is rejected",
            scenario: "Adding a duplicate",
            outcome,
            steps,
            ...(outcome === "passed" ? {} : NON_PASSED),
            ...overrides,
          },
        ],
      }),
    );
    try {
      return { code: 0, out: execFileSync("node", [CHECKER, dir], { encoding: "utf8" }) };
    } catch (e) {
      return { code: e.status, out: `${e.stdout ?? ""}${e.stderr ?? ""}` };
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

const WHEN = { keyword: "When", text: "Dan adds it again", command: "agent-browser click @e3", exit: 0 };
const THEN = { keyword: "Then", text: "the row appears", command: 'agent-browser wait --text "Milk"', exit: 0 };
const AND_OK = {
  keyword: "And",
  text: "it shows today's date",
  command: "agent-browser get value @e1",
  exit: 0,
  observed: "2026-09-18",
};
const FAILING = [WHEN, { ...THEN, exit: 1, observed: "the row never appeared" }, AND_OK];

// Gherkin says a continuation IS the keyword above it, and the step that settles
// a scenario is routinely the continuation. Matching the raw keyword made every
// rule here skip it: it did not count as an assertion, so it could not fail a
// `passed`; it was never asked for `observed`; and it could not name a `failed`.
// The Go reader resolves these the same way, so a report that slipped past here
// was still read as an assertion there.
test("an `And` continuing a `Then` is an assertion, and must be backed like one", () => {
  const { code, out } = check([WHEN, THEN, { keyword: "And", text: "it shows today's date" }]);
  assert.equal(code, 2, `a passed whose And carries no command must fail:\n${out}`);
  assert.match(out, /2 Then steps carry no command\+exit/);
});

test("an `And` that IS backed passes", () => {
  const { code, out } = check([
    WHEN,
    THEN,
    { keyword: "And", text: "it shows today's date", command: "agent-browser get value @e1", exit: 0, observed: "2026-09-18" },
  ]);
  assert.equal(code, 0, out);
});

// `observed` is required wherever the exit code does not settle the step — every
// step, not only the assertions. A `When` settled by a value-returning command is
// where the run records what the system did: the 401 rule reads it, and the Go
// reader falls back to it when no `Then` observed anything.
test("a `When` that prints a value must record what it read", () => {
  const { code, out } = check([
    { keyword: "When", text: "Dan adds it again", command: 'agent-browser get count ".item"', exit: 0 },
    THEN,
    { keyword: "And", text: "it shows today's date", command: "agent-browser get value @e1", exit: 0, observed: "2026-09-18" },
  ]);
  assert.equal(code, 2, `a value-returning When with no observed must fail:\n${out}`);
  assert.match(out, /a When carries no `observed`/);
});

test("a nonzero exit on any step must say what was there instead", () => {
  const { code, out } = check(
    [WHEN, { ...THEN, exit: 1 }, { keyword: "And", text: "it shows today's date", command: "agent-browser get value @e1", exit: 0, observed: "2026-09-18" }],
    "failed",
  );
  assert.equal(code, 2, `a nonzero exit with no observed must fail:\n${out}`);
  assert.match(out, /carries no `observed` and the command exited nonzero/);
});

// A version validates its built scope (B4): a feature designed but not built,
// and a rule whose stories wait on a feature nobody built, are not run — and a
// report that answers them reports on code this version did not build.
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

function checkScoped(entries, args) {
  const dir = mkdtempSync(join(tmpdir(), "acc-scope-"));
  try {
    mkdirSync(join(dir, "specs/validation/acceptance"), { recursive: true });
    mkdirSync(join(dir, "tests/acceptance"), { recursive: true });
    writeFileSync(join(dir, "specs/validation/acceptance/F1-claims.feature"), CLAIMS);
    writeFileSync(join(dir, "specs/validation/acceptance/F2-approvals.feature"), APPROVALS);
    const entry = ([feature, file, line, rule, scenario]) => ({
      feature,
      featureFile: `specs/validation/acceptance/${file}`,
      line,
      rule,
      scenario,
      outcome: "passed",
      steps: [WHEN, THEN],
    });
    writeFileSync(
      join(dir, "tests/acceptance/report.json"),
      JSON.stringify({ schemaVersion: 3, commit: "abc123", isolation: "own data", scenarios: entries.map(entry) }),
    );
    try {
      return { code: 0, out: execFileSync("node", [CHECKER, dir, ...args], { encoding: "utf8" }) };
    } catch (e) {
      return { code: e.status, out: `${e.stdout ?? ""}${e.stderr ?? ""}` };
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

const CLAIM_ENTRY = ["F1 Claims", "F1-claims.feature", 6, "A claim needs a receipt", "Submitting with a receipt"];
const QUEUE_ENTRY = ["F2 Approvals", "F2-approvals.feature", 6, "A manager sees pending claims", "The queue"];

test("--features expects only the built features' scenarios", () => {
  const { code, out } = checkScoped([CLAIM_ENTRY], ["--features", "F1"]);
  assert.equal(code, 0, out);
  assert.equal(checkScoped([CLAIM_ENTRY], []).code, 2, "unscoped, F2's scenarios are missing");
});

test("--held-back drops a rule whose every story is held back", () => {
  const { code, out } = checkScoped([CLAIM_ENTRY, QUEUE_ENTRY], ["--features", "F1,F2", "--held-back", "F2.4"]);
  assert.equal(code, 0, out);
});

test("an entry outside the scope is a breach", () => {
  const { code, out } = checkScoped([CLAIM_ENTRY, QUEUE_ENTRY], ["--features", "F1"]);
  assert.equal(code, 2);
  assert.match(out, /no such scenario/);
});

test("a failure carrying everything it owes passes", () => {
  const { code, out } = check(FAILING, "failed");
  assert.equal(code, 0, out);
});

// A failure that passes on a second pass is a race, not a logic defect, and
// nothing else in the report separates the two. Unrecoverable once the run ends,
// so the gate is here rather than in prose.
test("a non-passed scenario must say whether it reproduced", () => {
  const { code, out } = check(FAILING, "failed", { reproduced: undefined });
  assert.equal(code, 2, `a failure with no \`reproduced\` must fail:\n${out}`);
  assert.match(out, /`reproduced` is undefined/);
});

test("`reproduced` outside its vocabulary is refused", () => {
  const { code, out } = check(FAILING, "failed", { reproduced: "sometimes" });
  assert.equal(code, 2, out);
  assert.match(out, /must be one of yes, no, unattempted/);
});

// The schema's other fields are what the agent was asked for. This one is where it
// says what nobody thought to ask — the class of observation that three real runs
// put in invented keys because there was nowhere to put it.
test("a non-passed scenario must carry a note", () => {
  const { code, out } = check(FAILING, "failed", { note: "   " });
  assert.equal(code, 2, `a failure with a blank \`note\` must fail:\n${out}`);
  assert.match(out, /no `note`/);
});

// Capture nobody reads is the mistake a real run already made eleven times in one
// report, every one of them the same stale page.
test("a pass may not carry evidence", () => {
  const { code, out } = check([WHEN, THEN, AND_OK], "passed", {
    evidence: { network: [], console: [] },
  });
  assert.equal(code, 2, `a passed carrying evidence must fail:\n${out}`);
  assert.match(out, /nothing reads it on a pass/);
});

// A block is the outcome a person has to judge from the report alone, and the
// network trace is what separates "the control was disabled so I never tried" from
// "I attempted it against the API and this came back".
test("a block owes network and console too", () => {
  const { code, out } = check([WHEN, { ...THEN, command: "", exit: undefined, observed: "the control was [disabled]" }], "blocked", {
    evidence: undefined,
  });
  assert.equal(code, 2, `a blocked with no evidence must fail:\n${out}`);
  assert.match(out, /blocked with no `evidence`/);
});

test("a block does not owe the page tree", () => {
  const { code, out } = check(
    [WHEN, { ...THEN, command: "", exit: undefined, observed: "the control was [disabled]" }],
    "blocked",
    { evidence: { network: [], console: [] } },
  );
  assert.equal(code, 0, out);
});

// A path to a file that was never written reads exactly like evidence until
// somebody follows it — and that is the one moment it cannot be recovered.
test("a snapshotFile that does not resolve is refused", () => {
  const { code, out } = check(FAILING, "failed", {
    evidence: { network: [], console: [], snapshotFile: "tests/acceptance/snapshots/gone.txt" },
  });
  assert.equal(code, 2, out);
  assert.match(out, /does not exist/);
});

test("a failure may state the gap instead of capturing it", () => {
  const { code, out } = check(FAILING, "failed", {
    evidence: { notCaptured: "the page navigated away before it could be read" },
  });
  assert.equal(code, 0, out);
});
