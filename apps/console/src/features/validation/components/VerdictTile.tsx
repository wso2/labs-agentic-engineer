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

import { Alert, AlertTitle, Typography } from "@wso2/oxygen-ui";
import type { CriterionTally } from "@aep/ui-validation-view";
import { validationView, type StageTone } from "../../projects/lib/pipeline";
import { countsFromTally, verdictCounts, verdictSentence } from "../lib/verdict";

// The verdicts this tile speaks for. `skipped` is absent on purpose: the page
// answers it with an empty state, because there is no report and no criteria to
// put a tile above. Anything else — "", "running", a value from a newer server —
// renders nothing rather than an empty box.
const TILE_VERDICTS = new Set([
  "passed",
  "partial",
  "failed",
  "inconclusive",
  "unreported",
]);

// StageTone → Alert severity, which is also what picks the Alert's icon. Kept a
// total map for exhaustiveness; `ghost`/`neutral` are unreachable here because
// none of the five tile verdicts maps to them.
const SEVERITY: Record<StageTone, "success" | "info" | "warning" | "error"> = {
  ghost: "info",
  neutral: "info",
  info: "info",
  warning: "warning",
  success: "success",
  error: "error",
};

/**
 * The verdict tile: what the validation run concluded, above the per-criterion
 * evidence. It exists because a chip label cannot finish the sentence for the
 * verdicts that matter most — "Validated*" begs *which part*, "Validation?" begs
 * *why is that a question*, "Validation error" begs *what broke* — and
 * because three of the five ask the reader to do something (validate the rest by
 * hand, fix a failure), which no chip can say.
 *
 * The headline is the SHARED mapper's label (projects/lib/pipeline), capitalized,
 * never restated — so the tile and the header chip cannot drift apart. Only the
 * sentence is local copy. There are no Pass/Fail controls: nothing about a verdict
 * waits on a person.
 *
 * The headline comes from `state` and the copy from `verdict`, because they answer
 * different questions: mid-repair the tile has to lead with what the platform is
 * DOING ("Awaiting fix") while still explaining what the last attempt FOUND. Whether
 * the tile appears at all stays keyed on the verdict — there is no evidence to put a
 * tile above until an attempt has produced one.
 */
export function VerdictTile({
  verdict,
  state = verdict,
  repairing = false,
  tally,
}: {
  verdict: string;
  /**
   * The loop's position, from projects/lib/pipeline validationState. Defaults to the
   * verdict, which is what "this verdict is the run's answer" looks like.
   */
  state?: string;
  /** The attempt in flight repairs this verdict rather than re-asking it. */
  repairing?: boolean;
  tally?: CriterionTally;
}) {
  const view = validationView(state);
  if (!view || !TILE_VERDICTS.has(verdict)) return null;

  const counts = verdictCounts(tally, state);
  return (
    // No margins: the page's body container owns the gap below and PageTitle owns
    // the space above. A tile that insets itself put the page's one 24px-inset
    // element beside bodies that were flush.
    <Alert severity={SEVERITY[view.tone]}>
      {/* The shared labels are lowercase for mid-sentence use; a headline leads. */}
      <AlertTitle>
        {view.label.charAt(0).toUpperCase() + view.label.slice(1)}
      </AlertTitle>
      <Typography variant="body2">
        {verdictSentence(verdict, tally && countsFromTally(tally), state, repairing)}
      </Typography>
      {counts && (
        <Typography variant="body2" sx={{ mt: 0.5, fontWeight: 500 }}>
          {counts}
        </Typography>
      )}
    </Alert>
  );
}
