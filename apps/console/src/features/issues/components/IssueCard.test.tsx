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
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

// An Issue card says when the issue is closed: a closed issue has no chat of
// its own, so the panel beside it draws none.

let state: "open" | "closed" | "unknown" = "open";
vi.mock("../../agent-chat/useIssueThread", () => ({ useIssueThreadState: () => state }));
vi.mock("../../projects/components/CardOverlay", () => ({ CardOverlay: ({ children }: { children: ReactNode }) => <>{children}</> }));
vi.mock("../../builds/components/TaskLog", () => ({ TaskLog: () => null }));
vi.mock("../api/issues", () => ({
  useProjectIssues: () => ({
    data: [{ Number: 7, Title: "Save does nothing", Body: "It does nothing.", URL: "https://github.com/a/b/issues/7", State: "open", Labels: [] }],
    isPending: false,
    isError: false,
    error: null,
  }),
  useIssueDetail: () => ({ data: undefined, isPending: false, isError: false, error: null }),
}));

const { IssueCard } = await import("./IssueCard");

function renderCard() {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <IssueCard projectName="shop" number="7" />
    </OxygenUIThemeProvider>,
  );
}

afterEach(cleanup);

describe("IssueCard", () => {
  it("says a closed issue is closed, and offers no hand-off", () => {
    state = "closed";
    renderCard();
    expect(screen.getByText("This issue is closed.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Hand to the coding agent" })).toBeNull();
  });

  it("says nothing of the kind for an open issue", () => {
    state = "open";
    renderCard();
    expect(screen.queryByText("This issue is closed.")).toBeNull();
    expect(screen.getByRole("button", { name: "Hand to the coding agent" })).toBeTruthy();
  });
});
