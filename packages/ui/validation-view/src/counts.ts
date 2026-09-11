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

/**
 * The tallies over an oracle — by run STATUS once a report is joined in ("35
 * passed · 5 manual"), and by verification METHOD before one exists ("12 auto ·
 * 3 manual").
 *
 * Pure derivations kept out of the view because both are rendered by the
 * CONSUMER (the console's tiles) rather than inside ValidationView: the verdict —
 * or, mid-run, what is about to be checked — is what a reader wants first, and
 * one tally above the criteria beats the same tally twice on one page.
 */

import type { ValidationCriteria } from "./parse.js";
import type { ValidationReport } from "./report.js";

/**
 * report.json status → its human label. The single source for these five words:
 * ValidationView's per-criterion chips read their label from here too, so the
 * tally line and the chip beside a criterion can never disagree.
 */
export const CRITERION_STATE_LABEL: Record<string, string> = {
  pass: "Passed",
  fail: "Failed",
  not_run: "Not run",
  not_validated: "Not validated",
  manual: "Manual",
};

/**
 * criterion method → what a badge SAYS, as against the wire value it is keyed by.
 * `e2e` is the contract shared with the runner, the report generator and the
 * spec-path convention, so it cannot be renamed — but it is an acronym the reader
 * has to expand, which the console lexicon forbids. The same split
 * CRITERION_STATE_LABEL draws for run statuses, and here for the same reason: two
 * surfaces naming a method must name it the same way. A method with no entry
 * renders verbatim, so `manual` and anything unrecognised are unaffected.
 *
 * Read by a consumer's prose and its tally, not by the criterion rows — those mark
 * the method with a glyph rather than a word (ValidationView's methodMark).
 */
export const METHOD_LABEL: Record<string, string> = {
  e2e: "auto",
};

/**
 * criterion method → its identifying colour, as a wash behind the method's word
 * said in prose, so a consumer naming a method in a sentence can mark it as a term
 * rather than leave it as ordinary text.
 *
 * A consumer's sentence is the only reader: the criterion rows carry a glyph on
 * the console's own agent/person colours, so nothing else has to match these hexes.
 */
export const METHOD_COLOR: Record<string, string> = {
  e2e: "#1976d2",
  scenario: "#ed6c02",
  manual: "#7b1fa2",
};

/** The colour for a method this vocabulary does not know. */
export const METHOD_FALLBACK_COLOR = "#616161";

/**
 * Whether a validation run produces a VERDICT for this method.
 *
 * `e2e` only. generate-report.mjs gives an e2e criterion its test's own result and
 * decides every other method from the method alone — `manual` for a manual
 * criterion, `not_validated` for anything else it cannot automate — so what
 * becomes of those rows is knowable before the run starts.
 */
export function runAnswers(method: string): boolean {
  return method === "e2e";
}

/**
 * Whether a run WORKS ON this criterion — explores it, authors a spec for it,
 * runs it — and therefore emits live progress naming it.
 *
 * Deliberately wider than `runAnswers`, and the gap between them is not
 * academic: a run works on a legacy `scenario` criterion and still reports
 * `not_validated` for it, so it emits progress for a row it will never answer.
 * Only `manual` is outside both, being a person's to judge, which is why a run
 * never names it.
 *
 * Do not collapse the two. The console's run-wide progress line counts exactly
 * this set, so a criterion row that answers the narrower question refuses a status
 * the line beside it has already counted, and the two then contradict each other
 * about the same criterion.
 */
export function runWorksOn(method: string): boolean {
  return method !== "manual";
}

/** Display order for the method tally; unknown methods sort after these. */
export const METHOD_ORDER = ["e2e", "scenario", "manual"];

/** Display order for the state tally — a failure reads first. */
const STATE_ORDER = ["fail", "pass", "not_run", "not_validated", "manual"];

/** The statuses that mean the criterion WAS actually checked and got an answer. */
const DECIDED = ["pass", "fail"];

export interface CriterionStateCount {
  /** Raw report status; one of CriterionRunState in practice. */
  status: string;
  count: number;
}

