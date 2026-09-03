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
import type {
  RunProgressCycle,
  RunProgressPhase,
} from "../hooks/useRunProgress";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];
type CycleBuild = components["schemas"]["CycleBuild"];

// Router stubbed to plain anchors — no RouterProvider needed. createLink is what
// the provisioning stage's way out uses, so it has to survive the stub.
vi.mock("@tanstack/react-router", () => ({
  createLink:
    (Component: React.ElementType) =>
    ({
      to,
      params,
      search,
      children,
      ...rest
    }: {
      to: string;
      params?: Record<string, string>;
      search?: Record<string, string>;
      children?: React.ReactNode;
    }) => {
      const path = Object.entries(params ?? {}).reduce(
        (acc, [k, v]) => acc.replace(`$${k}`, v),
        to,
      );
      const query = new URLSearchParams(search ?? {}).toString();
      return (
        <Component
          {...rest}
          component="a"
          href={query ? `${path}?${query}` : path}
        >
          {children}
        </Component>
      );
    },
}));

let mockCycles: RunProgressCycle[] = [];
let mockPhase: RunProgressPhase = "live";
// Every call's `enabled`, so a test can assert the stream was never opened.
let enabledCalls: boolean[] = [];
// Every cycle-builds read and whether it was enabled — the cluster-derived read
// is the one expensive thing on this surface.
let buildCalls: Array<{ cycleId: string; enabled: boolean }> = [];
let mockBuilds: CycleBuild[] = [];

vi.mock("../hooks/useRunProgress", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../hooks/useRunProgress")>();
  return {
    ...actual,
    useRunProgress: (_p: string, _r: string | undefined, enabled = true) => {
      enabledCalls.push(enabled);
      return { cycles: mockCycles, phase: mockPhase, settledState: undefined };
    },
  };
});

vi.mock("../api/queries", () => ({
  useCycleBuilds: (
    _p: string,
    _t: string | undefined,
    cycleId: string,
    enabled: boolean,
  ) => {
    buildCalls.push({ cycleId, enabled });
    return { data: enabled ? mockBuilds : undefined, isPending: false };
  },
}));

// The build ROWS are their own surface; here they are a marker proving they were
// handed the same read the stage above them was derived from.
vi.mock("./CycleBuilds", () => ({
  CycleBuilds: ({ builds }: { builds: CycleBuild[] | undefined }) => (
    <div data-testid="build-rows">
      {(builds ?? []).map((b) => b.component).join(",")}
    </div>
  ),
}));

vi.mock("./DeploymentStage", () => ({
  DeploymentStage: ({ state }: { state: string }) => (
    <div data-testid="deployment">{state}</div>
  ),
}));

import { RunSpine } from "./RunSpine";

function cycle(over: Partial<RunCycleView> & { id: string }): RunCycleView {
  return {
    kind: "coding",
    attempts: 1,
    createdAt: "2026-07-10T09:00:00Z",
    ...over,
  } as RunCycleView;
}

function task(over: Partial<TaskView> & { issueNumber: number }): TaskView {
  return {
    title: `issue ${over.issueNumber}`,
    issueUrl: `https://github.com/acme/repo/issues/${over.issueNumber}`,
    executorClass: "coding",
    dependsOn: [],
    lineage: {},
    derivedStatus: "pending",
    hold: false,
    attention: [],
    executions: {},
    ...over,
  } as TaskView;
}

function run(
  cycles: RunCycleView[],
  state: MilestoneRunView["state"] = "running",
): MilestoneRunView {
  return {
    id: "run-1",
    milestoneNumber: 2,
    milestoneTitle: "v2",
    kind: "dev",
    origin: "spec-build",
    state,
    budgets: {
      cyclesTotal: cycles.length,
      cycleCeiling: 8,
      fixCycles: 0,
      conflictCycles: 0,
      buildRetriggers: 0,
      validationCycles: 0,
    },
    validation: {},
    cycles,
    createdAt: "2026-07-10T09:00:00Z",
  };
}

