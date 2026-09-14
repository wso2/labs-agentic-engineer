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

// The timeline's lanes, MEASURED.
//
// This is in the browser lane because there is nothing here jsdom can answer. A
// lane is a flex track of unknown pixel width holding absolutely positioned
// spans sized in PERCENT, so every claim the view makes — that a span sits
// inside its own lane, that the split is visible at all, that a one-second agent
// is still a mark — is a layout computation. A unit test could only re-read the
// `sx` object it is meant to be checking.
//
// It exists because both of the timeline's real defects were exactly that shape
// and both looked correct in the source. `minWidth: 1` reached CSS as
// `min-width: 100%` — MUI reads a bare 1 as the fraction 1 — so every span was
// the full width of its lane and the working/waiting split was invisible. A
// `width` function nested inside a `{ md: … }` breakpoint map was dropped
// silently, so the tree column had no width and squeezed the inspector off the
// side. Neither is catchable without a layout engine.

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { buildCrew } from "@aep/progress-view";
import { CrewTimeline } from "./CrewTimeline";
import type { StampedRunEvent } from "../hooks/useRunProgress";

const T0 = Date.parse("2026-09-04T09:00:00Z");
const at = (seconds: number) => new Date(T0 + seconds * 1000).toISOString();

let seq = 0;
const ev = (
  seconds: number,
  rest: Partial<StampedRunEvent> & { kind: string; agentId: string },
): StampedRunEvent =>
  ({ v: 2, cycleId: "c1", attempt: 1, seq: ++seq, ts: at(seconds), ...rest }) as StampedRunEvent;

/**
 * A lead that spends most of a 100-second cycle blocked inside one agent, and a
 * second agent that lives for a single second.
 */
function events(): StampedRunEvent[] {
  seq = 0;
  return [
    ev(0, { kind: "run_started", agentId: "lead", taskKind: "implementation" }),
    ev(10, { kind: "agent_started", agentId: "a1", label: "long", depth: 1, background: false }),
    ev(90, { kind: "agent_settled", agentId: "a1", status: "completed", report: "done" }),
    ev(95, { kind: "agent_started", agentId: "a2", label: "brief", depth: 1, background: true }),
    ev(96, { kind: "agent_settled", agentId: "a2", status: "completed", report: "done" }),
    ev(100, { kind: "run_settled", agentId: "lead", outcome: "success" }),
  ];
}

const LANE_PX = 900;

function renderTimeline() {
  const crew = buildCrew(events(), T0 + 100_000);
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div style={{ width: `${String(LANE_PX)}px`, fontFamily: "monospace", fontSize: "13px" }}>
        <CrewTimeline crew={crew} onSelect={() => {}} />
      </div>
    </OxygenUIThemeProvider>,
  );
}

/** One lane's track and the spans drawn in it, as boxes on the screen. */
function lane(label: string) {
  const button = screen.getByRole("button", { name: new RegExp(`^${label}:`) });
  const track = button.children[1];
  if (!(track instanceof HTMLElement)) throw new Error(`no track in the ${label} lane`);
  return {
    track: track.getBoundingClientRect(),
    spans: [...track.children].map((span) => ({
      box: span.getBoundingClientRect(),
      faded: Number(getComputedStyle(span).opacity) < 1,
    })),
  };
}

afterEach(cleanup);

describe("a timeline lane", () => {
  it("keeps every span inside its own track", () => {
    renderTimeline();
    for (const label of ["lead agent", "long", "brief"]) {
      const { track, spans } = lane(label);
      expect(spans.length).toBeGreaterThan(0);
      for (const { box } of spans) {
        // A span wider than its track puts one agent's time on the row beside
        // it, in the one view whose whole job is to say who spent what.
        expect(box.left).toBeGreaterThanOrEqual(Math.floor(track.left));
        expect(box.right).toBeLessThanOrEqual(Math.ceil(track.right));
      }
    }
  });

  it("draws the lead's wait as a faded stretch between two solid ones", () => {
    renderTimeline();
    const { track, spans } = lane("lead agent");
    expect(spans.map((s) => s.faded)).toEqual([false, true, false]);
    // The wait is 80 of the cycle's 100 seconds, so it has to LOOK like most of
    // the lane — the whole point being that the lead was not the slow part.
    const waiting = spans[1]?.box.width ?? 0;
    expect(waiting / track.width).toBeGreaterThan(0.7);
    expect(waiting / track.width).toBeLessThan(0.9);
    // Contiguous: gaps between the stretches would read as time nobody owns.
    expect(spans[1]?.box.left).toBeCloseTo(spans[0]?.box.right ?? 0, 0);
    expect(spans[2]?.box.left).toBeCloseTo(spans[1]?.box.right ?? 0, 0);
  });

  it("still marks an agent that lived for a second", () => {
    renderTimeline();
    const { track, spans } = lane("brief");
    const only = spans[0]?.box.width ?? 0;
    // One second of a hundred is 9px on this track and would round to nothing
    // without the hairline minimum; an agent that ran and left no mark is an
    // agent a reader never learns about.
    expect(only).toBeGreaterThan(0);
    expect(only / track.width).toBeLessThan(0.05);
  });
});
