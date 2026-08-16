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

import { useMemo } from "react";
import {
  parseValidationCriteria,
  parseValidationReport,
  tallyCriterionStates,
} from "@aep/ui-validation-view";
import { useBuildRuns } from "../../builds/api/queries";
import { countsFromTally, type ValidationCounts } from "../lib/verdict";
import { useValidationCriteria, useValidationReport } from "./queries";

// Validation evidence for surfaces OUTSIDE the Validation page (#395: the
// Deployments rail says "8/12 passed" beside its verdict). Same join the
// Validation page performs — the newest run's verdict, its last MERGED validation
// cycle's commit pinning the report read, the authored oracle as the denominator —
// packaged as one hook so the two surfaces cannot resolve the run differently.

// The states whose counts inform: a report was (or should have been) joined.
//
// The two LIFECYCLE values are in it because the loop made them countable. A run
// repairing or re-running a failed attempt still has that attempt's report at its
// merge commit, and those numbers are the whole explanation of the state — without
// them the rail can only say "awaiting fix" and leave the reader to click through.
//
// `unreported` and `inconclusive` stay out: both would tally 0/N, a number that
// reads as "everything failed" about runs whose actual story is "nothing was
// joined". A lifecycle state sitting over an `unreported` verdict resolves to no
// counts by the same route — its report genuinely does not exist.
const COUNTABLE = new Set(["passed", "partial", "failed", "awaiting-fix", "running"]);

/**
 * What the deployed version's validation is, in the two facts a surface outside the
 * Validation page needs to put it into words: the run's VERDICT and the criteria
 * COUNTS behind it.
 *
 * The verdict is here because `deploy.validation` cannot supply it — `awaiting-fix`
 * folds `failed` and `unreported` into one word, and those two need opposite copy
 * (one has a report and repair work, the other has neither). Pairing it with
 * deploy.validation through projects/lib/pipeline validationState is what the caller
 * then renders.
 *
 * counts is undefined while loading, when the state has no countable report, and in
 * every failure mode (no run, no report, unparseable files) — an upgrade, never a
 * blocker, so every caller has a count-free form.
 *
 * `version` is the BUILD version (status.build.version) — the newest run's tag,
 * which is what `deploy.validation` describes. deploy.version names the newest
 * SUCCEEDED run and lags while validation is in flight.
 */
export function useValidationEvidence(
  projectName: string,
  version: string,
  deployValidation: string,
): { verdict: string; counts?: ValidationCounts } {
  const wanted = COUNTABLE.has(deployValidation);
  const runs = useBuildRuns(projectName, wanted && version ? version : undefined);
  const run = runs.data?.runs?.[0];
  const rawVerdict = run?.validation?.verdict ?? "";
  const settled = wanted && rawVerdict !== "" && rawVerdict !== "skipped";
  const missingReport = rawVerdict === "unreported";
  // The last validation cycle that MERGED, not simply the last one. A cycle still in
  // flight has no report by definition, and pinning to it passes an empty ref — which
  // silently degrades to a branch-tip read, the one thing this join exists to avoid.
  // The tip happens to hold the previous attempt's report until the new one merges,
  // so the bug returns the right answer by accident and would stop doing so the
  // moment anything else wrote the path.
  const cycle = run?.cycles
    ?.filter((c) => c.kind === "validation" && c.mergeSha)
    .at(-1);

  const criteria = useValidationCriteria(projectName, version, settled);
  const report = useValidationReport(
    projectName,
    version,
    settled && !missingReport,
    run?.validation?.reportPath ?? "",
    cycle?.mergeSha,
  );

  const criteriaContent = criteria.data?.content;
  const reportContent = report.data?.content;
  const counts = useMemo(() => {
    if (!settled || !criteriaContent || !reportContent) return undefined;
    const oracle = parseValidationCriteria(criteriaContent);
    if ("kind" in oracle) return undefined;
    const parsed = parseValidationReport(reportContent);
    if ("kind" in parsed) return undefined;
    return countsFromTally(tallyCriterionStates(oracle, parsed));
  }, [settled, criteriaContent, reportContent]);

  return { verdict: rawVerdict, ...(counts ? { counts } : {}) };
}
