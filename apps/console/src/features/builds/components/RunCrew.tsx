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
import { Box, Stack, ToggleButton, ToggleButtonGroup, Typography } from "@wso2/oxygen-ui";
import { buildCrew, formatDuration } from "@aep/progress-view";
import { LogNote, LogSurface } from "../../../components/LogSection";
import { CrewInspector } from "./CrewInspector";
import { CrewTimeline } from "./CrewTimeline";
import { CrewTree } from "./CrewTree";
import { useRunView } from "../hooks/useRunView";
import { useTicker } from "../hooks/useTicker";
import type { StampedRunEvent } from "../hooks/useRunProgress";

// ONE cycle's agents, under one governing rule: a reader must never wonder
// whether the run is stuck.
//
// The crew answers "who is doing what now" and the timeline answers "where did
// the time go". They are never shown at once — both draw the same agents, and
// stacking them spends the cycle's height twice on one fact. The reader switches
// when the question changes, and the choice is remembered per browser.
//
// Crew is the default because liveness is the primary job. The timeline is a
// retrospective, and a retrospective is the wrong thing to open on a run that is
// still going.

/**
 * The one fact both views share, which is why it sits with the toggle rather
 * than inside either of them.
 *
 * The age is what turns "quiet" into a number. It is shown only while something
 * is still running: on a settled cycle it would count how long ago the build
 * was, which the page header already says, and it would keep ticking about a
 * run nobody is waiting on.
 */
function CrewHint({
  agents,
  running,
  silentForMs,
}: {
  agents: number;
  running: number;
  silentForMs: number | undefined;
}) {
  const parts = [`${String(agents)} agent${agents === 1 ? "" : "s"}`];
  if (running > 0) {
    parts.push(`${String(running)} running`);
    if (silentForMs !== undefined) parts.push(`last event ${formatDuration(silentForMs)} ago`);
  } else {
    parts.push("all settled");
  }
  return (
    <Typography variant="caption" color="text.secondary">
      {parts.join(" · ")}
    </Typography>
  );
}

/**
 * One cycle's crew, with the timeline behind a toggle.
 *
 * The clock lives HERE and nowhere lower: `buildCrew` is pure and takes `now` as
 * an argument, so this is the single place a rendered age can come from. The
 * ticker is what makes it honest — react-query's structural sharing and an SSE
 * stream that has gone quiet both leave the tree with no reason to re-render,
 * and an age frozen at whatever it read on first paint is the precise lie this
 * view exists to stop telling.
 */
export function RunCrew({ events }: { events: StampedRunEvent[] }) {
  const [view, setView] = useRunView();
  const [chosenId, setChosenId] = useState<string>();

  // Rebuilt every render rather than memoised: the clock is an INPUT, so a memo
  // keyed on the events would return a crew whose ages stopped moving — which is
  // exactly the bug. The model is a fold over one cycle's events; the 761-event
  // recording folds in well under a millisecond.
  const crew = buildCrew(events, Date.now());
  // Only while somebody is still working. A settled cycle's rows do not move, so
  // a timer on one is a re-render a second for no reader.
  useTicker(crew.running > 0);

  if (events.length === 0) {
    return (
      <LogSurface>
        <LogNote>No output from this cycle yet.</LogNote>
      </LogSurface>
    );
  }

  // The lead is the fallback, not a stored default: a selection can be for an
  // agent that has not arrived yet on a reconnecting stream, and a blank
  // inspector beside a full tree reads as a broken page.
  const selected = crew.members.find((m) => m.id === chosenId) ?? crew.lead;

  // THE CREW IS THERE FROM THE FIRST AGENT.
  //
  // A cycle that fans out starts as one agent, so a layout that only becomes a
  // tree once a second one arrives RESHAPES ITSELF mid-run — the very moment a
  // reader is watching it most closely. A tree of one row costs a little
  // chrome; a page that rearranges costs the reader the surface they had just
  // learned. So one agent gets the same tree and the same inspector a fanned-out
  // cycle gets, and a spawned agent then appears IN PLACE, as a row under the
  // lead, with nothing else moving.
  //
  // The TIMELINE is the exception, and it is not chrome-avoidance: a timeline
  // compares lanes, and one lane compares nothing — it would draw a single bar
  // spanning the whole cycle, which the hint beside it already says in words.
  // The toggle therefore appears with the second agent. That adds a control
  // rather than moving anything already on screen, which is the rule above.
  const fannedOut = crew.agents > 1;
  // The choice is remembered per browser, so a reader who left the timeline on
  // can arrive at a single-agent cycle: the crew is what such a cycle draws,
  // and their choice still stands for the next one that fanned out.
  const showTimeline = fannedOut && view === "timeline";

  const show = (id: string) => {
    setChosenId(id);
    // Picking a lane goes BACK to the crew: "where did the time go" always ends
    // in "so what was it doing", and that is the crew's question.
    setView("crew");
  };

  return (
    <Box>
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ alignItems: "center", justifyContent: "flex-end", mb: 1 }}
      >
        <CrewHint agents={crew.agents} running={crew.running} silentForMs={crew.silentForMs} />
        {fannedOut && (
          <ToggleButtonGroup
            exclusive
            size="small"
            value={view}
            onChange={(_, next: string | null) => {
              // Null is the reader clicking the button that is already on. A view
              // is not a filter — there is no "neither", so the choice stands.
              if (next === "crew" || next === "timeline") setView(next);
            }}
            aria-label="How to read this cycle"
          >
            <ToggleButton value="crew">Crew</ToggleButton>
            <ToggleButton value="timeline">Timeline</ToggleButton>
          </ToggleButtonGroup>
        )}
      </Stack>

      <LogSurface maxHeight="none">
        {showTimeline ? (
          <CrewTimeline crew={crew} onSelect={show} />
        ) : (
          <Stack
            direction={{ xs: "column", md: "row" }}
            spacing={2}
            sx={{ alignItems: "stretch" }}
          >
            {/* The tree is the narrow column: its rows are one line of name and
                one of state, while the inspector holds whole commands. */}
            <Box
              sx={(t) => ({
                // One object from the theme rather than a function nested inside
                // a breakpoint map: sx resolves a function at the top level only,
                // and a function inside `{ md: … }` is dropped silently — which
                // left the tree unconstrained and the inspector squeezed off the
                // side of the surface.
                width: { xs: "100%", md: t.spacing(44) },
                flexShrink: 0,
                maxHeight: t.spacing(50),
                overflowY: "auto",
              })}
            >
              <CrewTree members={crew.members} selectedId={selected.id} onSelect={setChosenId} />
            </Box>
            <Box
              sx={(t) => ({
                flex: 1,
                minWidth: 0,
                maxHeight: t.spacing(50),
                overflowY: "auto",
                borderLeft: { xs: 0, md: 1 },
                borderColor: "grey.800",
                pl: { xs: 0, md: 2 },
              })}
            >
              {/* Picking a dispatched agent moves the inspector to it — the
                  same selection the tree drives, so the two cannot disagree
                  about which agent is being read. */}
              <CrewInspector member={selected} onSelect={setChosenId} />
            </Box>
          </Stack>
        )}
      </LogSurface>
    </Box>
  );
}
