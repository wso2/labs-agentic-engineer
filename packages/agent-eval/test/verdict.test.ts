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

import { describe, expect, it } from "vitest";
import { readVerdict } from "../src/verdict.js";
import type { ScenarioFile } from "../src/scenario.js";

// SC-001 exercises the mustNot veto (single mustCover, one mustNot).
// SC-002 exercises weighted mustCover arithmetic (4 + 1 = 5, so a single
// item flips the fraction between 0.2, 0.8 and 1.0 — enough to pin the
// exact-0.8 boundary and a genuine below-threshold case on real weights).
const FILE: ScenarioFile = {
  version: 1,
  component: "trip-agent",
  scenarios: [
    {
      id: "SC-001",
      criteria: [],
      brief: { goal: "g", facts: {}, withholds: [] },
      rubric: {
        mustCover: [{ id: "MC-1", must: "asks for dates", weight: 2 }],
        mustNot: [{ id: "MN-1", mustNot: "invents a price" }],
      },
    },
    {
      id: "SC-002",
      criteria: [],
      brief: { goal: "g2", facts: {}, withholds: [] },
      rubric: {
        mustCover: [
          { id: "MC-2", must: "confirms the destination", weight: 4 },
          { id: "MC-3", must: "confirms the budget", weight: 1 },
        ],
        mustNot: [],
      },
    },
    {
      id: "SC-003",
      criteria: [],
      brief: { goal: "g3", facts: {}, withholds: [] },
      rubric: {
        mustCover: [{ id: "MC-4", must: "confirms the party size", weight: 1 }],
        mustNot: [{ id: "MN-2", mustNot: "books without confirmation" }],
      },
    },
  ],
};

function row(overrides: Record<string, unknown>): unknown {
  return overrides;
}

