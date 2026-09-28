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
import {
  countsFromScenarios,
  verdictCounts,
  verdictSentence,
  type ValidationCounts,
} from "./verdict";

function counts(over: Partial<ValidationCounts> = {}): ValidationCounts {
  return { total: 40, passed: 0, failed: 0, uncovered: 0, ...over };
}

describe("countsFromScenarios", () => {
  // The tile and the deployments rail both read this; one derivation keeps them
  // from disagreeing about what "uncovered" means.
  const scenarios = (...outcomes: string[]) =>
    outcomes.map((outcome, i) => ({
      feature: "F",
      rule: "R",
      scenario: `S${i}`,
      outcome,
      steps: [],
    }));

  it("counts everything that is neither passed nor failed as uncovered", () => {
    expect(
      countsFromScenarios(scenarios("passed", "passed", "failed", "blocked", "unjudgeable")),
    ).toEqual({ total: 5, passed: 2, failed: 1, uncovered: 2 });
  });

  // The complement, deliberately: an outcome word a newer runner invents still has
  // to land somewhere, and it is certainly not coverage — which is the same call
  // the Go verdict ladder makes for an unrecognised outcome.
  it("counts an outcome word it has never seen as uncovered", () => {
    expect(countsFromScenarios(scenarios("passed", "abandoned"))).toEqual({
      total: 2,
      passed: 1,
      failed: 0,
      uncovered: 1,
    });
  });

  it("has nothing to say about a report with no scenarios", () => {
    expect(countsFromScenarios([])).toEqual({ total: 0, passed: 0, failed: 0, uncovered: 0 });
  });
});

