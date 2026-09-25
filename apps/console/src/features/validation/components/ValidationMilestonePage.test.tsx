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

import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type ValidationDetail = components["schemas"]["ValidationDetail"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}));

// The log feed opens an SSE connection on mount; this page only decides
// WHETHER it is mounted, which is what the collapse test below asserts.
const runFeed = vi.fn();
vi.mock("../../builds/components/RunFeed", () => ({
  RunFeed: (props: { runId: string }) => {
    runFeed(props);
    return <div data-testid="run-feed">{props.runId}</div>;
  },
}));

let mockDetail: ValidationDetail | undefined;
let mockDetailState = { isPending: false, isError: false };
let mockSnapshot: { report?: string | null; criteria: unknown[] } | undefined;
const startMutate = vi.fn();
const cancelMutate = vi.fn();
const snapshotCalls: { cycleId: string; enabled: boolean }[] = [];

vi.mock("../api/queries", () => ({
  useValidation: () => ({
    data: mockDetail,
    isPending: mockDetailState.isPending,
    isError: mockDetailState.isError,
    error: null,
    refetch: vi.fn(),
  }),
  useValidationSnapshot: (
    _p: string,
    _t: string,
    cycleId: string,
    enabled: boolean,
  ) => {
    snapshotCalls.push({ cycleId, enabled });
    return {
      data: enabled ? mockSnapshot : undefined,
      isPending: false,
      isError: false,
    };
  },
  useStartValidation: () => ({ mutate: startMutate, isPending: false }),
}));

vi.mock("../../builds/api/queries", () => ({
  useCancelRun: () => ({ mutate: cancelMutate, isPending: false }),
}));
vi.mock("../../projects/api/queries", () => ({
  useProjectStatus: () => ({ data: undefined, isError: false }),
}));
vi.mock("../../tasks/api/queries", () => ({
  useTask: () => ({ data: undefined }),
}));

import { ValidationMilestonePage } from "./ValidationMilestonePage";

const cycle = (over: Partial<RunCycleView> = {}): RunCycleView =>
  ({
    id: "c1",
    kind: "validation",
    attempts: 1,
    createdAt: "2026-08-14T16:20:00Z",
    endedAt: "2026-08-14T16:52:47Z",
    mergeSha: "abc1234",
    validationVerdict: "passed",
    validationIssue: 7,
    recording: "complete",
    ...over,
  }) as RunCycleView;

/**
 * An attempt still in flight: no end, no commit, and NO VERDICT — the three
 * absences together are what a running cycle is. Its own literal rather than an
 * override, because `exactOptionalPropertyTypes` refuses an explicit undefined
 * and the verdict has to be missing, not empty.
 */
const runningCycle = (id: string): RunCycleView =>
  ({
    id,
    kind: "validation",
    attempts: 1,
    createdAt: "2026-08-14T17:00:00Z",
    endedAt: null,
    mergeSha: "",
    validationIssue: 7,
    recording: "recording",
  }) as RunCycleView;

const run = (over: Partial<MilestoneRunView> = {}): MilestoneRunView =>
  ({
    id: "r1",
    kind: "validation",
    origin: "revalidate",
    state: "succeeded",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    budgets: {},
    validation: { verdict: "passed" },
    cycles: [cycle()],
    createdAt: "2026-08-14T16:00:00Z",
    ...over,
  }) as MilestoneRunView;

const detail = (over: Partial<ValidationDetail> = {}): ValidationDetail => ({
  tag: "v1",
  milestoneNumber: 1,
  state: "passed",
  live: false,
  deployed: true,
  runs: [run()],
  ...over,
});

beforeEach(() => {
  mockDetail = detail();
  mockDetailState = { isPending: false, isError: false };
  mockSnapshot = { report: '{"schemaVersion":2,"scenarios":[]}', criteria: [] };
  snapshotCalls.length = 0;
  startMutate.mockClear();
  cancelMutate.mockClear();
  runFeed.mockClear();
});

