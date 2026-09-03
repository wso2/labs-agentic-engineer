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
import type { components } from "../../../generated/aep-api";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunValidation = components["schemas"]["RunValidation"];

// Router replaced so the PageHeader back-link renders as a plain anchor — no
// RouterProvider needed (mirrors DeploymentsPage.test.tsx / NotFound.test.tsx).
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}));

// The live log is the RUN feed filtered to the validation cycle, and it opens
// an SSE stream. Stub it to a marker so we can assert which lifecycle states
// show the log vs. the report, without a stream.
// The run it was pointed at and whether it may open a box are attributes rather than
// rendered text, so the page's WIRING is assertable without a stream: which run leads
// and which feed owns the one open log are decisions this page makes, not RunFeed.
vi.mock("../../builds/components/RunFeed", () => ({
  RunFeed: ({
    runId,
    cycleKinds,
    expandNewest,
    runNumber,
  }: {
    runId: string;
    cycleKinds?: readonly string[];
    expandNewest?: boolean;
    runNumber?: number;
  }) => (
    <div
      data-testid="run-feed"
      data-run-id={runId}
      data-expand-newest={String(expandNewest)}
      data-run-number={String(runNumber)}
    >
      {(cycleKinds ?? []).join(",")}
    </div>
  ),
}));

// Live per-criterion statuses ride an SSE stream (useRunProgress). Stubbed to a
// settable map so the page's chip PRECEDENCE is assertable without a stream — the
// fold that builds the map is tested on its own in useValidationLive.test.ts.
let mockLive: Record<string, string> = {};
let mockLiveActive = true;
vi.mock("../hooks/useValidationLive", () => ({
  useValidationLive: () => ({ statuses: mockLive, active: mockLiveActive }),
}));

// Controllable status + runs + file queries (no QueryClientProvider / MSW).
let mockValidation = "none";
let mockRun: MilestoneRunView | undefined;
// Runs NEWER than mockRun on the same milestone, newest first — list-build-runs
// answers newest-first, so these sit ahead of it. A milestone accumulates runs
// across its life and only some of them validate, which is what these exercise.
let mockNewerRuns: MilestoneRunView[] = [];
// What get-task answers with for the validation issue. Settable to undefined so a
// test can pin the case where the number exists but its url cannot be resolved —
// a GitHub read that failed is not the same state as "no issue yet", and the page
// is required to treat them alike.
let mockIssueUrl: string | undefined = "https://github.com/acme/demo/issues/30";

function run(over: {
  validation?: RunValidation;
  cycles?: MilestoneRunView["cycles"];
}): MilestoneRunView {
  return {
    id: "run-1",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind: "dev",
    origin: "spec-build",
    state: "succeeded",
    budgets: {
      cyclesTotal: 2,
      cycleCeiling: 8,
      fixCycles: 0,
      conflictCycles: 0,
      buildRetriggers: 0,
      validationCycles: 1,
    },
    validation: over.validation ?? {},
    cycles: over.cycles ?? [],
    createdAt: "2026-07-10T09:00:00Z",
  };
}

const validationCycle = {
  id: "cycle-2",
  kind: "validation" as const,
  attempts: 1,
  prNumber: 42,
  // The host's own page, as the webhook reported it. Deliberately NOT
  // `${repoUrl}/pull/42`: repoUrl is a clone URL, and this page used to compose
  // one from it — which 404s the moment the clone URL carries a `.git` suffix.
  prUrl: "https://github.com/acme/demo/pull/42",
  // The issue that FRAMED this attempt. A number only — the cycle record carries no
  // issue url, which is why the page has to ask get-task for one.
  validationIssue: 30,
  createdAt: "2026-07-10T10:00:00Z",
};

// `error` is widened because the page BRANCHES on it: the envelope's `not_found`
// means the oracle was never authored, which reads differently from a read that
// merely failed.
const mockCriteria = {
  isPending: false,
  isError: false,
  error: null as Error | null,
  refetch: vi.fn(),
  data: undefined as { content: string } | undefined,
};
const mockReport = {
  isPending: false,
  isError: false,
  error: null,
  refetch: vi.fn(),
  data: undefined as { content: string } | undefined,
};

// build.version is the NEWEST run's version and deploy.version the newest
// SUCCEEDED one; they differ for exactly as long as a run is live, which is the
// whole time validation is running. Both are settable so a test can pin that gap.
let mockBuildVersion = "v1";
let mockDeployVersion = "v1";

vi.mock("../../projects/api/queries", () => ({
  useProjectStatus: () => ({
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
    data: {
      repoUrl: "https://github.com/acme/demo",
      build: { version: mockBuildVersion, status: "running" },
      deploy: { version: mockDeployVersion, validation: mockValidation },
    },
  }),
}));

// The cancel mutation, spied so a test can assert WHICH run the button targets —
// only one run on a milestone can be live, and it is not necessarily the one
// answering for the version.
const mockCancelMutate = vi.fn();
let mockCancelError: Error | null = null;

