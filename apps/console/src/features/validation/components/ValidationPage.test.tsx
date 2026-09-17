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

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
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
// The validation issue's comment thread — the status line lives in the NEWEST
// one. Oldest first, matching the contract. `observed` marks a line the PLATFORM
// posted from what it watched the run do, which is most of them.
let mockIssueComments: { id: string; body: string; observed?: boolean }[] = [];
// Whether the page asked get-task to poll. This read is GitHub-backed, so an
// idle version must cost nothing and a live one must not go stale.
let mockIssueLive: boolean | undefined;

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

// The oracle is a SET of files now, so the hook reports its own absence rather
// than the page reading a 404 off one read: `isAbsent` means the version authored
// no feature files, which reads differently from a read that merely failed.
const mockFeatures = {
  features: [] as { path: string; content: string }[],
  isPending: false,
  isError: false,
  isAbsent: false,
  refetch: vi.fn(),
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
  useTask: (
    _project: string,
    issueNumber: number,
    opts: { live?: boolean } = {},
  ) => {
    mockIssueLive = opts.live;
    return {
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
      data:
        issueNumber > 0 && mockIssueUrl !== undefined
          ? {
              issueNumber,
              issueUrl: mockIssueUrl,
              ...(mockIssueComments.length > 0
                ? {
                    comments: mockIssueComments.map((c) => ({
                      ...c,
                      author: "aep-bot",
                      createdAt: "2026-09-04T10:00:00Z",
                      url: `${mockIssueUrl}#issuecomment-${c.id}`,
                    })),
                  }
                : {}),
            }
          : undefined,
    };
  },
}));

vi.mock("../api/queries", () => ({
  useAcceptanceFeatures: () => mockFeatures,
  useValidationReport: () => mockReport,
}));

import { ValidationPage } from "./ValidationPage";

const CATALOG_FEATURE = {
  path: "specs/acceptance/browsing-the-catalog.feature",
  content: [
    "Feature: Browsing the catalog",
    "",
    "  @story-1",
    "  Rule: A shopper can find a product by name",
    "",
    "    Scenario: Searching for a product by name",
    '      Given the catalog has a product named "Cedar Desk Lamp"',
    '      When Priya searches for "Cedar Desk"',
    '      Then the results include "Cedar Desk Lamp"',
    "",
    "    @negative",
    "    Scenario: A search that matches nothing explains itself",
    '      Given the catalog has no product named "Zeppelin"',
    '      When Priya searches for "Zeppelin"',
    "      Then she is shown that nothing matched, and no products",
  ].join("\n"),
};

const CHECKOUT_FEATURE = {
  path: "specs/acceptance/checkout.feature",
  content: [
    "Feature: Checkout",
    "",
    "  @story-3",
    "  Rule: Payment details are transmitted over an encrypted connection",
    "",
    "    Scenario: The payment step is encrypted",
    "      Given Priya is at the payment step",
    "      When she submits her card details",
    "      Then the details leave the browser encrypted",
  ].join("\n"),
};

const FEATURES = [CATALOG_FEATURE, CHECKOUT_FEATURE];

const CATALOG = "Browsing the catalog";
const SEARCH_RULE = "A shopper can find a product by name";

function scenarioEntry(
  over: Record<string, unknown> & { scenario: string; outcome: string },
): Record<string, unknown> {
  return {
    feature: CATALOG,
    featureFile: CATALOG_FEATURE.path,
    rule: SEARCH_RULE,
    steps: [],
    ...over,
  };
}

// One of each outcome the page has to keep apart: a pass, a real defect, and a
// scenario whose answer lives outside the running app.
const REPORT = JSON.stringify({
  schemaVersion: 2,
  commit: "a1b2c3d4e5f6",
  isolation: "Each scenario creates the cart it asserts on.",
  scenarios: [
    scenarioEntry({
      scenario: "Searching for a product by name",
      outcome: "passed",
      steps: [
        {
          text: 'the results include "Cedar Desk Lamp"',
          keyword: "Then",
          command: 'agent-browser wait --text "Cedar Desk Lamp" --timeout 3000',
          exit: 0,
        },
      ],
    }),
    scenarioEntry({
      scenario: "A search that matches nothing explains itself",
      outcome: "failed",
      steps: [
        {
          text: "she is shown that nothing matched, and no products",
          keyword: "Then",
          command: 'agent-browser get count "[data-testid=product]"',
          exit: 1,
          observed: "3 — products were listed for a search that matched nothing",
        },
      ],
    }),
    scenarioEntry({
      feature: "Checkout",
      featureFile: CHECKOUT_FEATURE.path,
      rule: "Payment details are transmitted over an encrypted connection",
      scenario: "The payment step is encrypted",
      outcome: "unjudgeable",
      steps: [
        {
          text: "the details leave the browser encrypted",
          keyword: "Then",
          observed: "the development deployment terminates TLS at the gateway",
        },
      ],
    }),
  ],
});

