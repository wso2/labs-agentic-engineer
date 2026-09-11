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

// The crew view: the surface half of the rule this feature exists for — a reader
// must never wonder whether the run is stuck.
//
// What the MODEL decides (states, ages, lane spans, the amber threshold) is
// tested in `@aep/progress-view` against two real recordings. What is tested
// here is only what this surface adds: which of the two views is on screen, that
// the other one is NOT, what a row shows, and where a click goes.

import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RunCrew } from "./RunCrew";
import { resetRunViewForTest } from "../hooks/useRunView";
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
 * A fan-out with every shape the tree has to draw: a lead, a foreground child
 * that settles, a BACKGROUND child that fails, and a depth-2 grandchild.
 */
function fanOut(): StampedRunEvent[] {
  seq = 0;
  return [
    ev(0, { kind: "run_started", agentId: "lead", taskKind: "implementation" }),
    ev(1, { kind: "tool_use", agentId: "lead", tool: "Bash", summary: "git status", toolUseId: "m1" }),
    ev(2, { kind: "tool_result", agentId: "lead", tool: "Bash", ok: true, durationMs: 240, toolUseId: "m1" }),
    ev(3, { kind: "agent_started", agentId: "a1", label: "Implement todo-api", role: "coder", depth: 1, background: false }),
    ev(4, { kind: "agent_started", agentId: "a2", label: "Implement todo-webapp", role: "coder", depth: 1, background: true }),
    ev(5, { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal build", toolUseId: "t1" }),
    ev(6, { kind: "agent_started", agentId: "a3", label: "Write the OpenAPI contract", parentAgentId: "a1", depth: 2 }),
    ev(7, { kind: "tool_use", agentId: "a3", tool: "Write", summary: "contracts/api.yaml", toolUseId: "c1" }),
    ev(8, { kind: "tool_result", agentId: "a3", tool: "Write", ok: true, durationMs: 70, toolUseId: "c1" }),
    ev(9, { kind: "agent_settled", agentId: "a3", status: "completed", durationMs: 41_200, toolCount: 6, report: "Wrote the contract." }),
    ev(30, { kind: "tool_result", agentId: "a1", tool: "Bash", ok: false, exitCode: 1, summary: "error: compilation contains errors", durationMs: 25_100, toolUseId: "t1" }),
    ev(31, { kind: "agent_settled", agentId: "a1", status: "completed", durationMs: 209_158, toolCount: 19, linesAdded: 553, linesRemoved: 4, report: "Implemented the service and its smoke test." }),
    ev(40, { kind: "agent_settled", agentId: "a2", status: "failed", durationMs: 353_000, toolCount: 31, report: "Could not get vite build to pass." }),
    ev(41, { kind: "run_settled", agentId: "lead", outcome: "success" }),
  ];
}

/** The tree row for an agent, found by the accessible name the row carries. */
function row(label: string): HTMLElement {
  return screen.getByRole("button", { name: new RegExp(`^${label}:`) });
}

/**
 * A plan row, by its title and by WHICH surface drew it.
 *
 * A plan appears twice on a fan-out — once under its owner in the tree, once in
 * the inspector for the agent the reader picked — so a query that did not say
 * which would pass on either. `where` picks by the tree's list element: the tree
 * is a `<ul>` of rows and the inspector is not.
 *
 * In the tree a plan row is a SIBLING of the owning agent's button rather than a
 * child of it, the same way a backgrounded command's row is, so where it lands
 * in the DOM is the proof it sits under the right agent.
 */
function planRow(title: string, where: "tree" | "inspector" = "tree"): HTMLElement {
  const rows = screen
    .getAllByText(title)
    .map((el) => el.parentElement)
    .filter((el): el is HTMLElement => el !== null)
    .filter((el) => (where === "tree" ? el.closest("ul") !== null : el.closest("ul") === null));
  if (rows.length !== 1) {
    throw new Error(`expected one ${where} plan row titled ${title}, found ${String(rows.length)}`);
  }
  return rows[0]!;
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  // The crew's clock is `Date.now()`, so every rendered age and state is decided
  // here rather than by how long the test took to run.
  vi.setSystemTime(T0 + 60_000);
  localStorage.clear();
  // The choice is ONE value for the page, so a test that switched the view
  // would otherwise hand its choice to the next test.
  resetRunViewForTest();
});

afterEach(() => {
  vi.useRealTimers();
  localStorage.clear();
});

describe("RunCrew", () => {
  // Crew is the default because liveness is the primary job: a timeline is a
  // retrospective, and a retrospective is the wrong thing to open on a run that
  // is still going.
  it("opens on the crew, and never draws both views at once", () => {
    render(<RunCrew events={fanOut()} />);
    expect(screen.getByRole("button", { name: "Crew" })).toHaveAttribute("aria-pressed", "true");
    // The inspector — crew only. Its presence is the proof the timeline is not
    // also on screen, since the two never share the height.
    expect(screen.getByText("$ git status")).toBeInTheDocument();
    expect(screen.queryByText(/solid · working/)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    expect(screen.getByText(/solid · working/)).toBeInTheDocument();
    expect(screen.queryByText("$ git status")).toBeNull();
  });

  it("remembers the reader's choice per browser", () => {
    const { unmount } = render(<RunCrew events={fanOut()} />);
    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    expect(localStorage.getItem("aep:builds:run-view")).toBe("timeline");
    unmount();

    // A reader who came to ask where the time went is usually about to ask it of
    // the next cycle too. A fresh page reads the choice back off storage.
    resetRunViewForTest();
    render(<RunCrew events={fanOut()} />);
    expect(screen.getByRole("button", { name: "Timeline" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText(/solid · working/)).toBeInTheDocument();
  });

  // A run holds several cycles and each draws its own toggle. Held as component
  // state, switching one left the others in the view they mounted with — so one
  // page showed a crew and a timeline at once, which is the exact thing this
  // toggle exists to prevent.
  it("switches every cycle on the page at once", () => {
    render(
      <>
        <RunCrew events={fanOut()} />
        <RunCrew events={fanOut()} />
      </>,
    );
    expect(screen.getAllByRole("button", { name: "Crew" })).toHaveLength(2);
    fireEvent.click(screen.getAllByRole("button", { name: "Timeline" })[0]!);
    for (const toggle of screen.getAllByRole("button", { name: "Timeline" })) {
      expect(toggle).toHaveAttribute("aria-pressed", "true");
    }
  });

  // A private window, a browser set to block site data and a thumbnail capture
  // all THROW here rather than returning null. A remembered preference is never
  // worth a blank page.
  it("still renders when the browser refuses site data", () => {
    const getItem = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("The operation is insecure.");
    });
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("The operation is insecure.");
    });
    try {
      render(<RunCrew events={fanOut()} />);
      expect(screen.getByRole("button", { name: "Crew" })).toHaveAttribute("aria-pressed", "true");
      // …and switching still works, it just is not remembered.
      fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
      expect(screen.getByText(/solid · working/)).toBeInTheDocument();
    } finally {
      getItem.mockRestore();
      setItem.mockRestore();
    }
  });

  it("gives every agent a row, indented by the depth the runtime declared", () => {
    render(<RunCrew events={fanOut()} />);
    // Four agents plus the lead, each once — not one row per contiguous stretch,
    // which is what a flat log gave when three agents interleaved.
    for (const label of [
      "lead agent",
      "Implement todo-api",
      "Implement todo-webapp",
      "Write the OpenAPI contract",
    ]) {
      expect(row(label)).toBeInTheDocument();
    }
    // The depth-2 agent is indented past its parent. v1 could not describe this
    // shape at all — it filed a grandchild's work under the lead.
    const indent = (label: string) =>
      parseFloat(getComputedStyle(row(label)).paddingLeft);
    expect(indent("Write the OpenAPI contract")).toBeGreaterThan(indent("Implement todo-api"));
    expect(indent("Implement todo-api")).toBeGreaterThan(indent("lead agent"));
  });

  it("says when an agent was spawned in the background", () => {
    render(<RunCrew events={fanOut()} />);
    // Printed only when the runtime said TRUE, which is the ordinary case for a
    // builder: fan-out is backgrounded by default and the skill decides its
    // shape (ADR-0011). A `false` means the parent is blocked inside this
    // agent's call, and the row says that in words, so only true needs a chip.
    expect(within(row("Implement todo-webapp")).getByText("background")).toBeInTheDocument();
    expect(within(row("Implement todo-api")).queryByText("background")).toBeNull();
  });

  it("a settled row carries the agent's own report, and the inspector its totals", () => {
    render(<RunCrew events={fanOut()} />);
    // The sub-line of a settled row IS its report — the only copy there will
    // ever be, since a spawned agent's transcript dies with its pod.
    expect(
      within(row("Implement todo-api")).getByText(/Implemented the service and its smoke test/),
    ).toBeInTheDocument();
    // An agent the runtime called failed reads as failed, not as merely quiet.
    expect(within(row("Implement todo-webapp")).getByText("failed")).toBeInTheDocument();

    fireEvent.click(row("Implement todo-api"));
    // Every figure is the runtime's OWN: it measured the agent's whole life,
    // including the parts that never reached this feed.
    expect(
      screen.getByText("completed · 3m29s · 19 tools · +553/−4 lines"),
    ).toBeInTheDocument();
  });

  it("shows the selected agent's steps, and nobody else's", () => {
    render(<RunCrew events={fanOut()} />);
    // The lead is the default selection, so its own row is what shows.
    expect(screen.getByText("$ git status")).toBeInTheDocument();
    expect(screen.queryByText("$ bal build")).toBeNull();

    fireEvent.click(row("Implement todo-api"));
    expect(screen.getByText("$ bal build")).toBeInTheDocument();
    // The grandchild's work stays on the grandchild — the whole point of the
    // declared tree.
    expect(screen.queryByText(/contracts\/api.yaml/)).toBeNull();
    expect(screen.queryByText("$ git status")).toBeNull();

    // The outcome trails on its own action row rather than repeating the command
    // a second time further down.
    expect(
      screen.getByText("exit 1 · error: compilation contains errors · 25.1s"),
    ).toBeInTheDocument();
  });

  it("tells a reader when an agent forwarded nothing at all", () => {
    render(<RunCrew events={fanOut()} />);
    fireEvent.click(row("Implement todo-webapp"));
    // A backgrounded agent forwards none of its own messages, so an empty
    // inspector is a real state — and a blank panel would read as a bug.
    expect(screen.getByText("This agent forwarded no steps.")).toBeInTheDocument();
  });

  // Where "where did the time go" always ends: "so what was it doing".
  it("returns to the crew with that agent selected when a lane is picked", () => {
    render(<RunCrew events={fanOut()} />);
    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    fireEvent.click(screen.getByRole("button", { name: /^Implement todo-api: completed/ }));

    expect(screen.getByRole("button", { name: "Crew" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("$ bal build")).toBeInTheDocument();
  });

  it("draws no blank rows for the kinds that are state rather than output", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 }),
      ev(1, { kind: "agent_progress", agentId: "a1", phrase: "Writing todo-api/service.bal" }),
      ev(2, { kind: "work_item", agentId: "a1", source: "criterion", itemId: "AC-001-a", itemStatus: "pass" }),
      ev(3, { kind: "heartbeat", agentId: "a1", waitingOn: "tool", ref: "t1", elapsedMs: 130_000 }),
    ];
    const { container } = render(<RunCrew events={events} />);

    // The heartbeat displaces the stale phrase on the agent's row — it fires
    // only when nothing else is happening, so it is the fresher truth.
    expect(
      within(row("todo-api")).getByText(/waiting on a tool call for 2m10s/),
    ).toBeInTheDocument();
    // …and none of the three became a row of its own.
    expect(container.textContent).not.toContain("Writing todo-api/service.bal");
    expect(container.textContent).not.toContain("AC-001-a");
  });

  // The tree is agents, not their commands. It used to carry a row per
  // backgrounded command, and a live run produced 47 — so the column that
  // answers "who is working" was mostly raw command lines.
  //
  // The concern that put them there is still real and still served: an orphaned
  // `dev:mock` holding a port after the run ends has to have somebody's name on
  // it. That name is now on its `task_settled` row in the inspector, under the
  // agent that ran it, which is where a reader goes once they have picked the
  // agent this column is for.
  it("names the agent in the tree and leaves its commands to the inspector", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "a1", label: "todo-webapp", depth: 1 }),
      ev(1, { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "pnpm dev:mock", toolUseId: "t1" }),
      ev(2, { kind: "task_started", agentId: "a1", taskId: "bg1", summary: "pnpm dev:mock" }),
      ev(3, { kind: "task_settled", agentId: "a1", taskId: "bg1", summary: "pnpm dev:mock", status: "stopped" }),
    ];
    render(<RunCrew events={events} />);

    // The agent has a row, and the command is nowhere in the tree — not even
    // once, which is the whole point of the change.
    expect(row("todo-webapp")).toBeInTheDocument();
    expect(screen.queryByText(/pnpm dev:mock/)).not.toBeInTheDocument();

    // Pick the agent, and its feed answers with the command and what became of
    // it. This is also the cost of the change, stated honestly: a subagent's
    // commands are one click away rather than on screen from the start.
    fireEvent.click(row("todo-webapp"));
    const shown = screen.getAllByText(/pnpm dev:mock/);
    expect(shown.length).toBeGreaterThan(0);
    for (const el of shown) {
      expect(el.closest("ul")).toBeNull();
    }
    expect(screen.getByText(/stopped/)).toBeInTheDocument();
  });

  // The gap: a spawn is filed under the agent it CREATES, so it became that
  // agent's section header and left no trace in the log of the agent that
  // ordered it. Reading the lead's own steps, a run's whole fan-out was
  // invisible — its steps jumped from reading the design to opening the pull
  // request, with three agents and nine minutes unaccounted for in between.
  it("shows the lead what it dispatched, and what became of each one", () => {
    render(<RunCrew events={fanOut()} />);

    // The lead is the default selection, so this is what a reader sees first.
    expect(screen.getByText(/dispatched 2 agents/)).toBeInTheDocument();

    // Each dispatched agent is named in the lead's own panel, with its state —
    // which is the answer to "why is the lead quiet": it is waiting on these.
    const dispatched = screen.getByRole("button", { name: /^Show Implement todo-api/ });
    expect(dispatched).toBeInTheDocument();

    // And it is a way IN: picking one moves the inspector to that agent, the
    // same selection the tree drives.
    fireEvent.click(dispatched);
    expect(
      screen.getByText("Implemented the service and its smoke test."),
    ).toBeInTheDocument();
  });

  // A crew of one dispatched nobody, and an empty "dispatched 0 agents" heading
  // would be chrome around a fact the panel already makes obvious.
  it("says nothing about dispatch when an agent spawned nobody", () => {
    seq = 0;
    const events = [ev(0, { kind: "tool_use", agentId: "lead", tool: "Read", summary: "specs/design.md", toolUseId: "t1" })];
    render(<RunCrew events={events} />);
    expect(screen.queryByText(/dispatched/)).not.toBeInTheDocument();
  });

  it("the hint says how many agents, how many are running, and how quiet it is", () => {
    seq = 0;
    const events = [ev(0, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 })];
    render(<RunCrew events={events} />);
    // The one fact both views share, which is why it sits with the toggle.
    expect(screen.getByText("2 agents · 2 running · last event 1m0s ago")).toBeInTheDocument();
  });

  it("says nothing is running once the cycle has settled", () => {
    render(<RunCrew events={fanOut()} />);
    // A settled cycle's age would count how long ago the build was, which the
    // page header already says — and it would tick about a run nobody awaits.
    expect(screen.getByText("4 agents · all settled")).toBeInTheDocument();
    expect(screen.queryByText(/last event/)).toBeNull();
  });

  // --- liveness ---------------------------------------------------------------

  it("turns a row amber after a minute of silence with a call unanswered", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 }),
      ev(1, { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal build", toolUseId: "t1" }),
    ];
    vi.setSystemTime(T0 + 1_000 + 59_000);
    const { rerender } = render(<RunCrew events={events} />);
    expect(within(row("todo-api")).getByText("working")).toBeInTheDocument();

    vi.setSystemTime(T0 + 1_000 + 60_000);
    rerender(<RunCrew events={events} />);
    const amber = within(row("todo-api"));
    expect(amber.getByText("stalled")).toBeInTheDocument();
    // An amber row always says what it is amber ABOUT. The unanswered call is
    // usually the only witness, and it is a true one.
    expect(amber.getByText(/waiting on Bash for 1m0s/)).toBeInTheDocument();
  });

  it("never calls silence a failure", () => {
    seq = 0;
    // Ten minutes of nothing, with no call outstanding. The age is the honest
    // report; a verdict here would put a red row on a run that is thinking.
    const events = [ev(0, { kind: "agent_progress", agentId: "lead", phrase: "Reading the design" })];
    vi.setSystemTime(T0 + 600_000);
    render(<RunCrew events={events} />);
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("Reading the design")).toBeInTheDocument();
    // The age is the honest report, and it is stated rather than judged.
    expect(screen.getByText("1 agent · 1 running · last event 10m0s ago")).toBeInTheDocument();
  });

  // The whole reason this surface owns a clock. A stream that has gone quiet
  // gives React no reason to re-render, and an age frozen at whatever it read on
  // first paint is the precise lie the crew exists to stop telling.
  it("ticks every agent's age while no events arrive at all", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 }),
      ev(1, { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal build", toolUseId: "t1" }),
    ];
    vi.setSystemTime(T0 + 6_000);
    render(<RunCrew events={events} />);
    expect(screen.getByText("2 agents · 2 running · last event 5.0s ago")).toBeInTheDocument();
    expect(within(row("todo-api")).getByText("5.0s ago")).toBeInTheDocument();

    // Not one new event. Only the clock moved.
    act(() => {
      vi.advanceTimersByTime(4_000);
    });
    expect(screen.getByText("2 agents · 2 running · last event 9.0s ago")).toBeInTheDocument();
    expect(within(row("todo-api")).getByText("9.0s ago")).toBeInTheDocument();
  });

  it("runs no clock for a cycle where nothing is running", () => {
    render(<RunCrew events={fanOut()} />);
    const settled = screen.getByText("4 agents · all settled");
    // A settled cycle's rows do not move, so a timer on one would be a
    // re-render a second for no reader — and there is no age on them to move.
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(settled).toBeInTheDocument();
    expect(screen.queryByText(/ago$/)).toBeNull();
  });

  // A validation cycle runs a single validator, and a small coding cycle never
  // fans out. A tree with one row and a timeline with one lane would be chrome
  // around a fact already on screen.
  it("gives a single-agent cycle the same tree and inspector as a fanned-out one", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "run_started", agentId: "lead", taskKind: "validation" }),
      ev(1, { kind: "tool_use", agentId: "lead", tool: "Bash", summary: "pnpm playwright test", toolUseId: "v1" }),
      ev(2, { kind: "tool_result", agentId: "lead", tool: "Bash", ok: true, durationMs: 61_400, toolUseId: "v1" }),
      ev(3, { kind: "agent_settled", agentId: "lead", status: "completed", durationMs: 184_000, toolCount: 22, report: "Checked 5 automated criteria." }),
      ev(4, { kind: "run_settled", agentId: "lead", outcome: "success" }),
    ];
    render(<RunCrew events={events} />);
    // The tree is there with its one row — the layout a second agent will join,
    // rather than one it would replace.
    expect(row("lead agent")).toBeInTheDocument();
    // And the inspector beside it: the steps, the totals and the report.
    expect(screen.getByText("$ pnpm playwright test")).toBeInTheDocument();
    expect(screen.getByText("completed · 3m4s · 22 tools")).toBeInTheDocument();
    expect(screen.getAllByText("Checked 5 automated criteria.").length).toBeGreaterThan(0);
    expect(screen.getByText("1 agent · all settled")).toBeInTheDocument();
  });

  // The timeline is the one thing a crew of one does NOT get: it compares lanes,
  // and one lane compares nothing — a single bar spanning the cycle, which the
  // hint beside it already says in words.
  it("offers no timeline for a single lane", () => {
    seq = 0;
    const events = [ev(0, { kind: "run_started", agentId: "lead", taskKind: "validation" })];
    render(<RunCrew events={events} />);
    expect(screen.queryByRole("button", { name: "Timeline" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Crew" })).toBeNull();
  });

  // THE PRODUCT RULE this shape exists for: the page must not change shape
  // mid-run. A reader learns one surface while the lead works alone, and the
  // first spawned agent then appears IN PLACE, as a row under it.
  it("adds a row when the first subagent arrives, rather than rearranging the page", () => {
    seq = 0;
    const alone = [
      ev(0, { kind: "run_started", agentId: "lead", taskKind: "implementation" }),
      ev(1, { kind: "tool_use", agentId: "lead", tool: "Bash", summary: "git status", toolUseId: "m1" }),
    ];
    const { rerender } = render(<RunCrew events={alone} />);
    const treeBefore = row("lead agent").closest("ul");
    expect(treeBefore).not.toBeNull();
    expect(screen.getByText("$ git status")).toBeInTheDocument();

    rerender(
      <RunCrew
        events={[
          ...alone,
          ev(2, { kind: "agent_started", agentId: "a1", label: "Implement todo-api", depth: 1 }),
        ]}
      />,
    );

    // The same tree, one row longer — not a different surface.
    expect(row("lead agent").closest("ul")).toBe(treeBefore);
    expect(row("Implement todo-api")).toBeInTheDocument();
    // The lead's own steps are still what the inspector shows: a new agent must
    // not move the reader's selection.
    expect(screen.getByText("$ git status")).toBeInTheDocument();
    // …and the second lane is what brings the timeline with it.
    expect(screen.getByRole("button", { name: "Timeline" })).toBeInTheDocument();
  });

  // A reader who left the timeline on and then opened a single-agent cycle has
  // no toggle to switch back with, so the crew has to be what it draws.
  it("draws the crew for one agent even when the remembered view is the timeline", () => {
    localStorage.setItem("aep:builds:run-view", "timeline");
    resetRunViewForTest();
    seq = 0;
    const events = [
      ev(0, { kind: "run_started", agentId: "lead", taskKind: "validation" }),
      ev(1, { kind: "tool_use", agentId: "lead", tool: "Bash", summary: "pnpm playwright test", toolUseId: "v1" }),
    ];
    render(<RunCrew events={events} />);
    expect(screen.getByText("$ pnpm playwright test")).toBeInTheDocument();
    expect(screen.queryByText(/solid · working/)).toBeNull();
  });

  // --- the agent's own plan ---------------------------------------------------
  //
  // The `aep` skill tells a run that the platform shows its task list to the
  // person watching, "so it is the one place your plan has to be true". These
  // are the tests that make that sentence true of this surface.

  it("draws the lead's plan under the lead, and a handed-off entry under its owner", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0 }),
      ev(1, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 }),
      ev(2, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p1", title: "Read the design", itemStatus: "completed" }),
      ev(3, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p2", title: "Implement the API", itemStatus: "in_progress", ownerAgentId: "a1" }),
    ];
    render(<RunCrew events={events} />);

    // Under the agent it NAMES, not under the lead that wrote it: "what was this
    // one sent to do" is the question a reader has about the spawned row.
    expect(planRow("Implement the API").previousElementSibling).toBe(row("todo-api"));
    expect(planRow("Read the design").previousElementSibling).toBe(row("lead agent"));
    // Indented under its owner, so a depth-1 agent's entry cannot read as the
    // lead's own.
    const indent = (el: HTMLElement) => parseFloat(getComputedStyle(el).paddingLeft);
    expect(indent(planRow("Implement the API"))).toBeGreaterThan(indent(planRow("Read the design")));
    // Colour is never the only signal: where an entry stands is a word too.
    expect(within(planRow("Implement the API")).getByText("in progress")).toBeInTheDocument();
  });

  it("keeps a plan entry's title when a later update carries only its status", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0 }),
      ev(1, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 }),
      ev(2, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p1", title: "Open the pull request", itemStatus: "pending" }),
      // The runtime sends a subject only when the agent renamed the entry, so a
      // tick-off carries none — and a row that blanked itself on completion is
      // the failure this pins.
      ev(3, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p1", itemStatus: "completed" }),
    ];
    render(<RunCrew events={events} />);
    expect(within(planRow("Open the pull request")).getByText("completed")).toBeInTheDocument();
  });

  it("draws no row for an entry the agent removed, nor for a validation criterion", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0 }),
      ev(1, { kind: "agent_started", agentId: "a1", label: "todo-api", depth: 1 }),
      ev(2, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p1", title: "Rewrite the deploy script", itemStatus: "pending" }),
      ev(3, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p1", itemStatus: "deleted" }),
      // A criterion shares the kind and nothing else: it is the PLATFORM's unit
      // of work in a validating run, and folding one here would paint acceptance
      // criteria onto an agent's to-do list.
      ev(4, { kind: "work_item", agentId: "lead", source: "criterion", itemId: "AC-003-a", itemStatus: "fail" }),
    ];
    const { container } = render(<RunCrew events={events} />);
    expect(container.textContent).not.toContain("Rewrite the deploy script");
    expect(container.textContent).not.toContain("AC-003-a");
  });

  it("shows the plan on a cycle with one agent, where there is no tree to hang it on", () => {
    seq = 0;
    const events = [
      ev(0, { kind: "run_started", agentId: "lead", taskKind: "implementation" }),
      ev(1, { kind: "work_item", agentId: "lead", source: "plan", itemId: "p1", title: "Fix the redirect handler", itemStatus: "completed" }),
      ev(2, { kind: "agent_settled", agentId: "lead", status: "completed", durationMs: 40_000, toolCount: 3, report: "Fixed it." }),
      ev(3, { kind: "run_settled", agentId: "lead", outcome: "success" }),
    ];
    render(<RunCrew events={events} />);
    // The commonest shape a run takes, and the skill's promise has to hold on
    // it: the entry is under the agent in the tree AND labelled in the
    // inspector, exactly as it is on a cycle that fanned out.
    expect(planRow("Fix the redirect handler").previousElementSibling).toBe(row("lead agent"));
    expect(screen.getByText("Plan")).toBeInTheDocument();
    // …and a settled agent KEEPS it: the list is what it set out to do and
    // whether it got there, which only becomes a record once the run is over.
    expect(
      within(planRow("Fix the redirect handler", "inspector")).getByText("completed"),
    ).toBeInTheDocument();
  });

  it("gives an agent that kept no list no plan section at all", () => {
    render(<RunCrew events={fanOut()} />);
    // An empty heading would report an absence as a section. Most agents keep
    // no list, so this is the common case rather than the edge one.
    expect(screen.queryByText("Plan")).toBeNull();
  });

  it("says so plainly when a cycle has produced nothing", () => {
    render(<RunCrew events={[]} />);
    expect(screen.getByText("No output from this cycle yet.")).toBeInTheDocument();
  });
});
