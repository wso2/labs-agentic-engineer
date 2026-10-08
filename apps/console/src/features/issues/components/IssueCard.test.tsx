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
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

// An Issue card says when the issue is closed: a closed issue has no chat of
// its own, so the panel beside it draws none, and it cannot be handed over.
// An open one can be handed to the coding agent from the card: the person
// picks the component it is about, and the card's log takes over.

let state: "open" | "closed" | "unknown" = "open";
let components: string[] = ["api", "web"];
let designFails = false;
let listed: { Labels: string[]; milestoneNumber?: number } = { Labels: [] };
const post = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>();

vi.mock("../../../api/client", () => ({
  client: { POST: (path: string, init: Record<string, unknown>) => post(path, init) },
}));
vi.mock("../../agent-chat/useIssueThread", () => ({ useIssueThreadState: () => state }));
vi.mock("../../projects/components/CardOverlay", () => ({ CardOverlay: ({ children }: { children: ReactNode }) => <>{children}</> }));
vi.mock("../../builds/components/TaskLog", () => ({ TaskLog: () => <div>the task log</div> }));
vi.mock("../../deploy/api/deploy", () => ({
  useDesignDependencies: () =>
    designFails
      ? { data: undefined, isPending: false, isError: true }
      : { data: components.map((componentName) => ({ componentName, dependencies: [] })), isPending: false, isError: false },
}));
vi.mock("../api/issues", () => ({
  issuesListKey: (p: string) => ["projects", p, "issues"],
  issueDetailKey: (p: string, n: number) => ["projects", p, "issues", n],
  useProjectIssues: () => ({
    data: [{ Number: 7, Title: "Save does nothing", Body: "It does nothing.", URL: "https://github.com/a/b/issues/7", State: "open", ...listed }],
    isPending: false,
    isError: false,
    error: null,
  }),
  useIssueDetail: () => ({ data: undefined, isPending: false, isError: false, error: null }),
}));

const { IssueCard } = await import("./IssueCard");

const HAND = "Hand to the coding agent";
const NO_VERSION = "Deploy a version first: the coding agent works in a deployed version's milestone.";

function renderCard() {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <IssueCard projectName="shop" number="7" />
      </OxygenUIThemeProvider>
    </QueryClientProvider>,
  );
}

function handOver() {
  fireEvent.click(screen.getByRole("button", { name: "Hand it over" }));
}

beforeEach(() => {
  state = "open";
  components = ["api", "web"];
  designFails = false;
  listed = { Labels: [] };
  post.mockReset();
});
afterEach(cleanup);

describe("IssueCard", () => {
  it("says a closed issue is closed, and offers no hand-off", () => {
    state = "closed";
    renderCard();
    expect(screen.getByText("This issue is closed.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: HAND })).toBeNull();
  });

  it("says closed in its header too while the list still lags the server's word", () => {
    // The list reads the issue open; the server has said its thread is gone.
    state = "closed";
    renderCard();
    expect(screen.getByText(/· Closed$/)).toBeTruthy();
    expect(screen.queryByText(/· Open$/)).toBeNull();
  });

  it("says nothing of the kind for an open issue, and offers the hand-off", () => {
    renderCard();
    expect(screen.queryByText("This issue is closed.")).toBeNull();
    expect((screen.getByRole("button", { name: HAND }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("hands the issue over with the component the person picked, and its log takes over", async () => {
    post.mockResolvedValueOnce({ data: undefined, error: undefined, response: { status: 202 } });
    renderCard();
    expect(screen.queryByText("the task log")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    expect(screen.getByText("Which component is it about?")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Hand it over" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("radio", { name: "web" }));
    handOver();
    expect(await screen.findByText("Handed to the coding agent.")).toBeTruthy();
    expect(post).toHaveBeenCalledWith("/projects/{projectName}/tasks/{issueNumber}/promote-from-issue", {
      params: { path: { projectName: "shop", issueNumber: 7 } },
      body: { componentName: "web" },
      parseAs: "text",
    });
    expect(screen.getByText("the task log")).toBeTruthy();
    expect(screen.queryByRole("button", { name: HAND })).toBeNull();
  });

  it("still asks which component when the design has one, and sends nothing until asked", async () => {
    components = ["api"];
    post.mockResolvedValueOnce({ data: undefined, error: undefined, response: { status: 202 } });
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    expect((screen.getByRole("radio", { name: "api" }) as HTMLInputElement).checked).toBe(true);
    expect(post).not.toHaveBeenCalled();
    handOver();
    await screen.findByText("Handed to the coding agent.");
    expect(post.mock.calls[0]?.[1]).toMatchObject({ body: { componentName: "api" } });
  });

  it("shows the server's words when there is no deployed version", async () => {
    post.mockResolvedValueOnce({ data: undefined, error: { code: "conflict", message: NO_VERSION }, response: { status: 409 } });
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    fireEvent.click(screen.getByRole("radio", { name: "api" }));
    handOver();
    expect(await screen.findByText(NO_VERSION)).toBeTruthy();
    expect(screen.queryByText("Handed to the coding agent.")).toBeNull();
    expect(screen.queryByText("the task log")).toBeNull();
  });

  it("says it could not hand it over when anything else fails", async () => {
    post.mockResolvedValueOnce({ data: undefined, error: { code: "internal", message: "boom" }, response: { status: 500 } });
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    fireEvent.click(screen.getByRole("radio", { name: "api" }));
    handOver();
    expect(await screen.findByText("Couldn't hand it to the coding agent. Try again.")).toBeTruthy();
    expect(screen.queryByText("boom")).toBeNull();
  });

  it("puts the picker away on Cancel", async () => {
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Which component is it about?")).toBeNull());
    expect(screen.getByRole("button", { name: HAND })).toBeTruthy();
  });

  it("says so when the design has no components to hand it to", () => {
    components = [];
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    expect(screen.getByText("The design has no components yet, so there is nothing to hand it to.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Hand it over" })).toBeNull();
  });

  it("shows the coding agent's log, and no hand-off, for an issue handed over before (armed, in a version's milestone)", () => {
    listed = { Labels: ["bug", "aep"], milestoneNumber: 3 };
    renderCard();
    expect(screen.getByText("the task log")).toBeTruthy();
    expect(screen.queryByRole("button", { name: HAND })).toBeNull();
  });

  it("offers the hand-off for an armed issue in no milestone: nothing has taken it on", () => {
    listed = { Labels: ["aep"] };
    renderCard();
    expect(screen.queryByText("the task log")).toBeNull();
    expect(screen.getByRole("button", { name: HAND })).toBeTruthy();
  });

  it("offers no hand-off for an issue the platform works another way", () => {
    listed = { Labels: ["validation"] };
    renderCard();
    expect(screen.queryByRole("button", { name: HAND })).toBeNull();
  });

  it("holds the picker still while the hand-off is in flight", async () => {
    post.mockReturnValueOnce(new Promise(() => {}));
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    fireEvent.click(screen.getByRole("radio", { name: "api" }));
    handOver();
    await waitFor(() => expect((screen.getByRole("button", { name: "Hand it over" }) as HTMLButtonElement).disabled).toBe(true));
    expect((screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement).disabled).toBe(true);
    for (const radio of screen.getAllByRole("radio")) expect((radio as HTMLInputElement).disabled).toBe(true);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("says so when the design's components cannot be read", () => {
    designFails = true;
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: HAND }));
    expect(screen.getByText("The design's components could not be read.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Hand it over" })).toBeNull();
  });
});