// A report where nothing was settled — the shape behind `inconclusive`.
const NOTHING_RAN = JSON.stringify({
  schemaVersion: 2,
  commit: "a1b2c3d4e5f6",
  isolation: "Each scenario creates the cart it asserts on.",
  scenarios: [
    scenarioEntry({
      scenario: "Searching for a product by name",
      outcome: "blocked",
      steps: [{ text: "Priya searches", keyword: "When", observed: "the search box is absent" }],
    }),
    scenarioEntry({
      scenario: "A search that matches nothing explains itself",
      outcome: "blocked",
      steps: [{ text: "Priya searches", keyword: "When", observed: "the search box is absent" }],
    }),
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
  mockFeatures.isPending = false;
  mockFeatures.isError = false;
  mockFeatures.isAbsent = false;
  mockFeatures.features = [];
  mockReport.isError = false;
  mockReport.data = undefined;
  mockIssueUrl = "https://github.com/acme/demo/issues/30";
  mockIssueComments = [];
  mockIssueLive = undefined;
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
    mockFeatures.features = FEATURES;
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
    mockFeatures.features = FEATURES;
    mockReport.data = { content: "{ not json" };

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
    // The event is a DEPLOYMENT, not a build: validation drives a running
    // instance and needs its resolved endpoints, so a build alone cannot trigger
    // it. And it names the same subject the agent's own status line does.
    expect(
      screen.getByText(/driven against the acceptance criteria/),
    ).toBeInTheDocument();
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
    mockFeatures.features = FEATURES;

    renderPage(undefined);

    expect(
      screen.queryByText(/Nothing validated yet/),
    ).not.toBeInTheDocument();
    // The run was found, so the version's criteria are on screen under the tile
    // that says an attempt is under way.
    expect(
      screen.getByText("Browsing the catalog"),
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
    mockFeatures.features = FEATURES;
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
          reportPath: "tests/acceptance/report.json",
        },
        cycles: [
          validationCycle,
          { ...validationCycle, id: "cycle-3", kind: "coding" },
        ],
      }),
      state: "running",
    };
    mockFeatures.features = FEATURES;
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // Chip AND tile headline, both from the shared mapper.
    expect(screen.getAllByText("Awaiting fix").length).toBe(2);
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
    expect(
      screen.queryByText(/the milestone stays open for the fix/),
    ).not.toBeInTheDocument();
    // The failed report stays — it is the evidence of WHAT is being fixed, and the
    // coding cycle in flight has no validation log to show in its place.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Browsing the catalog"),
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
          reportPath: "tests/acceptance/report.json",
        },
        cycles: [validationCycle],
      }),
      state: "running",
    };
    mockFeatures.features = FEATURES;
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    // The previous attempt's report is real, and it is what the reader wants while
    // the fix is being re-checked. That it belongs to the last attempt is the tile's
    // job to say — twice, in the sentence and in the tally.
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Browsing the catalog"),
    ).toBeInTheDocument();
    // Chip and tile headline both, as with every other state.
    expect(screen.getAllByText("Validating").length).toBe(2);
    expect(screen.queryByText("Validation failed")).not.toBeInTheDocument();
  });

  it("keeps the report/log toggle working while a repeat attempt runs", () => {
    mockValidation = "running";
    mockRun = {
      ...run({
        validation: {
          verdict: "failed",
          reportPath: "tests/acceptance/report.json",
        },
        cycles: [validationCycle],
      }),
      state: "running",
    };
    mockFeatures.features = FEATURES;
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

  it("stamps the run's verdict on the header, not the coarse lifecycle", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
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
        reportPath: "tests/acceptance/report.json",
      },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
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
      screen.getByText("Browsing the catalog"),
    ).toBeInTheDocument();
  });

  it("renders the report for an INCONCLUSIVE verdict", () => {
    mockValidation = "inconclusive";
    mockRun = run({
      validation: {
        verdict: "inconclusive",
        reportPath: "tests/acceptance/report.json",
      },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
    mockReport.data = { content: NOTHING_RAN };
    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(screen.getAllByText("Validation?").length).toBe(2);
    expect(
      screen.getByText(/please check them yourself/),
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
    mockFeatures.features = FEATURES;
    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(screen.getAllByText("Validation error").length).toBe(2);
    expect(
      screen.getByText(/validation report couldn't be generated/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/report wasn't found/)).not.toBeInTheDocument();
    // The criteria still render — they live under specs/, not in the report.
    expect(
      screen.getByText("Browsing the catalog"),
    ).toBeInTheDocument();
    // And with no report there is nothing to count.
    expect(screen.queryByText(/\d+ passed/)).not.toBeInTheDocument();
  });

  it("links the validation cycle's PR, learned from the cycle record", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
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
    mockFeatures.features = FEATURES;
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
    mockFeatures.features = FEATURES;
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
    mockFeatures.features = FEATURES;
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
    mockFeatures.features = FEATURES;
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
    mockFeatures.features = FEATURES;
    mockReport.data = { content: REPORT };
    const onViewChange = renderPage("logs");

    expect(screen.getByTestId("run-feed")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /View report/ }));
    expect(onViewChange).toHaveBeenCalledWith(undefined);
  });

  it("falls back to criteria-only with a note when the report is missing", () => {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed" },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
    mockReport.isError = true;
    renderPage(undefined);

    expect(screen.getByText(/report wasn't found/)).toBeInTheDocument();
    expect(
      screen.getByText("Browsing the catalog"),
    ).toBeInTheDocument();
    // No state chips without a report.
    expect(screen.queryByText("Passed")).not.toBeInTheDocument();
  });
});

