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

import { Box, Card, Divider, Typography } from "@wso2/oxygen-ui";
import { StatusChip } from "../../../components/StatusChip";
import { runStamp } from "../../builds/lib/format";
import { buildDuration } from "../../builds/lib/ledger";
import type { StatusLine } from "../../tasks/lib/statusLine";
import { validationChip } from "../lib/chip";
import { verdictCounts, verdictSentence, type ValidationCounts } from "../lib/verdict";
import { LiveNote } from "./LiveNote";

/**
 * What this version's validation concluded, in the four facts a reader asks
 * for, on the silhouette the build page established.
 *
 * It replaced an Alert-shaped tile whose severity tinted the WHOLE card by
 * verdict, which spent the loudest colour on the ordinary outcome: a passing
 * run rendered as a green banner and left nothing louder for a real failure.
 * Here the verdict keeps its tone on the one cell that is about it, and the
 * card's own border carries the only page-level signal worth shouting — that
 * something is moving.
 */
export function ValidationSummaryCard({
  state,
  verdict,
  counts,
  countsLine,
  startedAt,
  endedAt,
  live,
  repairing,
  note,
}: {
  /** The 10-value lifecycle — what the chip and the sentence are keyed on. */
  state: string;
  /** The run's own verdict, which differs from `state` mid-loop. */
  verdict: string;
  counts: ValidationCounts | undefined;
  /** "7 of 9 passed · 2 blocked", from the report the page already parsed. */
  countsLine: string;
  /** The latest ATTEMPT's clock — not the run's. */
  startedAt: string | null | undefined;
  endedAt: string | null | undefined;
  live: boolean;
  repairing: boolean;
  /** The agent's own status line, while an attempt is running. */
  note?: StatusLine | null;
}) {
  const chip = validationChip(state);
  const counting = Boolean(startedAt) && !endedAt;
  const duration = buildDuration(startedAt, endedAt);
  const tally = verdictCounts(countsLine, state);

  const cells: Array<{ label: string; value: React.ReactNode }> = [
    {
      label: "Verdict",
      // A chip rather than text, and it is the one cell that earns colour. It
      // also carries the spoken form: `Validated*` read aloud from a plain
      // Typography is "validated asterisk", and the mark is the whole hedge.
      value: chip ? (
        <StatusChip
          label={chip.label}
          tone={chip.tone}
          appearance="soft"
          dot
          {...(chip.spokenLabel ? { spokenLabel: chip.spokenLabel } : {})}
        />
      ) : (
        "Not validated"
      ),
    },
    { label: "Scenarios", value: tally || "—" },
    { label: "Last validated", value: runStamp(endedAt) || "—" },
    {
      label: "Duration",
      value: (
        <>
          <Box component="span" sx={{ fontVariantNumeric: "tabular-nums" }}>
            {duration || "—"}
          </Box>
          {counting && (
            <Box component="span" sx={{ color: "text.secondary" }}>
              {" "}
              and counting
            </Box>
          )}
        </>
      ),
    },
  ];

  return (
    <Card
      variant="outlined"
      sx={{ p: 2.5, ...(live && { borderColor: "info.main" }) }}
    >
      <Box
        sx={{
          display: "grid",
          gap: 2.5,
          gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr", lg: "repeat(4, minmax(0, 1fr))" },
        }}
      >
        {cells.map((c) => (
          <Box key={c.label} sx={{ minWidth: 0 }}>
            <Typography
              variant="overline"
              color="text.secondary"
              sx={{ fontWeight: 700, letterSpacing: "0.07em" }}
            >
              {c.label}
            </Typography>
            <Typography variant="body2" component="div" sx={{ mt: 0.5, fontWeight: 500 }}>
              {c.value}
            </Typography>
          </Box>
        ))}
      </Box>

      <Divider sx={{ my: 2 }} />

      {/* The sentence and the live note BOTH, never one instead of the other.
          The sentence carries the "(last attempt)" qualifier that says the
          numbers above are stale — which matters most in exactly the state
          where a note exists, because that is when a repeat is running. */}
      <Typography variant="body2" color="text.secondary">
        {verdictSentence(verdict, counts, state, repairing)}
      </Typography>
      {note && (
        <Box sx={{ mt: 1.5 }}>
          <LiveNote note={note} />
        </Box>
      )}
    </Card>
  );
}
