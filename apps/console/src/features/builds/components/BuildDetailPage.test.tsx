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

import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type BuildSummary = components["schemas"]["BuildSummary"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];
type DeployStage = components["schemas"]["DeployStage"];

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
// say anything; the build page only decides WHETHER to mount it.
vi.mock("./RunFeed", () => ({
  RunFeed: () => <div>run feed</div>,
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
      data: [],
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
    expect(screen.getByText("v2 is live in development.")).toBeInTheDocument();
  });

  it("stays away when there is no run to have merged anything", () => {
    mockBuilds = [build()];
    mockRuns = [];
    renderPage();
    expect(deploymentsLink()).toBeNull();
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
