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

// Which run in a version's story answers for validation, and which cycle holds the
// report. Both surfaces that render a verdict ask these questions, and asking them
// differently is a bug each has had: the Validation page took the newest run until
// #423, and the deployments hook still did after that was fixed.

import type { components } from "../../../generated/aep-api";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];

/**
 * The run origins that ask a version's validation criteria — the console's mirror of
 * delivery.RunValidates. A spec build validates the version it delivered, and a
 * revalidation exists to ask again; an incident adoption is absent on purpose,
 * because it fixes one thing in an already-judged version.
 */
export const VALIDATING_ORIGINS: readonly string[] = ["spec-build", "revalidate"];

/**
 * The run whose verdict is the version's answer, from a newest-first list.
 *
 * NOT the newest run. A milestone sees sequential runs across its life and only some
 * of them validate: an incident adoption never does, and `settle` stamps `skipped` on
 * any succeeded run that never did — so the newest run is routinely one whose verdict
 * means "I was never asked". Reading it made a single adopted issue report a
 * genuinely passed version as unvalidated (#423).
 */
export function validatingRun(
  runs: readonly MilestoneRunView[],
): MilestoneRunView | undefined {
  return runs.find((r) => VALIDATING_ORIGINS.includes(r.origin));
}

/**
 * The run holding the version's last ANSWER, which is not always the run being asked.
 *
 * A revalidation is a fresh run row on the same milestone: it enters the loop at
 * validation with an empty verdict while the run that delivered the version still
 * holds `passed`. Reading the asking run's verdict there reported a validated version
 * as having nothing to show, because "no verdict on this row" was being taken to mean
 * "this version has never been judged" — true of a first attempt and of a self-heal
 * repeat (which stays on ONE row), false of a revalidation.
 *
 * `skipped` counts as an answer: the version was reached and passed over, which is a
 * result a revalidation is asking to replace.
 */
export function answeredRun(
  runs: readonly MilestoneRunView[],
): MilestoneRunView | undefined {
  return runs.find(
    (r) =>
      VALIDATING_ORIGINS.includes(r.origin) && (r.validation?.verdict ?? "") !== "",
  );
}

/**
 * Whether the attempt in flight is a REPAIR of the verdict on hand, rather than a
 * fresh ask of it. True only when one run both holds the verdict and is running
 * again — the self-heal loop, which repeats within a single run.
 *
 * It is what lets the copy say "the implementation has been fixed and deployed",
 * which is a fact about a repair and false about a revalidation: nothing was fixed
 * between a passed version and someone asking again.
 */
export function isRepairing(runs: readonly MilestoneRunView[]): boolean {
  const answered = answeredRun(runs);
  return answered !== undefined && answered.id === validatingRun(runs)?.id;
}

/**
 * The last validation cycle that MERGED, across every run that attempted one, oldest
 * to newest — the cycle whose merge commit the report should be read at.
 *
 * Merged rather than simply last: a repeat attempt in flight has no report yet by
 * definition and its cycle record carries no mergeSha, so pinning to it passes an
 * empty ref and the read silently degrades to a branch tip. Across runs rather than
 * within one, because a version can be judged more than once and a revalidation is a
 * later run on the same milestone.
 */
export function lastMergedValidationCycle(
  runs: readonly MilestoneRunView[],
): RunCycleView | undefined {
  return [...runs]
    .reverse()
    .flatMap((r) => (r.cycles ?? []).filter((c) => c.kind === "validation"))
    .filter((c) => c.mergeSha)
    .at(-1);
}
