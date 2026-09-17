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
import { validationView } from "../../projects/lib/pipeline";
import { FULL_WIDTH_ALERT_MESSAGE, LiveNote } from "./LiveNote";
import type { StatusLine } from "../../tasks/lib/statusLine";

// The oracle was never authored, which is also why this run will settle as skipped.
// Said in the tile rather than as a note above the log because the tile is then the
// whole body — there are no criteria to put under it.
const NO_CRITERIA =
  "This version has no acceptance criteria, so there is nothing to check the deployment against.";

/**
 * The tile over a FIRST validation attempt in flight: what is being checked, by
 * whom, and how much of it.
 *
 * A sibling of VerdictTile rather than a branch inside it. That tile is keyed to
 * verdicts in its gate, its severity, its sentence and its tally — a run that has
 * concluded nothing shares only the Alert shell with it, and the two would have
 * ended up as one component with an escape hatch in every one of those four places.
 *
 * The headline comes from the shared mapper (projects/lib/pipeline) exactly as
 * VerdictTile's does, so the tile and the header chip above it cannot drift.
 *
 * `scenarios` is how many the feature files declare, absent while they are still
 * loading — the count line is then simply not drawn, rather than the tile waiting
 * for a number to explain a run that is already under way.
 *
 * There is no method split here, and that is the path's doing rather than an
 * omission: a validation criterion declared at design time whether an agent or a
 * person would check it, and an acceptance scenario does not. Every one of them is
 * driven against the deployed system, and whether a person has to finish the job
 * is something the RUN discovers — `blocked`, `unjudgeable` — and says afterwards.
 */
export function PendingTile({
  scenarios,
  noCriteria = false,
  note,
}: {
  scenarios?: number;
  /** No feature files at this version: none were ever authored. */
  noCriteria?: boolean;
  /** What the run is doing while no criterion has anything to say — see LiveNote. */
  note?: string | StatusLine;
}) {
  const view = validationView("running");
  if (!view) return null;


  return (
    // No margins, same as VerdictTile: the page's body container owns the gap below.
    <Alert severity="info" sx={FULL_WIDTH_ALERT_MESSAGE}>
      {/* The shared labels are lowercase for mid-sentence use; a headline leads. */}
      <AlertTitle>
        {view.label.charAt(0).toUpperCase() + view.label.slice(1)}
      </AlertTitle>
      <Typography variant="body2">
        {noCriteria
          ? NO_CRITERIA
          : "Every acceptance scenario is being driven against the deployed system."}
      </Typography>
      {!noCriteria && scenarios !== undefined && scenarios > 0 && (
        <Typography variant="body2" sx={{ mt: 0.5, fontWeight: 500 }}>
          {`${scenarios} ${scenarios === 1 ? "scenario" : "scenarios"}`}
        </Typography>
      )}
      {note && <LiveNote note={note} />}
    </Alert>
  );
}
