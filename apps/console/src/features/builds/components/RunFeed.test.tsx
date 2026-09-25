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

// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  RunProgressCycle,
  RunProgressPhase,
  StampedRunEvent,
} from "../hooks/useRunProgress";

let mockCycles: RunProgressCycle[] = [];
let mockPhase: RunProgressPhase = "live";
let mockSettled: string | undefined;

vi.mock("../hooks/useRunProgress", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../hooks/useRunProgress")>();
  return {
    ...actual,
    useRunProgress: () => ({
      cycles: mockCycles,
      phase: mockPhase,
      settledState: mockSettled,
    }),
  };
});

import { RunFeed } from "./RunFeed";

/** One v2 event, stamped with the cycle and attempt aep-api relays it under. */
const ev = (
  cycleId: string,
  seq: number,
  rest: Partial<StampedRunEvent> & { kind: string; agentId: string },
): StampedRunEvent =>
  ({
    v: 2,
    ts: "2026-07-10T09:01:00Z",
    cycleId,
    attempt: 1,
    seq,
    ...rest,
  }) as StampedRunEvent;

function cycleOf(
  id: string,
  kind: string,
  events: StampedRunEvent[],
  pr?: { number: number; url: string },
): RunProgressCycle {
  return {
    cycle: {
      id,
      kind: kind as never,
      attempts: 1,
      createdAt: "2026-07-10T09:00:00Z",
      ...(pr ? { prNumber: pr.number, prUrl: pr.url } : {}),
    },
    events,
  };
}

/** A cycle with one plain lead step, for the tests that are about the boxes. */
function section(
  id: string,
  kind: string,
  agents: string[],
  pr?: { number: number; url: string },
): RunProgressCycle {
  return cycleOf(
    id,
    kind,
    agents.map((agentId, i) =>
      ev(id, i + 1, { kind: "tool_use", agentId, tool: "Bash", summary: `${agentId} step ${String(i + 1)}` }),
    ),
    pr,
  );
}

afterEach(() => {
  mockCycles = [];
  mockPhase = "live";
  mockSettled = undefined;
});

