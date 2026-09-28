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

import type { ReportScenario } from "@aep/ui-acceptance-view";

/**
 * The oracle joined with a report, as the four numbers any of this copy needs.
 *
 * Produced by validation/api/counts (which reads the pair once for both surfaces)
 * and by the tile from the tally it already holds. Undefined everywhere the join
 * has not resolved — the report is still loading, or the verdict never had one —
 * and every sentence below has a count-free form for exactly that.
 */
export interface ValidationCounts {
  /** Scenarios the run answered: the denominator, and the only honest one. */
  total: number;
  passed: number;
  failed: number;
  /** Answered by neither — blocked, or unjudgeable, or a word we do not know. */
  uncovered: number;
}

/**
 * The counts an acceptance report implies.
 *
 * NO JOIN. The criteria path needed the oracle for its denominator, because a
 * criterion could be authored and never tested; the acceptance run reports one
 * entry per scenario in the feature files — its own checker fails the run
 * otherwise — so the report counts itself. `uncovered` is the complement of
 * passed and failed, which is what keeps the three summing to the total however
 * many outcome words a future run invents.
 */
export function countsFromScenarios(
  scenarios: readonly ReportScenario[],
): ValidationCounts {
  const passed = scenarios.filter((s) => s.outcome === "passed").length;
  const failed = scenarios.filter((s) => s.outcome === "failed").length;
  return {
    total: scenarios.length,
    passed,
    failed,
    uncovered: scenarios.length - passed - failed,
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

// "2 of 6 scenarios failed." — the evidence half, which is the same sentence on
// both surfaces.
//
// The numbered form is gated on more than one scenario so nothing has to inflect a
// verb for a count of one, and it carries no deixis ("marked below") because the
// banner has nothing below it and the page does not always show the report.
function failureEvidence(counts: ValidationCounts | undefined, state: string): string {
  const when = numbersAreStale(state) ? " in the last attempt" : "";
  return counts && counts.total > 1 && counts.failed > 0
    ? `${counts.failed} of ${counts.total} scenarios failed${when}.`
    : `At least one scenario failed${when}.`;
}

// What a NON-FATAL verdict reads as while a new attempt is in flight — a
// revalidation, since nothing else re-asks a green result.
//
// A short stale summary rather than the settled sentence, and deliberately without
// its call to action ("please check them yourself"): the attempt in flight may
// change what is left to do by hand, so advising on it now is premature.
function staleSummary(verdict: string, counts: ValidationCounts | undefined): string {
  const counted = (counts?.total ?? 0) > 1;
  switch (verdict) {
    case "passed":
      return counted
        ? `All ${counts?.total} scenarios passed in the last attempt.`
        : "Every scenario passed in the last attempt.";
    case "partial":
      return counted && (counts?.uncovered ?? 0) > 0
        ? `${counts?.uncovered} of ${counts?.total} scenarios weren't settled in the last attempt.`
        : "Some scenarios weren't settled in the last attempt.";
    default:
      return "No scenario could be settled in the last attempt.";
  }
}

/**
 * "7 of 9 passed · 2 blocked" — the outcome as a tally, or "" with no report.
 *
 * The words come from the acceptance package (`tallySentence`), which reads them
 * off the report; this only decides whether they need marking as stale.
 *
 * Lives beside the sentence rather than in the tile that renders it because it needs
 * the same staleness marker for the same reason, in the state where a repeat attempt
 * is running. It is the most standalone-readable thing on the tile, so unmarked it
 * reads as the current state of a system that has already been fixed.
 */
export function verdictCounts(line: string | undefined, state = ""): string {
  if (!line) return "";
  // Parenthetical, not the sentence's " in the last attempt": this is a list, and a
  // clause tacked onto a list of numbers reads as another entry in it.
  return numbersAreStale(state) ? `${line} (last attempt)` : line;
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
 * files NOTHING for it: there is no failing scenario to turn into work, so the
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
      // Names coverage, not just the result: `passed` REQUIRES that every scenario
      // was settled, which is the whole point of the vocabulary.
      return counted
        ? `All ${counts?.total} scenarios were settled and passed.`
        : "Every scenario was settled and passed.";
    case "partial": {
      const uncovered = counts?.uncovered ?? 0;
      // Ends on what the reader can do about it. NOT "couldn't be automated":
      // every scenario is driven against the deployed app, and these are the ones
      // the app would not let the run settle — a control that was absent, or an
      // answer that lives outside the running system. Only a person can tell a
      // product that correctly refuses from one that is broken, which is also why
      // no repair is filed for them.
      return counted && uncovered > 0
        ? `Everything that ran passed, but ${uncovered} of ${counts?.total} scenarios couldn't be settled against the deployed app — please check ${
            uncovered === 1 ? "it" : "them"
          } yourself.`
        : "Everything that ran passed, but some scenarios couldn't be settled against the deployed app — please check them yourself.";
    }
    case "failed":
      return `${failureEvidence(counts, state)} ${loopTail(state, repairing)}`;
    case "inconclusive":
      return counted
        ? `None of the ${counts?.total} scenarios could be settled against the deployed app — please check them yourself.`
        : "No scenario could be settled against the deployed app — please check them yourself.";
    case "unreported":
      // A reporting failure, not a run outcome: no scenario produced one. The
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
      // Names the ACTOR and nothing else. There is no evidence to summarise yet, so
      // the sentence's only job is to say who the reader is waiting on; the stage
      // note beside it carries the progress, and the counts line stays empty until
      // an attempt reports.
      if (state === "running") {
        return "The validation agent is running.";
      }
      // `cancelled` is the other lifecycle value with no verdict, and it needs a
      // sentence for the same reason: it is the ABSENCE of one, so falling through
      // prints "This deployment's verdict: validation cancelled." `skipped` may be
      // named that way and this may not — skipped IS a verdict.
      //
      // Names the CONSEQUENCE and the recourse rather than the event, on the same
      // no-restatement rule: the stage note beside it already says validation was
      // cancelled and the version was never checked.
      if (state === "cancelled") {
        return "This version has no verdict — run validation again when you want one.";
      }
      return "";
  }
}
