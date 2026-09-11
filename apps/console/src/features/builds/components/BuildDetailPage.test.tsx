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
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type BuildSummary = components["schemas"]["BuildSummary"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];
type DeployStage = components["schemas"]["DeployStage"];
type CycleBuild = components["schemas"]["CycleBuild"];

// Router stubbed to plain anchors — no RouterProvider needed.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
  createLink:
    (Component: React.ElementType) =>
    ({
      to,
      params,
      children,
      ...rest
    }: {
      to: string;
      params?: Record<string, string>;
      children?: React.ReactNode;
    }) => {
      const path = Object.entries(params ?? {}).reduce(
        (acc, [k, v]) => acc.replace(`$${k}`, v),
        to,
      );
      return (
        <Component {...rest} component="a" href={path}>
          {children}
        </Component>
      );
    },
}));

const invalidateQueries = vi.fn();
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries }),
}));

// The coding agent's stream is its own tested surface and needs a live run to
// say anything; the build page decides WHICH runs to mount it for, in what order,
// and which one may open a box. Those are attributes rather than rendered text, so
// the page's WIRING is assertable without a stream.
vi.mock("./RunFeed", () => ({
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

let mockTasks: TaskView[] = [];
vi.mock("../../tasks/api/queries", () => ({
  useAllTasks: () => ({
    data: mockTasks,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
}));

// The external-resources section's two reads. Both are stubbed here rather
// than in the section's own mocks because this page mounts it FOR REAL — that
// is the only way this suite can prove where it sits on the page.
let mockReadiness:
  | components["schemas"]["ProjectDependencyReadiness"]
  | undefined;
let mockDeploy: DeployStage | undefined;
vi.mock("../../projects/api/queries", () => ({
  useProjectStatus: () => ({
    data: {
      repoUrl: "https://github.com/acme/demo.git",
      ...(mockDeploy ? { deploy: mockDeploy } : {}),
    },
  }),
  useProjectDependencyReadiness: () => ({
    data: mockReadiness,
    isPending: false,
    isSuccess: true,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useSaveConnectionValues: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
}));

let mockDesignDeps: components["schemas"]["ComponentDependencies"][] = [];
vi.mock("../../spec/api/queries", () => ({
  useDesignDependencies: () => ({
    data: mockDesignDeps,
    isPending: false,
    isSuccess: true,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
}));

let mockBuilds: BuildSummary[] = [];
let mockRuns: MilestoneRunView[] = [];
// The cluster's answer for every cycle this page asks about. One list rather
// than one per cycle: the page asks about the current session and the merged
// one, and every test that cares has them be the same session.
let mockCycleBuilds: CycleBuild[] = [];
// Which cycle the Build logs section asked the cluster about, in order.
const cycleBuildsCalls: Array<{ cycleId: string; enabled: boolean }> = [];
vi.mock("../api/queries", () => ({
  useBuilds: () => ({
    data: mockBuilds,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useBuildRuns: () => ({ data: { runs: mockRuns } }),
  useCycleBuilds: (_p: string, _t: string, cycleId: string, enabled: boolean) => {
    cycleBuildsCalls.push({ cycleId, enabled });
    return {
      data: enabled ? mockCycleBuilds : undefined,
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    };
  },
  useCancelRun: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
}));

import { BuildDetailPage } from "./BuildDetailPage";

const build = (over: Partial<BuildSummary> = {}): BuildSummary => ({
  tag: "v2",
  milestoneNumber: 2,
  status: "in_progress",
  startedAt: "2026-08-14T16:20:00Z",
  ...over,
});

const run = (over: Partial<MilestoneRunView> = {}): MilestoneRunView =>
  ({
    id: "run-1",
    milestoneNumber: 2,
    milestoneTitle: "v2",
    kind: "dev",
    origin: "spec-build",
    state: "running",
    budgets: {
      cyclesTotal: 1,
      cycleCeiling: 8,
      fixCycles: 0,
      conflictCycles: 0,
      buildRetriggers: 0,
      validationCycles: 0,
    },
    validation: {},
    cycles: [{ id: "cycle-1", kind: "coding", state: "running" }],
    createdAt: "2026-08-14T16:20:00Z",
    ...over,
  }) as MilestoneRunView;

const cycle = (over: Partial<RunCycleView> = {}): RunCycleView => ({
  id: "cycle-1",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-08-14T16:21:00Z",
  ...over,
});

const task = (issueNumber: number, over: Partial<TaskView> = {}): TaskView => ({
  issueNumber,
  title: `Task ${issueNumber}`,
  issueUrl: `https://github.com/acme-dev/demo-shop/issues/${issueNumber}`,
  executorClass: "coding",
  dependsOn: [],
  lineage: { specTag: "v2" },
  derivedStatus: "pending",
  hold: false,
  attention: [],
  executions: {},
  ...over,
});

const merged = (issueNumber: number) => task(issueNumber, { derivedStatus: "merged" });

const componentBuild = (over: Partial<CycleBuild> = {}): CycleBuild => ({
  component: "catalog-api",
  buildName: "catalog-api-build-1",
  status: "Running",
  completed: false,
  attempt: 1,
  ...over,
});

const greenBuild = (component: string): CycleBuild =>
  componentBuild({ component, buildName: `${component}-build-1`, status: "WorkflowSucceeded", completed: true });

const renderPage = () =>
  render(<BuildDetailPage projectName="demo-shop" tag="v2" />);

const deploymentsLink = () => screen.queryByText("Go to Deployments");

const withOneExternal = () => {
  mockDesignDeps = [
    {
      componentName: "catalog-api",
      dependencies: [
        { kind: "external", name: "stripe", config: [{ key: "api_key" }] },
      ],
    },
  ] as components["schemas"]["ComponentDependencies"][];
  mockReadiness = {
    configured: false,
    dependencies: [
      { name: "stripe", state: "unset", missingKeys: ["api_key"] },
    ],
  } as components["schemas"]["ProjectDependencyReadiness"];
};

afterEach(() => {
  mockBuilds = [];
  mockRuns = [];
  mockTasks = [];
  mockDeploy = undefined;
  mockCycleBuilds = [];
  mockDesignDeps = [];
  mockReadiness = undefined;
  cycleBuildsCalls.length = 0;
  vi.clearAllMocks();
  vi.useRealTimers();
});

// ADR-0023 moved the collection of external configuration off the Build button and
// onto the run. ADR-0021 then made a VERSION's page the place that says why
// that version is or is not moving, so this is where the section lives.
describe("BuildDetailPage — external resources", () => {
  it("offers the values as a peer of Tasks, ahead of the logs", () => {
    mockBuilds = [build()];
    mockRuns = [run()];
    withOneExternal();
    renderPage();

    expect(screen.getByText("External resources")).toBeInTheDocument();
    expect(screen.getByText("1 of 1 need configuration")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Configure now: stripe" }),
    ).toBeInTheDocument();

    // ORDER, not membership. It is outstanding work a person must do, so it is
    // a peer of a task row — after Tasks, and before the two log sections,
    // which are a record rather than a request.
    // Addressed by each section's disclosure control: "Tasks" is also a cell
    // label on the summary card, and the heading text alone is ambiguous.
    const section = (title: string) =>
      screen.getByRole("button", { name: `Collapse ${title}` });
    const tasks = section("Tasks");
    const external = section("External resources");
    const agentLog = section("Coding agent log");
    expect(
      tasks.compareDocumentPosition(external) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      external.compareDocumentPosition(agentLog) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("says nothing when the design declares no external dependencies", () => {
    mockBuilds = [build()];
    mockRuns = [run()];
    mockReadiness = {
      configured: true,
      dependencies: [],
    } as components["schemas"]["ProjectDependencyReadiness"];
    renderPage();

    expect(screen.queryByText("External resources")).not.toBeInTheDocument();
  });
});

// A run parked at the deploy gate is UNBOUNDED and only a person can end it,
// so `waiting` with nothing beside it reads as a hang.
describe("BuildDetailPage — the deploy gate's park", () => {
  const parked = (deps?: string[]) =>
    run({
      state: "waiting",
      waitingReason: "external-values",
      ...(deps ? { blockingDependencies: deps } : {}),
    });

  it("names the blocking dependencies and points at the section below", () => {
    mockBuilds = [build()];
    mockRuns = [parked(["stripe", "sendgrid"])];
    withOneExternal();
    renderPage();

    expect(
      screen.getByText("Waiting for configuration: stripe, sendgrid"),
    ).toBeInTheDocument();
    // The promise the reader needs most: there is no restart button to hunt for.
    expect(
      screen.getByText(/the run resumes and deploys on its own/),
    ).toBeInTheDocument();
    // On THIS page, deliberately — a route would be a second way into one
    // configuration surface.
    expect(screen.getByRole("link", { name: "Add configuration" })).toHaveAttribute(
      "href",
      "#external-resources",
    );
  });

  // An older run row, or a lost write, carries no names. That park still has to
  // be explainable.
  it("still explains a park that names nothing", () => {
    mockBuilds = [build()];
    mockRuns = [parked()];
    renderPage();

    expect(screen.getByText("Waiting for external configuration")).toBeInTheDocument();
  });

  // The regression this exists to stop. `BuildSummary` has no waiting reason,
  // so the ledger's derivation calls a parked run "Running · Coding agent" —
  // true of the ledger, which cannot afford the run read, and a lie on the one
  // page that already has it.
  it("does not claim the coding agent is working while the run is parked", () => {
    mockBuilds = [build()];
    mockRuns = [parked(["stripe"])];
    renderPage();

    expect(screen.getByText("Waiting for configuration")).toBeInTheDocument();
    expect(screen.queryByText("Running · Coding agent")).not.toBeInTheDocument();
    // And the rollout line must not contradict the notice above it.
    expect(
      screen.getByText("v2 is built and waiting for its external configuration."),
    ).toBeInTheDocument();
  });

  it("says nothing about a park on a run that is not parked", () => {
    mockBuilds = [build()];
    mockRuns = [run()];
    renderPage();

    expect(screen.queryByText(/Waiting for/)).not.toBeInTheDocument();
    expect(screen.getByText("Running · Coding agent")).toBeInTheDocument();
  });
});

describe("BuildDetailPage — the Deployments link", () => {
  it("stays away until a pull request has merged", () => {
    // The reported bug: the card offered a board that had nothing to show for
    // this version, beside a note saying it deploys as its tasks merge.
    mockBuilds = [build()];
    mockTasks = [task(1), task(2)];
    mockRuns = [run({ cycles: [cycle({ prNumber: 4, mergeSha: "" })] })];
    renderPage();
    expect(deploymentsLink()).toBeNull();
    // The note stays: the card must still say what has to happen.
    expect(screen.getByText("v2 deploys as its tasks merge.")).toBeInTheDocument();
  });

  it("is not fooled by closed issues that no pull request ever produced", () => {
    // The live case that corrected this gate: a cancelled run whose two tasks
    // both read `derivedStatus: "merged"` — the field only says the issue is
    // closed — while its one cycle had `prNumber` 0 and no merge SHA.
    mockBuilds = [build()];
    mockTasks = [merged(1), merged(2)];
    mockRuns = [
      run({ state: "cancelled", cycles: [cycle({ prNumber: 0, mergeSha: "" })] }),
    ];
    renderPage();
    expect(deploymentsLink()).toBeNull();
  });

  it("appears once a cycle records a merge", () => {
    mockBuilds = [build()];
    mockTasks = [merged(1), merged(2)];
    mockRuns = [run({ cycles: [cycle({ prNumber: 4, mergeSha: "abc1234" })] })];
    renderPage();
    expect(deploymentsLink()).toBeInTheDocument();
  });

  it("asks every run of the version, not just the newest", () => {
    // A version whose coding cycle merged pull request #15, later reworked by a
    // `task` run that opened no cycle at all. Reading only the newest run made
    // merged code look unmerged.
    mockBuilds = [build()];
    mockTasks = [merged(1)];
    mockRuns = [
      run({ id: "newer", kind: "task", state: "cancelled", cycles: [] }),
      run({ id: "older", cycles: [cycle({ prNumber: 15, mergeSha: "c185b23" })] }),
    ];
    renderPage();
    expect(deploymentsLink()).toBeInTheDocument();
  });

  it("appears for a version the platform has already deployed", () => {
    mockBuilds = [build()];
    mockTasks = [merged(1), task(2)];
    mockRuns = [run({ cycles: [cycle({ prNumber: 0 })] })];
    mockDeploy = {
      version: "v2",
      status: "deployed",
      components: { total: 3, ready: 3 },
      validation: "passed",
    };
    renderPage();
    expect(deploymentsLink()).toBeInTheDocument();
    expect(screen.getByText("v2 is live.")).toBeInTheDocument();
  });

  it("stays away when there is no run to have merged anything", () => {
    mockBuilds = [build()];
    mockRuns = [];
    renderPage();
    expect(deploymentsLink()).toBeNull();
  });
});

// The header pill names WHO IS WORKING NOW, not who worked first. A run stays
// `in_progress` from the agent's first token to the rollout, so a hard-coded
// "Running · Coding agent" was true for the first of five stages and a lie for
// the rest — measured on a live run: both components green at 05:50, the header
// still crediting the coding agent at 05:52, with the Build logs section on the
// same screen showing them succeeded.
describe("BuildDetailPage — what the header says is happening", () => {
  const headerPill = (label: string) => screen.getAllByText(label)[0];

  it("credits the coding agent while the agent is the one working", () => {
    mockBuilds = [build()];
    mockRuns = [run({ cycles: [cycle({ prNumber: 0 })] })];
    renderPage();
    expect(screen.getByText("Running · Coding agent")).toBeInTheDocument();
  });

  it("names the platform once the pull request is open", () => {
    mockBuilds = [build()];
    mockRuns = [run({ cycles: [cycle({ prNumber: 9 })] })];
    renderPage();
    expect(headerPill("Running · Merging the pull request")).toBeInTheDocument();
    expect(screen.queryByText("Running · Coding agent")).not.toBeInTheDocument();
  });

  it("names the component builds while they are building", () => {
    mockBuilds = [build()];
    mockRuns = [run({ cycles: [cycle({ prNumber: 9, mergeSha: "abc1234" })] })];
    mockCycleBuilds = [componentBuild(), componentBuild({ component: "web-app" })];
    renderPage();
    expect(headerPill("Running · Building components")).toBeInTheDocument();
    expect(screen.queryByText("Running · Coding agent")).not.toBeInTheDocument();
  });

  // The exact minute the reported page contradicted itself.
  it("moves on to the rollout once every component is green", () => {
    mockBuilds = [build()];
    mockRuns = [run({ cycles: [cycle({ prNumber: 9, mergeSha: "abc1234" })] })];
    mockCycleBuilds = [greenBuild("catalog-api"), greenBuild("web-app")];
    renderPage();
    expect(headerPill("Deploying to development")).toBeInTheDocument();
    expect(screen.queryByText("Running · Coding agent")).not.toBeInTheDocument();
  });

  // A run in its planning phase has no build session, so there is no stage to
  // name and the header claims no actor at all.
  it("claims no actor before a build session exists", () => {
    mockBuilds = [build()];
    mockRuns = [run({ state: "planning", cycles: [] })];
    renderPage();
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.queryByText("Running · Coding agent")).not.toBeInTheDocument();
  });
});

describe("BuildDetailPage — the Duration cell", () => {
  it("counts up second by second while the build has not ended", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:40:00Z"));
    mockBuilds = [build({ completedAt: null })];
    mockRuns = [run()];
    renderPage();

    expect(screen.getByText("20m 00s")).toBeInTheDocument();
    expect(screen.getByText(/and counting/)).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(4000);
    });
    // No refetch, no new data — the card re-rendered on its own clock. Before
    // this, react-query's structural sharing meant the number never moved.
    expect(screen.getByText("20m 04s")).toBeInTheDocument();
  });

  it("freezes a finished build, and drops 'and counting' with it", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:40:00Z"));
    mockBuilds = [
      build({ status: "completed", completedAt: "2026-08-14T16:38:04Z" }),
    ];
    mockRuns = [run({ state: "succeeded" })];
    renderPage();

    expect(screen.getByText("18m 04s")).toBeInTheDocument();
    expect(screen.queryByText(/and counting/)).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByText("18m 04s")).toBeInTheDocument();
  });

  it("keeps counting a build whose status has settled but whose end is unrecorded", () => {
    // `and counting` used to key on `isLedgerLive`, so a build that had left
    // in_progress without an end stamp showed a frozen number with no hint
    // that it was still open.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:40:00Z"));
    mockBuilds = [build({ status: "completed", completedAt: null })];
    mockRuns = [run({ state: "succeeded" })];
    renderPage();

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.getByText("20m 02s")).toBeInTheDocument();
  });
});

// THE CLOCK IS THE PAGE'S, NOT ONE COMPONENT'S.
//
// The reported bug: task rows sat at `0m 24s` for two minutes while the summary
// card's `1m 43s and counting` ticked above them. The ticker was called inside
// `BuildSummaryCard`, so the forced re-render landed in that subtree only —
// and `BuildTaskList` is a sibling, formatting its own elapsed time against
// `Date.now()` with nothing driving it. Polling does not save it: react-query's
// structural sharing hands back the same objects when a payload has not
// changed, so a poll on a run that has not transitioned re-renders nothing.
describe("BuildDetailPage — one clock for the whole page", () => {
  // An open build session that claims issue 7 and has no pull request yet: the
  // row is in progress, counting from the session's start.
  const working = () =>
    run({
      cycles: [cycle({ resolves: [7], prNumber: 0, createdAt: "2026-08-14T16:20:00Z" })],
    });

  it("advances a live task row's elapsed time with no payload change at all", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:20:24Z"));
    // The build started earlier than the session, so the card's number and the
    // row's cannot be mistaken for one another.
    mockBuilds = [build({ startedAt: "2026-08-14T16:10:00Z", completedAt: null })];
    mockTasks = [task(7)];
    mockRuns = [working()];
    renderPage();

    expect(screen.getByText("0m 24s")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    // Not a refetch, not a prop change — the same events the frozen rows saw.
    expect(screen.getByText("0m 34s")).toBeInTheDocument();
  });

  it("keeps the row and the card on the same second", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:21:43Z"));
    mockBuilds = [build({ startedAt: "2026-08-14T16:20:00Z", completedAt: null })];
    mockTasks = [task(7)];
    mockRuns = [working()];
    renderPage();

    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    // Both are measured from the same instant against the same clock, so they
    // read the same span — the card counts the build, the row counts the
    // session, and here they started together.
    expect(screen.getAllByText("1m 48s")).toHaveLength(2);
  });

  // The rows are their OWN reason to run the clock, not a side effect of the
  // card's. A version can be complete while a later run reworks it — its
  // duration is frozen and its rows are not.
  it("counts a live row on a version whose own duration has stopped", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:20:24Z"));
    mockBuilds = [
      build({ status: "completed", completedAt: "2026-08-14T16:18:00Z" }),
    ];
    mockTasks = [task(7)];
    mockRuns = [working()];
    renderPage();

    expect(screen.getByText("0m 24s")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(3_000);
    });
    expect(screen.getByText("0m 27s")).toBeInTheDocument();
  });

  // The interval is the page's, but its CONDITION is still what is actually
  // counting: a settled build with settled rows must not re-render once a
  // second for a reader with nothing to watch.
  it("runs no clock when nothing on the page is counting", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:40:00Z"));
    mockBuilds = [
      build({ status: "completed", completedAt: "2026-08-14T16:38:04Z" }),
    ];
    mockTasks = [merged(7)];
    mockRuns = [
      run({
        state: "succeeded",
        cycles: [cycle({ resolves: [7], prNumber: 9, mergeSha: "abc", endedAt: "2026-08-14T16:38:00Z" })],
      }),
    ];
    renderPage();
    expect(vi.getTimerCount()).toBe(0);
  });
});

// A version is routinely delivered by more than one RUN — the dev run writes it, a
// validation run finds a defect, a task run repairs it — and each is its own row with
// its own feed. The page mounted `deliveryRuns[0]` only, so the newest run's log was
// the only one reachable: testing9231 v1 on the live stack showed a 105-event two-file
// fix labelled "Cycle 1" and offered no way at all to the 841-event run that wrote the
// version. The fixtures below are that run list.
describe("BuildDetailPage — every delivery run's agent log", () => {
  const feeds = () => screen.getAllByTestId("run-feed");
  const devRun = () =>
    run({
      id: "run-dev",
      kind: "dev",
      origin: "spec-build",
      state: "succeeded",
      cycles: [cycle({ id: "c1", prNumber: 6, mergeSha: "aaa1111" })],
    });
  const validationRun = () =>
    run({
      id: "run-validation",
      kind: "validation",
      origin: "revalidate",
      state: "failed",
      cycles: [cycle({ id: "v1", kind: "validation", prNumber: 8, mergeSha: "bbb2222" })],
    });
  const fixRun = () =>
    run({
      id: "run-fix",
      kind: "task",
      origin: "incident-adoption",
      state: "succeeded",
      cycles: [cycle({ id: "c2", prNumber: 13, mergeSha: "ccc3333" })],
    });

  it("mounts one feed per delivery run, newest first", () => {
    mockBuilds = [build()];
    // Newest first, the order list-build-runs answers in.
    mockRuns = [fixRun(), validationRun(), devRun()];
    renderPage();

    expect(feeds().map((f) => f.dataset.runId)).toEqual(["run-fix", "run-dev"]);
  });

  it("numbers the runs from the OLDEST, so the numbers descend down the page", () => {
    // Every feed numbers its own cycles from 1, so without this the page shows two
    // boxes both called "Cycle 1". Counted over the runs this section SHOWS: the
    // validation run has no feed here, so numbering the full list would print
    // "Run 3" over "Run 1" with no Run 2 anywhere.
    mockBuilds = [build()];
    mockRuns = [fixRun(), validationRun(), devRun()];
    renderPage();

    expect(feeds().map((f) => f.dataset.runNumber)).toEqual(["2", "1"]);
  });

  it("numbers nothing when one run delivered the version", () => {
    // `RunFeed` prefixes its heading whenever `runNumber` is defined, so passing
    // 1 here would relabel the ordinary case's only box from "Cycle 1" to
    // "Run 1 · Cycle 1" — a number that tells it apart from nothing.
    mockBuilds = [build()];
    mockRuns = [devRun()];
    renderPage();

    expect(feeds()[0]!.dataset.runNumber).toBe("undefined");
  });

  it("lets only the newest run open a box", () => {
    // One open log on the page, not one per feed.
    mockBuilds = [build()];
    mockRuns = [fixRun(), devRun()];
    renderPage();

    expect(feeds().map((f) => f.dataset.expandNewest)).toEqual(["true", "false"]);
  });

  it("captions the earlier runs once, above the second feed", () => {
    mockBuilds = [build()];
    mockRuns = [fixRun(), devRun()];
    renderPage();

    const caption = screen.getByText("EARLIER RUNS OF V2");
    const [newest, earlier] = feeds();
    expect(
      newest!.compareDocumentPosition(caption) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      caption.compareDocumentPosition(earlier!) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("draws no caption for a version delivered by one run", () => {
    // The ordinary case: a caption over a single feed would be a rule with no
    // boundary under it.
    mockBuilds = [build()];
    mockRuns = [devRun()];
    renderPage();

    expect(feeds()).toHaveLength(1);
    expect(screen.queryByText(/^EARLIER RUNS OF/)).not.toBeInTheDocument();
  });

  it("shows each feed only the cycle kinds this surface owns", () => {
    // A validation run that REPAIRS what it found is a delivery run — kept for its
    // coding cycles — so without the filter its validation cycle would render here
    // as well as on the Validation board, which is the disagreement `buildCycles`
    // and `mergedCycle` already avoid everywhere else on this page.
    mockBuilds = [build()];
    mockRuns = [devRun()];
    renderPage();

    expect(feeds()[0]).toHaveTextContent("coding,fix,conflict");
  });

  it("leaves out a run that only re-judged the version", () => {
    // Its verdict lives on the Validation board, which draws its own feed for it.
    mockBuilds = [build()];
    mockRuns = [validationRun(), devRun()];
    renderPage();

    expect(feeds().map((f) => f.dataset.runId)).toEqual(["run-dev"]);
  });

  it("says nothing was dispatched when no run delivered anything", () => {
    mockBuilds = [build()];
    mockRuns = [validationRun()];
    renderPage();

    expect(screen.queryAllByTestId("run-feed")).toHaveLength(0);
    expect(
      screen.getByText(/Nothing has been dispatched for this version yet/),
    ).toBeInTheDocument();
  });
});

describe("BuildDetailPage — the coding agent log's header note", () => {
  it("says streaming while a build session is open", () => {
    mockBuilds = [build()];
    mockRuns = [run({ cycles: [cycle({ endedAt: null })] })];
    renderPage();
    expect(screen.getByText("Streaming")).toBeInTheDocument();
  });

  it("stops the moment the agent finishes, though the run is still in progress", () => {
    // The bug: the chip read the BUILD's status, and a run stays in_progress
    // through the merge, the component builds and the deployment.
    mockBuilds = [build({ status: "in_progress", completedAt: null })];
    mockRuns = [
      run({ cycles: [cycle({ endedAt: "2026-08-14T16:38:00Z", mergeSha: "abc1234" })] }),
    ];
    renderPage();
    expect(screen.queryByText("Streaming")).not.toBeInTheDocument();
  });

  it("stays quiet while the run is PARKED, even with a cycle still open", () => {
    // The two clauses answer different questions, and this is the case that
    // needs both: a park is a run-level fact, so no cycle has to close for it.
    mockBuilds = [build()];
    mockRuns = [
      run({
        state: "waiting",
        waitingReason: "external-values",
        blockingDependencies: ["stripe"],
        cycles: [cycle({ endedAt: null })],
      }),
    ];
    renderPage();
    expect(screen.queryByText("Streaming")).not.toBeInTheDocument();
  });
});

// The run's ending labels the SECTION HEADER, beside "Coding agent log", the way
// every other section on this page carries its status. It used to sit under the
// log as `run settled — succeeded`: the stream contract's own word for the
// transition, with the raw state pasted on in its wire spelling.
describe("BuildDetailPage — the coding agent log's settled label", () => {
  it("names how the run ended, beside the section title", () => {
    mockBuilds = [build()];
    mockRuns = [
      run({
        state: "succeeded",
        cycles: [cycle({ endedAt: "2026-08-14T16:38:00Z" })],
      }),
    ];
    renderPage();
    expect(screen.getByText("Run finished successfully")).toBeInTheDocument();
    // The old body line, in either spelling, is gone.
    expect(screen.queryByText(/settled/)).not.toBeInTheDocument();
  });

  it("names a cancelled run as cancelled, not as finished", () => {
    mockBuilds = [build()];
    mockRuns = [
      run({ state: "cancelled", cycles: [cycle({ endedAt: "2026-08-14T16:38:00Z" })] }),
    ];
    renderPage();
    expect(screen.getByText("Run cancelled")).toBeInTheDocument();
  });

  // Live beats settled, and a non-terminal run is labelled by NEITHER: a run
  // parked at the deploy gate has not ended, and "Run finished" over its log
  // would contradict the summary card telling the reader it is waiting on them.
  it("says nothing about a run that has not ended", () => {
    mockBuilds = [build()];
    mockRuns = [
      run({
        state: "waiting",
        waitingReason: "external-values",
        blockingDependencies: ["stripe"],
        cycles: [cycle({ endedAt: "2026-08-14T16:38:00Z" })],
      }),
    ];
    renderPage();
    expect(screen.queryByText(/^Run /)).not.toBeInTheDocument();
  });
});

describe("BuildDetailPage — which cycle the build logs ask about", () => {
  it("asks about the cycle that MERGED, not the newest one", () => {
    // The reported bug: Build logs never showed anything. The section was handed
    // `cycles.at(-1)`, and the cluster read answers empty for a cycle with no
    // merge SHA — so it asked about a commit that had built nothing.
    mockBuilds = [build()];
    mockRuns = [
      run({
        cycles: [
          cycle({ id: "merged-cycle", prNumber: 9, mergeSha: "118c794" }),
          cycle({ id: "validation-cycle", kind: "validation" }),
          cycle({ id: "retry-cycle", prNumber: 0 }),
        ],
      }),
    ];
    renderPage();
    expect(cycleBuildsCalls.at(-1)).toEqual({ cycleId: "merged-cycle", enabled: true });
  });

  it("finds the merge in an EARLIER run when the newest one never merged", () => {
    // based-portal-insurance v2 on the live stack: the merged coding cycle is in
    // a succeeded run, and the newest run failed with an unmerged cycle.
    mockBuilds = [build()];
    mockRuns = [
      run({ id: "newest", state: "failed", cycles: [cycle({ id: "failed-cycle", prNumber: 0 })] }),
      run({ id: "older", cycles: [cycle({ id: "the-merge", prNumber: 9, mergeSha: "118c794" })] }),
    ];
    renderPage();
    expect(cycleBuildsCalls.at(-1)?.cycleId).toBe("the-merge");
  });

  it("asks nothing at all when nothing has merged", () => {
    mockBuilds = [build()];
    mockRuns = [run({ cycles: [cycle({ prNumber: 0 })] })];
    renderPage();
    expect(cycleBuildsCalls.at(-1)).toEqual({ cycleId: "", enabled: false });
    expect(
      screen.getByText(/Build logs appear once a build session's pull request has merged/),
    ).toBeInTheDocument();
  });
});

describe("BuildDetailPage — a task row's state", () => {
  it("says the same thing about every issue the one pull request claims", () => {
    mockBuilds = [build()];
    mockTasks = [task(7), task(8)];
    mockRuns = [run({ cycles: [cycle({ resolves: [7, 8], prNumber: 9 })] })];
    renderPage();
    expect(screen.getAllByText("PR sent")).toHaveLength(2);
  });

  it("reads Merged from the recorded SHA, before GitHub closes the issue", () => {
    mockBuilds = [build()];
    mockTasks = [task(7)];
    mockRuns = [
      run({
        cycles: [
          cycle({ resolves: [7], prNumber: 9, mergeSha: "abc", endedAt: "2026-08-14T16:38:00Z" }),
        ],
      }),
    ];
    renderPage();
    expect(screen.getByText("Merged")).toBeInTheDocument();
  });

  it("keeps PR sent after the session ends — it used to fall back to Pending", () => {
    mockBuilds = [build()];
    mockTasks = [task(7)];
    mockRuns = [
      run({ cycles: [cycle({ resolves: [7], prNumber: 9, endedAt: "2026-08-14T16:38:00Z" })] }),
    ];
    renderPage();
    expect(screen.getByText("PR sent")).toBeInTheDocument();
    expect(screen.queryByText("Pending")).not.toBeInTheDocument();
  });
});

describe("BuildDetailPage — the task list's order and its links", () => {
  it("reads ascending by issue number, the order the milestone was planned in", () => {
    // `list-tasks` promises no order, and GitHub's newest-first default was
    // showing through: the gates the platform files first sat at the BOTTOM.
    mockBuilds = [build()];
    mockTasks = [task(4), task(3), task(2), task(1)];
    mockRuns = [run()];
    renderPage();

    const titles = screen.getAllByTitle(/^Task \d+$/).map((el) => el.textContent);
    expect(titles).toEqual(["Task 1", "Task 2", "Task 3", "Task 4"]);
  });

  it("does not sort the array the counts are derived from", () => {
    // The same array backs the tally and the header pulse; sorting in place
    // would reorder them behind their own backs.
    const given = [task(4), task(3)];
    mockBuilds = [build()];
    mockTasks = given;
    mockRuns = [run()];
    renderPage();
    expect(given.map((t) => t.issueNumber)).toEqual([4, 3]);
  });

  it("does not link a task title anywhere — that detail view is not used", () => {
    mockBuilds = [build()];
    mockTasks = [task(1)];
    mockRuns = [run()];
    renderPage();

    // The title is text. The issue chip is still the way out, and it goes to
    // GitHub rather than to a console page.
    expect(screen.queryByRole("link", { name: "Task 1" })).toBeNull();
    expect(document.querySelectorAll('a[href*="/tasks/"]')).toHaveLength(0);
    expect(screen.getByRole("link", { name: "#1" })).toHaveAttribute(
      "href",
      "https://github.com/acme-dev/demo-shop/issues/1",
    );
  });
});

// A failed build used to say `Failed · plan-failed` and nothing else; the reason
// lived in one aep-api log line. The card under the header is where the run
// explains itself, in the platform's recorded words, with the facts a bug report
// needs one click away.
describe("BuildDetailPage — why the run failed", () => {
  const sendgrid = (over: Partial<components["schemas"]["RunFailure"]> = {}) => ({
    code: "dependency-unprovisionable" as const,
    phase: "planning",
    component: "allocation-api",
    dependency: "sendgrid",
    permanent: true,
    attempts: 1,
    maxAttempts: 3,
    firstAt: "2026-08-14T16:20:54Z",
    lastAt: "2026-08-14T16:20:54Z",
    detail: 'external resourcetype "sendgrid": at least one config key required',
    workflowId: "dev-default-demo-shop-2",
    ...over,
  });

  it("explains a failed run from its record, and opens the platform's facts on request", () => {
    mockBuilds = [build({ status: "failed", reason: "plan-failed", failureCode: "dependency-unprovisionable" })];
    mockRuns = [run({ state: "failed", terminalReason: "plan-failed", cycles: [], failure: sendgrid() })];
    renderPage();

    expect(screen.getByText("The platform could not provision `sendgrid`")).toBeInTheDocument();
    expect(screen.getByText(/allocation-api depends on it/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open sendgrid in the design/ })).toBeInTheDocument();
    // The header chip reads the same words.
    expect(screen.getAllByText("Failed · Dependency could not be provisioned").length).toBeGreaterThan(0);
    // The agent log says the agent never started rather than that it wrote nothing.
    expect(screen.getByText(/Did not start — the run ended while the platform was preparing the version/)).toBeInTheDocument();

    expect(screen.queryByTestId("run-failure-details")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show details" }));
    const details = screen.getByTestId("run-failure-details");
    expect(details).toHaveTextContent("dependency-unprovisionable · permanent");
    expect(details).toHaveTextContent("1 of 3");
    expect(details).toHaveTextContent("at least one config key required");
    expect(details).toHaveTextContent("dev-default-demo-shop-2");
  });

  it("shows a fault being retried in amber while the run is still planning", () => {
    mockBuilds = [build({ status: "in_progress" })];
    mockRuns = [
      run({
        state: "planning",
        cycles: [],
        failure: sendgrid({ code: "dependency-provision-failed", permanent: false, attempts: 2, dependency: "orders-db", component: "api" }),
      }),
    ];
    renderPage();

    expect(screen.getByRole("status", { name: "Build retrying" })).toHaveTextContent(
      "Provisioning `orders-db` failed — retrying (attempt 2 of 3)",
    );
  });

  it("says the platform recorded no details for a run failed before the record existed", () => {
    mockBuilds = [build({ status: "failed", reason: "plan-failed" })];
    mockRuns = [run({ state: "failed", terminalReason: "plan-failed", cycles: [] })];
    renderPage();

    expect(screen.getByText("The build failed while preparing the version")).toBeInTheDocument();
    expect(screen.getByText(/recorded no further details/)).toBeInTheDocument();
  });

  it("draws nothing for a cancelled run", () => {
    mockBuilds = [build({ status: "cancelled" })];
    mockRuns = [run({ state: "cancelled", cycles: [], failure: sendgrid() })];
    renderPage();

    expect(screen.queryByRole("status", { name: /Build (failure|retrying)/ })).not.toBeInTheDocument();
  });
});