describe("RunFeed", () => {
  // The placeholder is drawn by the SHARED `EmptyState`, the same one the Build
  // logs section one card below uses. It used to be a bare left-aligned
  // paragraph, which put two differently-drawn placeholders side by side on one
  // page. `textAlign: center` is the tell that this is the shared component and
  // not a paragraph that happens to say the same words.
  it("draws its empty state the way every other section on the page does", () => {
    mockCycles = [];
    render(<RunFeed projectName="acme" runId="run-1" />);
    const note = screen.getByText(
      "No output yet — the run's first agent has not written a line.",
    );
    expect(note.parentElement).toHaveStyle({ textAlign: "center" });
  });

  // The tail is the feed's connection status, not a log line, so it sits on the
  // opposite edge from the content. Asserted because nothing else would catch it
  // drifting back to the left when this block is next edited.
  it("right-aligns the stream status line", () => {
    mockCycles = [section("c1", "coding", ["lead"])];
    mockPhase = "reconnecting";
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText(/Connection lost/)).toHaveStyle({
      textAlign: "right",
    });
  });

  it("renders one section per cycle, labelled by kind", () => {
    mockCycles = [section("c1", "coding", ["lead"]), section("c2", "fix", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText("Cycle 1")).toBeInTheDocument();
    expect(screen.getByText("Cycle 2")).toBeInTheDocument();
    expect(screen.getByText("coding")).toBeInTheDocument();
    expect(screen.getByText("fix")).toBeInTheDocument();
  });

  // The newest cycle LEADS. It is the one still being written, and a reader who opened
  // the feed should not scroll past however much history the run accumulated to reach
  // it. The numbers still count from the OLDEST, so they run down the page — that is
  // what keeps a box's name stable when the render order flips.
  it("renders the newest cycle first, numbered from the oldest", () => {
    mockCycles = [section("c1", "coding", ["lead"]), section("c2", "fix", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    // The ORDER is the assertion: both labels are present whichever end the newest is
    // drawn at, which is why the tests around this one could not have caught the flip.
    expect(screen.getAllByText(/^Cycle \d+$/).map((el) => el.textContent)).toEqual([
      "Cycle 2",
      "Cycle 1",
    ]);
  });

  it("opens the newest cycle and leaves the earlier ones collapsed", () => {
    mockCycles = [section("c1", "coding", ["lead"]), section("c2", "fix", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByRole("button", { name: /Cycle 2/ })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("button", { name: /Cycle 1/ })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  // A surface showing one feed per run would otherwise open one box per feed. Only the
  // newest run's feed may open its newest cycle; every earlier attempt is a record.
  it("opens nothing when it is not the newest feed on the page", () => {
    mockCycles = [
      section("c1", "validation", ["lead"]),
      section("c2", "validation", ["lead"]),
    ];
    render(<RunFeed projectName="acme" runId="run-1" expandNewest={false} />);
    const summaries = screen.getAllByRole("button", { name: /Cycle \d/ });
    expect(summaries).toHaveLength(2);
    for (const summary of summaries) {
      expect(summary).toHaveAttribute("aria-expanded", "false");
    }
  });

  // Each feed numbers its own cycles from 1, so the run is what tells two "Cycle 1"s
  // apart when a version was validated by more than one run.
  it("prefixes the run when it is given one", () => {
    mockCycles = [
      section("c1", "validation", ["lead"]),
      section("c2", "validation", ["lead"]),
    ];
    render(<RunFeed projectName="acme" runId="run-1" runNumber={2} />);
    expect(
      screen.getAllByText(/^Run \d+ · Cycle \d+$/).map((el) => el.textContent),
    ).toEqual(["Run 2 · Cycle 2", "Run 2 · Cycle 1"]);
  });

  // The single-feed case: there is nothing to disambiguate, so nothing is prefixed and
  // the heading is exactly what it was before the prop existed.
  it("says nothing about a run when it is given no number", () => {
    mockCycles = [section("c1", "validation", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText("Cycle 1")).toBeInTheDocument();
    expect(screen.queryByText(/Run \d/)).toBeNull();
  });

  // Two runs' chips open DIFFERENT pull requests, so they have to be tellable apart by
  // name alone — "Cycle 1 pull request" would name both of them.
  it("carries the run into the pull request's accessible name", () => {
    mockCycles = [
      section("c1", "validation", ["lead"], {
        number: 41,
        url: "https://github.com/acme/demo/pull/41",
      }),
    ];
    render(<RunFeed projectName="acme" runId="run-1" runNumber={2} />);
    expect(
      screen.getByRole("link", { name: "Run 2 · Cycle 1 pull request #41" }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/41");
  });

  // A surface whose unit is not the cycle heads the boxes itself. The validation
  // page calls a box an attempt on the version, numbered across every run, so it
  // hands the feed a label and no run number; the feed still supplies the
  // chronological position, so the surface's numbers descend with the stack.
  it("heads its sections with the surface's own label when given one", () => {
    mockCycles = [
      section("c1", "validation", ["lead"]),
      section("c2", "validation", ["lead"], {
        number: 41,
        url: "https://github.com/acme/demo/pull/41",
      }),
    ];
    render(
      <RunFeed
        projectName="acme"
        runId="run-1"
        cycleKinds={["validation"]}
        label={(ordinal) => `Attempt ${String(2 + ordinal)}`}
      />,
    );
    expect(screen.getAllByText(/^Attempt \d+$/).map((el) => el.textContent)).toEqual([
      "Attempt 4",
      "Attempt 3",
    ]);
    expect(screen.queryByText(/Cycle/)).toBeNull();
    // The pull request's accessible name follows the heading, as always.
    expect(
      screen.getByRole("link", { name: "Attempt 4 pull request #41" }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/41");
  });

  // The kind chip tells cycles apart. A feed filtered to one kind would stamp the
  // same word on every row, so it says nothing there; an unfiltered feed, and a
  // builds feed showing three kinds, keep it.
  it("drops the kind chip when the feed shows a single kind", () => {
    mockCycles = [section("c1", "validation", ["lead"])];
    const { unmount } = render(
      <RunFeed projectName="acme" runId="run-1" cycleKinds={["validation"]} />,
    );
    expect(screen.queryByText("validation")).toBeNull();
    unmount();

    mockCycles = [section("c1", "coding", ["lead"])];
    render(
      <RunFeed projectName="acme" runId="run-1" cycleKinds={["coding", "fix", "conflict"]} />,
    );
    expect(screen.getByText("coding")).toBeInTheDocument();
  });

  // Dispatches of one cycle — an agent that died was started again for the same
  // work. Said as "started", never "attempts": the validation page calls a box
  // an attempt, and the two would otherwise share a row meaning different things.
  it("says how many times a re-dispatched cycle was started", () => {
    const twice = section("c1", "coding", ["lead"]);
    mockCycles = [{ ...twice, cycle: { ...twice.cycle, attempts: 2 } }];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText("started 2 times")).toBeInTheDocument();
    expect(screen.queryByText(/attempts/)).toBeNull();
  });

  // The stream keeps moving which cycle is newest, but a reader reading an earlier
  // one must not have it yanked shut underneath them.
  it("lets the reader open an earlier cycle instead of the newest", () => {
    mockCycles = [section("c1", "coding", ["lead"]), section("c2", "fix", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    fireEvent.click(screen.getByRole("button", { name: /Cycle 1/ }));
    expect(screen.getByRole("button", { name: /Cycle 1/ })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByRole("button", { name: /Cycle 2/ })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  // Closing the open one leaves the feed closed rather than snapping back to the
  // newest, which is what a naive "follow the newest" default re-derives.
  it("stays closed when the reader shuts the open cycle", () => {
    mockCycles = [section("c1", "coding", ["lead"]), section("c2", "fix", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    fireEvent.click(screen.getByRole("button", { name: /Cycle 2/ }));
    for (const summary of screen.getAllByRole("button", { name: /Cycle \d/ })) {
      expect(summary).toHaveAttribute("aria-expanded", "false");
    }
  });

  // The agent-shape tests live in RunCrew.test.tsx: inside a cycle the events
  // are a CREW now, and this file is about the accordion boxes holding them.
  it("filters to the cycle kinds a surface owns", () => {
    mockCycles = [
      section("c1", "coding", ["lead"]),
      section("c2", "validation", ["lead"]),
    ];
    render(
      <RunFeed projectName="acme" runId="run-1" cycleKinds={["validation"]} />,
    );
    // One box, and it is the validation cycle's: the coding one is not drawn, and
    // the ordinal counts within what is shown.
    expect(screen.getAllByRole("button", { name: /Cycle \d/ })).toHaveLength(1);
    expect(screen.getByRole("button", { name: /Cycle 1/ })).toBeInTheDocument();
    expect(screen.queryByText("coding")).not.toBeInTheDocument();
  });

  // How the run ENDED moved to the section header (see BuildDetailPage), so the
  // feed body must stay quiet about it — a settled stream repeating the header
  // beneath the log is the duplication that move was meant to remove.
  it("leaves the run's ending to the section header", () => {
    mockCycles = [section("c1", "coding", ["lead"])];
    mockPhase = "ended";
    mockSettled = "succeeded";
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.queryByText(/Run finished/)).not.toBeInTheDocument();
    expect(screen.queryByText(/settled/)).not.toBeInTheDocument();
  });

  it("says it is reattaching after a dropped connection", () => {
    mockPhase = "reconnecting";
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText(/reconnecting/)).toBeInTheDocument();
  });

  // Per CYCLE, not per run: a run holds several and each opens its own pull
  // request, so one run-level link would reach only the last of them.
  it("links each cycle to the pull request that cycle produced", () => {
    mockCycles = [
      section("c1", "validation", ["lead"], {
        number: 41,
        url: "https://github.com/acme/demo/pull/41",
      }),
      section("c2", "validation", ["lead"], {
        number: 47,
        url: "https://github.com/acme/demo/pull/47",
      }),
    ];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(
      screen.getByRole("link", { name: /Cycle 1 pull request #41/ }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/41");
    expect(
      screen.getByRole("link", { name: /Cycle 2 pull request #47/ }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/47");
  });

  it("shows no pull request link for a cycle that has not opened one", () => {
    mockCycles = [section("c1", "validation", ["lead"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.queryByRole("link", { name: /pull request/ })).toBeNull();
  });

  // The link sits inside the summary, whose whole surface toggles the section.
  // Without stopPropagation, opening the pull request would collapse the log the
  // reader was looking at.
  it("opens a cycle's pull request without collapsing its log", () => {
    mockCycles = [
      section("c1", "validation", ["lead"], {
        number: 41,
        url: "https://github.com/acme/demo/pull/41",
      }),
    ];
    render(<RunFeed projectName="acme" runId="run-1" />);
    const summary = screen.getByRole("button", { name: /Cycle 1/ });
    // The newest cycle opens expanded — that is the state the click must not change.
    expect(summary).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(screen.getByRole("link", { name: /Cycle 1 pull request #41/ }));
    expect(summary).toHaveAttribute("aria-expanded", "true");
  });
});
