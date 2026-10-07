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

import type { components } from "../../../generated/aep-api";

// A version's validation as the Validation ledger and card read it, copied
// from the old console (features/validation/lib/, ValidationMilestonePage):
// its state in words, its attempts in order, and when it can be asked again.
// Pure, so the ledger, the card and their tests agree.

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type ValidationDetail = components["schemas"]["ValidationDetail"];

export type VerdictTone = "primary" | "success" | "warning" | "error" | null;

/** A judging in flight, or the repair before the next one: what keeps the ledger's poll close. */
export function validationIsLive(state: string): boolean {
  return state === "running" || state === "awaiting-fix";
}

/**
 * A version's validation state, or an attempt's verdict, in words and a tone;
 * null for `none` (nothing has judged the version), which a surface says in a
 * sentence instead. One reading of the vocabulary for every surface.
 */
export function verdictView(state: string): { label: string; tone: VerdictTone } | null {
  switch (state) {
    case "running":
      return { label: "Validating", tone: "primary" };
    case "awaiting-fix":
      return { label: "Awaiting fix", tone: "warning" };
    case "passed":
      return { label: "Passed", tone: "success" };
    case "partial":
      return { label: "Passed, partly checked", tone: "success" };
    case "failed":
      return { label: "Failed", tone: "error" };
    case "inconclusive":
      return { label: "Inconclusive", tone: "warning" };
    case "unreported":
      return { label: "No report", tone: "error" };
    case "skipped":
      return { label: "Skipped: no scenarios", tone: null };
    case "cancelled":
      return { label: "Cancelled", tone: null };
    default:
      return null;
  }
}

/** One judging of the version: a validation cycle, the run it ran in, and its number. */
export interface Attempt {
  cycle: RunCycleView;
  runId: string;
  /** Counted from the oldest across every run of the version: the number a reader calls it by. */
  number: number;
}

/**
 * Every attempt, newest first. Runs arrive newest first and each run's cycles
 * oldest first, so the runs keep their order and only each run's cycles are
 * walked backwards; numbers count from the oldest attempt of the oldest run.
 */
export function attemptsOf(runs: readonly Pick<MilestoneRunView, "id" | "cycles">[]): Attempt[] {
  return runs.flatMap((run, i) => {
    const before = runs.slice(i + 1).reduce((n, r) => n + r.cycles.length, 0);
    return run.cycles.map((cycle, j): Attempt => ({ cycle, runId: run.id, number: before + j + 1 })).reverse();
  });
}

/** An attempt's result: in flight, a verdict, or ended without landing a report. */
export function attemptResult(cycle: Pick<RunCycleView, "endedAt" | "mergeSha" | "validationVerdict">): {
  label: string;
  tone: VerdictTone;
} {
  if (!cycle.endedAt) return { label: "Validating", tone: "primary" };
  const verdict = cycle.validationVerdict ? verdictView(cycle.validationVerdict) : null;
  if (verdict) return verdict;
  return cycle.mergeSha ? { label: "No verdict", tone: null } : { label: "Did not land", tone: "error" };
}

/**
 * Why the version cannot be validated again now, or null when it can: a run
 * is already on it, or it is not the version deployed (validation drives what
 * runs, so asking an older version would judge code it never shipped). The
 * platform refuses the rest (open work, no scenarios) in its own words.
 */
export function revalidateRefusal(detail: Pick<ValidationDetail, "live" | "deployed">): string | null {
  if (detail.live) return "A run is already working this version.";
  if (!detail.deployed) return "Only the deployed version can be validated: this one is not what runs.";
  return null;
}

/** Why a version has no attempt yet, and whether the reader should do something. */
export function noAttemptsReason(state: string, live: boolean): string {
  if (state === "skipped") return "This version has no acceptance scenarios, so there is nothing to validate it against.";
  if (live) return "Nothing validated yet. Once it deploys, the running version is checked against its acceptance scenarios.";
  return "Nothing validated yet. Revalidate checks this version against its acceptance scenarios.";
}
