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
vi.mock("../../builds/components/RunFeed", () => ({
  RunFeed: ({ cycleKinds }: { cycleKinds?: readonly string[] }) => (
    <div data-testid="run-feed">{(cycleKinds ?? []).join(",")}</div>
  ),
}));

// Controllable status + runs + file queries (no QueryClientProvider / MSW).
let mockValidation = "none";
let mockRun: MilestoneRunView | undefined;
// Runs NEWER than mockRun on the same milestone, newest first — list-build-runs
// answers newest-first, so these sit ahead of it. A milestone accumulates runs
// across its life and only some of them validate, which is what these exercise.
let mockNewerRuns: MilestoneRunView[] = [];

function run(over: {
  validation?: RunValidation;
  cycles?: MilestoneRunView["cycles"];
}): MilestoneRunView {
  return {
    id: "run-1",
    milestoneNumber: 1,
    milestoneTitle: "v1",
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
  createdAt: "2026-07-10T10:00:00Z",
};

const mockCriteria = {
  isPending: false,
  isError: false,
  error: null,
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

vi.mock("../api/queries", () => ({
  useValidationCriteria: () => mockCriteria,
  useValidationReport: () => mockReport,
}));

import { ValidationPage } from "./ValidationPage";

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
  mockCriteria.data = undefined;
  mockReport.isError = false;
  mockReport.data = undefined;
});

// A milestone sees SEQUENTIAL runs across its life and only some of them
// validate, so "the newest run" is not this page's subject — "the newest run that
// ASKED" is. These reproduce without any revalidation: an incident adoption alone
// was enough to erase a version's validation record.
// Cancel is the only expiry a run's unbounded wait has, and until now it was
// reachable only from the Builds rail — so a validation, which can hold an agent
// for up to two hours, had no stop button on the page that owns it.
describe("ValidationPage cancel", () => {
  it("offers cancel while a run is live", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      { ...run({ cycles: [validationCycle] }), id: "run-live", origin: "revalidate", state: "running" },
    ];

    renderPage(undefined);
    fireEvent.click(screen.getByRole("button", { name: /Cancel run/ }));

    // The LIVE run, not the one answering for the version: only one run on a
    // milestone can be live, and it need not be the one holding the verdict.
    expect(mockCancelMutate).toHaveBeenCalledWith("run-live");
  });

  it("hides cancel once every run has settled", () => {
    mockRun = run({ validation: { verdict: "passed" }, cycles: [validationCycle] });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };

    renderPage(undefined);

    expect(screen.queryByRole("button", { name: /Cancel run/ })).not.toBeInTheDocument();
  });

  // A 503 means the workflow engine was unreachable and NOTHING was cancelled,
  // so the failure has to say that rather than leave the reader assuming it took.
  it("surfaces a failed cancel and says nothing was cancelled", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    mockNewerRuns = [
      { ...run({ cycles: [validationCycle] }), id: "run-live", origin: "revalidate", state: "running" },
    ];
    mockCancelError = new Error("the workflow engine is unavailable");

    renderPage(undefined);

    expect(screen.getByText(/Nothing was cancelled/)).toBeInTheDocument();
  });
});

describe("ValidationPage across a milestone's runs", () => {
  // The incident run never validates, and settle stamps `skipped` on a succeeded
  // run that never did. Reading the newest run therefore sent a version that had
  // PASSED to the "not validated" empty state, and stopped the report being
  // fetched at all.
  it("keeps the verdict when a later incident run never validated", () => {
    mockRun = run({
      validation: { verdict: "passed", reportPath: "tests/validation/report.md" },
      cycles: [validationCycle],
    });
    mockNewerRuns = [
      {
        ...run({ validation: { verdict: "skipped" } }),
        id: "run-incident",
        origin: "incident-adoption",
      },
    ];
    mockCriteria.data = { content: "{}" };
    mockReport.data = { content: "# report" };

    renderPage(undefined);

    expect(screen.queryByText(/This version was not validated/)).not.toBeInTheDocument();
    expect(screen.queryByText(/No validation has run yet/)).not.toBeInTheDocument();
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
        origin: "revalidate",
      },
    ];

    renderPage(undefined);

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
        origin: "incident-adoption",
      },
    ];

    renderPage(undefined);

    expect(screen.getAllByTestId("run-feed")).toHaveLength(1);
  });
});

