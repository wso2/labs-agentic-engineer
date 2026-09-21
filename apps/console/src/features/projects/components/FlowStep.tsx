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

import { Box, Stack, Typography, alpha } from "@wso2/oxygen-ui";
import { Check, X } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import type { FlowStep as FlowStepView, StepState } from "../lib/deploymentFlow";

// One step on the Development card's rail (ADR-0032). The MARK is the state —
// a check for done, a numbered ring for the step in play, a grey number for
// what has not happened and for what settled without a result (a skipped
// validation earns no green check) — and the rail below it is the sequence. Unlike the
// build page's spine (StageRow), the mark carries the step number inside it:
// there are only ever three steps here, and the number is the reader's first
// question ("which one am I on?").

const MARK = 22;
const RAIL = 2;

function markTone(state: StepState): "success" | "warning" | "info" | "error" | null {
  switch (state) {
    case "done":
      return "success";
    case "hold":
      return "warning";
    case "error":
      return "error";
    case "active":
      return "warning";
    default:
      // pending and settled: a grey ring around the number.
      return null;
  }
}

function Mark({ step, state, ringTone }: { step: number; state: StepState; ringTone?: "info" | "warning" }) {
  const tone = state === "active" && ringTone ? ringTone : markTone(state);
  const filled = state === "done" || state === "error";
  return (
    <Box
      aria-hidden
      sx={(theme) => ({
        width: MARK,
        height: MARK,
        borderRadius: "50%",
        flexShrink: 0,
        boxSizing: "border-box",
        display: "grid",
        placeItems: "center",
        fontSize: 11,
        fontWeight: 700,
        fontVariantNumeric: "tabular-nums",
        ...(filled && tone
          ? { bgcolor: theme.palette[tone].main, color: theme.palette[tone].contrastText }
          : tone
            ? { border: 2, borderColor: theme.palette[tone].main, color: theme.palette[tone].main }
            : {
                border: 2,
                borderColor: alpha(theme.palette.text.primary, 0.2),
                color: theme.palette.text.secondary,
              }),
      })}
    >
      {state === "done" ? (
        <Check size={13} strokeWidth={3} />
      ) : state === "error" ? (
        <X size={13} strokeWidth={3} />
      ) : (
        step
      )}
    </Box>
  );
}

/**
 * A step: its mark, its title, its chip, one sentence, and its own content —
 * the components and connections under Deployed, the verdict under
 * Validation, the button under Promote. `last` draws no rail below it.
 */
export function FlowStep({
  step,
  view,
  ringTone,
  last = false,
  grow = false,
  children,
}: {
  step: number;
  view: FlowStepView;
  /** The ring's colour for an ACTIVE step: amber by default, blue while the
   *  platform itself is moving (a rollout). */
  ringTone?: "info" | "warning";
  last?: boolean;
  /** Absorb the card's spare height in THIS step, which stretches its rail
   *  through the slack. The flow's cards share a height, so the step before
   *  each card's trailing one grows and the trailing step lands on the card's
   *  bottom — with the connector drawn the whole way down.
   *
   *  This is deliberately not `mt: "auto"` on the trailing step itself: that
   *  put the slack in a MARGIN, which no rail is drawn through, so on every
   *  card shorter than the tallest the connector visibly stopped and
   *  restarted (measured at 476px of naked gap in `EnvironmentFlow.browser.test`). */
  grow?: boolean;
  children?: React.ReactNode;
}) {
  const inactive = view.state === "pending";
  const spoken = `Step ${step}, ${view.title}${view.chip ? `, ${view.chip.spoken ?? view.chip.label}` : ""}`;
  return (
    <Stack
      direction="row"
      spacing={1.5}
      role="listitem"
      aria-label={spoken}
      sx={{ alignItems: "stretch", ...(grow && { flexGrow: 1 }) }}
    >
      <Stack sx={{ alignItems: "center", width: MARK, flexShrink: 0 }}>
        <Mark step={step} state={view.state} {...(ringTone ? { ringTone } : {})} />
        {!last && (
          <Box
            sx={(theme) => ({
              flexGrow: 1,
              width: RAIL,
              minHeight: 12,
              mt: 0.5,
              borderRadius: RAIL,
              bgcolor:
                view.state === "done"
                  ? theme.palette.success.main
                  : alpha(theme.palette.text.primary, 0.16),
            })}
          />
        )}
      </Stack>
      <Box sx={{ minWidth: 0, flexGrow: 1, pb: last ? 0 : 2.25 }}>
        <Stack
          direction="row"
          spacing={1}
          sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 0.5, minHeight: MARK }}
        >
          <Typography
            variant="subtitle2"
            sx={{ fontWeight: 600 }}
            color={inactive ? "text.secondary" : "text.primary"}
          >
            {view.title}
          </Typography>
          {view.chip && (
            <StatusChip
              label={view.chip.label}
              tone={view.chip.tone}
              appearance="soft"
              dot={view.chip.tone !== "neutral"}
              {...(view.chip.spoken ? { spokenLabel: view.chip.spoken } : {})}
            />
          )}
        </Stack>
        {view.note && (
          <Typography
            variant="body2"
            color={inactive ? "text.disabled" : "text.secondary"}
            sx={{ mt: 0.5 }}
          >
            {view.note}
          </Typography>
        )}
        {children && <Stack spacing={1.25} sx={{ mt: 1 }}>{children}</Stack>}
      </Box>
    </Stack>
  );
}