vi.mock("../../builds/api/queries", () => ({
  useCancelRun: () => ({
    mutate: mockCancelMutate,
    isPending: false,
    isError: mockCancelError !== null,
    error: mockCancelError,
  }),
  // Models two things the real hook does, both of which a laxer mock would hide:
  // `enabled: Boolean(tag)` (no tag → the query never runs, so no data), and
  // per-version scoping (list-build-runs answers with THAT version's runs). The
  // run under test belongs to the newest version, so asking for any other tag
  // finds nothing — which is what makes "asked for the wrong version" visible.
  useBuildRuns: (_project: string, tag: string | undefined) => ({
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
    data: tag
      ? {
          tag,
          milestoneNumber: 1,
          runs:
            mockRun && tag === mockBuildVersion
              ? [...mockNewerRuns, mockRun]
              : [],
        }
      : undefined,
  }),
}));

// The validation issue's url, which no run record holds — get-task serves this one
// by number even though list-tasks hides it. The mock models the real hook's gate
// (`enabled: issueNumber > 0`): no number means no request and therefore no data,
// which is what makes "asked before an issue existed" visible rather than silently
// answered.
vi.mock("../../tasks/api/queries", () => ({
  useTask: (_project: string, issueNumber: number) => ({
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
    data:
      issueNumber > 0 && mockIssueUrl !== undefined
        ? { issueNumber, issueUrl: mockIssueUrl }
        : undefined,
  }),
}));

vi.mock("../api/queries", () => ({
  useValidationCriteria: () => mockCriteria,
  useValidationReport: () => mockReport,
}));

import { ValidationPage } from "./ValidationPage";
import { ApiRequestError } from "../../../api/errors";

const CRITERIA = JSON.stringify({
  requirements: [
    {
      id: "REQ-001",
      statement: "Shoppers can search the catalog.",
      criteria: [
        { id: "AC-001-a", must: "Search returns matches", method: "e2e" },
        { id: "AC-001-b", must: "Category filter works", method: "e2e" },
        { id: "AC-003-b", must: "Payment is encrypted", method: "manual" },
      ],
    },
  ],
});

const REPORT = JSON.stringify({
  criteria: [
    { id: "AC-001-a", status: "pass" },
    {
      id: "AC-001-b",
      status: "fail",
      spec: "tests/e2e/specs/AC-001-b.spec.ts",
      failure: "TimeoutError: category option never appeared",
    },
    { id: "AC-003-b", status: "manual" },
  ],
});

// A failure the reporter could locate but not describe: `location` with no
// `message`, and no `spec` path either. generate-report.mjs writes `failure` as
// { message, location } and the message can come back empty, so this is the one
// shape where the location is all the evidence there is.
const LOCATION_ONLY_FAILURE = JSON.stringify({
  criteria: [
    { id: "AC-001-a", status: "pass" },
    {
      id: "AC-001-b",
      status: "fail",
      failure: { message: "", location: "tests/e2e/specs/AC-001-b.spec.ts:42" },
    },
    { id: "AC-003-b", status: "manual" },
  ],
});

// A report where nothing produced a result — the shape behind `inconclusive`.
const NOTHING_RAN = JSON.stringify({
  criteria: [
    { id: "AC-001-a", status: "not_run" },
    { id: "AC-001-b", status: "not_run" },
    { id: "AC-003-b", status: "manual" },
  ],
});

function renderPage(view: "logs" | undefined, onViewChange = vi.fn()) {
  render(
    <ValidationPage
      projectName="acme"
      view={view}
      onViewChange={onViewChange}
    />,
  );
  return onViewChange;
}

afterEach(() => {
  mockValidation = "none";
  mockRun = undefined;
  mockNewerRuns = [];
  mockCancelMutate.mockClear();
  mockCancelError = null;
  mockBuildVersion = "v1";
  mockDeployVersion = "v1";
  mockCriteria.isPending = false;
  mockCriteria.isError = false;
  mockCriteria.error = null;
  mockCriteria.data = undefined;
  mockReport.isError = false;
  mockReport.data = undefined;
  mockLive = {};
  mockLiveActive = true;
  mockIssueUrl = "https://github.com/acme/demo/issues/30";
});

// A milestone sees SEQUENTIAL runs across its life and only some of them
// validate, so "the newest run" is not this page's subject — "the newest run that
// ASKED" is. These reproduce without any revalidation: an incident adoption alone
// was enough to erase a version's validation record.
// Cancel is the only expiry a run's unbounded wait has, and until now it was
// reachable only from the Builds rail — so a validation, which can hold an agent
// for up to two hours, had no stop button on the page that owns it.
describe("ValidationPage cancel", () => {
  it("offers cancel while a validation cycle is in flight", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({ cycles: [validationCycle] }),
        id: "run-live",
        kind: "validation",
        origin: "revalidate",
        state: "running",
      },
    ];

    renderPage(undefined);
    fireEvent.click(screen.getByRole("button", { name: /Cancel run/ }));

    // The LIVE run, not the one answering for the version: only one run on a
    // milestone can be live, and it need not be the one holding the verdict.
    expect(mockCancelMutate).toHaveBeenCalledWith("run-live");
  });

  // The repair loop is validation's, even though the cycle in flight is coding: the
  // run is only still alive because a criterion failed, and each repair is followed by
  // another attempt. That is the unbounded wait cancel exists for, and this is the page
  // that explains it — so the button belongs here rather than only on the Builds rail.
  it("offers cancel while the run repairs a failed validation", () => {
    mockValidation = "awaiting-fix";
    mockRun = {
      ...run({ validation: { verdict: "failed" }, cycles: [validationCycle] }),
      state: "running",
    };

    renderPage(undefined);
    fireEvent.click(screen.getByRole("button", { name: /Cancel run/ }));

    expect(mockCancelMutate).toHaveBeenCalledWith("run-1");
  });

  // The regression: liveness alone gated this button, and every run is live through
  // its coding cycles. A first delivery still writing code therefore offered to cancel
  // it from underneath "Nothing validated yet" — on the one page that has nothing
  // to say about the work being cancelled.
  it("hides cancel while the live run is still coding", () => {
    mockValidation = "none";
    mockRun = {
      ...run({
        cycles: [
          {
            id: "cycle-1",
            kind: "coding",
            attempts: 1,
            createdAt: "2026-07-10T09:14:00Z",
          },
        ],
      }),
      state: "running",
    };

    renderPage(undefined);

    expect(screen.getByText(/Nothing validated yet/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Cancel run/ }),
    ).not.toBeInTheDocument();
  });

  it("hides cancel once every run has settled", () => {
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };

    renderPage(undefined);

    expect(
      screen.queryByRole("button", { name: /Cancel run/ }),
    ).not.toBeInTheDocument();
  });

  // A 503 means the workflow engine was unreachable and NOTHING was cancelled,
  // so the failure has to say that rather than leave the reader assuming it took.
  it("surfaces a failed cancel and says nothing was cancelled", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({ cycles: [validationCycle] }),
        id: "run-live",
        kind: "validation",
        origin: "revalidate",
        state: "running",
      },
    ];
    mockCancelError = new Error("the workflow engine is unavailable");

    renderPage(undefined);

    expect(screen.getByText(/Nothing was cancelled/)).toBeInTheDocument();
  });
});