function build(over: Partial<CycleBuild> = {}): CycleBuild {
  return {
    component: "workout-api",
    buildName: "proj-workout-api-4a91c2f8ab31-1",
    status: "Succeeded",
    completed: true,
    attempt: 1,
    ...over,
  };
}

/** A gate nothing is driving — no provisioning run was ever admitted, which is
 *  the one case that genuinely waits on a person. */
function gate(issueNumber: number, title: string): TaskView {
  return task({ issueNumber, title, executorClass: "provision" });
}

/** A gate the platform is standing up right now. */
function provisioningGate(issueNumber: number, title: string): TaskView {
  return task({
    issueNumber,
    title,
    executorClass: "provision",
    executions: {
      provision: {
        id: `x${issueNumber}`,
        kind: "provision",
        status: "running",
        createdAt: "2026-07-10T09:01:00Z",
      },
    },
  });
}

function renderSpine(
  r: MilestoneRunView,
  work: TaskView[] = [],
  gates: TaskView[] = [],
) {
  render(
    <RunSpine projectName="acme" tag="v2" run={r} gates={gates} work={work} />,
  );
}

/**
 * The step numbers the rail actually rendered, in document order — the whole
 * claim of this layout is that a run is ONE numbered flow, so it is asserted as
 * a sequence rather than by spot-checking a label.
 */
function stepsOnScreen(): number[] {
  return screen
    .getAllByTestId("stage-step")
    .map((el) => Number(el.textContent));
}

afterEach(() => {
  mockCycles = [];
  mockPhase = "live";
  enabledCalls = [];
  buildCalls = [];
  mockBuilds = [];
});

