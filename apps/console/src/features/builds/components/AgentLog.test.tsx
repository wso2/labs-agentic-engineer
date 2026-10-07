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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import type { RunProgressState } from "../hooks/useRunProgress";
import { AgentLog } from "./AgentLog";

// A build session's log says why it is empty when the platform knows: no
// longer kept, or not loadable right now (with Try again).

type RunCycleView = components["schemas"]["RunCycleView"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type Recording = NonNullable<RunCycleView["recording"]>;

afterEach(cleanup);

const cycle = (recording: Recording): RunCycleView => ({
  id: "c1",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-10-01T09:00:00Z",
  endedAt: "2026-10-01T09:20:00Z",
  recording,
});

function renderLog(recording: Recording, reconnect = vi.fn()) {
  const progress: RunProgressState = {
    cycles: [{ cycle: cycle(recording), events: [] }],
    settledState: "completed",
    phase: "ended",
    reconnect,
  };
  const run = { id: "r1", state: "completed", createdAt: "2026-10-01T09:00:00Z", cycles: [cycle(recording)] } as unknown as MilestoneRunView;
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <AgentLog projectName="shop" runs={[run]} progress={progress} />
    </OxygenUIThemeProvider>,
  );
  return reconnect;
}

describe("a build session's log", () => {
  it("says it is no longer kept, instead of reading as an agent that wrote nothing", () => {
    renderLog("expired");
    expect(screen.getByText("This run's log is no longer kept (logs are kept for a few days)")).toBeInTheDocument();
    expect(screen.queryByText("No output yet.")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("says it cannot be loaded right now, and Try again attaches afresh", () => {
    const reconnect = renderLog("unavailable");
    expect(screen.getByText("Couldn't load this run's log right now")).toBeInTheDocument();
    expect(screen.queryByText("No output yet.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(reconnect).toHaveBeenCalledOnce();
  });

  it.each(["kept", "live"] as const)("shows the lines of a %s log", (recording) => {
    renderLog(recording);
    expect(screen.getByText("No output yet.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
