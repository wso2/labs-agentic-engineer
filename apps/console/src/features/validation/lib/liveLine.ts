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

// The run-wide line above the criterion rows — and the ONE rule it obeys:
// it is derived from the rows, never reported alongside them.
//
// The alternative was a `phase` event the runner emits at each step boundary of
// the aep-validation workflow. That is a second claim about the same run, and it
// would have been wrong for most of it: authoring.md requires a spec to pass
// twice consecutively against the live app, so the agent runs tests all through
// the authoring step. A marker saying "Authoring tests…" while rows say
// `Running…`, then flipping to "Running tests…" an hour after the first test
// ran, is a narration that disagrees with the evidence beside it.
//
// Derived from the rows, it cannot disagree — it IS the rows. And it says
// something only in the two windows where they say nothing.

import type { LiveStatuses, ValidationCriteria } from "@aep/ui-validation-view";

/** report.json's terminal words, which the live feed also emits. */
const TERMINAL = new Set(["pass", "fail"]);

/**
 * The criteria a RUN can act on. `manual` criteria are answered by a human and
 * never move, so counting them would mean the line never reaches "all settled"
 * on any project that has one.
 */
function agentCriteriaIds(oracle: ValidationCriteria): string[] {
  return oracle.requirements.flatMap((r) =>
    r.criteria.filter((c) => c.method !== "manual").map((c) => c.id),
  );
}

/**
 * What to say above the rows, or "" to say nothing.
 *
 * Empty is the normal answer. Once any criterion has a status the rows carry
 * the whole story, and a summary sentence over them would only be a coarser
 * version of what the reader is already looking at.
 */
export function validationLiveLine(
  oracle: ValidationCriteria | undefined,
  live: LiveStatuses | undefined,
  hasReport: boolean,
): string {
  if (!oracle) return "";
  const ids = agentCriteriaIds(oracle);
  if (ids.length === 0) return "";

  const touched = ids.filter((id) => live?.[id] !== undefined);

  // Nothing has been picked up yet. This is SKILL.md steps 1-5 — reading the
  // issue, cutting the branch, reading the validation context, scaffolding
  // tests/e2e — several minutes in which every row reads "Pending" and the page
  // is otherwise indistinguishable from a run that died at dispatch.
  if (touched.length === 0) return "Setting up the test harness…";

  // Every criterion has an answer but the report has not landed. This is steps
  // 9-10: the final full run, generate-report.mjs, the push and the pull
  // request. The rows are all settled, so nothing on the page moves until the
  // platform reads the report at the merge commit.
  if (!hasReport && touched.length === ids.length && ids.every((id) => TERMINAL.has(live?.[id] ?? ""))) {
    return "Writing the validation report…";
  }

  return "";
}
