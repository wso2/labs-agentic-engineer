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
import type { RunProgressState, StampedRunEvent } from "../hooks/useRunProgress";
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

// What the server sends for a log it cannot serve (expired or unavailable):
// the cycle frame AND one platform notice (seq -20, `code: gap`), never an
// empty feed (aep-api codingagent cycle_feed.go, logsUnavailableRunEvent).
const logsUnavailable: StampedRunEvent = {
  cycleId: "c1",
  attempt: 1,
  ts: "2026-10-01T09:20:00Z",
  seq: -20,
  v: 2,
  kind: "notice",
  agentId: "lead",
  level: "warn",
  code: "gap",
  detail: "This cycle's log is not available.",
};

function renderLog(recording: Recording, reconnect = vi.fn(), events: StampedRunEvent[] = []) {
  const progress: RunProgressState = {
    cycles: [{ cycle: cycle(recording), events }],
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
    renderLog("expired", vi.fn(), [logsUnavailable]);
    expect(screen.getByRole("alert")).toHaveTextContent("This run's log is no longer kept (logs are kept for a few days)");
    expect(screen.queryByText(/events are missing from this feed/)).not.toBeInTheDocument();
    expect(screen.queryByText("No output yet.")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("says it cannot be loaded right now, and Try again attaches afresh", () => {
    const reconnect = renderLog("unavailable", vi.fn(), [logsUnavailable]);
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load this run's log right now");
    expect(screen.queryByText(/events are missing from this feed/)).not.toBeInTheDocument();
    expect(screen.queryByText("No output yet.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(reconnect).toHaveBeenCalledOnce();
  });

  it.each(["kept", "live"] as const)("shows the lines of a %s log", (recording) => {
    renderLog(recording);
    expect(screen.getByText("No output yet.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps the lines, with the inline gap notice, for a kept log with a hole", () => {
    renderLog("kept", vi.fn(), [logsUnavailable]);
    expect(screen.getByText(/events are missing from this feed/)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("a build session's header", () => {
  function renderOpen(over: Partial<RunCycleView>) {
    const open = { ...cycle("live"), endedAt: undefined, ...over } as RunCycleView;
    const progress: RunProgressState = { cycles: [{ cycle: open, events: [] }], settledState: undefined, phase: "live", reconnect: vi.fn() };
    const run = { id: "r1", state: "running", createdAt: "2026-10-01T09:00:00Z", cycles: [open] } as unknown as MilestoneRunView;
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <AgentLog projectName="shop" runs={[run]} progress={progress} />
      </OxygenUIThemeProvider>,
    );
  }

  it("says writing now while the agent runs", () => {
    renderOpen({});
    expect(screen.getByText(/writing now/)).toBeInTheDocument();
  });

  it("says waiting to start while the cluster holds the agent", () => {
    renderOpen({ startupWait: { reason: "Unschedulable", since: "2026-10-01T09:00:30Z", failsAt: "2026-10-01T09:10:00Z" } });
    expect(screen.getByText(/waiting to start/)).toBeInTheDocument();
    expect(screen.queryByText(/writing now/)).not.toBeInTheDocument();
  });
});

