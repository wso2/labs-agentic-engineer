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
import type { CriterionTally } from "@aep/ui-validation-view";
import {
  countsFromTally,
  verdictCounts,
  verdictSentence,
  type ValidationCounts,
} from "./verdict";

function counts(over: Partial<ValidationCounts> = {}): ValidationCounts {
  return { total: 40, passed: 0, failed: 0, uncovered: 0, ...over };
}

describe("countsFromTally", () => {
  // The tile holds a tally and the rail holds counts; one derivation keeps them from
  // disagreeing about what "uncovered" means.
  it("splits a tally into passed, failed and uncovered", () => {
    const tally: CriterionTally = {
      total: 40,
      states: [
        { status: "pass", count: 33 },
        { status: "fail", count: 2 },
        { status: "manual", count: 3 },
        { status: "not_run", count: 2 },
      ],
    };
    expect(countsFromTally(tally)).toEqual({
      total: 40,
      passed: 33,
      failed: 2,
      uncovered: 5,
    });
  });
});

describe("verdictSentence", () => {
  // `passed` REQUIRES full coverage, so its sentence must say so — claiming only
  // "everything passed" is what let a green banner sit over criteria nobody checked.
  it("passed names coverage, not just the result", () => {
    expect(verdictSentence("passed", counts({ passed: 40 }))).toBe(
      "All 40 criteria were covered by a test and passed.",
    );
  });

  // The pair the vocabulary exists for. The numbers are the whole point: without
  // them "Validated" leaves the reader asking which part.
  it("partial counts the uncovered criteria against the authored total", () => {
    expect(verdictSentence("partial", counts({ passed: 35, uncovered: 5 }))).toBe(
      "Everything that ran passed, but 5 of 40 criteria couldn't be automated — please validate them manually.",
    );
  });

  it("partial inflects for a single uncovered criterion", () => {
    expect(
      verdictSentence("partial", counts({ passed: 39, uncovered: 1 })),
    ).toContain("1 of 40 criteria couldn't be automated — please validate it manually");
  });

  // A failing verdict ENDS the run once its attempts are spent, which is a
  // consequence no chip can state — so the sentence has to carry it.
  it("failed counts the failures and states what it did to the run", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }));
    expect(s).toBe(
      "2 of 40 criteria failed. The run stopped here, so the milestone stays open for the fix.",
    );
  });

  // The consequence clause is the half that goes STALE mid-loop: a fatal verdict on
  // a live run is not the run's answer, and "the run stopped here" would tell a
  // reader the version was abandoned while it is being repaired. The evidence half —
  // what the report found — is unchanged, because that much is still true.
  it("failed swaps its ending, not its evidence, while a repair is in flight", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }), "awaiting-fix");
    expect(s).toBe(
      "2 of 40 criteria failed. The implementation is being fixed. Validation will run again.",
    );
  });

  // A repeat attempt is running, so the numbers are the PREVIOUS attempt's and would
  // otherwise read as the current state of the system. The fix being deployed is a
  // fact: a repeat attempt only exists once the repair shipped.
  it("failed marks its numbers stale while a repeat attempt runs", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }), "running", true);
    expect(s).toBe(
      "2 of 40 criteria failed in the last attempt. The implementation has been fixed and deployed. Validation is running again.",
    );
  });

  // "has been fixed and deployed" is a fact about a REPAIR. A revalidation re-asks
  // the same question with nothing changed in between, so claiming a fix there would
  // be false — the clause is said only when the run holding the verdict is the one
  // running again.
  it("failed claims no fix when the attempt is a revalidation, not a repair", () => {
    const s = verdictSentence("failed", counts({ failed: 2, passed: 38 }), "running");
    expect(s).toBe(
      "2 of 40 criteria failed in the last attempt. Validation is running again.",
    );
    expect(s).not.toContain("fixed and deployed");
  });

  // `unreported` gets its OWN live sentences rather than the failed one's ending: the
  // platform files nothing for it (there is no failing criterion to turn into work),
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
      "All 6 criteria passed in the last attempt. Validation is running again.",
    );
    expect(
      verdictSentence("partial", counts({ passed: 4, uncovered: 2, total: 6 }), "running"),
    ).toBe(
      "2 of 6 criteria were never covered in the last attempt. Validation is running again.",
    );
    expect(
      verdictSentence("inconclusive", counts({ uncovered: 6, total: 6 }), "running"),
    ).toBe("No criteria could be automated in the last attempt. Validation is running again.");
  });

  it("gives the stale summaries a count-free form too", () => {
    expect(verdictSentence("passed", undefined, "running")).toBe(
      "Every criterion passed in the last attempt. Validation is running again.",
    );
    expect(verdictSentence("partial", undefined, "running")).toBe(
      "Some criteria were never covered in the last attempt. Validation is running again.",
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
      "None of the 12 criteria could be automated — please validate them manually.",
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
      "At least one criterion failed. The implementation is being fixed. Validation will run again.",
    );
    expect(verdictSentence("failed", undefined, "running", true)).toBe(
      "At least one criterion failed in the last attempt. The implementation has been fixed and deployed. Validation is running again.",
    );
  });

  // A total of one would force verb agreement on every numbered form, so the numbered
  // forms are gated on total > 1 rather than inflected six ways.
  it("skips the numbers for a single-criterion oracle", () => {
    expect(verdictSentence("passed", counts({ total: 1, passed: 1 }))).toBe(
      "Every criterion was covered by a test and passed.",
    );
  });

  // A FIRST attempt has no verdict yet. Returning "" sent the deployments banner to
  // its verdict-naming fallback, which rendered "This deployment's verdict:
  // validating." — a lifecycle value announced as a verdict, the exact defect this
  // shared copy exists to stop.
  it("speaks for a first attempt, which has no verdict yet", () => {
    expect(verdictSentence("", undefined, "running")).toBe(
      "Nothing reported yet — the validation attempt is still running.",
    );
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
  const t: CriterionTally = {
    total: 40,
    states: [
      { status: "fail", count: 2 },
      { status: "pass", count: 35 },
      { status: "manual", count: 3 },
    ],
  };

  it("reads as a run-on line, lowercased", () => {
    expect(verdictCounts(t)).toBe("2 failed · 35 passed · 3 manual");
  });

  // The tally is the most standalone-readable thing on the tile, so in the one state
  // where a newer attempt is already running it has to carry the same staleness the
  // sentence above it does — unmarked it reads as the current state of a system that
  // has since been fixed. Parenthetical rather than the sentence's clause: a clause
  // tacked onto a list of numbers reads as another entry in the list.
  it("marks the numbers as the last attempt's while a repeat attempt runs", () => {
    expect(verdictCounts(t, "running")).toBe(
      "2 failed · 35 passed · 3 manual (last attempt)",
    );
  });

  // Nothing has re-run under `awaiting-fix`, so these numbers ARE the current state.
  it("leaves the numbers unmarked in every other state", () => {
    for (const state of ["awaiting-fix", "failed", "passed", ""]) {
      expect(verdictCounts(t, state), `${state} marked its counts`).toBe(
        "2 failed · 35 passed · 3 manual",
      );
    }
  });

  it("names an unknown status verbatim rather than dropping it", () => {
    expect(verdictCounts({ total: 1, states: [{ status: "quarantined", count: 1 }] })).toBe(
      "1 quarantined",
    );
  });

  it("is empty with no report and with no tally — marked or not", () => {
    expect(verdictCounts({ total: 40, states: [] })).toBe("");
    expect(verdictCounts({ total: 40, states: [] }, "running")).toBe("");
    expect(verdictCounts(undefined, "running")).toBe("");
  });
});