// `e2e` is an acronym the console lexicon forbids and nothing in the product
// expands, and it cannot change — the runner, the report generator and the
// tests/e2e/specs/<AC-ID>.spec.ts path all key on it. So no surface may leak it,
// which is part of what these assertions hold.
describe("ValidationPage scenario rows", () => {
  function renderWithScenarios() {
    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed", reportPath: "tests/acceptance/report.json" },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
    mockReport.data = { content: REPORT };
    renderPage(undefined);
  }

  // The tile above already prints the tally. The view's own would repeat those
  // numbers a few rows lower on the same screen.
  it("drops the summary tally the tile above already prints", () => {
    renderWithScenarios();

    expect(screen.queryByText(/capabilities ·/)).not.toBeInTheDocument();
  });

  // Every outcome gets its own word, and each one its own glyph — four outlined
  // chips that differed by hue alone would be nothing to a colour-blind reader.
  it("chips each scenario with the report's own word", () => {
    renderWithScenarios();

    // The outcome words appear twice over — once as a filter segment, once as a
    // row's pill — so the assertion says which it means.
    const list = within(screen.getByRole("region", { name: "Acceptance scenarios" }));
    expect(list.getByText("Passed")).toBeInTheDocument();
    expect(list.getByText("Failed")).toBeInTheDocument();
    expect(list.getByText("Unjudgeable")).toBeInTheDocument();
  });

  // Nothing opens by itself, so the one line under a non-passed row is all the
  // page says about WHY — and it has to be enough to triage on.
  it("says why a scenario did not pass without being opened", () => {
    renderWithScenarios();

    expect(
      screen.getByText("3 — products were listed for a search that matched nothing"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('the results include "Cedar Desk Lamp"'),
    ).not.toBeInTheDocument();
  });

  // The description explaining what the criteria ARE belongs to the Spec view,
  // where a reader meets the document cold. Here they arrived to read run results.
  it("omits the spec view's explanation", () => {
    renderWithScenarios();

    expect(screen.queryByText(/Each scenario is one concrete example/)).not.toBeInTheDocument();
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
    mockFeatures.features = FEATURES;

    renderPage(undefined);

    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    expect(
      screen.getByText("Browsing the catalog"),
    ).toBeInTheDocument();
    // Chip and tile headline both, as with every other state.
    expect(screen.getAllByText("Validating").length).toBe(2);
    // No method split: an acceptance scenario does not declare who checks it, so
    // every one of them is driven and the tile has one sentence rather than two.
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Every acceptance scenario is being driven against the deployed system.",
    );
    // Counted off the FEATURE FILES — there is no report to count yet. Scoped to
    // the tile, because the view's own filter header prints the same total.
    expect(within(screen.getByRole("alert")).getByText("3 scenarios")).toBeInTheDocument();
  });

  it("keeps the log one click away, and the way back from it", () => {
    runningFirstAttempt();
    mockFeatures.features = FEATURES;

    const onViewChange = renderPage(undefined);
    fireEvent.click(screen.getByRole("button", { name: /View logs/ }));
    expect(onViewChange).toHaveBeenCalledWith("logs");

    renderPage("logs");
    expect(screen.getByTestId("run-feed")).toHaveTextContent("validation");
    expect(
      screen.getAllByRole("button", { name: /View report/ }).length,
    ).toBeGreaterThan(0);
  });

  // The count inflects, and it counts SCENARIOS rather than files — a capability
  // with three of them is not three capabilities.
  it("counts the scenarios, not the feature files", () => {
    runningFirstAttempt();
    mockFeatures.features = [CHECKOUT_FEATURE];

    renderPage(undefined);

    expect(within(screen.getByRole("alert")).getByText("1 scenario")).toBeInTheDocument();
  });

  // No feature files at this version — the state its run eventually settles as
  // `skipped`. The tile is then the whole body: there is nothing to list under it.
  it("says the version has no criteria when the spec authored none", () => {
    runningFirstAttempt();
    mockFeatures.isAbsent = true;

    renderPage(undefined);

    expect(
      screen.getByText(
        "This version has no acceptance criteria, so there is nothing to check the deployment against.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/Failed to load the acceptance criteria/),
    ).not.toBeInTheDocument();
    expect(screen.queryByTestId("run-feed")).not.toBeInTheDocument();
    // The log is still reachable — the header reads the same in both running shapes.
    expect(
      screen.getByRole("button", { name: /View logs/ }),
    ).toBeInTheDocument();
  });

  // The other half of that branch, and the reason the hook reports absence
  // separately from failure: a read that merely FAILED must not be reported as a
  // spec that authored nothing.
  it("offers a retry when the criteria read merely failed", () => {
    runningFirstAttempt();
    mockFeatures.isError = true;

    renderPage(undefined);

    expect(
      screen.getByText(/Failed to load the acceptance criteria/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(
      screen.queryByText(/no acceptance criteria/),
    ).not.toBeInTheDocument();
  });
});

// Until now this page could say what a validation WOULD check and what it
// eventually FOUND, and nothing at all in between — every row read "Pending" for
// up to two hours (the validation cycle's deadline), or worse, held the previous
// attempt's verdict while a new attempt re-worked exactly those criteria.
// The previous attempt's evidence has to survive the repair that follows it —
// that is the whole reason the report is pinned to its own merge commit.
describe("ValidationPage across a repair", () => {
  it("keeps the failed attempt's evidence while a repair cycle writes code", () => {
    mockValidation = "awaiting-fix";
    mockRun = {
      ...run({
        validation: { verdict: "failed", reportPath: "tests/acceptance/report.json" },
        cycles: [validationCycle, { ...validationCycle, id: "cycle-3", kind: "coding" }],
      }),
      state: "running",
    };
    mockFeatures.features = FEATURES;
    mockReport.data = { content: REPORT };
    renderPage(undefined);

    expect(
      screen.getByText(/products were listed for a search that matched nothing/),
    ).toBeInTheDocument();
  });
});

// The run-wide narration a reader arrives with. Until now the tile had exactly
// two sentences in its whole vocabulary and said NOTHING for the entire middle of
// a run — the longest stretch, and the one where "is this progressing or stuck?"
// is the only question anyone has. The agent now keeps a status line on its own
// validation issue (skills/aep/SKILL.md, "The status line") and this renders it.
describe("ValidationPage agent status line", () => {
  const validating = () => {
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockFeatures.features = FEATURES;
  };

  it("shows the agent's own words", () => {
    validating();
    // The derived line would say "Setting up the test harness…" here, because no
    // criterion has been touched. The agent knows better and said so.
    mockIssueComments = [{ id: "c1", body: "Reading the criteria; 12 to author." }];
    renderPage(undefined);

    expect(screen.getByText("Reading the criteria; 12 to author.")).toBeInTheDocument();
    expect(screen.queryByText("Setting up the test harness…")).not.toBeInTheDocument();
  });

  it("takes the NEWEST comment, which is the point of a durable line", () => {
    validating();
    mockIssueComments = [
      { id: "c1", body: "Starting validation: 12 criteria, 9 to author." },
      { id: "c2", body: "Healing AC-004-b — the login step raced the redirect." },
    ];
    renderPage(undefined);

    expect(
      screen.getByText("Healing AC-004-b — the login step raced the redirect."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/^Starting validation/)).not.toBeInTheDocument();
  });

  it("shows the platform's observed line unlabelled — the pulse beside it says machine", () => {
    // Most lines on a validation issue are this, and labelling the common case
    // would spend the reader's attention where none is needed.
    validating();
    mockIssueComments = [
      { id: "o1", body: "Running automated tests against the deployed system…", observed: true },
    ];
    renderPage(undefined);

    expect(screen.getByText("Running automated tests against the deployed system…")).toBeInTheDocument();
    expect(screen.queryByText(/The agent:/)).not.toBeInTheDocument();
  });

  it("labels the AGENT's line, which is the one carrying a judgement", () => {
    // The platform reports tool calls; the agent speaks between them for what no
    // command can show. That line is worth more than the one before it, and a
    // reader who cannot tell them apart over-trusts the mechanical one.
    validating();
    mockIssueComments = [
      { id: "o1", body: "Running automated tests against the deployed system…", observed: true },
      { id: "c2", body: "AC-001-b blocked: the roles gate published no second login." },
    ];
    renderPage(undefined);

    expect(screen.getByText(/The agent:/)).toBeInTheDocument();
    expect(
      screen.getByText("AC-001-b blocked: the roles gate published no second login."),
    ).toBeInTheDocument();
  });

  it("renders one line of a multi-line comment", () => {
    // A comment body is markdown over an unbounded textarea; the tile is a note.
    validating();
    mockIssueComments = [
      { id: "c1", body: "Authoring the last three specs.\n\n- AC-005-a\n- AC-005-b" },
    ];
    renderPage(undefined);

    expect(screen.getByText("Authoring the last three specs.")).toBeInTheDocument();
    expect(screen.queryByText(/AC-005-a/)).not.toBeInTheDocument();
  });

  it("says nothing when the agent has posted nothing", () => {
    // The skill ASKS for the line, and an asked-for thing can be skipped — so the
    // page must degrade to what it showed before rather than to a blank.
    validating();
    mockIssueComments = [];
    renderPage(undefined);

    // The derived fallback that used to speak here counted criterion rows moving,
    // and nothing emits those any more — it was deleted rather than re-derived.
    expect(screen.queryByText(/Setting up the test harness/)).not.toBeInTheDocument();
  });

  it("survives the switch to the log body", () => {
    // The live fold is opened only for the report body, so `live.active` is false
    // here and the derived sentence is empty by construction. The status line is
    // polled rather than streamed, so it does not care — and the log view is
    // exactly where someone watching a long run sits.
    validating();
    mockIssueComments = [{ id: "c1", body: "Running the whole suite." }];
    renderPage("logs");

    expect(screen.getByText("Running the whole suite.")).toBeInTheDocument();
  });

  it("polls the issue while the loop is live, and not once it settles", () => {
    // This read is GitHub-backed: an idle version must cost nothing, and a live
    // one must not freeze at whatever the line said when the page opened.
    validating();
    renderPage(undefined);
    expect(mockIssueLive).toBe(true);

    mockValidation = "passed";
    mockRun = {
      ...run({
        validation: { verdict: "passed", reportPath: "tests/acceptance/report.json" },
        cycles: [validationCycle],
      }),
      state: "succeeded",
    };
    mockReport.data = { content: REPORT };
    renderPage(undefined);
    expect(mockIssueLive).toBe(false);
  });

  // A comment OUTLIVES the run that wrote it. The closing summary stays the
  // newest comment on the issue forever, so an ungated line kept announcing a
  // finished run as if it were still going.
  it("says nothing over a settled verdict, however recent the comment", () => {
    mockValidation = "passed";
    mockRun = {
      ...run({
        validation: { verdict: "passed", reportPath: "tests/acceptance/report.json" },
        cycles: [validationCycle],
      }),
      state: "succeeded",
    };
    mockReport.data = { content: REPORT };
    mockIssueComments = [
      { id: "c1", body: "Validation complete: 2/2 e2e criteria passing." },
    ];
    renderPage(undefined);

    expect(screen.queryByText(/Validation complete/)).not.toBeInTheDocument();
    // The verdict itself still speaks — this removes a duplicate, not the answer.
    expect(screen.getByText(/were settled and passed/)).toBeInTheDocument();
  });

  // The sharper half: `awaiting-fix` is a lifecycle state, so the loop IS live —
  // but the cycle running is CODING. The newest validation cycle closed when it
  // reported the failure, so its last words describe a finished attempt, and
  // showing them claims an agent is validating while none is.
  it("says nothing while a repair cycle codes, even though the loop is live", () => {
    mockValidation = "awaiting-fix";
    mockRun = {
      ...run({
        validation: { verdict: "failed", reportPath: "tests/acceptance/report.json" },
        cycles: [validationCycle, { ...validationCycle, id: "cycle-3", kind: "coding" }],
      }),
      state: "running",
    };
    mockFeatures.features = FEATURES;
    mockReport.data = { content: REPORT };
    mockIssueComments = [{ id: "c1", body: "3 of 12 failed — report committed." }];
    renderPage(undefined);

    expect(screen.queryByText(/3 of 12 failed/)).not.toBeInTheDocument();
    // And it must not poll GitHub for a line it will not render.
    expect(mockIssueLive).toBe(false);
  });

  // The pulse is the claim that an agent is WORKING. It rides with the line, so
  // the gate above is also what keeps it from animating over a settled run.
  it("carries the working pulse while it speaks", () => {
    validating();
    mockIssueComments = [{ id: "c1", body: "Authoring the last three specs." }];
    renderPage(undefined);

    expect(screen.getByText("Authoring the last three specs.")).toBeInTheDocument();
    expect(screen.getByTestId("working-pulse")).toBeInTheDocument();
  });
});

// The header's controls are two 24px chips and two 32px buttons. Ordered
// chips-then-buttons so the row does not dip in the middle — and with cancel
// LAST, which is doing two jobs beyond the heights.
describe("ValidationPage header controls", () => {
  /** The action row itself, reached through a control known to be in it. */
  function rowOf(control: HTMLElement): string[] {
    const row = control.parentElement as HTMLElement;
    return [...row.children].map((el) => el.textContent?.trim() ?? "");
  }

  function liveValidation() {
    mockValidation = "running";
    mockRun = { ...run({ cycles: [validationCycle] }), state: "running" };
    mockFeatures.features = FEATURES;
  }

  it("groups the chips before the buttons, with cancel last", () => {
    liveValidation();
    renderPage(undefined);

    const order = rowOf(screen.getByRole("button", { name: /Cancel run/ }));
    const index = (label: string) => order.findIndex((t) => t.includes(label));

    // issue · PR · View logs · Cancel run — 24px, 24px, 32px, 32px.
    expect(index("#30")).toBe(0);
    expect(index("View logs")).toBeGreaterThan(index("#30"));
    expect(index("Cancel run")).toBe(order.length - 1);
  });

  // Header actions are right-aligned, so a control that disappears shifts
  // everything to its left. Cancel exists only while a run is live, and last is
  // the one position where its coming and going leaves the control people
  // actually reach for exactly where it was.
  it("keeps View logs put when the run stops being cancellable", () => {
    liveValidation();
    renderPage(undefined);
    const live = rowOf(screen.getByRole("button", { name: /Cancel run/ }));
    const fromEnd = (row: string[], label: string) =>
      row.length - row.findIndex((t) => t.includes(label));
    const before = fromEnd(live, "View logs");
    cleanup();

    mockValidation = "passed";
    mockRun = run({
      validation: { verdict: "passed", reportPath: "tests/acceptance/report.json" },
      cycles: [validationCycle],
    });
    mockFeatures.features = FEATURES;
    renderPage(undefined);

    const settled = rowOf(screen.getByRole("button", { name: /View logs/ }));
    expect(settled.some((t) => t.includes("Cancel run"))).toBe(false);
    // Cancel was the only thing to its right, so View logs is now last — and
    // every chip to its left has held its position.
    expect(fromEnd(settled, "View logs")).toBe(before - 1);
    expect(settled.findIndex((t) => t.includes("View logs"))).toBe(
      live.findIndex((t) => t.includes("View logs")),
    );
  });
});
