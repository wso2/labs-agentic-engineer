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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ProjectChat } from "../chatStore";
import type { ChatView } from "../chatView";

// The chat panel talks to the agent of the view the user is on: the Issues
// Page has its own, the rest of the project the main chat.

const ready = (items: ProjectChat["items"] = []): ProjectChat => ({
  status: "ready",
  error: null,
  items,
  turn: { phase: "idle" },
});

const mainSend = vi.fn(() => Promise.resolve(true));
const issuesSend = vi.fn(() => Promise.resolve(true));
const stores: Record<ChatView, { send: typeof mainSend }> = { main: { send: mainSend }, issues: { send: issuesSend } };
const viewsRead: Array<ChatView | undefined> = [];
vi.mock("../useProjectChat", () => ({
  useProjectChat: (_projectName: string, view?: ChatView) => {
    viewsRead.push(view);
    return ready();
  },
  chatStoreFor: (view: ChatView) => stores[view],
  chatStore: { retry: vi.fn(), answer: vi.fn() },
  canSend: () => true,
}));
vi.mock("../../spec/useSpecWorkspace", () => ({
  useSpecFeature: () => null,
  useSpecModel: () => ({ data: { features: [] } }),
}));
vi.mock("../../../auth/SessionContext", () => ({ useSession: () => ({ orgHandle: "acme" }) }));
vi.mock("../../projects/api/queries", () => ({
  useProject: () => ({ data: undefined }),
  projectLabel: (p: { name: string }) => p.name,
}));
vi.mock("../../prototype/usePrototypes", () => ({ usePrototypes: () => undefined }));
vi.mock("../useStartInterview", () => ({ useStartInterview: () => ({ start: vi.fn(), ready: true, waiting: false }) }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);

const { ChatPanel } = await import("./ChatPanel");

function renderPanel(page: "issues" | "overview", card: "issue" | null = null) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ChatPanel projectName="shop" page={page} card={card} specFile={null} onClose={() => {}} />
    </OxygenUIThemeProvider>,
  );
}

function typeAndSend(text: string) {
  fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: text } });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
}

describe("ChatPanel", () => {
  beforeEach(() => {
    mainSend.mockClear();
    issuesSend.mockClear();
    viewsRead.length = 0;
  });
  afterEach(cleanup);

  it("on the Issues Page, reads the issues chat, says it is about the project's issues and invites a report", () => {
    renderPanel("issues");
    expect(new Set(viewsRead)).toEqual(new Set(["issues"]));
    expect(screen.getByText(/Talking about/).textContent).toBe("Talking about the project's issues.");
    expect(screen.getByText("Tell me what's broken or what you need, and I'll draft an issue.")).toBeTruthy();
  });

  it("on the Issues Page, sends to the issues store", () => {
    renderPanel("issues");
    typeAndSend("Login is broken");
    expect(issuesSend).toHaveBeenCalledWith("shop", "Login is broken", { kind: "product" });
    expect(mainSend).not.toHaveBeenCalled();
  });

  it("on the overview, reads and sends to the main chat", () => {
    renderPanel("overview");
    expect(new Set(viewsRead)).toEqual(new Set(["main"]));
    expect(screen.getByText(/Talking about/).textContent).toBe("Talking about the whole product.");
    typeAndSend("Add approvals");
    expect(mainSend).toHaveBeenCalledWith("shop", "Add approvals", { kind: "product" });
    expect(issuesSend).not.toHaveBeenCalled();
  });

  it("on the Issue card, which has no agent of its own, stays in the main chat", () => {
    renderPanel("issues", "issue");
    expect(new Set(viewsRead)).toEqual(new Set(["main"]));
    typeAndSend("Look at this one");
    expect(mainSend).toHaveBeenCalled();
    expect(issuesSend).not.toHaveBeenCalled();
  });
});
