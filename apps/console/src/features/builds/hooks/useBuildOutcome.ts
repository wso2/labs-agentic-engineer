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
import type { components } from "../../../generated/aep-api";
import { deliveryRun, useBuildRuns, useValidationSnapshot } from "../api/runs";
import { groupByFeature, validationOutcome, type FeatureResults, type ValidationOutcome } from "../model/validation";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];

export interface BuildOutcome {
  run: MilestoneRunView | undefined;
  /** The run's validation attempt, once it has one. */
  validation: RunCycleView | undefined;
  /** Every scenario, grouped by feature; null until the criteria have been read. */
  groups: FeatureResults[] | null;
  /** The settled result; null while validation has not finished. */
  outcome: ValidationOutcome | null;
  /** The previous validated version the failures are read against; null when none. */
  baseline: string | null;
  error: Error | null;
}

/**
 * A version's validation: its newest validation attempt among the version's
 * runs (list-build-runs) — on the platform that is a run of its own, started
 * once the dev run has delivered, not a cycle of the dev run — and that
 * attempt's report with the criteria it judged and the version's reading of
 * it (get-validation-report), grouped by feature. `liveCycle` is the stream's
 * fresher record of a validation cycle, when a card is watching a run that
 * holds one; without it the run poll says.
 */
export function useBuildOutcome(
  projectName: string,
  tag: string | undefined,
  liveCycle?: RunCycleView,
): BuildOutcome {
  const runs = useBuildRuns(projectName, tag);
  const run = runs.data ? deliveryRun(runs.data.runs) : undefined;
  // Newest first, so the first run holding a validation cycle holds the newest attempt.
  const validating = runs.data?.runs.find((r) => r.cycles.some((c) => c.kind === "validation"));
  const polled = validating?.cycles.filter((c) => c.kind === "validation").at(-1);
  const validation = liveCycle ?? polled;
  const settled = Boolean(validation?.endedAt);
  const snapshot = useValidationSnapshot(projectName, tag ?? "", validation?.id ?? "", Boolean(validation), settled);
  const data = snapshot.data;
  const groups = useMemo(() => (data ? groupByFeature(data) : null), [data]);
  const outcome = useMemo(() => (settled && groups && data?.report ? validationOutcome(groups) : null), [settled, groups, data]);
  return {
    run,
    validation,
    groups,
    outcome,
    baseline: data?.baseline?.version ?? null,
    error: runs.error ?? snapshot.error ?? null,
  };
}
