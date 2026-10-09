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

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import { StartupWaitNotice } from "./StartupWaitNotice";

type RunCycleView = components["schemas"]["RunCycleView"];

// A cycle as the run view serves it while the watcher sees its pod stuck in
// Pending: open, and carrying the wait the watcher recorded.
const stuck: RunCycleView = {
  id: "c1",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-10-06T13:58:00Z",
  recording: "live",
  startupWait: {
    reason: "Unschedulable",
    since: "2026-10-06T13:58:30Z",
    failsAt: "2026-10-06T14:08:00Z",
  },
};

describe("StartupWaitNotice", () => {
  it("shows the wait, its cause and its deadline as a status", () => {
    render(<StartupWaitNotice cycle={stuck} />);
    const notice = screen.getByRole("status");
    expect(notice).toHaveTextContent("Waiting for room in the cluster to start the agent");
    expect(notice).toHaveTextContent("The cluster has no room for the agent right now (CPU, memory or a scheduling rule).");
    expect(notice).toHaveTextContent(/If it has not started by .+, this run fails\./);
  });

  // NotYetApplied is the normal path on Cloud (8-13 min): a calm in-progress
  // status, not a warning or an alert.
  it("shows the platform still preparing the agent as a neutral status, not a warning", () => {
    render(<StartupWaitNotice cycle={{ ...stuck, startupWait: { ...stuck.startupWait!, reason: "NotYetApplied" } }} />);
    const notice = screen.getByRole("status");
    expect(notice).toHaveTextContent("Preparing the agent");
    expect(notice).toHaveTextContent("The platform is still preparing the agent.");
    expect(notice).not.toHaveTextContent("this run fails");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(notice.closest(".MuiAlert-root")).toBeNull();
  });

  it("renders nothing once the agent is running or the cycle has ended", () => {
    const running: RunCycleView = { id: "c1", kind: "coding", attempts: 1, createdAt: "2026-10-06T13:58:00Z", recording: "live" };
    const { container, rerender } = render(<StartupWaitNotice cycle={running} />);
    expect(container).toBeEmptyDOMElement();
    rerender(<StartupWaitNotice cycle={{ ...stuck, endedAt: "2026-10-06T14:08:00Z" }} />);
    expect(container).toBeEmptyDOMElement();
    rerender(<StartupWaitNotice cycle={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });
});
