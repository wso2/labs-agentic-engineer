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
function check(steps, outcome = "passed") {
  const dir = mkdtempSync(join(tmpdir(), "acc-check-"));
  try {
    mkdirSync(join(dir, "specs/validation/acceptance"), { recursive: true });
    mkdirSync(join(dir, "tests/acceptance"), { recursive: true });
    writeFileSync(join(dir, "specs/validation/acceptance/lists.feature"), FEATURE);
    writeFileSync(
      join(dir, "tests/acceptance/report.json"),
      JSON.stringify({
        schemaVersion: 2,
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
            ...(outcome === "failed"
              ? { evidence: { network: [], console: [] } }
              : {}),
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