// The feed tests below ask for ?view=logs: a validating run with no verdict opens
// on its criteria now, so the log — which is what these are about — is the
// reader's second click rather than the default body.
describe("ValidationPage across a milestone's runs", () => {
  // The incident run never validates, and settle stamps `skipped` on a succeeded
  // run that never did. Reading the newest run therefore sent a version that had
  // PASSED to the "not validated" empty state, and stopped the report being
  // fetched at all.
  it("keeps the verdict when a later incident run never validated", () => {
    mockRun = run({
      validation: {
        verdict: "passed",
        reportPath: "tests/validation/report.md",
      },
      cycles: [validationCycle],
    });
    mockNewerRuns = [
      {
        ...run({ validation: { verdict: "skipped" } }),
        id: "run-incident",
        kind: "task",
        origin: "incident-adoption",
      },
    ];
    mockCriteria.data = { content: "{}" };
    mockReport.data = { content: "# report" };

    renderPage(undefined);

    expect(
      screen.queryByText(/This version was not validated/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/Nothing validated yet/),
    ).not.toBeInTheDocument();
  });

  // One feed per validating run, so the version reads as a chronology of attempts
  // rather than only its latest. A revalidation is a second run on the milestone,
  // and keying the feed to the newest run hid every earlier attempt's log.
  it("feeds every validating run, not just the newest", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({ cycles: [validationCycle] }),
        id: "run-revalidate",
        kind: "validation",
        origin: "revalidate",
      },
    ];

    renderPage("logs");

    expect(screen.getAllByTestId("run-feed")).toHaveLength(2);
  });

  // The counterpart: a run with no validation cycle contributes no feed, so an
  // incident adoption does not add an empty section to the version's story.
  it("gives a non-validating run no feed of its own", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({}),
        id: "run-incident",
        kind: "task",
        origin: "incident-adoption",
      },
    ];

    renderPage("logs");

    expect(screen.getAllByTestId("run-feed")).toHaveLength(1);
  });

  // The newest run leads, and the line between attempts is drawn at the RUN boundary —
  // where the Builds page draws its own — so the caption separates the run being read
  // from the ones before it rather than sitting above everything.
  it("draws the newest validating run first, with the earlier runs captioned below it", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({ cycles: [validationCycle] }),
        id: "run-revalidate",
        kind: "validation",
        origin: "revalidate",
      },
    ];

    renderPage("logs");

    // The whole arrangement in one assertion: presence alone would pass whichever
    // end the newest run were drawn at, which is the bug this replaces.
    const stack = screen.getAllByTestId("run-feed")[0]?.parentElement;
    const arrangement = Array.from(stack?.children ?? []).map((el) =>
      el.getAttribute("data-testid") === "run-feed"
        ? el.getAttribute("data-run-id")
        : el.textContent,
    );
    expect(arrangement).toEqual([
      "run-revalidate",
      "EARLIER VALIDATION RUNS",
      "run-1",
    ]);
  });

  // Exactly one log is open on the page, not one per feed: a settled attempt is a
  // record, and only the newest run's is still being written.
  it("lets only the newest run's feed open a log", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({ cycles: [validationCycle] }),
        id: "run-revalidate",
        kind: "validation",
        origin: "revalidate",
      },
    ];

    renderPage("logs");

    expect(
      screen
        .getAllByTestId("run-feed")
        .map((f) => f.getAttribute("data-expand-newest")),
    ).toEqual(["true", "false"]);
  });

  // Counted from the OLDEST validating run, so the newest carries the HIGHEST number
  // and the run count descends the page alongside each feed's cycle count. Counted over
  // the runs this page SHOWS: a run that never validated has no box here, so numbering
  // the milestone's whole list would leave gaps.
  it("numbers the validating runs from the oldest", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      {
        ...run({ cycles: [validationCycle] }),
        id: "run-revalidate",
        kind: "validation",
        origin: "revalidate",
      },
    ];

    renderPage("logs");

    expect(
      screen
        .getAllByTestId("run-feed")
        .map((f) => f.getAttribute("data-run-number")),
    ).toEqual(["2", "1"]);
  });

  // Unconditional, unlike the caption: a prefix that appeared only once a second run
  // existed would RENAME a box mid-session when a revalidation starts, and this page
  // polls while a version is live.
  it("numbers the run even when one run validated the version", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });

    renderPage("logs");

    expect(screen.getByTestId("run-feed")).toHaveAttribute(
      "data-run-number",
      "1",
    );
  });

  // The ordinary case: one run validated the version, so there is no history to
  // separate and a caption would announce a boundary that does not exist.
  it("draws no caption when a single run validated the version", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });

    renderPage("logs");

    expect(screen.getAllByTestId("run-feed")).toHaveLength(1);
    expect(
      screen.queryByText("EARLIER VALIDATION RUNS"),
    ).not.toBeInTheDocument();
  });
});

