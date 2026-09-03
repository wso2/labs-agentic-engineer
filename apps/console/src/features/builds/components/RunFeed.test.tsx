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
import type { RunProgressCycle, RunProgressPhase } from "../hooks/useRunProgress";

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

function section(
  id: string,
  kind: string,
  emitters: string[],
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
    lines: emitters.map((emitter, i) => ({
      cycleId: id,
      cycleKind: kind,
      cycleIndex: 1,
      kind: "log",
      emitter: emitter as "main" | "subagent",
      seq: i + 1,
      summary: `${emitter} line ${i + 1}`,
    })),
  };
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
      "No cycle output yet — the run's first agent has not written a line.",
    );
    expect(note.parentElement).toHaveStyle({ textAlign: "center" });
  });

  // The tail is the feed's connection status, not a log line, so it sits on the
  // opposite edge from the content. Asserted because nothing else would catch it
  // drifting back to the left when this block is next edited.
  it("right-aligns the stream status line", () => {
    mockCycles = [section("c1", "coding", ["main"])];
    mockPhase = "reconnecting";
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText(/Connection lost/)).toHaveStyle({
      textAlign: "right",
    });
  });

  it("renders one section per cycle, labelled by kind", () => {
    mockCycles = [section("c1", "coding", ["main"]), section("c2", "fix", ["main"])];
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
    mockCycles = [section("c1", "coding", ["main"]), section("c2", "fix", ["main"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    // The ORDER is the assertion: both labels are present whichever end the newest is
    // drawn at, which is why the tests around this one could not have caught the flip.
    expect(screen.getAllByText(/^Cycle \d+$/).map((el) => el.textContent)).toEqual([
      "Cycle 2",
      "Cycle 1",
    ]);
  });

  it("opens the newest cycle and leaves the earlier ones collapsed", () => {
    mockCycles = [section("c1", "coding", ["main"]), section("c2", "fix", ["main"])];
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
      section("c1", "validation", ["main"]),
      section("c2", "validation", ["main"]),
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
      section("c1", "validation", ["main"]),
      section("c2", "validation", ["main"]),
    ];
    render(<RunFeed projectName="acme" runId="run-1" runNumber={2} />);
    expect(
      screen.getAllByText(/^Run \d+ · Cycle \d+$/).map((el) => el.textContent),
    ).toEqual(["Run 2 · Cycle 2", "Run 2 · Cycle 1"]);
  });

  // The single-feed case: there is nothing to disambiguate, so nothing is prefixed and
  // the heading is exactly what it was before the prop existed.
  it("says nothing about a run when it is given no number", () => {
    mockCycles = [section("c1", "validation", ["main"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.getByText("Cycle 1")).toBeInTheDocument();
    expect(screen.queryByText(/Run \d/)).toBeNull();
  });

  // Two runs' chips open DIFFERENT pull requests, so they have to be tellable apart by
  // name alone — "Cycle 1 pull request" would name both of them.
  it("carries the run into the pull request's accessible name", () => {
    mockCycles = [
      section("c1", "validation", ["main"], {
        number: 41,
        url: "https://github.com/acme/demo/pull/41",
      }),
    ];
    render(<RunFeed projectName="acme" runId="run-1" runNumber={2} />);
    expect(
      screen.getByRole("link", { name: "Run 2 · Cycle 1 pull request #41" }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/41");
  });

  // The stream keeps moving which cycle is newest, but a reader reading an earlier
  // one must not have it yanked shut underneath them.
  it("lets the reader open an earlier cycle instead of the newest", () => {
    mockCycles = [section("c1", "coding", ["main"]), section("c2", "fix", ["main"])];
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
    mockCycles = [section("c1", "coding", ["main"]), section("c2", "fix", ["main"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    fireEvent.click(screen.getByRole("button", { name: /Cycle 2/ }));
    for (const summary of screen.getAllByRole("button", { name: /Cycle \d/ })) {
      expect(summary).toHaveAttribute("aria-expanded", "false");
    }
  });

  it("stamps a subagent line and leaves the main agent's unstamped", () => {
    mockCycles = [section("c1", "coding", ["main", "subagent"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    // Exactly one chip: absence of a stamp is the positive fact "main agent".
    expect(screen.getAllByText("subagent")).toHaveLength(1);
  });

  it("gives each subagent its own section, and keeps the main agent's lines loose", () => {
    // A cycle fans out to several subagents at once and their lines arrive
    // INTERLEAVED — read flat, three components' work reads as one agent
    // contradicting itself.
    mockCycles = [
      {
        cycle: { id: "c1", kind: "coding" as never, attempts: 1, createdAt: "2026-07-10T09:00:00Z" },
        lines: [
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "log", emitter: "main", seq: 1, summary: "planning" },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "log", emitter: "subagent", emitterId: "a1", emitterLabel: "Implement todo-api", seq: 2, summary: "bal build" },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "log", emitter: "subagent", emitterId: "a2", emitterLabel: "Implement todo-webapp", seq: 3, summary: "npm install" },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "log", emitter: "subagent", emitterId: "a1", emitterLabel: "Implement todo-api", seq: 4, summary: "bal test" },
        ],
      },
    ];
    render(<RunFeed projectName="acme" runId="run-1" />);

    // One section per subagent, named — not one per contiguous stretch, so the
    // two todo-api lines land together despite todo-webapp interleaving.
    expect(screen.getByText("Implement todo-api")).toBeInTheDocument();
    expect(screen.getByText("Implement todo-webapp")).toBeInTheDocument();
    // Collapsed, the header is ALL a reader gets about a subagent, so it carries
    // the verdict rather than a line count. Neither has settled here.
    expect(screen.getAllByText("running")).toHaveLength(2);

    // Sections are open by default: a progress feed that hides its work behind
    // a click reads as a run doing nothing.
    expect(screen.getByText(/bal build/)).toBeInTheDocument();
    expect(screen.getByText(/bal test/)).toBeInTheDocument();

    // The main agent's line is NOT swept into a section.
    expect(screen.getByText(/planning/)).toBeInTheDocument();
  });

  it("a settled subagent reports the SDK's own figures, and a dead one reads as failed", () => {
    mockCycles = [
      {
        cycle: { id: "c1", kind: "coding" as never, attempts: 1, createdAt: "2026-07-10T09:00:00Z" },
        lines: [
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "tool_use", tool: "Write", summary: "todo-api/service.bal", toolUseId: "s1", emitter: "subagent", emitterId: "a1", emitterLabel: "todo-api", seq: 1 },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "tool_result", tool: "Agent", ok: true, status: "completed", summary: "todo-api", durationMs: 209158, toolCount: 19, linesAdded: 553, linesRemoved: 4, toolUseId: "a1", emitter: "subagent", emitterId: "a1", seq: 2 },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "tool_result", tool: "Agent", ok: false, status: "error_during_execution", summary: "todo-webapp", durationMs: 353000, toolCount: 31, toolUseId: "a2", emitter: "subagent", emitterId: "a2", emitterLabel: "todo-webapp", seq: 3 },
        ],
      },
    ];
    render(<RunFeed projectName="acme" runId="run-1" />);

    // Every figure is the SDK's own — the audit signal is how much code it made.
    expect(screen.getByText("completed \u00b7 3m29s \u00b7 19 tools \u00b7 +553/\u22124 lines")).toBeInTheDocument();
    // A subagent that died reads as a failure, not as merely going quiet.
    expect(screen.getByText("error_during_execution \u00b7 5m53s \u00b7 31 tools")).toBeInTheDocument();
    // Its closing report is the header, so it is NOT also a row in the section.
    expect(screen.queryByText(/\u25aa/)).not.toBeInTheDocument();
  });

  it("attaches a step's outcome to its own action row, not to a second row", () => {
    mockCycles = [
      {
        cycle: { id: "c1", kind: "coding" as never, attempts: 1, createdAt: "2026-07-10T09:00:00Z" },
        lines: [
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, emitter: "main" as const, kind: "tool_use", tool: "Bash", summary: "bal build", toolUseId: "t1", seq: 1 },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, emitter: "main" as const, kind: "tool_use", tool: "Read", summary: "db.bal", toolUseId: "t2", seq: 2 },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, emitter: "main" as const, kind: "tool_result", tool: "Read", ok: true, durationMs: 20, toolUseId: "t2", seq: 3 },
          { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, emitter: "main" as const, kind: "tool_result", tool: "Bash", ok: false, exitCode: 1, summary: "error: compilation contains errors", durationMs: 25100, toolUseId: "t1", seq: 4 },
        ],
      },
    ];
    render(<RunFeed projectName="acme" runId="run-1" />);

    // The action keeps its row; the outcome trails on it rather than repeating
    // the command a second time further down.
    expect(screen.getByText("$ bal build")).toBeInTheDocument();
    expect(screen.getByText("exit 1 \u00b7 error: compilation contains errors \u00b7 25.1s")).toBeInTheDocument();
    expect(screen.queryByText(/\u2717 Bash/)).not.toBeInTheDocument();
    // A fast success adds nothing — its action row stands alone.
    expect(screen.getByText("$ Read db.bal")).toBeInTheDocument();
  });

  it("filters to the cycle kinds a surface owns", () => {
    mockCycles = [
      section("c1", "coding", ["main"]),
      section("c2", "validation", ["main"]),
    ];
    render(
      <RunFeed projectName="acme" runId="run-1" cycleKinds={["validation"]} />,
    );
    expect(screen.getByText("validation")).toBeInTheDocument();
    expect(screen.queryByText("coding")).not.toBeInTheDocument();
  });

  // How the run ENDED moved to the section header (see BuildDetailPage), so the
  // feed body must stay quiet about it — a settled stream repeating the header
  // beneath the log is the duplication that move was meant to remove.
  it("leaves the run's ending to the section header", () => {
    mockCycles = [section("c1", "coding", ["main"])];
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
      section("c1", "validation", ["main"], {
        number: 41,
        url: "https://github.com/acme/demo/pull/41",
      }),
      section("c2", "validation", ["main"], {
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
    mockCycles = [section("c1", "validation", ["main"])];
    render(<RunFeed projectName="acme" runId="run-1" />);
    expect(screen.queryByRole("link", { name: /pull request/ })).toBeNull();
  });

  // The link sits inside the summary, whose whole surface toggles the section.
  // Without stopPropagation, opening the pull request would collapse the log the
  // reader was looking at.
  it("opens a cycle's pull request without collapsing its log", () => {
    mockCycles = [
      section("c1", "validation", ["main"], {
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
