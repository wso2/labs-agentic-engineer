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

// verdict — everything that turns a run's validation verdict into what a reader
// sees: the counts behind it, and the sentence that says what it means.
//
// TWO surfaces say it: the Validation page's verdict tile, and the deployments
// rail's verdict banner. They used to write their own sentences, which is how the
// banner came to render "This deployment's verdict: awaiting fix." — a lifecycle
// state announced as a verdict. Sharing the copy is what makes the two agree by
// construction rather than by whoever edits both.

import {
  countOf,
  CRITERION_STATE_LABEL,
  uncoveredCount,
  type CriterionTally,
} from "@aep/ui-validation-view";

/**
 * The oracle joined with a report, as the four numbers any of this copy needs.
 *
 * Produced by validation/api/counts (which reads the pair once for both surfaces)
 * and by the tile from the tally it already holds. Undefined everywhere the join
 * has not resolved — the report is still loading, or the verdict never had one —
 * and every sentence below has a count-free form for exactly that.
 */
export interface ValidationCounts {
  /** Criteria the oracle authored: the denominator, and the only honest one. */
  total: number;
  passed: number;
  failed: number;
  /** Authored but never answered by a test — manual, scenario, or never run. */
  uncovered: number;
}

/** The counts a CriterionTally implies, for callers that already hold one. */
export function countsFromTally(tally: CriterionTally): ValidationCounts {
  return {
    total: tally.total,
    passed: countOf(tally, "pass"),
    failed: countOf(tally, "fail"),
    uncovered: uncoveredCount(tally),
  };
}

/**
 * What a fatal verdict means for the run, which depends on where the LOOP is and
 * not on what the report said.
 *
 * A fatal verdict on a live run is not the run's answer: validation repeats, so
 * the platform files the failures as ordinary work, waits for the fix to build and
 * deploy, and validates again. The settled clause would tell a reader the version
 * was abandoned while it is actively being repaired.
 *
 * The two live forms track BOTH halves of the loop as it moves — the fix (being
 * fixed → fixed and deployed) and validation (will run → is running) — so a reader
 * watching the state change can follow what happened between them.
 */
function loopTail(state: string, repairing: boolean): string {
  switch (state) {
    case "awaiting-fix":
      return "The implementation is being fixed. Validation will run again.";
    case "running":
      // "has been fixed and deployed" is a fact about a REPAIR — a repeat attempt on
      // the same run can only exist once the repair issues closed, the working set
      // emptied and the build went deployed-green. It is false about a revalidation,
      // which asks the same question again with nothing changed in between, so it is
      // said only when the run holding the verdict is the one running again.
      return repairing
        ? "The implementation has been fixed and deployed. Validation is running again."
        : RUNNING_AGAIN;
    default:
      return "The run stopped here, so the milestone stays open for the fix.";
  }
}

// The clause every in-flight repeat ends on, whichever kind it is.
const RUNNING_AGAIN = "Validation is running again.";

// A newer attempt is in flight, so every number on screen is the PREVIOUS attempt's
// and has to say so. True for `running` alone: under `awaiting-fix` nothing has
// re-run, so the last attempt's numbers are still the current state of the system.
function numbersAreStale(state: string): boolean {
  return state === "running";
}

// "2 of 6 criteria failed." — the evidence half, which is the same sentence on both
// surfaces.
//
// The numbered form is gated on more than one criterion so nothing has to inflect a
// verb for a count of one, and it carries no deixis ("marked below") because the
// banner has nothing below it and the page does not always show the report.
function failureEvidence(counts: ValidationCounts | undefined, state: string): string {
  const when = numbersAreStale(state) ? " in the last attempt" : "";
  return counts && counts.total > 1 && counts.failed > 0
    ? `${counts.failed} of ${counts.total} criteria failed${when}.`
    : `At least one criterion failed${when}.`;
}

// What a NON-FATAL verdict reads as while a new attempt is in flight — a
// revalidation, since nothing else re-asks a green result.
//
// A short stale summary rather than the settled sentence, and deliberately without
// its call to action ("please validate them manually"): the attempt in flight may
// change what is left to do by hand, so advising on it now is premature.
function staleSummary(verdict: string, counts: ValidationCounts | undefined): string {
  const counted = (counts?.total ?? 0) > 1;
  switch (verdict) {
    case "passed":
      return counted
        ? `All ${counts?.total} criteria passed in the last attempt.`
        : "Every criterion passed in the last attempt.";
    case "partial":
      return counted && (counts?.uncovered ?? 0) > 0
        ? `${counts?.uncovered} of ${counts?.total} criteria were never covered in the last attempt.`
        : "Some criteria were never covered in the last attempt.";
    default:
      return "No criteria could be automated in the last attempt.";
  }
}