describe("verdictSentence", () => {
  // `passed` REQUIRES full coverage, so its sentence must say so — claiming only
  // "everything passed" is what let a green banner sit over scenarios nobody settled.
  it("passed names coverage, not just the result", () => {
    expect(verdictSentence("passed", counts({ passed: 40 }))).toBe(
      "All 40 scenarios were settled and passed.",
    );
  });

  // The pair the vocabulary exists for. The numbers are the whole point: without
  // them "Validated" leaves the reader asking which part.
  it("partial counts the unsettled scenarios against the run's total", () => {
    expect(verdictSentence("partial", counts({ passed: 35, uncovered: 5 }))).toBe(
      "Everything that ran passed, but 5 of 40 scenarios couldn't be settled against the deployed app — please check them yourself.",
    );
  });

  it("partial inflects for a single unsettled scenario", () => {
    expect(
      verdictSentence("partial", counts({ passed: 39, uncovered: 1 })),
    ).toContain("1 of 40 scenarios couldn't be settled against the deployed app — please check it yourself");
  });

  // A failing verdict ENDS the run once its attempts are spent, which is a
  // consequence no chip can state — so the sentence has to carry it.
  it("failed counts the failures and states what it did to the run", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }));
    expect(s).toBe(
      "2 of 40 scenarios failed. The run stopped here, so the milestone stays open for the fix.",
    );
  });

  // The consequence clause is the half that goes STALE mid-loop: a fatal verdict on
  // a live run is not the run's answer, and "the run stopped here" would tell a
  // reader the version was abandoned while it is being repaired. The evidence half —
  // what the report found — is unchanged, because that much is still true.
  it("failed swaps its ending, not its evidence, while a repair is in flight", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }), "awaiting-fix");
    expect(s).toBe(
      "2 of 40 scenarios failed. The implementation is being fixed. Validation will run again.",
    );
  });

  // A repeat attempt is running, so the numbers are the PREVIOUS attempt's and would
  // otherwise read as the current state of the system. The fix being deployed is a
  // fact: a repeat attempt only exists once the repair shipped.
  it("failed marks its numbers stale while a repeat attempt runs", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }), "running", true);
    expect(s).toBe(
      "2 of 40 scenarios failed in the last attempt. The implementation has been fixed and deployed. Validation is running again.",
    );
  });

  // "has been fixed and deployed" is a fact about a REPAIR. A revalidation re-asks
  // the same question with nothing changed in between, so claiming a fix there would
  // be false — the clause is said only when the run holding the verdict is the one
  // running again.
  it("failed claims no fix when the attempt is a revalidation, not a repair", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }), "running");
    expect(s).toBe(
      "2 of 40 scenarios failed in the last attempt. Validation is running again.",
    );
    expect(s).not.toContain("fixed and deployed");
  });

  // `unreported` gets its OWN live sentences rather than the failed one's ending: the
  // platform files nothing for it (there is no failing scenario to turn into work),
  // so promising a fix would name work that does not exist.
  it("unreported promises a retry, never a fix, while the loop repeats it", () => {
    expect(verdictSentence("unreported", undefined, "awaiting-fix")).toBe(
      "The validation report couldn't be generated. Validation will run again.",
    );
    expect(verdictSentence("unreported", undefined, "running")).toContain(
      "in the last attempt",
    );
    for (const state of ["awaiting-fix", "running"]) {
      expect(
        verdictSentence("unreported", undefined, state),
        `${state} promised a fix`,
      ).not.toContain("fix");
    }
  });

  it("unreported never quotes the terminal reason", () => {
    const s = verdictSentence("unreported", undefined);
    expect(s).toContain("validation report couldn't be generated");
    expect(s).not.toContain("validation-unreported");
  });

  // `awaiting-fix` can only sit over a fatal verdict, so a green one beside it is
  // skew — its settled sentence must survive untouched.
  it("leaves a final verdict's sentence alone under awaiting-fix", () => {
    for (const v of ["passed", "partial", "inconclusive"]) {
      const c = counts({ passed: 35, uncovered: 5 });
      expect(verdictSentence(v, c, "awaiting-fix"), `${v} changed mid-loop`).toBe(
        verdictSentence(v, c),
      );
    }
  });

  // A REVALIDATION re-asks a settled version, so a non-fatal verdict can sit under
  // `running` after all — and its settled sentence would report the last attempt's
  // result as the current one. Short stale summaries, deliberately WITHOUT the
  // settled copy's "please validate them manually": the attempt in flight may change
  // what is left to do by hand.
  it("marks a green verdict as the last attempt's while a revalidation runs", () => {
    expect(verdictSentence("passed", counts({ passed: 6, total: 6 }), "running")).toBe(
      "All 6 scenarios passed in the last attempt. Validation is running again.",
    );
    expect(
      verdictSentence("partial", counts({ passed: 4, uncovered: 2, total: 6 }), "running"),
    ).toBe(
      "2 of 6 scenarios weren't settled in the last attempt. Validation is running again.",
    );
    expect(
      verdictSentence("inconclusive", counts({ uncovered: 6, total: 6 }), "running"),
    ).toBe("No scenario could be settled in the last attempt. Validation is running again.");
  });

  it("gives the stale summaries a count-free form too", () => {
    expect(verdictSentence("passed", undefined, "running")).toBe(
      "Every scenario passed in the last attempt. Validation is running again.",
    );
    expect(verdictSentence("partial", undefined, "running")).toBe(
      "Some scenarios weren't settled in the last attempt. Validation is running again.",
    );
  });

  // None of them advises manual work while the answer is being re-computed.
  it("drops the manual-validation ask while an attempt is in flight", () => {
    for (const v of ["partial", "inconclusive"]) {
      expect(
        verdictSentence(v, counts({ passed: 4, uncovered: 2, total: 6 }), "running"),
        `${v} advised manual work mid-attempt`,
      ).not.toContain("manually");
    }
  });

  it("inconclusive asks for manual validation", () => {
    expect(verdictSentence("inconclusive", counts({ uncovered: 12, total: 12 }))).toBe(
      "None of the 12 scenarios could be settled against the deployed app — please check them yourself.",
    );
  });

  // Every sentence renders before the report loads, and `unreported` has no report to
  // count at all — so each must still read as a whole sentence with no counts.
  it("every verdict degrades to a count-free sentence", () => {
    for (const v of ["passed", "partial", "failed", "inconclusive", "unreported"]) {
      const s = verdictSentence(v, undefined);
      expect(s, `no sentence for ${v}`).not.toBe("");
      expect(s, `${v} leaked a count`).not.toMatch(/\d+ of \d+|All 0|the 0 /);
    }
  });

  // The count-free form has to survive the lifecycle states too, which is where it is
  // most likely: `unreported` mid-loop never has a report to count.
  it("degrades count-free in the lifecycle states as well", () => {
    expect(verdictSentence("failed", undefined, "awaiting-fix")).toBe(
      "At least one scenario failed. The implementation is being fixed. Validation will run again.",
    );
    expect(verdictSentence("failed", undefined, "running", true)).toBe(
      "At least one scenario failed in the last attempt. The implementation has been fixed and deployed. Validation is running again.",
    );
  });

  // A total of one would force verb agreement on every numbered form, so the numbered
  // forms are gated on total > 1 rather than inflected six ways.
  it("skips the numbers for a single-scenario oracle", () => {
    expect(verdictSentence("passed", counts({ total: 1, passed: 1 }))).toBe(
      "Every scenario was settled and passed.",
    );
  });

  // A FIRST attempt has no verdict yet. Returning "" sent the deployments banner to
  // its verdict-naming fallback, which rendered "This deployment's verdict:
  // validating." — a lifecycle value announced as a verdict, the exact defect this
  // shared copy exists to stop.
  it("speaks for a first attempt, which has no verdict yet", () => {
    expect(verdictSentence("", undefined, "running")).toBe(
      "The validation agent is running.",
    );
  });

  // Same defect, same guard, for the other lifecycle value that carries no verdict.
  // The deployments banner passes `cancelled` as BOTH arguments (it is not one of the
  // in-flight states, so the state IS what it reads as the verdict), and "" here sent
  // it to the fallback that printed "This deployment's verdict: validation cancelled."
  // — announcing the absence of a verdict as one.
  it("speaks for cancelled judging, which is the absence of a verdict", () => {
    const s = verdictSentence("cancelled", undefined, "cancelled");
    expect(s).toBe("This version has no verdict — run validation again when you want one.");
    // Complements the rail's stage note instead of restating it.
    expect(s).not.toContain("was cancelled");
  });

  // `awaiting-fix` cannot reach this path — it requires a fatal verdict — and a
  // settled state with no verdict has nothing to say.
  it("still says nothing with no verdict and no lifecycle state", () => {
    expect(verdictSentence("", undefined, "awaiting-fix")).toBe("");
    expect(verdictSentence("", undefined)).toBe("");
  });

  it("is empty for a verdict it does not speak for", () => {
    expect(verdictSentence("skipped", counts({ total: 0 }))).toBe("");
    expect(verdictSentence("", undefined)).toBe("");
  });
});