describe("RunSpine", () => {
  it("tells one build session's whole story as a sequence of named stages", () => {
    const live = cycle({
      id: "c1",
      branch: "aep/m2-c1",
      prNumber: 12,
      prUrl: "https://github.com/acme/repo/pull/12",
      mergeSha: "4a91c2f8ab31",
      resolves: [2],
    });
    mockBuilds = [build()];
    mockCycles = [
      {
        cycle: live,
        lines: [
          {
            cycleId: "c1",
            cycleKind: "coding",
            cycleIndex: 1,
            kind: "git_push",
            emitter: "main",
            seq: 1,
            branch: "aep/m2-c1",
          },
        ],
      },
    ];
    renderSpine(run([live]), [
      task({ issueNumber: 2, title: "workout-api Go backend" }),
    ]);

    // A run with ONE session is just the flow — no session boundary to announce.
    expect(
      screen.queryByText("Build session 1 · coding"),
    ).not.toBeInTheDocument();
    // Every stage is named and present, in order — including the two the old
    // surface drew nowhere at all.
    for (const stage of [
      "Coding agent",
      "Pull request",
      "Merge",
      "Builds",
      "Deployment",
    ]) {
      expect(screen.getByText(stage)).toBeInTheDocument();
    }
    // The facts are attached to the stage that learned them — and the pull
    // request number IS the way to the pull request, opened in a new tab
    // because it leaves the console.
    expect(screen.getByText("4a91c2f8")).toBeInTheDocument();
    const pr = screen.getByText("#12");
    expect(pr).toHaveAttribute("href", "https://github.com/acme/repo/pull/12");
    expect(pr).toHaveAttribute("target", "_blank");
    // The agent's own line, the issue it resolved, and the builds its merge
    // produced — all under the SAME session.
    expect(screen.getByText("↑ push aep/m2-c1")).toBeInTheDocument();
    expect(screen.getByText("workout-api Go backend")).toBeInTheDocument();
    expect(screen.getByTestId("build-rows")).toHaveTextContent("workout-api");
    expect(screen.getByTestId("deployment")).toHaveTextContent("done");
  });

  // The whole point of the redesign: a stage that has not been reached is drawn,
  // saying what it waits for, rather than being absent until it happens.
  it("draws the stages a session has not reached yet, with what they wait for", () => {
    const live = cycle({ id: "c1", branch: "aep/m2-c1" });
    renderSpine(run([live]));

    expect(
      screen.getByText(/The agent opens one when its work builds/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Runs as soon as a pull request is ready/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/The merge is what triggers them/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/A green build deploys itself/),
    ).toBeInTheDocument();
  });

  // The silent stalls. Each one used to look exactly like a session still
  // thinking.
  it("names a draft pull request as the thing the session is waiting behind", () => {
    const live = cycle({ id: "c1", prNumber: 12, prDraft: true });
    renderSpine(run([live]));
    expect(screen.getByText(/Still a draft/)).toBeInTheDocument();
  });

  it("says why a merge was declined, in the policy's own words", () => {
    const live = cycle({
      id: "c1",
      prNumber: 12,
      mergeVerdict: "declined",
      mergeReason: "no resolved issue is agent work in this milestone",
    });
    renderSpine(run([live]));
    expect(
      screen.getByText(/no resolved issue is agent work in this milestone/),
    ).toBeInTheDocument();
    expect(screen.getByText(/left for a human/)).toBeInTheDocument();
  });

  it("says a refused merge is a conflict the next session rebases", () => {
    const live = cycle({
      id: "c1",
      prNumber: 12,
      mergeVerdict: "refused",
      mergeReason: "Pull Request is not mergeable",
    });
    renderSpine(run([live]));
    expect(screen.getByText(/not mergeable/)).toBeInTheDocument();
    expect(screen.getByText(/next build session rebases/)).toBeInTheDocument();
  });

  it("names the red component and says nothing deploys from the session", () => {
    const live = cycle({ id: "c1", prNumber: 12, mergeSha: "4a91c2f8ab31" });
    mockBuilds = [build({ status: "Failed" })];
    renderSpine(run([live]));

    expect(
      screen.getByText(/workout-api did not go green/),
    ).toBeInTheDocument();
    expect(screen.getByTestId("deployment")).toHaveTextContent("failed");
  });

  // Attribution: exact once the platform has recorded the merge policy's matched
  // set, and honestly labelled as a fallback before that.
  it("labels the issue set as a fallback until a pull request records one", () => {
    const live = cycle({ id: "c1" });
    renderSpine(run([live]), [task({ issueNumber: 2 })]);
    expect(screen.getByText(/The milestone's open work/)).toBeInTheDocument();
  });

  it("attributes a settled session's CLOSED issues from what it resolved", () => {
    const done = cycle({
      id: "c1",
      prNumber: 12,
      mergeSha: "4a91c2f8ab31",
      resolves: [2],
      endedAt: "2026-07-10T09:40:00Z",
    });
    renderSpine(run([done]), [
      task({
        issueNumber: 2,
        title: "workout-api Go backend",
        derivedStatus: "merged",
      }),
      // Later work, closed by a LATER session. It must not be attributed here.
      task({ issueNumber: 9, title: "later work", derivedStatus: "merged" }),
    ]);
    expect(screen.getByText("workout-api Go backend")).toBeInTheDocument();
    expect(screen.queryByText("later work")).not.toBeInTheDocument();
    expect(
      screen.getByText(/Resolved by pull request #12/),
    ).toBeInTheDocument();
  });

  // Validation belongs to the deployment surface: the deployment is what gets
  // validated, and its verdict renders there.
  it("does not show the validation session", () => {
    const coding = cycle({ id: "c1", endedAt: "2026-07-10T09:40:00Z" });
    const validation = cycle({ id: "c2", kind: "validation" });
    renderSpine(run([coding, validation]));

    expect(screen.getByText("Coding agent")).toBeInTheDocument();
    expect(screen.queryByText(/validation/)).not.toBeInTheDocument();
  });

  it("labels a re-entry, and numbers its stages on through the same flow", () => {
    const done = cycle({ id: "c1", endedAt: "2026-07-10T09:40:00Z" });
    const live = cycle({ id: "c2", kind: "fix" });
    renderSpine(run([done, live]));

    // The first session IS the flow; a fix session re-enters it, and that is
    // what needs announcing — otherwise the rail reads as the agent going
    // backwards.
    expect(screen.getByText("Build session 2 · fix")).toBeInTheDocument();
    expect(
      screen.queryByText("Build session 1 · coding"),
    ).not.toBeInTheDocument();
    // Two sessions, five stages each, counted straight through from 1.
    expect(stepsOnScreen()).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
  });

  it("makes provisioning step 1, and starts the first session at 2", () => {
    const live = cycle({ id: "c1" });
    renderSpine(run([live]), [], [gate(3, "Provide configuration: stripe")]);

    expect(screen.getByText("Provisioning")).toBeInTheDocument();
    expect(stepsOnScreen()).toEqual([1, 2, 3, 4, 5, 6]);
    // The stage names the connection, says who is acting, and offers the way out.
    expect(screen.getByText("stripe")).toBeInTheDocument();
    expect(screen.getByText("needs you")).toBeInTheDocument();
    expect(screen.getByText(/Supply the configuration/)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Go to Builds/ }),
    ).toHaveAttribute("href", "/projects/acme/builds");
  });

  // The reported bug: a postgres cluster and an identity app take ~5 minutes to
  // stand up, and the platform closes both gates the moment they are Ready. The
  // page called that "Held by 2 unresolved connections" and offered a button to
  // resolve them, sending the user after work that did not exist.
  it("reads gates the platform is provisioning as work in progress, not a hold", () => {
    renderSpine(
      run([cycle({ id: "c1" })]),
      [],
      [
        provisioningGate(1, "Provision resource: user-auth (thunder-app)"),
        provisioningGate(4, "Provision resource: ceramics-db (postgres-cnpg)"),
      ],
    );

    expect(screen.getByText("2 of 2 open")).toBeInTheDocument();
    expect(screen.getByText(/nothing is held on you/)).toBeInTheDocument();
    expect(
      screen.queryByText(/Supply the configuration/),
    ).not.toBeInTheDocument();
    // Nothing to go and do, so no way out is offered.
    expect(
      screen.queryByRole("link", { name: /Go to Builds/ }),
    ).not.toBeInTheDocument();
    // The dependencies are still named — that is what is being waited on.
    expect(screen.getByText("ceramics-db (postgres-cnpg)")).toBeInTheDocument();
  });

  it("keeps the way out when one gate is stalled among gates in flight", () => {
    renderSpine(
      run([cycle({ id: "c1" })]),
      [],
      [
        provisioningGate(1, "Provision resource: db"),
        gate(2, "Provide configuration: stripe"),
      ],
    );

    // The stalled one speaks for the STAGE, but BOTH connections are listed —
    // each with who is acting on it, which is the thing a single notice could
    // never say about a mixed set.
    expect(screen.getByText(/Supply the configuration/)).toBeInTheDocument();
    expect(screen.getByText("stripe")).toBeInTheDocument();
    expect(screen.getByText("needs you")).toBeInTheDocument();
    expect(screen.getByText("db")).toBeInTheDocument();
    expect(screen.getByText("provisioning")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Go to Builds/ }),
    ).toHaveAttribute("href", "/projects/acme/builds");
  });

  // A resolved connection is part of how the version came to exist, so the stage
  // stays on the rail as record rather than disappearing once it is done.
  it("keeps provisioning on the rail once every connection is resolved", () => {
    renderSpine(
      run([cycle({ id: "c1" })]),
      [],
      [
        task({
          issueNumber: 1,
          title: "Provision resource: db",
          executorClass: "provision",
          derivedStatus: "merged",
        }),
      ],
    );

    expect(screen.getByText("1 resolved")).toBeInTheDocument();
    expect(screen.getByText("provisioned")).toBeInTheDocument();
  });

  // A version needing no connections gets no reassurance about connections: the
  // stage is absent, and the flow starts at the agent.
  it("omits provisioning entirely when the version needs no connections", () => {
    const live = cycle({ id: "c1" });
    renderSpine(run([live]));

    expect(screen.queryByText("Provisioning")).not.toBeInTheDocument();
    expect(stepsOnScreen()).toEqual([1, 2, 3, 4, 5]);
  });

  // Every Builds stage is on the rail now, so every merged session is asked —
  // there is no collapsed history left to save a read on.
  it("reads builds for every merged session", () => {
    const first = cycle({
      id: "c1",
      prNumber: 8,
      mergeSha: "aaaa1111bbbb",
      endedAt: "2026-07-10T09:40:00Z",
    });
    const second = cycle({
      id: "c2",
      kind: "fix",
      prNumber: 9,
      mergeSha: "cccc2222dddd",
      endedAt: "2026-07-10T10:40:00Z",
    });
    renderSpine(run([first, second]));

    const asked = (id: string) =>
      buildCalls.filter((c) => c.enabled && c.cycleId === id);
    expect(asked("c1").length).toBeGreaterThan(0);
    expect(asked("c2").length).toBeGreaterThan(0);
  });

  // The one thing still behind a click. Attaching replays every session's
  // history, which is not worth doing to a finished version nobody asked to read.
  it("opens no stream on a SETTLED run until the log is asked for", () => {
    const done = cycle({
      id: "c1",
      prNumber: 4,
      endedAt: "2026-07-10T09:40:00Z",
    });
    renderSpine(run([done], "succeeded"));

    expect(enabledCalls.every((e) => e === false)).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Show agent log" }));
    expect(enabledCalls.at(-1)).toBe(true);
  });

  // …and a LIVE run needs no asking. The log must not vanish the moment the
  // agent opens its pull request and the run moves on to the merge.
  it("keeps a live run's log on screen past the agent's own exit", () => {
    const live = cycle({ id: "c1", branch: "aep/m2-c1", prNumber: 12 });
    mockCycles = [
      {
        cycle: live,
        lines: [
          {
            cycleId: "c1",
            cycleKind: "coding",
            cycleIndex: 1,
            kind: "git_push",
            emitter: "main",
            seq: 1,
            branch: "aep/m2-c1",
          },
        ],
      },
    ];
    renderSpine(run([live]));

    expect(screen.getByText("↑ push aep/m2-c1")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Show agent log" }),
    ).not.toBeInTheDocument();
    expect(enabledCalls.at(-1)).toBe(true);
  });

  // A settled run opens no stream, so it must not narrate one. The hook reports
  // `idle` for a stream nobody asked for; reporting `connecting` had a
  // connection-free page claiming it was "Attaching to the run feed" forever.
  it("says nothing about the feed while no log has been asked for", () => {
    mockPhase = "idle";
    const done = cycle({
      id: "c1",
      prNumber: 4,
      endedAt: "2026-07-10T09:40:00Z",
    });
    renderSpine(run([done], "succeeded"));

    expect(
      screen.queryByText(/Attaching to the run feed/),
    ).not.toBeInTheDocument();
    expect(enabledCalls.every((e) => e === false)).toBe(true);
  });

  it("narrates the feed while the stream is connecting", () => {
    mockPhase = "connecting";
    const live = cycle({ id: "c1" });
    renderSpine(run([live]));
    expect(screen.getByText(/Attaching to the run feed/)).toBeInTheDocument();
    expect(screen.getByText(/Loading agent output/)).toBeInTheDocument();
  });

  it("says so when the run has not dispatched a build session yet", () => {
    renderSpine(run([]));
    expect(
      screen.getByText(/No build session has been dispatched yet/),
    ).toBeInTheDocument();
  });
});
