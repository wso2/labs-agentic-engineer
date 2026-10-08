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

import type { ReactNode } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import type { BuildCardData } from "../hooks/useBuildCard";
import { failureExplanation } from "../model/explain";

// F1 on the Build card: while the cluster holds the coding agent, the card
// says it is waiting (never that the agent is writing); once the platform
// gave up on starting it, the card says it could not start, and Retry is
// the button the copy names.

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  createLink:
    () =>
    ({ children }: { children?: ReactNode }) => <a href="#">{children}</a>,
}));
vi.mock("../../projects/components/CardOverlay", () => ({
  CardOverlay: ({ actions, children }: { actions?: ReactNode; children?: ReactNode }) => (
    <div>
      {actions}
      {children}
    </div>
  ),
}));
vi.mock("../../agent-chat/useProjectChat", () => ({ chatStore: { post: vi.fn() } }));
vi.mock("./BuildLogs", () => ({ BuildLogs: () => null }));
vi.mock("./BuildTasks", () => ({ BuildTasks: () => null }));
vi.mock("../hooks/useNextInterview", () => ({ useNextInterview: () => null }));
vi.mock("../hooks/useBuildOutcome", () => ({
  useBuildOutcome: () => ({ run: undefined, validation: undefined, groups: null, outcome: null, baseline: null, error: null }),
}));
const idle = { mutate: vi.fn(), isPending: false, error: null };
vi.mock("../api/runs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/runs")>()),
  useCancelRun: () => idle,
}));
vi.mock("../api/builds", () => ({ useStartBuild: () => idle, useStartFix: () => idle }));
let data: BuildCardData;
vi.mock("../hooks/useBuildCard", () => ({ useBuildCard: () => data }));

const { BuildCard } = await import("./BuildCard");

afterEach(cleanup);

const cycle = (over: Partial<RunCycleView>): RunCycleView => ({
  id: "c1",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-10-06T13:58:00Z",
  recording: "live",
  ...over,
});

function cardData(run: MilestoneRunView, live: boolean): BuildCardData {
  return {
    build: { version: "v1", status: live ? "building" : "failed", features: [{ id: "F1", name: "Expenses", lines: [] }], productWide: [] },
    summary: undefined,
    rows: [],
    buildsError: null,
    retryBuilds: vi.fn(),
    runs: [run],
    current: run,
    runState: run.state,
    live,
    progress: { cycles: run.cycles.map((c) => ({ cycle: c, events: [] })), settledState: undefined, phase: "live", reconnect: vi.fn() },
    steps: { phases: [], current: null, fraction: 0 },
    replaying: false,
    tasks: { data: [], isError: false, error: null, refetch: vi.fn() },
    claims: { byIssue: new Map() },
    park: null,
    explanation: failureExplanation(run) ?? null,
    deploy: undefined,
    repoUrl: undefined,
  } as unknown as BuildCardData;
}

const renderCard = () =>
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <BuildCard projectName="shop" version="v1" />
    </OxygenUIThemeProvider>,
  );

describe("the Build card, for an agent the cluster has not started", () => {
  it("says it is waiting for room, and nothing says the agent is writing", () => {
    const waiting = cycle({ startupWait: { reason: "Unschedulable", since: "2026-10-06T13:58:30Z", failsAt: "2026-10-06T14:08:00Z" } });
    data = cardData({ id: "r1", state: "running", createdAt: "2026-10-06T13:58:00Z", cycles: [waiting] } as unknown as MilestoneRunView, true);
    renderCard();
    expect(screen.getByText("Waiting for room in the cluster to start the agent")).toBeInTheDocument();
    expect(screen.queryByText(/writing now/)).not.toBeInTheDocument();
  });

  it("says it could not start, and names the Retry button the card shows", () => {
    const failed = cycle({ endedAt: "2026-10-06T14:08:00Z", recording: "kept", agentReason: "startup_failed:Unschedulable: 0/1 nodes are available." });
    data = cardData(
      { id: "r1", state: "failed", terminalReason: "agent-start-failed", createdAt: "2026-10-06T13:58:00Z", cycles: [failed] } as unknown as MilestoneRunView,
      false,
    );
    renderCard();
    expect(screen.getByText("The coding agent could not start")).toBeInTheDocument();
    expect(screen.getByText(/Retry once the cluster has room\./)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.queryByText("Waiting for room in the cluster to start the agent")).not.toBeInTheDocument();
  });
});
