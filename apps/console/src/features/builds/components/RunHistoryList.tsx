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

import { useState } from "react";
import {
  Box,
  ButtonBase,
  Card,
  Collapse,
  Divider,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronDown } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { runStamp } from "../lib/format";
import {
  buildCycles,
  runKind,
  runKindLabel,
  runStateChip,
  spentBudgets,
  terminalReasonText,
} from "../lib/runView";
import { CycleLines } from "./EarlierSessions";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

/**
 * The milestone's EARLIER RUNS — one collapsed line each.
 *
 * A settled run is a record, not something to watch, so the line carries only
 * what a reader scans for: how it ended, what started it, when it ran, and
 * what it left behind. Everything else — its build sessions, the reason it
 * stopped, any budget it spent — is one click down, because a page that shows
 * all of it for every past run is the rail this redesign replaced.
 */
export function RunHistoryList({
  runs,
  tag,
}: {
  runs: MilestoneRunView[];
  tag: string;
}) {
  if (runs.length === 0) return null;

  return (
    <Box>
      <Typography
        variant="caption"
        sx={{
          display: "block",
          mb: 1,
          fontWeight: 700,
          letterSpacing: "0.08em",
          color: "text.secondary",
        }}
      >
        EARLIER RUNS OF {tag.toUpperCase()}
      </Typography>
      <Stack spacing={1}>
        {runs.map((run) => (
          <RunRow key={run.id} run={run} />
        ))}
      </Stack>
    </Box>
  );
}

function RunRow({ run }: { run: MilestoneRunView }) {
  const [open, setOpen] = useState(false);
  const chip = runStateChip(run);
  const cycles = buildCycles(run.cycles);
  const merged = cycles.filter((c) => c.mergeSha).length;
  const reason = terminalReasonText(run.terminalReason ?? "");
  const spent = spentBudgets(run.budgets);

  // What it left behind, in the platform's own terms: a run whose sessions
  // merged nothing landed nothing — the fact worth reading at a glance.
  const outcome =
    reason ||
    (merged > 0
      ? `${merged} of ${cycles.length} build session${cycles.length === 1 ? "" : "s"} merged`
      : "Landed nothing — no build session merged.");

  return (
    <Card variant="outlined">
      <ButtonBase
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        // No aria-label: it would REPLACE the row's accessible name, hiding
        // the state, origin, times and outcome from assistive technology.
        // The content is the name; aria-expanded says which way it toggles.
        sx={{ width: "100%", justifyContent: "flex-start", px: 2, py: 1.25 }}
      >
        <Stack
          direction="row"
          spacing={1.5}
          sx={{
            alignItems: "center",
            flexWrap: "wrap",
            rowGap: 0.5,
            width: "100%",
            textAlign: "left",
          }}
        >
          <StatusChip
            label={chip.label}
            tone={chip.tone}
            appearance="soft"
            dot
          />
          <StatusChip
            label={runKindLabel(runKind(run))}
            tone="neutral"
            appearance="soft"
          />
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ whiteSpace: "nowrap" }}
          >
            {runStamp(run.startedAt ?? run.createdAt)}
            {run.endedAt ? ` → ${runStamp(run.endedAt)}` : ""}
          </Typography>
          <Typography
            variant="body2"
            color={chip.tone === "error" ? "error.main" : "text.secondary"}
            sx={{ flexGrow: 1, minWidth: 0 }}
          >
            {outcome}
          </Typography>
          <ChevronDown
            size={16}
            style={{
              flexShrink: 0,
              transition: "transform 0.15s",
              transform: open ? "rotate(180deg)" : "none",
            }}
          />
        </Stack>
      </ButtonBase>

      <Collapse in={open} unmountOnExit>
        <Divider />
        <Box sx={{ px: 2, py: 1.5 }}>
          {cycles.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No build session was ever dispatched — the run settled before the
              supervisor started one.
            </Typography>
          ) : (
            <CycleLines cycles={cycles} />
          )}
          {spent.length > 0 && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                display: "block",
                mt: 1.5,
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {`Budget spent: ${spent
                .map((budget) => `${budget.label} ${budget.text}`)
                .join(" · ")}`}
            </Typography>
          )}
        </Box>
      </Collapse>
    </Card>
  );
}