describe("verdictCounts", () => {
  // The words come from the acceptance package, which reads them off the report;
  // this only decides whether they need marking as stale.
  const t = "35 of 40 passed · 2 failed · 3 blocked";

  it("passes the line through untouched in a settled state", () => {
    expect(verdictCounts(t)).toBe("35 of 40 passed · 2 failed · 3 blocked");
  });

  // The tally is the most standalone-readable thing on the tile, so in the one state
  // where a newer attempt is already running it has to carry the same staleness the
  // sentence above it does — unmarked it reads as the current state of a system that
  // has since been fixed. Parenthetical rather than the sentence's clause: a clause
  // tacked onto a list of numbers reads as another entry in the list.
  it("marks the numbers as the last attempt's while a repeat attempt runs", () => {
    expect(verdictCounts(t, "running")).toBe(
      "35 of 40 passed · 2 failed · 3 blocked (last attempt)",
    );
  });

  // Nothing has re-run under `awaiting-fix`, so these numbers ARE the current state.
  it("leaves the numbers unmarked in every other state", () => {
    for (const state of ["awaiting-fix", "failed", "passed", ""]) {
      expect(verdictCounts(t, state), `${state} marked its counts`).toBe(
        "35 of 40 passed · 2 failed · 3 blocked",
      );
    }
  });

  it("is empty with no report — marked or not", () => {
    expect(verdictCounts("")).toBe("");
    expect(verdictCounts("", "running")).toBe("");
    expect(verdictCounts(undefined, "running")).toBe("");
  });
});