describe("ValidationMilestonePage", () => {
  it("names the version and its verdict", () => {
    render(<ValidationMilestonePage projectName="p" tag="v1" />);
    expect(screen.getByText("Validation v1")).toBeInTheDocument();
    expect(screen.getAllByText("Validated").length).toBeGreaterThan(0);
  });

  // The two cards exist because reports outlive logs: rows and reports are kept
  // forever, the agent's recording is pruned at 30 days (ADR-0027).
  it("puts the report above the log", () => {
    render(<ValidationMilestonePage projectName="p" tag="v1" />);
    const report = screen.getByText("Acceptance reports");
    const log = screen.getByText("Validation logs");
    expect(report.compareDocumentPosition(log) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  // Only the newest attempt is fetched with the page — the verdict card needs
  // its counts. Older ones cost a whole report plus every feature file, and
  // load when opened.
  it("fetches only the newest attempt's snapshot on arrival", () => {
    mockDetail = detail({
      runs: [
        run({
          cycles: [
            cycle({ id: "old", validationVerdict: "failed" }),
            cycle({ id: "new" }),
          ],
        }),
      ],
    });
    render(<ValidationMilestonePage projectName="p" tag="v1" />);

    const enabled = snapshotCalls.filter((c) => c.enabled).map((c) => c.cycleId);
    expect(enabled).toContain("new");
    expect(enabled).not.toContain("old");
  });

  // LogSection unmounts its children when collapsed, so a settled version opens
  // no SSE connection until the reader asks for one.
  it("collapses the log on a settled version and opens it on a live one", () => {
    render(<ValidationMilestonePage projectName="p" tag="v1" />);
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();

    mockDetail = detail({ state: "running", live: true });
    render(<ValidationMilestonePage projectName="p" tag="v1" />);
    expect(screen.getAllByTestId("run-feed").length).toBeGreaterThan(0);
  });

  describe("the empty states", () => {
    // The split is the same boolean that enables the trigger, so the sentence
    // and the control cannot contradict each other.
    it("says to wait while a run is still working the version", () => {
      mockDetail = detail({ state: "none", live: true, runs: [] });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      expect(screen.getByText(/After a deployment/)).toBeInTheDocument();
      expect(screen.queryByText(/Run validation to check/)).not.toBeInTheDocument();
    });

    it("invites the trigger when nothing is running", () => {
      mockDetail = detail({ state: "none", live: false, runs: [] });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      expect(screen.getByText(/Run validation to check/)).toBeInTheDocument();
    });

    it("does not invite a run on a version with no criteria", () => {
      mockDetail = detail({ state: "skipped", live: false, runs: [] });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      expect(screen.getByText(/no acceptance criteria/)).toBeInTheDocument();
      expect(screen.queryByText(/Run validation to check/)).not.toBeInTheDocument();
    });
  });

  describe("the history boundary", () => {
    const twoAttempts = () =>
      detail({
        runs: [
          run({
            cycles: [
              cycle({ id: "old", validationVerdict: "failed" }),
              cycle({ id: "new" }),
            ],
          }),
        ],
      });

    // One wording on both cards. "Attempts" rather than "runs" because a
    // self-heal repeat opens a second attempt on the SAME run — as here, where
    // one run holds both attempts, so only the report card has history to mark.
    it("marks the report card's history for two attempts on one run", () => {
      mockDetail = twoAttempts();
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      expect(screen.getAllByText("EARLIER ATTEMPTS OF V1")).toHaveLength(1);
    });

    // Two RUNS: the log card now has an older run to group under its own
    // caption, and it is mounted because the version is live.
    it("marks both cards when the history spans runs", () => {
      mockDetail = detail({
        state: "running",
        live: true,
        runs: [
          run({ id: "r2", state: "running", cycles: [runningCycle("new")] }),
          run({ id: "r1", cycles: [cycle({ id: "old", validationVerdict: "failed" })] }),
        ],
      });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      expect(screen.getAllByText("EARLIER ATTEMPTS OF V1")).toHaveLength(2);
    });

    // MUI rounds an accordion's corners by :first-of-type / :last-of-type among
    // its SIBLINGS. The newest attempt therefore lives alone in its own parent
    // (all four corners), and every older attempt shares one parent (one fitted
    // block). Splicing the caption into a single flat list gave the newest a
    // square bottom and the first older one a square top.
    it("keeps the newest attempt apart and the older ones together", () => {
      mockDetail = detail({
        runs: [
          run({
            cycles: [
              cycle({ id: "oldest", validationVerdict: "failed" }),
              cycle({ id: "old", validationVerdict: "failed" }),
              cycle({ id: "new" }),
            ],
          }),
        ],
      });
      const { container } = render(<ValidationMilestonePage projectName="p" tag="v1" />);
      const [newest, ...older] = Array.from(container.querySelectorAll(".MuiAccordion-root"));

      expect(older).toHaveLength(2);
      expect(older[0]!.parentElement).toBe(older[1]!.parentElement);
      expect(newest!.parentElement).not.toBe(older[0]!.parentElement);
      // The caption belongs to the history block, not to the seam between.
      expect(older[0]!.parentElement!.textContent).toContain("EARLIER ATTEMPTS OF V1");
    });

    // Runs arrive newest first with their cycles in dispatch order. The report
    // list has to descend the whole way — newest run on top, newest attempt
    // within it first — the same reading order as the log card below it.
    // Reversing the flattened list once got the cycles right and the runs
    // wrong, which only a version with more than one run could show.
    //
    // A heading is "Attempt N", counted from the oldest across the version.
    // Which run held it is not in the name: an attempt IS a run since
    // validation became its own workflow, and "cycle" is the loop's word.
    it("numbers the attempts across runs and orders them newest first", () => {
      mockDetail = detail({
        runs: [
          run({
            id: "r2",
            cycles: [
              cycle({ id: "r2-old", validationVerdict: "failed" }),
              cycle({ id: "r2-new" }),
            ],
          }),
          run({
            id: "r1",
            cycles: [
              cycle({ id: "r1-old", validationVerdict: "failed" }),
              cycle({ id: "r1-new", validationVerdict: "failed" }),
            ],
          }),
        ],
      });
      const { container } = render(<ValidationMilestonePage projectName="p" tag="v1" />);

      const headings = Array.from(
        container.querySelectorAll(".MuiAccordionSummary-root .MuiTypography-subtitle2"),
      ).map((el) => el.textContent);
      expect(headings).toEqual(["Attempt 4", "Attempt 3", "Attempt 2", "Attempt 1"]);
      // And the newest attempt is the one whose snapshot the page fetches.
      expect(snapshotCalls.filter((c) => c.enabled).map((c) => c.cycleId)).toEqual(["r2-new"]);
    });

    // The log card heads its boxes the same way, but each run's feed only
    // knows its own cycles, so the page hands every feed a label that carries
    // the count of attempts in the runs older than it.
    it("hands each log feed the attempt numbers its run continues from", () => {
      mockDetail = detail({
        state: "running",
        live: true,
        runs: [
          run({ id: "r3", state: "running", cycles: [runningCycle("r3-only")] }),
          run({ id: "r2", cycles: [cycle({ id: "r2-only", validationVerdict: "failed" })] }),
          run({
            id: "r1",
            cycles: [
              cycle({ id: "r1-old", validationVerdict: "unreported" }),
              cycle({ id: "r1-new", validationVerdict: "unreported" }),
            ],
          }),
        ],
      });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);

      type FeedProps = { runId: string; runNumber?: number; label: (ordinal: number) => string };
      const feeds = (runFeed.mock.calls as [FeedProps][]).map(([props]) => [
        props.runId,
        props.label(1),
        props.label(2),
      ]);
      expect(feeds).toEqual([
        ["r3", "Attempt 4", "Attempt 5"],
        ["r2", "Attempt 3", "Attempt 4"],
        ["r1", "Attempt 1", "Attempt 2"],
      ]);
      // No feed is told a run number: the attempt number is the whole heading.
      for (const [props] of runFeed.mock.calls as [FeedProps][]) {
        expect(props.runNumber).toBeUndefined();
      }
    });

    // The ordinary case is one attempt, and a rule over a single entry marks
    // nothing.
    it("draws no rule for a version with one attempt", () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      expect(screen.queryByText(/EARLIER ATTEMPTS/)).not.toBeInTheDocument();
    });
  });

  describe("an expanded attempt", () => {
    // The sentence has a counted form and a count-free one; it was being handed
    // `undefined` and so always read the poorer of the two, although the report
    // it needed was already fetched.
    it("leads with the counted sentence and the tally", () => {
      mockSnapshot = {
        report: JSON.stringify({
          schemaVersion: 2,
          scenarios: [
            { feature: "F", rule: "R", scenario: "a", outcome: "passed", steps: [] },
            { feature: "F", rule: "R", scenario: "b", outcome: "passed", steps: [] },
          ],
        }),
        criteria: [],
      };
      mockDetail = detail({
        runs: [
          run({
            cycles: [
              cycle({ id: "old", validationVerdict: "passed" }),
              cycle({ id: "new" }),
            ],
          }),
        ],
      });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);

      // Older attempts fetch on EXPAND, so until this click the older section
      // has no report and its lead can only say the count-free sentence. (The
      // verdict card above says the counted one for the newest attempt, which
      // is what an unscoped text query would find instead.)
      fireEvent.click(screen.getByText("Attempt 1"));

      // One line, numbers first, on the older attempt's own lead.
      const lead = screen.getByText(
        (_, el) =>
          el?.tagName === "P" &&
          el.textContent === "2 passed — All 2 scenarios were settled and passed.",
      );
      expect(lead.querySelector("span")?.textContent).toBe("2 passed");
    });
  });

  // The old page showed the previous attempt's counts marked "(last attempt)"
  // because it had one report on screen and no history. The history is now
  // directly below, so the card says nothing about the previous attempt — and
  // this pins it, because it currently falls out of WHICH verdict the card is
  // fed rather than from a decision anything states.
  it("shows no stale numbers while an attempt is running", () => {
    mockSnapshot = { report: null, criteria: [] };
    mockDetail = detail({
      state: "running",
      live: true,
      runs: [
        run({
          state: "running",
          cycles: [
            cycle({ id: "old", validationVerdict: "failed" }),
            runningCycle("running"),
          ],
        }),
      ],
    });
    render(<ValidationMilestonePage projectName="p" tag="v1" />);

    expect(screen.queryByText(/last attempt/)).not.toBeInTheDocument();
    expect(screen.getByText("The validation agent is running.")).toBeInTheDocument();
  });

  describe("the actions menu", () => {
    const open = () => fireEvent.click(screen.getByLabelText("Validation actions"));

    it('says "Run validation" until something has answered', () => {
      mockDetail = detail({ state: "none", live: false, runs: [] });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      expect(screen.getByText("Run validation")).toBeInTheDocument();
    });

    it('says "Revalidate" once an attempt has produced a verdict', () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      expect(screen.getByText("Revalidate")).toBeInTheDocument();
    });

    // The same order as the builds menu — cancel, then (re)start, then the
    // links — so a reader who learned one finds the same item in the same
    // place on the other.
    it("leads with cancel, as the builds menu does", () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      const names = screen.getAllByRole("menuitem").map((el) => el.textContent);
      expect(names.slice(0, 2)).toEqual(["Cancel run", "Revalidate"]);
    });

    // MUI's MenuList focuses and arrows between its OWN children, so an item
    // wrapped in a tooltip span is one it walks straight past — the page's main
    // action was clickable and unreachable by keyboard. Cancel is disabled on a
    // settled version, which makes this the first item the menu can land on.
    it("puts the start item in the keyboard's path", () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      expect(document.activeElement).toBe(
        screen.getByRole("menuitem", { name: "Revalidate" }),
      );
    });

    // The console checks ONE condition. Gating on the verdict too would invent
    // a rule the API does not have — re-asking a passed version is the point.
    it("offers a re-run on a version that already passed", () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      fireEvent.click(screen.getByText("Revalidate"));
      expect(startMutate).toHaveBeenCalled();
    });

    it("refuses while a run is live on the milestone", () => {
      mockDetail = detail({ state: "running", live: true });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      fireEvent.click(screen.getByText("Revalidate"));
      expect(startMutate).not.toHaveBeenCalled();
    });

    // A revalidation drives whatever is SERVING — the runner resolves its
    // endpoints from the cluster at request time — so asking an older version
    // would judge code that version never shipped, and file the verdict AND any
    // repair work on its milestone. The server refuses it too; this stops a
    // reader finding out by clicking.
    it("refuses a version that is not the deployed one", () => {
      mockDetail = detail({ deployed: false });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      fireEvent.click(screen.getByText("Revalidate"));
      expect(startMutate).not.toHaveBeenCalled();
    });

    // ADR-0016 decision 7: cancel follows the LIFECYCLE, not run liveness.
    it("offers cancel on the two lifecycle states and no others", () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      fireEvent.click(screen.getByText("Cancel run"));
      expect(cancelMutate).not.toHaveBeenCalled();

      mockDetail = detail({ state: "awaiting-fix", live: true });
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      fireEvent.click(screen.getAllByLabelText("Validation actions")[1]!);
      fireEvent.click(screen.getAllByText("Cancel run")[1]!);
      expect(cancelMutate).toHaveBeenCalled();
    });

    it("surfaces a refusal from the server in the page's one error slot", () => {
      render(<ValidationMilestonePage projectName="p" tag="v1" />);
      open();
      fireEvent.click(screen.getByText("Revalidate"));
      const onError = startMutate.mock.calls[0]?.[1]?.onError as (e: Error) => void;
      act(() => {
        onError(new Error("this version still has open work"));
      });
      expect(screen.getByText("this version still has open work")).toBeInTheDocument();
    });
  });
});