describe("readVerdict", () => {
  it("names which rubric lines failed, so a fix can cite them", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-001" },
            gradingResult: {
              componentResults: [
                { pass: true, assertion: { metric: "MC-1" }, reason: "ok" },
                { pass: false, assertion: { metric: "MN-1" }, reason: "invented a price" },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.failed).toEqual([{ id: "MN-1", reason: "invented a price" }]);
  });

  // A single mustNot violation fails a scenario outright, regardless of its
  // mustCover score — the score alone must never be sufficient for a pass.
  it("fails a scenario on a mustNot violation even at a perfect mustCover score", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-001" },
            gradingResult: {
              componentResults: [
                { pass: true, assertion: { metric: "MC-1" }, reason: "ok" },
                { pass: false, assertion: { metric: "MN-1" }, reason: "invented a price" },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.score).toBe(1);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  it("computes score as passed weight over gradeable mustCover weight, and passes at exactly 0.8", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: {
              componentResults: [
                { pass: true, assertion: { metric: "MC-2" }, reason: "ok" },
                { pass: false, assertion: { metric: "MC-3" }, reason: "budget never confirmed" },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.score).toBe(0.8);
    expect(v.scenarios[0]!.passed).toBe(true);
  });

  it("does not pass a scenario below the 0.8 threshold", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: {
              componentResults: [
                { pass: false, assertion: { metric: "MC-2" }, reason: "destination never confirmed" },
                { pass: true, assertion: { metric: "MC-3" }, reason: "ok" },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.score).toBeCloseTo(0.2);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  // "Achievable" weight excludes items that were never actually graded — a
  // scenario is not judged complete on the strength of the items that WERE
  // graded while others silently went missing.
  it("does not pass when a mustCover item was never gradeable, even if the graded items are perfect", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-2" }, reason: "ok" }],
              // MC-3 has no componentResult at all.
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  // Zero gradeable weight (no componentResults matched any mustCover item)
  // must report score 0, not NaN, and must never pass.
  it("does not divide by zero and does not pass when nothing was gradeable", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: { componentResults: [] },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.score).toBe(0);
    expect(Number.isNaN(v.scenarios[0]!.score)).toBe(false);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  // An errored row is not a genuine zero: it never passes, and its error
  // text becomes a citable failure reason instead of being discarded.
  it("marks an errored row as failed, carrying the error text as a reason", () => {
    const out = {
      results: {
        results: [
          row({
            error: "provider timeout",
            metadata: { scenarioId: "SC-003" },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.passed).toBe(false);
    expect(v.scenarios[0]!.failed).toEqual([{ id: "SC-003", reason: "provider timeout" }]);
  });

  // When a provider throws, promptfoo never attaches OUR metadata (it only
  // does that on a normal return) — but it still records the test's own
  // vars, which is where the scenario id actually is. Falling back to that
  // is what keeps a report from citing scenario "?" for exactly the rows a
  // reader most needs identified.
  it("falls back to vars.scenario.id when a thrown provider left no metadata", () => {
    const out = {
      results: {
        results: [
          row({
            error: "agent-eval: scenario and ask are required vars",
            vars: { scenario: { id: "SC-003" } },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.id).toBe("SC-003");
    expect(v.scenarios[0]!.failed).toEqual([
      { id: "SC-003", reason: "agent-eval: scenario and ask are required vars" },
    ]);
  });

  it("never lets an errored row pass, even if a non-zero score is also present", () => {
    const out = {
      results: {
        results: [
          row({
            error: "boom",
            score: 1,
            success: false,
            metadata: { scenarioId: "SC-003" },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-4" }, reason: "ok" }],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  // A missing, empty, or malformed promptfoo result must never read as a
  // clean pass — that would let the fix loop ship an un-evaluated prompt.
  it("does not report a vacuous pass when there are no results", () => {
    const v = readVerdict({ results: { results: [] } }, FILE);
    expect(v.overall).toBe(0);
    expect(v.scenarios).toEqual([]);
    expect(v.passed).toBe(false);
  });

  it("does not report a vacuous pass on malformed input", () => {
    expect(readVerdict(undefined, FILE).passed).toBe(false);
    expect(readVerdict(null, FILE).passed).toBe(false);
    expect(readVerdict({}, FILE).passed).toBe(false);
  });

  // `overall` is what the fix loop compares round-over-round to decide
  // whether to stop early, so its arithmetic must be pinned exactly, not
  // merely checked in the trivial empty-array case.
  it("computes overall as the exact mean of scenario scores", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-001" },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-1" }, reason: "ok" }],
            },
          }),
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: {
              componentResults: [
                { pass: true, assertion: { metric: "MC-2" }, reason: "ok" },
                { pass: false, assertion: { metric: "MC-3" }, reason: "no" },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    // SC-001 scores 1 (MC-1 passed, no mustNot fired); SC-002 scores 0.8
    // (4 of 5 weight passed). Mean of [1, 0.8] is exactly 0.9.
    expect(v.overall).toBe(0.9);
  });

  // The whole point of R5.3 is that the denominator is GRADEABLE weight, not
  // declared weight. MC-2 (weight 4) passes; MC-3 (weight 1) is never graded
  // at all. Gradeable weight is 4, passed weight is 4 -> score 1. Under a
  // denominator of total declared weight (4 + 1 = 5) this would read 0.8
  // instead -- a different, wrong number that still happens to look plausible.
  it("excludes an ungraded item from the denominator, not just the numerator", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-2" }, reason: "ok" }],
              // MC-3 has no componentResult at all -- ungraded, not failed.
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.score).toBe(1);
    // Still not a pass -- an ungraded mustCover item blocks completion,
    // independent of what the gradeable subset scored.
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  // An ungraded mustNot is not a satisfied veto. The same rule that blocks a
  // pass when a mustCover item goes ungraded must block it here too --
  // otherwise a grader crash on the ONE dimension with zero tolerance would
  // silently wave a harm through.
  it("does not pass when a mustNot line was never gradeable (errored component)", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-001" },
            gradingResult: {
              componentResults: [
                { pass: true, assertion: { metric: "MC-1" }, reason: "ok" },
                { error: "grader crashed", assertion: { metric: "MN-1" } },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  it("does not pass when a mustNot line has no componentResult at all", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-001" },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-1" }, reason: "ok" }],
              // MN-1 has no componentResult at all -- absent, not satisfied.
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.passed).toBe(false);
  });

  // `metadata.toolOverReach` is what the provider carries out of the run when
  // the agent called an operation its allow-list withholds — a security
  // finding, not a rubric miss. The verdict must carry it through, or it
  // only LOOKS visible: written to `out.json` but never actually read.
  it("carries toolOverReach through from the row's metadata", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-003", toolOverReach: ["LUNCH_API_URL: createRound"] },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-4" }, reason: "ok" }],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.toolOverReach).toEqual(["LUNCH_API_URL: createRound"]);
  });

  it("reports an empty toolOverReach when the row carries none", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-003" },
            gradingResult: {
              componentResults: [{ pass: true, assertion: { metric: "MC-4" }, reason: "ok" }],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.toolOverReach).toEqual([]);
  });

  // "not graded" and "genuinely missed" must not share a bucket: a revision
  // prompt built from `failed` would otherwise be told to fix a rubric line
  // the grader simply never returned a verdict on.
  it("keeps ungraded lines out of `failed`, so a fix is never asked to chase a phantom miss", () => {
    const out = {
      results: {
        results: [
          row({
            metadata: { scenarioId: "SC-002" },
            gradingResult: {
              componentResults: [
                // MC-2 genuinely fails; MC-3 is never graded.
                { pass: false, assertion: { metric: "MC-2" }, reason: "destination never confirmed" },
              ],
            },
          }),
        ],
      },
    };
    const v = readVerdict(out, FILE);
    expect(v.scenarios[0]!.failed).toEqual([{ id: "MC-2", reason: "destination never confirmed" }]);
    expect(v.scenarios[0]!.ungraded).toEqual([{ id: "MC-3", reason: "not graded" }]);
  });
});