export interface CriterionTally {
  /** Criteria the ORACLE authored — the honest denominator. */
  total: number;
  /** Per-status counts in display order; empty when no report is joined in. */
  states: CriterionStateCount[];
}

/** How many of the tallied criteria carry `status`. */
export function countOf(tally: CriterionTally, status: string): number {
  return tally.states.find((s) => s.status === status)?.count ?? 0;
}

/**
 * How many criteria were authored but never actually checked.
 *
 * Counted as the COMPLEMENT of the two statuses that mean an answer came back —
 * `pass` and `fail` — rather than by summing `not_run`/`not_validated`/`manual`.
 * Those three are not the only ways a criterion goes unchecked, and summing them
 * understated the gap in the two cases that matter most:
 *
 *  - a criterion the report OMITS entirely, which report.ts explicitly tolerates.
 *    It has no status at all, so it belonged to no bucket and vanished from the
 *    count while still sitting in `total`.
 *  - an UNRECOGNISED status, which is evidence nobody here can interpret. The
 *    server's verdict derivation already treats one as a coverage gap
 *    (VerdictFromReport's default arm), so counting it as covered would let the
 *    tile's number contradict the verdict it is explaining.
 *
 * Complementing also keeps the arithmetic closed: passed + failed + uncovered is
 * always `total`, so "5 of 40 were never covered" can never exceed the denominator
 * or leave a remainder the reader has to account for.
 */
export function uncoveredCount(tally: CriterionTally): number {
  const decided = DECIDED.reduce((n, s) => n + countOf(tally, s), 0);
  return tally.total - decided;
}

/**
 * Tally the oracle's criteria by the run status the report gives each one.
 *
 * `total` counts what was AUTHORED and the states count what RAN, which is the
 * gap that makes a partial verdict legible: 40 authored, 35 with a pass, 5 never
 * covered. Counting the report's own rows instead would make that gap invisible,
 * and a criterion the report omits — which report.ts explicitly tolerates —
 * would silently leave the denominator.
 */
export function tallyCriterionStates(
  criteria: ValidationCriteria,
  report: ValidationReport | undefined,
): CriterionTally {
  const counts = new Map<string, number>();
  let total = 0;
  for (const requirement of criteria.requirements) {
    for (const criterion of requirement.criteria) {
      total += 1;
      const status = report?.get(criterion.id)?.status;
      if (status) counts.set(status, (counts.get(status) ?? 0) + 1);
    }
  }
  // Known statuses in their fixed order, then anything unrecognised so a status
  // we have never seen still shows up rather than vanishing from the tally.
  const ordered = [
    ...STATE_ORDER.filter((s) => counts.has(s)),
    ...[...counts.keys()].filter((s) => !STATE_ORDER.includes(s)).sort(),
  ];
  return {
    total,
    states: ordered.map((status) => ({
      status,
      count: counts.get(status) ?? 0,
    })),
  };
}

export interface CriterionMethodCount {
  /** Raw criterion method; one of CriterionMethod in practice. */
  method: string;
  count: number;
}

/**
 * Tally the oracle's criteria by the METHOD that will verify each one.
 *
 * The oracle alone answers this — no report is involved — which is what makes it
 * the honest thing to show while an attempt is still running: how much of the
 * version an agent is about to drive end to end, and how much only a person can
 * judge. The view's own summary header reads it too, so the numbers above the
 * criteria and the numbers beside them come from one derivation.
 */
export function tallyCriterionMethods(
  criteria: ValidationCriteria,
): CriterionMethodCount[] {
  const counts = new Map<string, number>();
  for (const requirement of criteria.requirements) {
    for (const criterion of requirement.criteria) {
      counts.set(criterion.method, (counts.get(criterion.method) ?? 0) + 1);
    }
  }
  // Known methods in their fixed order, then anything unrecognised so a method we
  // have never seen still shows up rather than vanishing from the tally.
  const ordered = [
    ...METHOD_ORDER.filter((m) => counts.has(m)),
    ...[...counts.keys()].filter((m) => !METHOD_ORDER.includes(m)).sort(),
  ];
  return ordered.map((method) => ({ method, count: counts.get(method) ?? 0 }));
}