describe("ValidationPage lifecycle", () => {
  it("shows an empty state when the version's run never reached validation", () => {
    mockRun = run({});
    renderPage(undefined);
    expect(screen.getByText(/No validation has run yet/)).toBeInTheDocument();
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
  });

  it("shows an empty state when the version has no run rows at all", () => {
    renderPage(undefined);
    expect(screen.getByText(/No validation has run yet/)).toBeInTheDocument();
  });

  it("shows the validation cycle's feed while the run is validating", () => {
    mockValidation = "running";
    mockRun = run({ cycles: [validationCycle] });
    renderPage(undefined);
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

    renderPage(undefined);

    expect(screen.queryByText(/No validation has run yet/)).not.toBeInTheDocument();
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
  });

  // The same gap on a later build points the other way: deploy.version still
  // names the PREVIOUS version, so keying on it would show v1's settled report
  // under a chip announcing that v2 is validating.
  it("follows the newest run, not the last delivered version", () => {
    mockValidation = "running";
    mockDeployVersion = "v1"; // v1 is live in dev
    mockBuildVersion = "v2"; // v2's run is validating right now
    mockRun = run({ cycles: [validationCycle] });

    renderPage(undefined);

    // The run story is fetched for v2 — the version the chip is talking about.
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    expect(screen.queryByText(/No validation has run yet/)).not.toBeInTheDocument();
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
        validation: { verdict: "failed", reportPath: "tests/validation/report.json" },
        cycles: [validationCycle, { ...validationCycle, id: "cycle-3", kind: "coding" }],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // Chip AND tile headline, both from the shared mapper.
    expect(screen.getAllByText("Awaiting fix").length).toBe(2);
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
    expect(screen.queryByText(/the milestone stays open for the fix/)).not.toBeInTheDocument();
    expect(
      screen.getByText(/The run filed each failure as an issue on this version/),
    ).toBeInTheDocument();
    // The failed report stays — it is the evidence of WHAT is being fixed, and the
    // coding cycle in flight has no validation log to show in its place.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(screen.getByText("Shoppers can search the catalog.")).toBeInTheDocument();
    expect(screen.getByText(/category option never appeared/)).toBeInTheDocument();
  });

  // A repeat attempt has to read like the first one. This was unreachable while the
  // page had no lifecycle input: a second attempt runs with a verdict already on the
  // row, so the page opened on the PREVIOUS attempt's report under a tile claiming
  // the run had stopped.
  it("shows the live log, not the last attempt's report, while a repeat attempt runs", () => {
    mockValidation = "running";
    mockRun = {
      ...run({
        validation: { verdict: "failed", reportPath: "tests/validation/report.json" },
        cycles: [validationCycle],
      }),
      state: "running",
    };
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    // Chip and tile headline both, as with every other state.
    expect(screen.getAllByText("Validating").length).toBe(2);
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
    // The tile rides over the log — the last attempt's finding is still true — but
    // ends on the attempt in flight rather than on a run that stopped.
    expect(screen.getByText(/A new validation attempt is running/)).toBeInTheDocument();
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
      validation: { verdict: "passed", reportPath: "tests/validation/report.json" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // The report, not the log.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(screen.getByText("Shoppers can search the catalog.")).toBeInTheDocument();
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
      validation: { verdict: "partial", reportPath: "tests/validation/report.json" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    // Partial shares `passed`'s visible label since #401; the tile's copy and
    // the spoken form carry the uncovered-criteria distinction.
    expect(screen.getAllByText("Validated").length).toBe(2);
    expect(screen.getByText("Shoppers can search the catalog.")).toBeInTheDocument();
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
    expect(screen.getByText(/please validate them manually/)).toBeInTheDocument();
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
      screen.getByText(/generating the validation report/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/report wasn't found/)).not.toBeInTheDocument();
    // The criteria still render — they live under specs/, not in the report.
    expect(screen.getByText("Shoppers can search the catalog.")).toBeInTheDocument();
    // And with no report there is nothing to count.
    expect(screen.queryByText(/\d+ passed/)).not.toBeInTheDocument();
  });

  // The counts moved out of ValidationView and into the tile, so the page carries
  // exactly one tally rather than the same numbers twice.
  it("tallies the run's outcome once, in the tile", () => {
    mockValidation = "failed";
    mockRun = run({
      validation: { verdict: "failed", reportPath: "tests/validation/report.json" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // 3 criteria: one pass, one fail, one manual.
    expect(screen.getByText("1 failed · 1 passed · 1 manual")).toBeInTheDocument();
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
      screen.getByRole("link", { name: /Validation pull request/ }),
    ).toHaveAttribute("href", "https://github.com/acme/demo/pull/42");
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
      validation: { verdict: "failed", reportPath: "tests/validation/report.json" },
      cycles: [validationCycle],
    });
    mockCriteria.data = { content: CRITERIA };
    mockReport.data = { content: LOCATION_ONLY_FAILURE };
    renderPage(undefined);

    expect(screen.getByText("tests/e2e/specs/AC-001-b.spec.ts:42")).toBeInTheDocument();
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
    expect(screen.getByText("Shoppers can search the catalog.")).toBeInTheDocument();
    // No state chips without a report.
    expect(screen.queryByText("Passed")).not.toBeInTheDocument();
  });
});
