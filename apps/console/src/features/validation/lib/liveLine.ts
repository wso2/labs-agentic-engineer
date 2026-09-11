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

// The FALLBACK for the run-wide line above the criterion rows, derived from the
// rows and never reported alongside them. The agent's own status line — its
// validation issue's newest comment, `tasks/lib/statusLine.ts` — is what the
// tile shows when there is one; this speaks before the first comment lands, and
// for a run whose agent posts none.
//
// The alternative rejected here was a `phase` event the runner emits at each
// step boundary of the aep-validation workflow. That is a second claim about the
// same run, and it would have been wrong for most of it: authoring.md requires a
// spec to pass twice consecutively against the live app, so the agent runs tests
// all through the authoring step. A marker saying "Authoring tests…" while rows
// say `Running…`, then flipping to "Running tests…" an hour after the first test
// ran, is a narration that disagrees with the evidence beside it.
//
// The agent's line is NOT that marker returning. A phase enum claims the same
// fact the rows claim, at the same granularity, from a second source. The status
// line is prose at a different granularity — the skill asks it to say what the
// rows cannot — so the two can be read together without either being checkable
// against the other. Derived from the rows, this one cannot disagree with them:
// it IS the rows, and it says something only in the two windows where they say
// nothing.

import { runWorksOn } from "@aep/ui-validation-view";
import type { LiveStatuses, ValidationCriteria } from "@aep/ui-validation-view";

/** report.json's terminal words, which the live feed also emits. */
const TERMINAL = new Set(["pass", "fail"]);

/**
 * The criteria a RUN can act on. `manual` criteria are answered by a human and
 * never move, so counting them would mean the line never reaches "all settled"
 * on any project that has one.
 *
 * From the shared vocabulary rather than a local `!== "manual"`: the criterion
 * ROWS test the same thing to decide whether a live status may speak for a row, so
 * a second copy here can count a criterion as answerable that its own row is
 * refusing to show progress for.
 */
function agentCriteriaIds(oracle: ValidationCriteria): string[] {
  return oracle.requirements.flatMap((r) =>
    r.criteria.filter((c) => runWorksOn(c.method)).map((c) => c.id),
  );
}

/**
 * What to say above the rows, or "" when there are no rows to say it about.
 *
 * Empty means this page has nothing to describe — no criteria, or none a run can
 * act on. Every other answer is a sentence, because a live run with a blank line
 * over it looks exactly like one that died.
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
  if (touched.length === ids.length && ids.every((id) => TERMINAL.has(live?.[id] ?? ""))) {
    // …and once the report HAS landed, nothing. The rows read from it and the
    // verdict is beside them, so a line here could only restate them — and the
    // trailing ellipsis would promise work still in flight on a run that has
    // answered everything it was asked.
    return hasReport ? "" : "Writing the validation report…";
  }

  // The long middle: work is under way and not finished. The rows carry the
  // detail here, so this says the one thing they do not — how far through the
  // whole set the run is, which is a count no single row can show.
  //
  // It is a FALLBACK, and a distant one. The run posts its own line on the issue
  // as it works, so a reader normally sees that instead; this speaks when a post
  // failed, or before the first one lands. Blank was the old answer and it was
  // the worst of the three: the tile went empty for the longest stretch of the
  // run, which reads the same as a run that stopped.
  const answered = ids.filter((id) => TERMINAL.has(live?.[id] ?? "")).length;
  return `Checking the criteria, ${answered} of ${ids.length} answered…`;
}