/**
 * "35 passed · 5 manual" — the outcome as a per-state tally, or "" with no report.
 *
 * Lives beside the sentence rather than in the tile that renders it because it needs
 * the same staleness marker for the same reason, in the state where a repeat attempt
 * is running. It is the most standalone-readable thing on the tile, so unmarked it
 * reads as the current state of a system that has already been fixed.
 */
export function verdictCounts(
  tally: CriterionTally | undefined,
  state = "",
): string {
  if (!tally) return "";
  const line = tally.states
    .map(
      (s) =>
        `${s.count} ${(CRITERION_STATE_LABEL[s.status] ?? s.status).toLowerCase()}`,
    )
    .join(" · ");
  // Parenthetical, not the sentence's " in the last attempt": this is a list, and a
  // clause tacked onto a list of numbers reads as another entry in it.
  return line && numbersAreStale(state) ? `${line} (last attempt)` : line;
}

/**
 * The sentence for a verdict: what it means, and what it did to the run.
 *
 * `state` is the loop's position (projects/lib/pipeline validationState). Only
 * `failed` and `unreported` can pair with a lifecycle value — those are the two the
 * loop repeats — so it is read in their cases alone; it defaults to the verdict,
 * which is what "this verdict is the run's answer" looks like.
 *
 * `unreported` gets its own live forms rather than loopTail's, because the platform
 * files NOTHING for it: there is no failing criterion to turn into work, so the
 * empty working set sends the run straight back to validate again. Promising a fix
 * would name work that does not exist.
 */
export function verdictSentence(
  verdict: string,
  counts: ValidationCounts | undefined,
  state: string = verdict,
  repairing = false,
): string {
  const counted = (counts?.total ?? 0) > 1;
  // A non-fatal verdict CAN sit under a live state after all — a revalidation asks a
  // settled version again — and its settled sentence would report the last attempt's
  // result as the current one. Only `running` reaches here; `awaiting-fix` requires a
  // fatal verdict by construction.
  if (state === "running" && (verdict === "passed" || verdict === "partial" || verdict === "inconclusive")) {
    return `${staleSummary(verdict, counts)} ${RUNNING_AGAIN}`;
  }
  switch (verdict) {
    case "passed":
      // Names coverage, not just the result: `passed` REQUIRES that every criterion
      // was checked, which is the whole point of the vocabulary.
      return counted
        ? `All ${counts?.total} criteria were covered by a test and passed.`
        : "Every criterion was covered by a test and passed.";
    case "partial": {
      const uncovered = counts?.uncovered ?? 0;
      // Ends on what the reader can do about it. The count is the gap between what
      // was authored and what a test actually answered, so the ask is specific
      // rather than a vague "not a clean pass".
      return counted && uncovered > 0
        ? `Everything that ran passed, but ${uncovered} of ${counts?.total} criteria couldn't be automated — please validate ${
            uncovered === 1 ? "it" : "them"
          } manually.`
        : "Everything that ran passed, but some criteria couldn't be automated — please validate them manually.";
    }
    case "failed":
      return `${failureEvidence(counts, state)} ${loopTail(state, repairing)}`;
    case "inconclusive":
      return counted
        ? `None of the ${counts?.total} criteria could be automated — please validate them manually.`
        : "None of the criteria could be automated — please validate them manually.";
    case "unreported":
      // A reporting failure, not a test outcome: no criterion produced one. The
      // terminal reason (`validation-unreported`) is deliberately NOT quoted — a
      // wire value is not something to hand a reader.
      switch (state) {
        case "awaiting-fix":
          return "The validation report couldn't be generated. Validation will run again.";
        case "running":
          return `The validation report couldn't be generated in the last attempt. ${RUNNING_AGAIN}`;
        default:
          return "The validation report couldn't be generated, so there are no results to show for this run.";
      }
    default:
      // No verdict yet, which only a LIFECYCLE state can be: the first attempt of a
      // run, before anything has been concluded. It needs a sentence of its own or
      // the caller falls through to naming the state as a verdict — "This
      // deployment's verdict: validating." — the very thing sharing this copy was
      // meant to stop. `awaiting-fix` cannot reach here; it requires a fatal verdict.
      //
      // Says what there is to SEE rather than what is happening: the rail's stage
      // note beside it already says the deployed system is being checked, and two
      // adjacent elements saying that is a restatement.
      return state === "running"
        ? "Nothing reported yet — the validation attempt is still running."
        : "";
  }
}
