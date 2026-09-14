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
import {
  crewStateLabel,
  crewTone,
  formatDuration,
  type Crew,
  type CrewMember,
  type LaneSpan,
} from "@aep/progress-view";
import { LogNote } from "../../../components/LogSection";
import { toneColor } from "../../../components/logTone";
import type { StampedRunEvent } from "../hooks/useRunProgress";

// WHERE DID THE TIME GO, and what ran at once. One lane per agent on ONE axis —
// the cycle's first word to its last — so concurrency is a shape rather than
// something a reader reconstructs from timestamps.
//
// The lead's lane is split, and that split is the whole point. A lead that took
// 55 minutes reads as the slowest thing in the run until you can see that 41 of
// them are one unbroken stretch of it blocked inside a spawned agent. Solid is
// work it did; faded is time it spent waiting on somebody else's row.
//
// It is never shown beside the crew — the two draw the same agents, and stacking
// them spends the cycle's height twice on one fact. Picking a lane goes BACK to
// the crew with that agent selected, because "where did the time go" always ends
// in "so what was it doing".

/** How wide the label gutter is, in theme spacing steps (28 × 8px). */
const LABEL_WIDTH_STEPS = 28;

function Lane({
  member,
  window: axis,
  onSelect,
}: {
  member: CrewMember<StampedRunEvent>;
  window: { startMs: number; endMs: number };
  onSelect: (id: string) => void;
}) {
  const span = axis.endMs - axis.startMs;
  const pct = (ms: number) => `${String(((ms - axis.startMs) / span) * 100)}%`;
  const width = (s: LaneSpan) => `${String(((s.endMs - s.startMs) / span) * 100)}%`;
  const tone = toneColor(crewTone(member.state));

  return (
    <Box
      component="button"
      type="button"
      onClick={() => {
        onSelect(member.id);
      }}
      // Named the way the crew row names it, so the two views cannot describe
      // one agent two ways to a reader who hears rather than sees them.
      aria-label={`${member.agent.label}: ${crewStateLabel(member.state)}${
        member.elapsedMs === undefined ? "" : `, ${formatDuration(member.elapsedMs)}`
      }`}
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1,
        width: "100%",
        px: 1,
        py: 0.5,
        font: "inherit",
        textAlign: "left",
        color: "inherit",
        background: "none",
        border: 0,
        borderRadius: 1,
        cursor: "pointer",
        "&:hover": { bgcolor: "action.hover" },
      }}
    >
      <Typography
        component="span"
        title={member.agent.label}
        sx={{
          font: "inherit",
          color: "grey.300",
          // A depth-2 agent is a lane like any other; the indent in its LABEL is
          // what says whose it is. Indenting the bar instead would move it on
          // the time axis, which is the one thing the axis must not lie about.
          pl: member.depth * 1.5,
          width: (t) => t.spacing(LABEL_WIDTH_STEPS),
          flexShrink: 0,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {member.agent.label}
      </Typography>
      <Box
        sx={{
          position: "relative",
          flex: 1,
          minWidth: 0,
          height: (t) => t.spacing(1.5),
          borderRadius: 0.5,
          bgcolor: "action.hover",
        }}
      >
        {member.spans.length === 0 ? null : (
          member.spans.map((s) => (
            <Box
              key={`${s.kind}:${String(s.startMs)}`}
              sx={{
                position: "absolute",
                top: 0,
                bottom: 0,
                left: pct(s.startMs),
                width: width(s),
                // A hairline, so an agent that lived for a second is still a
                // mark on the axis rather than nothing. Spelled with a unit
                // deliberately: MUI reads a bare `1` here as the FRACTION 1,
                // i.e. `min-width: 100%`, which made every span the full width
                // of the lane and hid the split this view exists to show.
                minWidth: "2px",
                bgcolor: tone,
                borderRadius: 0.5,
                // Faded is "waiting on another agent". Same hue on purpose: it
                // is still this agent's lane, and a second colour would read as
                // a second state rather than as the same one, idle.
                opacity: s.kind === "waiting" ? 0.28 : 1,
              }}
            />
          ))
        )}
      </Box>
      <Typography
        component="span"
        sx={{
          font: "inherit",
          color: "grey.500",
          flexShrink: 0,
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {member.elapsedMs === undefined ? "" : formatDuration(member.elapsedMs)}
      </Typography>
    </Box>
  );
}

export function CrewTimeline({
  crew,
  onSelect,
}: {
  crew: Crew<StampedRunEvent>;
  /** Picking a lane returns to the crew with that agent selected. */
  onSelect: (id: string) => void;
}) {
  if (crew.window.endMs <= crew.window.startMs) {
    // Nothing timestamped yet — which is a real state at the very start of a
    // cycle, and a stack of zero-width bars would read as a broken chart.
    return <LogNote>Nothing has been timed yet — the crew view has the live picture.</LogNote>;
  }
  return (
    <Box>
      <Box sx={{ display: "flex", alignItems: "baseline", gap: 1, px: 1, pb: 0.5 }}>
        <Typography component="span" sx={{ font: "inherit", color: "grey.500" }}>
          {formatDuration(crew.window.endMs - crew.window.startMs)} across{" "}
          {crew.agents} agent{crew.agents === 1 ? "" : "s"}
        </Typography>
        <Typography
          component="span"
          sx={{ font: "inherit", color: "grey.500", ml: "auto", textAlign: "right" }}
        >
          solid · working &nbsp; faded · waiting on another agent
        </Typography>
      </Box>
      {crew.members.map((member) => (
        <Lane key={member.id} member={member} window={crew.window} onSelect={onSelect} />
      ))}
    </Box>
  );
}