describe("ValidationPage lifecycle", () => {
  it("shows an empty state when the version's run never reached validation", () => {
    mockRun = run({});
    renderPage(undefined);
    expect(screen.getByText(/Nothing validated yet/)).toBeInTheDocument();
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
  });

  it("shows an empty state when the version has no run rows at all", () => {
    renderPage(undefined);
    expect(screen.getByText(/Nothing validated yet/)).toBeInTheDocument();
  });

  it("filters the log to the validation cycle while the run is validating", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    // ?view=logs, because a validating run now OPENS on its criteria; the log is
    // one button away rather than the only thing there is.
    renderPage("logs");
    // The feed streams the WHOLE run; the page filters it to the one phase it
    // owns, so a coding cycle's output never leaks onto the validation page.
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
  });

  // The regression: validation is the last cycle before a run settles, so while
  // it runs the run is still `running` and deploy.version — the newest SUCCEEDED
  // run's version — names nothing on a project's first version. Keyed on that,
  // the page found no run at all and claimed validation had not started, while
  // the header chip beside it read "Validating".
  it("finds the run mid-validation on a first version, when nothing has been delivered yet", () => {
    mockValidation = "running";
    mockDeployVersion = ""; // no spec-build run has SUCCEEDED yet
    mockBuildVersion = "v1"; // ...but v1's run is live and validating
    mockRun = run({ cycles: [validationCycle] });
    mockCriteria.data = { content: CRITERIA };

    renderPage(undefined);

    expect(
      screen.queryByText(/Nothing validated yet/),
    ).not.toBeInTheDocument();
    // The run was found, so the version's criteria are on screen under the tile
    // that says an attempt is under way.
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
  });

  // The same gap on a later build points the other way: deploy.version still
  // names the PREVIOUS version, so keying on it would show v1's settled report
  // under a chip announcing that v2 is validating.
  it("follows the newest run, not the last delivered version", () => {
    mockValidation = "running";
    mockDeployVersion = "v1"; // v1 is live in dev
    mockBuildVersion = "v2"; // v2's run is validating right now
    mockRun = run({ cycles: [validationCycle] });

    renderPage("logs");

    // The run story is fetched for v2 — the version the chip is talking about.
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    expect(
      screen.queryByText(/Nothing validated yet/),
    ).not.toBeInTheDocument();
  });

  it("shows the feed for a run whose validation failed", () => {
    mockValidation = "failed";
    mockRun = run({
      validation: { verdict: "failed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage("logs");
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    // A failed verdict still committed a report, so the toggle back exists.
    expect(screen.getByRole("button", { name: /View report/ })).toBeTruthy();
    // Chip AND tile — a verdict does not stop being true because the reader
    // switched to the log, so the tile shows over the feed too.
    expect(screen.getAllByText("Validation failed").length).toBe(2);
    expect(
      screen.getByText(/the milestone stays open for the fix/),
    ).toBeInTheDocument();
  });

  // The drift this page carried: the chip and the tile were both derived from the
  // run's stored verdict, which is a COLUMN with no lifecycle in it, so a run
  // mid-self-heal read "Validation failed … the milestone stays open for the fix"
  // here while the deployments board beside it correctly read "awaiting fix". The
  // tile's sentence was the worse half — the run had not stopped, it had filed the
  // failures as work and dispatched a coding cycle.
  it("reads as a repair in flight, not a stopped run, while the loop is healing", () => {
    mockValidation = "awaiting-fix";
    mockRun = {
      ...run({
        validation: {
          verdict: "failed",
          reportPath: "tests/validation/report.json",
        },
        cycles: [
          validationCycle,
          { ...validationCycle, id: "cycle-3", kind: "coding" },
        ],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // Chip AND tile headline, both from the shared mapper.
    expect(screen.getAllByText("Awaiting fix").length).toBe(2);
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
    expect(
      screen.queryByText(/the milestone stays open for the fix/),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "1 of 3 criteria failed. The implementation is being fixed. Validation will run again.",
      ),
    ).toBeInTheDocument();
    // The failed report stays — it is the evidence of WHAT is being fixed, and the
    // coding cycle in flight has no validation log to show in its place.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/category option never appeared/),
    ).toBeInTheDocument();
  });

  // A repeat attempt has to read like the first one. This was unreachable while the
  // page had no lifecycle input: a second attempt runs with a verdict already on the
  // row, so the page opened on the PREVIOUS attempt's report under a tile claiming
  // the run had stopped.
  it("opens on the last attempt's report while a repeat attempt runs", () => {
    mockValidation = "running";
    mockRun = {
      ...run({
        validation: {
          verdict: "failed",
          reportPath: "tests/validation/report.json",
        },
        cycles: [validationCycle],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // The previous attempt's report is real, and it is what the reader wants while
    // the fix is being re-checked. That it belongs to the last attempt is the tile's
    // job to say — twice, in the sentence and in the tally.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
    // Chip and tile headline both, as with every other state.
    expect(screen.getAllByText("Validating").length).toBe(2);
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "1 of 3 criteria failed in the last attempt. The implementation has been fixed and deployed. Validation is running again.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/\(last attempt\)$/)).toBeInTheDocument();
    // And NOT the first-attempt view: real results beat "Pending" everywhere.
    expect(screen.queryByText("Pending")).not.toBeInTheDocument();
  });

  // The regression this replaced a default with: no state may FORCE a body, because
  // `?view=logs | absent` has no third value, so `onViewChange(undefined)` cannot
  // outrank a forced arm and the "View report" button silently does nothing.
  it("keeps the report/log toggle working while a repeat attempt runs", () => {
    mockValidation = "running";
    mockRun = {
      ...run({
        validation: {
          verdict: "failed",
          reportPath: "tests/validation/report.json",
        },
        cycles: [validationCycle],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };

    const onViewChange = renderPage("logs");

    // ?view=logs is honoured...
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    // ...and the way back is offered and lands on the report.
    fireEvent.click(screen.getByRole("button", { name: /View report/ }));
    expect(onViewChange).toHaveBeenCalledWith(undefined);
  });

  it("says so, and shows nothing else, when the run SKIPPED validation", () => {
    mockRun = run({ validation: { verdict: "skipped" } });
    renderPage(undefined);
    expect(screen.getByText(/was not validated/)).toBeInTheDocument();
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
  });

  it("renders the joined report on a passed verdict", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: {
        verdict: "passed",
        reportPath: "tests/validation/report.json",
      },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // The report, not the log.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
    // Per-criterion state chips from the join.
    expect(screen.getByText("Passed")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("Manual")).toBeInTheDocument();
    // Rich failure detail for the failing e2e criterion.
    expect(
      screen.getByText(/category option never appeared/),
    ).toBeInTheDocument();
  });

  it("stamps the run's verdict on the header, not the coarse lifecycle", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);
    // The header chip and the tile headline, both from the shared mapper — which
    // is why they read identically rather than being written twice.
    expect(screen.getAllByText("Validated").length).toBe(2);
  });

  // The three verdicts this page was blind to. It used to map the verdict with a
  // second, builds-local mapper that knew only passed/failed/skipped, so each of
  // these produced no chip — which made `settled` false and pinned the page to the
  // run log feed with no report, for the outcome any project with a manual
  // criterion lands on. Hence one shared mapper.
  it("renders the report, not the feed, for a PARTIAL verdict", () => {
    mockValidation = "partial";
    mockRun = run({
      validation: {
        verdict: "partial",
        reportPath: "tests/validation/report.json",
      },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    // The mark is the hedge, on both the chip and the tile headline.
    expect(screen.getAllByText("Validated*").length).toBe(2);
    expect(screen.queryByText("Validated")).not.toBeInTheDocument();
    // The chip stands alone at the top of the page, so it carries the spoken form —
    // a screen reader hears nothing of the asterisk otherwise. Visually-hidden TEXT,
    // because a Chip with no onClick has no role and would ignore an aria-label.
    expect(screen.getByText("Validated, partially")).toBeInTheDocument();
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
  });

  it("renders the report for an INCONCLUSIVE verdict", () => {
    mockValidation = "inconclusive";
    mockRun = run({
      validation: {
        verdict: "inconclusive",
        reportPath: "tests/validation/report.json",
      },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: NOTHING_RAN };
    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(screen.getAllByText("Validation?").length).toBe(2);
    expect(
      screen.getByText(/please validate them manually/),
    ).toBeInTheDocument();
  });

  // `unreported` means no report was committed at that commit, and the server
  // omits reportPath for it — so the tile carries the cause and the vague
  // "wasn't found" note stays out of the way.
  it("explains an UNREPORTED verdict over criteria-only, with no soft note", () => {
    mockValidation = "unreported";
    mockRun = run({
      validation: { verdict: "unreported" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(screen.getAllByText("Validation error").length).toBe(2);
    expect(
      screen.getByText(/validation report couldn't be generated/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/report wasn't found/)).not.toBeInTheDocument();
    // The criteria still render — they live under specs/, not in the report.
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
    // And with no report there is nothing to count.
    expect(screen.queryByText(/\d+ passed/)).not.toBeInTheDocument();
  });

  // The counts moved out of ValidationView and into the tile, so the page carries
  // exactly one tally rather than the same numbers twice.
  it("tallies the run's outcome once, in the tile", () => {
    mockValidation = "failed";
    mockRun = run({
      validation: {
        verdict: "failed",
        reportPath: "tests/validation/report.json",
      },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // 3 criteria: one pass, one fail, one manual.
    expect(
      screen.getByText("1 failed · 1 passed · 1 manual"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Passed 1")).not.toBeInTheDocument();
    expect(screen.queryByText("Failed 1")).not.toBeInTheDocument();
  });

  it("links the validation cycle's PR, learned from the cycle record", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);
    expect(
      screen.getByRole("link", { name: /Validation pull request #42/ }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/42");
  });

  // The cycle holds the issue NUMBER and nothing else, so the url is asked of
  // get-task — which serves this issue despite list-tasks hiding it — rather than
  // composed from the project's repoUrl, for the same reason the PR link isn't.
  it("links the validation issue, resolved by number rather than composed", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);
    expect(
      screen.getByRole("link", { name: /Validation issue #30/ }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/issues/30");
  });

  it("shows no issue link before a cycle has minted one", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      // 0 is what the wire carries before a cycle mints an issue — the field is
      // omitempty, so a run that never validated says 0, not "absent".
      cycles: [{ ...validationCycle, validationIssue: 0 }],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);
    expect(
      screen.queryByRole("link", { name: /Validation issue #30/ }),
    ).toBeNull();
    // The pull request beside it is unaffected — the two links are independent.
    expect(
      screen.getByRole("link", { name: /Validation pull request #42/ }),
    ).toBeInTheDocument();
  });

  // A GitHub read that failed leaves the number in hand and no url. The page shows
  // nothing rather than a link it cannot aim, which is the PR's rule too.
  it("shows no issue link when the issue url could not be resolved", () => {
    mockValidation = "passed";
    mockIssueUrl = undefined;
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);
    expect(
      screen.queryByRole("link", { name: /Validation issue #30/ }),
    ).toBeNull();
  });

  it("toggles to the log view via the View logs button", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    const onViewChange = renderPage(undefined);

    fireEvent.click(screen.getByRole("button", { name: /View logs/ }));
    expect(onViewChange).toHaveBeenCalledWith("logs");
  });

  it("shows the feed (and a View report button) when ?view=logs", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    const onViewChange = renderPage("logs");

    expect(screen.getByTestId("run-feed")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /View report/ }));
    expect(onViewChange).toHaveBeenCalledWith(undefined);
  });

  // A reporter can hand back a location with no message and no spec path —
  // report.ts parses exactly that shape ("keeps a location even when the failure
  // carries no message"), so the only pointer to the failing assertion the run
  // produced must survive the render gate rather than be dropped with it.
  it("renders a failure that carries only a location", () => {
    mockValidation = "failed";
    mockRun = run({
      validation: {
        verdict: "failed",
        reportPath: "tests/validation/report.json",
      },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: LOCATION_ONLY_FAILURE };
    renderPage(undefined);

    expect(
      screen.getByText("tests/e2e/specs/AC-001-b.spec.ts:42"),
    ).toBeInTheDocument();
  });

  it("falls back to criteria-only with a note when the report is missing", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.isError = true;
    renderPage(undefined);

    expect(screen.getByText(/report wasn't found/)).toBeInTheDocument();
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
    // No state chips without a report.
    expect(screen.queryByText("Passed")).not.toBeInTheDocument();
  });
});

// The method badge is the reader's only signal for who checks a criterion, and it
// used to render the wire value verbatim — `E2E`, an acronym the lexicon forbids
// and nothing in the product expanded. The wire value cannot change (the runner,
// the report generator and the tests/e2e/specs/<AC-ID>.spec.ts path all key on
// it), so the display name is the thing under test here.
describe("ValidationPage criterion method badges", () => {
  function renderWithCriteria() {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed", reportPath: "tests/validation/report.json" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);
  }

  it("says auto rather than the e2e wire value", () => {
    renderWithCriteria();

    // Two e2e criteria in the fixture, plus the summary tally's own badge.
    expect(screen.getAllByText("auto")).toHaveLength(2);
    expect(screen.queryByText("e2e")).not.toBeInTheDocument();
    expect(screen.getByText("auto 2")).toBeInTheDocument();
  });

  it("leaves the manual badge alone", () => {
    renderWithCriteria();

    expect(screen.getByText("manual")).toBeInTheDocument();
    expect(screen.getByText("manual 1")).toBeInTheDocument();
  });

  it("explains each method on hover", async () => {
    renderWithCriteria();

    fireEvent.mouseOver(screen.getAllByText("auto")[0]!);
    expect(
      await screen.findByText("Validated automatically by the agent."),
    ).toBeInTheDocument();

    fireEvent.mouseOver(screen.getByText("manual"));
    expect(
      await screen.findByText("Requires manual validation."),
    ).toBeInTheDocument();
  });

  // The description explaining what criteria ARE belongs to the Spec view, where
  // a reader meets the document cold. Here they arrived to read run results, so a
  // sentence telling them results appear under Validations would be redundant on
  // the page holding them. Gated by `hideDescription`, asserted so the gate cannot
  // be dropped silently.
  it("omits the spec view's explanation", () => {
    renderWithCriteria();

    expect(screen.queryByText(/Each criterion represents/)).not.toBeInTheDocument();
  });
});

// A version's FIRST attempt has no verdict, no report and — until now — nothing on
// the page but a log. The oracle is what there is to show, and it says the two
// things a reader in that state wants: what is being checked, and what the agent is
// never going to check for them.
describe("ValidationPage first attempt in flight", () => {
  function runningFirstAttempt() {
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
  }

  it("opens on the criteria, not the log", () => {
    runningFirstAttempt();
    mockCriteria.data = { content: CRITERIA };

    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Shoppers can search the catalog."),
    ).toBeInTheDocument();
    // Chip and tile headline both, as with every other state.
    expect(screen.getAllByText("Validating").length).toBe(2);
    // Read through the tile's own element rather than by text: the two method names
    // are marked up as terms, so no single text node holds the whole sentence. Their
    // text is the vocabulary's lowercase label — the uppercase is CSS, exactly as on
    // the badges.
    expect(screen.getByRole("alert")).toHaveTextContent(
      "auto criteria are being validated end to end against the deployed system. Please validate the manual criteria yourself.",
    );
    // Counted off the ORACLE — there is no report to count — and in the same words
    // the badges below use.
    expect(screen.getByText("2 auto · 1 manual")).toBeInTheDocument();
  });

  // The point of the chips: a manual criterion is not queued behind the agent, it is
  // queued behind the reader, and "Pending" on it would promise a result nobody is
  // going to produce.
  it("chips each criterion with what is about to happen to it", () => {
    runningFirstAttempt();
    mockCriteria.data = { content: CRITERIA };

    renderPage(undefined);

    expect(screen.getAllByText("Pending")).toHaveLength(2);
    expect(screen.getByText("Manual")).toBeInTheDocument();
  });

  it("keeps the log one click away, and the way back from it", () => {
    runningFirstAttempt();
    mockCriteria.data = { content: CRITERIA };

    const onViewChange = renderPage(undefined);
    fireEvent.click(screen.getByRole("button", { name: /View logs/ }));
    expect(onViewChange).toHaveBeenCalledWith("logs");

    renderPage("logs");
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    expect(
      screen.getAllByRole("button", { name: /View report/ }).length,
    ).toBeGreaterThan(0);
  });

  // The tile's second sentence is an instruction. Pointing a reader at manual
  // criteria that do not exist sends them looking for an empty list.
  it("asks for nothing by hand when every criterion is automated", () => {
    runningFirstAttempt();
    mockCriteria.data = {
      content: JSON.stringify({
        requirements: [
          {
            id: "REQ-001",
            statement: "Shoppers can search the catalog.",
            criteria: [
              { id: "AC-001-a", must: "Search returns matches", method: "e2e" },
            ],
          },
        ],
      }),
    };

    renderPage(undefined);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "auto criteria are being validated end to end against the deployed system.",
    );
    expect(screen.getByRole("alert")).not.toHaveTextContent(/Please validate/);
    expect(screen.getByText("1 auto")).toBeInTheDocument();
  });

  // `not_found` is the Files API's answer for a version whose spec authored no
  // criteria — the state its run eventually settles as `skipped`. The tile is then
  // the whole body: there is nothing to list under it.
  it("says the version has no criteria when the read comes back not_found", () => {
    runningFirstAttempt();
    mockCriteria.isError = true;
    mockCriteria.error = new ApiRequestError(
      { code: "not_found", message: "no spec file at validation-criteria.json" },
      "Failed to load",
    );

    renderPage(undefined);

    expect(
      screen.getByText(
        "This version has no validation criteria, so there is nothing to check the deployment against.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/Failed to load the validation criteria/),
    ).not.toBeInTheDocument();
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    // The log is still reachable — the header reads the same in both running shapes.
    expect(
      screen.getByRole("button", { name: /View logs/ }),
    ).toBeInTheDocument();
  });

  // The other half of that branch, and the reason it is keyed on the envelope's
  // code: a read that merely FAILED must not be reported as a spec that authored
  // nothing.
  it("offers a retry when the criteria read merely failed", () => {
    runningFirstAttempt();
    mockCriteria.isError = true;
    mockCriteria.error = new ApiRequestError(
      { code: "internal", message: "upstream unavailable" },
      "Failed to load",
    );

    renderPage(undefined);

    expect(
      screen.getByText(/Failed to load the validation criteria/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(
      screen.queryByText(/no validation criteria/),
    ).not.toBeInTheDocument();
  });
});

// Until now this page could say what a validation WOULD check and what it
// eventually FOUND, and nothing at all in between — every row read "Pending" for
// up to two hours (the validation cycle's deadline), or worse, held the previous
// attempt's verdict while a new attempt re-worked exactly those criteria.
describe("ValidationPage live per-criterion progress", () => {
  it("says what the run is doing while no criterion has been picked up yet", () => {
    // SKILL.md steps 1-5 — reading the issue, cutting the branch, scaffolding
    // tests/e2e. The rows cannot narrate it because none of them has been
    // touched, and this is the stretch that most looks like a dead run.
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockCriteria.data = { content: CRITERIA };
    renderPage(undefined);

    expect(screen.getByText("Setting up the test harness…")).toBeInTheDocument();
  });

  it("stops narrating the run once the rows can speak for themselves", () => {
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockCriteria.data = { content: CRITERIA };
    mockLive = { "AC-001-a": "exploring" };
    renderPage(undefined);

    expect(screen.queryByText("Setting up the test harness…")).not.toBeInTheDocument();
    expect(screen.getByText("Exploring…")).toBeInTheDocument();
    // Untouched auto criteria still read Pending; a manual one never will.
    expect(screen.getAllByText("Pending").length).toBe(1);
    expect(screen.getByText("Manual")).toBeInTheDocument();
  });

  it("shows what a REPEAT attempt is re-working, not the last attempt's verdict", () => {
    // The freeze this fixes: a repair run re-works precisely the criteria that
    // failed, and the page showed those rows stuck on `Failed` for the whole
    // two hours it took. ValidationPage's own comment argued the previous report
    // beat a wall of Pending chips — true, and beside the point once a row can
    // say what is happening to it right now.
    mockValidation = "running";
    mockRun = {
      ...run({
        validation: { verdict: "failed", reportPath: "tests/validation/report.json" },
        // A SECOND validation cycle, still open: the repeat attempt in flight.
        cycles: [validationCycle, { ...validationCycle, id: "cycle-9" }],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    mockLive = { "AC-001-b": "authoring" };
    renderPage(undefined);

    expect(screen.getByText("Authoring…")).toBeInTheDocument();
    // AC-001-b was the failure in REPORT; its chip is gone, replaced by the live
    // status. AC-001-a was not touched, so it keeps last attempt's result.
    expect(screen.queryByText("Failed")).not.toBeInTheDocument();
    expect(screen.getByText("Passed")).toBeInTheDocument();
  });

  it("renders a live pass with report.json's own chip, not a live one", () => {
    // `pass` and `fail` are report.json's words and arrive on the feed too. A
    // criterion that has passed reads the same whichever brought the news,
    // because it is the same fact.
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockCriteria.data = { content: CRITERIA };
    mockLive = { "AC-001-a": "pass", "AC-001-b": "healing" };
    renderPage(undefined);

    expect(screen.getByText("Passed")).toBeInTheDocument();
    expect(screen.getByText("Healing…")).toBeInTheDocument();
  });

  it("narrates the reporting tail, when every row is settled and nothing moves", () => {
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockCriteria.data = { content: CRITERIA };
    // Both auto criteria answered; the manual one never moves and must not hold
    // the line back.
    mockLive = { "AC-001-a": "pass", "AC-001-b": "fail" };
    renderPage(undefined);

    expect(screen.getByText("Writing the validation report…")).toBeInTheDocument();
  });
});

describe("ValidationPage live note is gated on an open cycle", () => {
  it("says nothing over a settled verdict whose report was never fetched", () => {
    // `unreported` settles the run AND skips the report read, so the page has no
    // statuses and no report — which read identically to a run that had not
    // started, and announced "Setting up the test harness…" over a finished one.
    mockValidation = "unreported";
    mockRun = run({
      validation: { verdict: "unreported" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockLive = {};
    mockLiveActive = false;
    renderPage(undefined);

    expect(screen.queryByText("Setting up the test harness…")).not.toBeInTheDocument();
  });

  it("says nothing while a repair cycle is writing code", () => {
    // The newest validation cycle is the PREVIOUS attempt's, already closed, so
    // nothing is validating even though the run is live.
    mockValidation = "awaiting-fix";
    mockRun = {
      ...run({
        validation: { verdict: "failed", reportPath: "tests/validation/report.json" },
        cycles: [validationCycle, { ...validationCycle, id: "cycle-3", kind: "coding" }],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    mockLive = {};
    mockLiveActive = false;
    renderPage(undefined);

    expect(screen.queryByText("Setting up the test harness…")).not.toBeInTheDocument();
    // The previous attempt's evidence still stands.
    expect(screen.getByText(/category option never appeared/)).toBeInTheDocument();
  });
});

// A run reports `planned` for EVERY criterion in the test plan, and SKILL.md has
// the agent write a plan section per criterion — manual ones included. So the
// feed carries a status for a criterion no agent will ever work, and the row used
// to render it: the method badge read `manual` while the chip beside it read
// "Planned", for the whole run, with nothing able to supersede it.
describe("ValidationPage manual criteria ignore the live feed", () => {
  it("keeps a manual criterion on Manual when the feed reports it planned", () => {
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockCriteria.data = { content: CRITERIA };
    // AC-003-b is the `manual` criterion in CRITERIA; AC-001-a is `e2e`.
    mockLive = { "AC-001-a": "planned", "AC-003-b": "planned" };
    renderPage(undefined);

    expect(screen.getByText("Manual")).toBeInTheDocument();
    // Only the e2e row may say Planned — the manual one must not.
    expect(screen.getAllByText("Planned")).toHaveLength(1);
  });

  it("keeps it on Manual through every live status, not just planned", () => {
    // Nothing else is reachable for a manual criterion today, but the rule is
    // about the METHOD rather than about which status happened to arrive.
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockCriteria.data = { content: CRITERIA };
    mockLive = { "AC-003-b": "running" };
    renderPage(undefined);

    expect(screen.getByText("Manual")).toBeInTheDocument();
    expect(screen.queryByText("Running…")).not.toBeInTheDocument();
  });

  it("still says Manual when no report was fetched and nothing is awaiting", () => {
    // `unreported` settles the run and skips the report read, so `awaiting` is
    // false and there is no report — the two branches that used to carry a manual
    // row. Before the guard it fell through to nothing and rendered no chip.
    mockValidation = "unreported";
    mockRun = run({
      validation: { verdict: "unreported" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockLive = { "AC-001-a": "authoring" };
    renderPage(undefined);

    expect(screen.getByText("Manual")).toBeInTheDocument();
    // Asserted negatively too: the method BADGE also carries the word "manual",
    // so presence alone would still pass if the chip regressed to something else
    // and only the badge matched. Neither of the two chips it could wrongly show
    // here may appear on any row.
    expect(screen.queryByText("Pending")).not.toBeInTheDocument();
    expect(screen.queryByText("Planned")).not.toBeInTheDocument();
  });
});
