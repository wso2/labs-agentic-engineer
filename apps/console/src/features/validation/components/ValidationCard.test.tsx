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

// F1 on the Validation card: an attempt the cluster holds says it is
// waiting; one it never started says so, and names the button the card shows
// (Validate before any verdict, Revalidate after one).

type RunCycleView = components["schemas"]["RunCycleView"];

vi.mock("../../projects/components/CardOverlay", () => ({
  CardOverlay: ({ actions, children }: { actions?: ReactNode; children?: ReactNode }) => (
    <div>
      {actions}
      {children}
    </div>
  ),
}));
vi.mock("../../builds/api/builds", () => ({ useBuilds: () => ({ data: [] }) }));
vi.mock("../../builds/api/runs", () => ({
  useValidationSnapshot: () => ({ data: undefined, isError: false }),
  useVersionLedger: () => ({ data: [] }),
}));
vi.mock("../../builds/hooks/useNextInterview", () => ({ useNextInterview: () => null }));
vi.mock("../../builds/hooks/useRunProgress", () => ({ useRunProgress: () => ({ cycles: [], phase: "ended" }) }));
let runs: { id: string; cycles: RunCycleView[] }[] = [];
vi.mock("../api/validations", () => ({
  useValidation: () => ({ data: { state: "running", live: false, deployed: true, runs }, isError: false }),
  useRevalidate: () => ({ mutate: vi.fn(), isPending: false, error: null, reset: vi.fn() }),
}));

const { ValidationCard } = await import("./ValidationCard");

afterEach(cleanup);

const cycle = (id: string, over: Partial<RunCycleView>): RunCycleView => ({
  id,
  kind: "validation",
  attempts: 1,
  createdAt: "2026-10-06T13:58:00Z",
  recording: "kept",
  ...over,
});
const neverStarted = (id: string) =>
  cycle(id, { endedAt: "2026-10-06T14:08:00Z", agentReason: "startup_failed:Unschedulable: no room" });

const renderCard = () =>
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ValidationCard projectName="shop" version="v1" />
    </OxygenUIThemeProvider>,
  );

describe("a validation attempt whose agent the cluster has not started", () => {
  it("is waiting to start, with the wait's cause and deadline", () => {
    runs = [
      {
        id: "r1",
        cycles: [
          cycle("c1", {
            recording: "live",
            startupWait: { reason: "Unschedulable", since: "2026-10-06T13:58:30Z", failsAt: "2026-10-06T14:08:00Z" },
          }),
        ],
      },
    ];
    renderCard();
    expect(screen.getAllByText("Waiting to start").length).toBeGreaterThan(0);
    expect(screen.getByText("Waiting for room in the cluster to start the agent")).toBeInTheDocument();
  });

  it("could not start; before any verdict the copy and the button both say Validate", () => {
    runs = [{ id: "r1", cycles: [neverStarted("c1")] }];
    renderCard();
    expect(screen.getByText("The validation agent could not start")).toBeInTheDocument();
    expect(screen.getByText(/Validate once the cluster has room\./)).toBeInTheDocument();
    expect(screen.queryByText(/Revalidate/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Validate" })).toBeInTheDocument();
  });

  it("could not start; after an earlier verdict the copy and the button both say Revalidate", () => {
    runs = [{ id: "r1", cycles: [cycle("c0", { endedAt: "2026-10-06T12:00:00Z", mergeSha: "abc", validationVerdict: "failed" }), neverStarted("c1")] }];
    renderCard();
    expect(screen.getByText(/Revalidate once the cluster has room\./)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revalidate" })).toBeInTheDocument();
  });
});
