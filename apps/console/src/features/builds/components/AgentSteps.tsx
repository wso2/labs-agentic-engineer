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

import { Box, Typography } from "@wso2/oxygen-ui";
import { formatEvent, formatOutcome, mergeOutcomes } from "@aep/progress-view";
import { LogNote } from "../../../components/LogSection";
import { toneColor } from "../../../components/logTone";
import {
  runEventKey,
  type RunProgressPhase,
  type StampedRunEvent,
} from "../hooks/useRunProgress";

// ONE agent's steps, in the order it took them. The crew inspector's whole body,
// and the only place a v2 event becomes a row.
//
// It is deliberately flat: the tree is the crew's job now, so a step here is
// always the selected agent's own — never a nested section, never another
// agent's line interleaved into it. That is the difference the crew view buys.

/**
 * One rendered row, with its outcome trailing on the same row.
 *
 * The wire carries an action and its outcome as two events on purpose — the
 * action is emitted before the command runs, which is what makes the feed live.
 * This surface holds every event in state, so it can put the outcome back where
 * it belongs instead of printing it as a second row.
 */
export function LogRow({
  text,
  tone,
  outcome,
}: {
  text: string;
  tone: string;
  outcome?: { text: string; tone: string } | undefined;
}) {
  return (
    <Box sx={{ display: "flex", alignItems: "baseline", gap: 1.5 }}>
      <Typography
        component="div"
        sx={{
          font: "inherit",
          color: tone,
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          minWidth: 0,
        }}
      >
        {text}
      </Typography>
      {outcome?.text ? (
        <Typography
          component="div"
          title={outcome.text}
          sx={{
            font: "inherit",
            color: outcome.tone,
            // Pushed to the right so the durations and exit codes line up in
            // their own column and an abnormal one is findable by scanning.
            ml: "auto",
            textAlign: "right",
            maxWidth: "45%",
            wordBreak: "break-word",
            flexShrink: 0,
          }}
        >
          {outcome.text}
        </Typography>
      ) : null}
    </Box>
  );
}

/**
 * What an agent SAID it did.
 *
 * The one thing the old feed could not show at all. A spawned agent's transcript
 * never reaches this stream and dies with its pod, so this is the only copy —
 * which is why it is given prose treatment rather than being squeezed onto the
 * status line beside the counters.
 */
export function AgentReportNote({ report }: { report: string }) {
  return (
    <Typography
      component="div"
      sx={{
        font: "inherit",
        color: "grey.400",
        whiteSpace: "pre-wrap",
        wordBreak: "break-word",
        borderLeft: 1,
        borderColor: "grey.800",
        ml: 0.75,
        pl: 1.5,
        py: 0.5,
      }}
    >
      {report}
    </Typography>
  );
}

/**
 * Empty-state copy for a cycle that has produced nothing yet. Distinguishes
 * attaching to a finished run's archive from a live agent that has not spoken
 * yet, and from a settled run that truly had nothing to say.
 */
export function agentLogEmptyNote(
  phase: RunProgressPhase,
  opts: { agentRunning?: boolean } = {},
): string {
  switch (phase) {
    case "connecting":
      return "Loading agent output…";
    case "reconnecting":
      return "Reconnecting…";
    case "live":
      return opts.agentRunning
        ? "Waiting for the agent's first line…"
        : "Loading agent output…";
    case "ended":
      return "No output was recorded.";
    default:
      return "No output from this cycle yet.";
  }
}

/** One agent's own steps, each outcome folded onto the action it answers. */
export function AgentSteps({ steps }: { steps: readonly StampedRunEvent[] }) {
  if (steps.length === 0) {
    // A real and common state, not a failure: a backgrounded agent forwards
    // none of its own messages, so its whole section arrives empty — and the
    // header saying `background` is what explains it.
    return <LogNote>This agent forwarded no steps.</LogNote>;
  }
  return (
    <>
      {mergeOutcomes(steps).map((row) => {
        const { text, tone } = formatEvent(row.line);
        // Deliberately silent events carry no row — see formatEvent.
        if (!text) return null;
        const { detail, duration, tone: outcomeTone } = formatOutcome(row.outcome);
        // `background` rides the outcome column, which is where a reader already
        // looks to find out what became of a row. On its own it means the
        // command was detached and has not come back; beside a status it says
        // both what the row was and how it ended. One row, both facts, at every
        // moment of the command's life — which is what the two rows it replaced
        // were each half of.
        const outcome = [row.backgrounded ? "background" : "", detail, duration]
          .filter(Boolean)
          .join(" · ");
        return (
          <LogRow
            key={runEventKey(row.line)}
            text={text}
            tone={toneColor(tone)}
            outcome={{ text: outcome, tone: toneColor(outcomeTone) }}
          />
        );
      })}
    </>
  );
}
