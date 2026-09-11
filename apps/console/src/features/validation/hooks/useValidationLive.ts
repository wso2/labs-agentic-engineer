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

// What the validating run is doing to each acceptance criterion, right now.
//
// The runner emits one `work_item` event per status change (see the runner's
// validation_progress.ts). This folds them into the map ValidationView paints
// its rows from: many events about one criterion become one row repainted, which
// is the whole reason that event kind exists — and why `work_item` renders as no
// row at all in the feed beside this page.
//
// `source` is what splits the kind's two populations. A criterion is a unit of
// work the PLATFORM put in front of the run, and its statuses are the validation
// method's (ADR-0009); an agent's own plan entry shares the kind and carries a
// different vocabulary, in which `completed` says an entry was ticked off rather
// than that anything was asserted. Folding a plan entry in here would paint an
// agent's to-do list onto the acceptance criteria.
//
// No store, unlike the chat's planStore. That one earns its module scope from
// cross-component access and history rehydration; this map has exactly one
// consumer on one page, and a hook keeps the lifetime tied to it.

import { useMemo } from "react";
import type { LiveStatuses } from "@aep/ui-validation-view";
import { useRunProgress, type RunProgressCycle } from "../../builds/hooks/useRunProgress";

const VALIDATION = "validation";
/** RunEventKind for a named unit of work whose status changed. */
const WORK_ITEM = "work_item";
/** RunEvent.source for a criterion the platform set, as against an agent's plan. */
const CRITERION = "criterion";

export interface ValidationLive {
  /** Per-criterion statuses from the open validation cycle. */
  statuses: LiveStatuses;
  /**
   * Whether a validation cycle is actually in flight on this run.
   *
   * Separate from `statuses` being empty, and the distinction is the point: an
   * open cycle that has not touched a criterion yet is a run still setting up,
   * while no open cycle at all is a run that has finished validating or is off
   * doing something else (a repair cycle writing code). Both have no statuses;
   * only the first has anything to narrate.
   */
  active: boolean;
}

const IDLE: ValidationLive = { statuses: {}, active: false };

/**
 * Fold one run's validation progress into per-criterion statuses.
 *
 * Only the NEWEST validation cycle counts. A run can validate more than once —
 * an `unreported` re-dispatch, or the loop's bounded re-check — and the earlier
 * attempt's statuses describe work that is over. Carrying them forward would
 * show a criterion as `pass` from an attempt whose report was already rejected.
 */
export function foldValidationProgress(
  cycles: readonly RunProgressCycle[],
): ValidationLive {
  const newest = cycles.filter((c) => c.cycle.kind === VALIDATION).at(-1);
  // A CLOSED cycle has nothing live to say, and saying it anyway is the failure
  // mode this guard exists for: while a repair cycle is coding, the newest
  // validation cycle is the previous attempt's, and folding it would override
  // that attempt's own report with the statuses that produced it — a page
  // insisting a criterion is "Running…" while nothing is running it.
  if (!newest || newest.cycle.endedAt) return IDLE;

  const out: Record<string, string> = {};
  for (const event of newest.events) {
    if (event.kind !== WORK_ITEM || event.source !== CRITERION) continue;
    // Both are required by the contract for this kind; an event missing either
    // is a producer this build does not understand, and is skipped rather than
    // folded into a blank row.
    if (!event.itemId || !event.itemStatus) continue;
    // Last write wins: the events arrive in the order the runner emitted them,
    // so the newest status for an item is simply the last one seen.
    out[event.itemId] = event.itemStatus;
  }
  return { statuses: out, active: true };
}

/**
 * Live per-criterion statuses for a validating run, or `{}` when there is none.
 *
 * `enabled` exists so the page opens no second connection to a run whose feed it
 * is already showing: the log view mounts RunFeed, which streams the same run.
 * The report view is where these statuses are rendered, so that is the only
 * place the stream is worth opening.
 */
export function useValidationLive(
  projectName: string,
  runId: string | undefined,
  enabled: boolean,
): ValidationLive {
  const { cycles } = useRunProgress(projectName, runId, enabled);
  return useMemo(() => foldValidationProgress(cycles), [cycles]);
}
