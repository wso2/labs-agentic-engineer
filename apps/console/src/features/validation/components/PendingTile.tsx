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

import { Alert, AlertTitle, alpha, Box, Typography } from "@wso2/oxygen-ui";
import {
  METHOD_COLOR,
  METHOD_FALLBACK_COLOR,
  METHOD_LABEL,
  type CriterionMethodCount,
} from "@aep/ui-validation-view";
import { validationView } from "../../projects/lib/pipeline";
import { FULL_WIDTH_ALERT_MESSAGE, LiveNote } from "./LiveNote";
import type { StatusLine } from "../../tasks/lib/statusLine";

// A method named inside a sentence, marked as a term rather than left as ordinary
// prose — otherwise nothing tells the reader that "auto" and "manual" are the two
// halves this page is about to split the criteria into.
//
// The rows below carry a glyph rather than a word, so this is the only place the
// words appear — which is the argument for marking them, not against it.
//
// A wash and not a fill: a filled pill mid-sentence stops the line dead, while a
// tint carries the mark and lets the words keep flowing. `text.primary` over an
// alpha fill rather than the raw colour as text, so it holds in both themes — the
// idiom StatusChip's soft tones and ValidationView's failure block both use.
//
// Word and colour both come from the shared vocabulary (counts.ts), so renaming or
// recolouring a method carries this sentence and the tally below it along.
function Method({ method }: { method: string }) {
  return (
    <Box
      component="span"
      sx={{
        px: 0.5,
        py: 0.125,
        borderRadius: 0.75,
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        whiteSpace: "nowrap",
        bgcolor: alpha(METHOD_COLOR[method] ?? METHOD_FALLBACK_COLOR, 0.16),
        color: "text.primary",
      }}
    >
      {METHOD_LABEL[method] ?? method}
    </Box>
  );
}

// The oracle was never authored, which is also why this run will settle as skipped.
// Said in the tile rather than as a note above the log because the tile is then the
// whole body — there are no criteria to put under it.
const NO_CRITERIA =
  "This version has no validation criteria, so there is nothing to check the deployment against.";

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
 * `methods` is the oracle's per-method tally, absent while the criteria are still
 * loading — the counts line is then simply not drawn, rather than the tile waiting
 * for numbers to explain a run that is already under way.
 */
export function PendingTile({
  methods,
  noCriteria = false,
  note,
}: {
  methods?: CriterionMethodCount[];
  /** The criteria read came back `not_found`: none were ever authored. */
  noCriteria?: boolean;
  /** What the run is doing while no criterion has anything to say — see LiveNote. */
  note?: string | StatusLine;
}) {
  const view = validationView("running");
  if (!view) return null;

  const manual = methods?.find((m) => m.method === "manual")?.count ?? 0;
  // "12 auto · 3 manual" — the same words as the sentence above, because
  // both read METHOD_LABEL.
  const counts = (methods ?? [])
    .map(({ method, count }) => `${count} ${METHOD_LABEL[method] ?? method}`)
    .join(" · ");

  return (
    // No margins, same as VerdictTile: the page's body container owns the gap below.
    <Alert severity="info" sx={FULL_WIDTH_ALERT_MESSAGE}>
      {/* The shared labels are lowercase for mid-sentence use; a headline leads. */}
      <AlertTitle>
        {view.label.charAt(0).toUpperCase() + view.label.slice(1)}
      </AlertTitle>
      <Typography variant="body2">
        {noCriteria ? (
          NO_CRITERIA
        ) : (
          <>
            {/* The two words the tally beneath counts by, so they are marked as
                terms rather than left as ordinary prose. */}
            <Method method="e2e" /> criteria are being validated end to end
            against the deployed system.
            {/* Only when there ARE manual criteria: this half is an instruction, and
                an instruction to check criteria that do not exist sends the reader
                looking for an empty list. */}
            {manual > 0 && (
              <>
                {" "}
                Please validate the <Method method="manual" /> criteria yourself.
              </>
            )}
          </>
        )}
      </Typography>
      {!noCriteria && counts && (
        <Typography variant="body2" sx={{ mt: 0.5, fontWeight: 500 }}>
          {counts}
        </Typography>
      )}
      {note && <LiveNote note={note} />}
    </Alert>
  );
}
