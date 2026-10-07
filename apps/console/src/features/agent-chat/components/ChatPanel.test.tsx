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

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ProjectChat } from "../chatStore";
import type { ChatView } from "../chatView";
import type { ComposeRequest } from "../../shell/chatPanel";

// The chat panel talks to the agent of the view the user is on: the Issues
// Page has its own, the rest of the project the main chat.

let chatStatus: ProjectChat["status"] = "ready";
const ready = (items: ProjectChat["items"] = []): ProjectChat => ({
  status: chatStatus,
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

const onComposeApplied = vi.fn();

function panel(page: "issues" | "overview", card: "issue" | null, composeRequest?: ComposeRequest | null) {
  return (
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ChatPanel
        projectName="shop"
        page={page}
        card={card}
        specFile={null}
        composeRequest={composeRequest ?? null}
        onComposeApplied={onComposeApplied}
        onClose={() => {}}
      />
    </OxygenUIThemeProvider>
  );
}

function renderPanel(page: "issues" | "overview", card: "issue" | null = null) {
  return render(panel(page, card));
}

function typeAndSend(text: string) {
  fireEvent.change(screen.getByLabelText("Message the agent"), { target: { value: text } });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
}

describe("ChatPanel", () => {
  beforeEach(() => {
    mainSend.mockClear();
    issuesSend.mockClear();
    onComposeApplied.mockClear();
    chatStatus = "ready";
    viewsRead.length = 0;
  });
  afterEach(cleanup);

  it("on the Issues Page, reads the issues chat, says it is about the project's issues and invites a report", () => {
    renderPanel("issues");
    expect(new Set(viewsRead)).toEqual(new Set(["issues"]));
    expect(screen.getByText(/Talking about/).textContent).toBe("Talking about the project's issues.");
    expect(screen.getByText("Tell me what's broken or what you need, and I'll draft an issue.")).toBeTruthy();
    expect(screen.getByLabelText("Message the agent").getAttribute("placeholder")).toBe(
      "Describe what's broken, or what you need…",
    );
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
    expect(screen.getByLabelText("Message the agent").getAttribute("placeholder")).toBe("Tell the agent what to change…");
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

  describe("a compose request", () => {
    const input = () => screen.getByLabelText("Message the agent") as HTMLTextAreaElement;
    const issue = (nonce: number): ComposeRequest => ({ text: "/issue ", view: "issues", projectName: "shop", nonce });

    it("fills the composer, focuses it with the cursor at the end, and sends nothing", async () => {
      render(panel("issues", null, issue(1)));
      await waitFor(() => expect(input().value).toBe("/issue "));
      await waitFor(() => expect(document.activeElement).toBe(input()));
      expect(input().selectionStart).toBe("/issue ".length);
      expect(input().selectionEnd).toBe("/issue ".length);
      expect(issuesSend).not.toHaveBeenCalled();
      expect(mainSend).not.toHaveBeenCalled();
    });

    it("tells the shell it was applied, by nonce, so the request is single-use", async () => {
      render(panel("issues", null, issue(4)));
      await waitFor(() => expect(onComposeApplied).toHaveBeenCalledWith(4));
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("applies one request once: the same nonce again leaves what was typed", async () => {
      const view = renderPanel("issues");
      view.rerender(panel("issues", null, issue(1)));
      await waitFor(() => expect(input().value).toBe("/issue "));
      fireEvent.change(input(), { target: { value: "/issue login is broken" } });
      view.rerender(panel("issues", null, issue(1)));
      expect(input().value).toBe("/issue login is broken");
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("replaces a typed draft when the nonce is new", async () => {
      const view = render(panel("issues", null, issue(1)));
      await waitFor(() => expect(input().value).toBe("/issue "));
      fireEvent.change(input(), { target: { value: "half a thought" } });
      view.rerender(panel("issues", null, issue(2)));
      await waitFor(() => expect(input().value).toBe("/issue "));
    });

    it("applies a request made before the chat mounted, once, on first mount", async () => {
      render(panel("issues", null, issue(3)));
      await waitFor(() => expect(input().value).toBe("/issue "));
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("does not refill after the shell has cleared it: closing and reopening the chat leaves an empty draft", async () => {
      const first = render(panel("issues", null, issue(1)));
      await waitFor(() => expect(onComposeApplied).toHaveBeenCalledWith(1));
      first.unmount();
      // The shell cleared the request when it was applied; the chat reopens.
      render(panel("issues", null, null));
      expect(input().value).toBe("");
    });

    it("is not applied by the main chat's composer after leaving the Issues Page", async () => {
      render(panel("overview", null, issue(1)));
      await act(async () => {});
      expect(input().value).toBe("");
      expect(document.activeElement).not.toBe(input());
      expect(onComposeApplied).not.toHaveBeenCalled();
    });

    it("is not applied to another project's composer", async () => {
      render(panel("issues", null, { ...issue(1), projectName: "other" }));
      await act(async () => {});
      expect(input().value).toBe("");
      expect(onComposeApplied).not.toHaveBeenCalled();
    });

    it("applied while the conversation loads, focuses once the input is enabled", async () => {
      chatStatus = "loading";
      const view = render(panel("issues", null, issue(1)));
      await waitFor(() => expect(input().value).toBe("/issue "));
      expect(document.activeElement).not.toBe(input());
      chatStatus = "ready";
      view.rerender(panel("issues", null, null));
      await waitFor(() => expect(document.activeElement).toBe(input()));
      expect(input().selectionStart).toBe("/issue ".length);
    });
  });
});
