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

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ProjectChat } from "../chatStore";
import type { ChatView } from "../chatView";
import { ChatPanelContext, type ChatPanelControls, type ComposeRequest } from "../../shell/chatPanel";

// The chat panel holds one thread, the one of the page in view: the project's
// main chat; on the Issues page the Issues chat; on an open issue's card that
// issue's chat. The breadcrumb names the thread and moves between threads;
// the threads menu lists them all.

let chatStatus: ProjectChat["status"] = "ready";
const chats: Record<ChatView, ProjectChat["items"]> = { main: [], issues: [], issue: [] };
const chatOf = (view: ChatView): ProjectChat => ({
  status: chatStatus,
  error: null,
  items: chats[view],
  turn: { phase: "idle" },
});

const mainSend = vi.fn(() => Promise.resolve(true));
const issuesSend = vi.fn(() => Promise.resolve(true));
const issueSend = vi.fn(() => Promise.resolve(true));
const stores: Record<ChatView, { send: typeof mainSend }> = {
  main: { send: mainSend },
  issues: { send: issuesSend },
  issue: { send: issueSend },
};
const storeIssue = vi.fn();
const post = vi.fn();
let issueThreads: { issueNumber: number; count: number }[] = [];
vi.mock("../useProjectChat", () => ({
  useProjectChat: (_projectName: string, view: ChatView = "main") => chatOf(view),
  chatStoreFor: (view: ChatView, issueNumber?: number) => {
    storeIssue(view, issueNumber);
    return stores[view];
  },
  chatStore: { retry: vi.fn(), answer: vi.fn(), post: (...args: unknown[]) => post(...args) },
  canSend: () => true,
}));
// Whether the issue in view is open: the issue list's word, or the server's
// 409 `issue_closed` once its thread is found removed.
let issueState: "open" | "closed" | "unknown" = "unknown";
vi.mock("../useIssueThread", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../useIssueThread")>()),
  useIssueThreadState: () => issueState,
  useOpenIssueThreads: () => issueThreads,
}));
vi.mock("../../spec/useSpecWorkspace", () => ({
  useSpecFeature: () => null,
  useSpecModel: () => ({ data: { features: [] } }),
}));
vi.mock("../../../auth/SessionContext", () => ({ useSession: () => ({ orgHandle: "acme" }) }));
vi.mock("../../projects/api/queries", () => ({
  useProject: () => ({ data: { name: "Acme Expenses" } }),
  projectLabel: (p: { name: string }) => p.name,
}));
vi.mock("../../prototype/usePrototypes", () => ({ usePrototypes: () => undefined }));
vi.mock("../useStartInterview", () => ({ useStartInterview: () => ({ start: vi.fn(), ready: true, waiting: false }) }));
const navigate = vi.fn(async (): Promise<void> => undefined);
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);

const { ChatPanel } = await import("./ChatPanel");

const onComposeApplied = vi.fn();
const controls: ChatPanelControls = { open: vi.fn(), compose: vi.fn() };

/** The issue whose chat the Questions card answers, in a test that opens it so. */
let questionsIssue: number | null = null;

function panel(page: "issues" | "overview", card: "issue" | "questions" | null = null, composeRequest: ComposeRequest | null = null) {
  // An issue's card, or the Questions card answering that issue's chat.
  const onCard = card === "issue" || (card === "questions" && questionsIssue !== null);
  return (
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ChatPanelContext.Provider value={controls}>
        <ChatPanel
          projectName="shop"
          page={page}
          card={card}
          specFile={null}
          issueNumber={onCard ? 7 : null}
          composeRequest={composeRequest}
          onComposeApplied={onComposeApplied}
          onClose={() => {}}
        />
      </ChatPanelContext.Provider>
    </OxygenUIThemeProvider>
  );
}

const thread = (name: string) => screen.queryByRole("region", { name });
const mainChat = () => thread("Main chat");
const issuesChat = () => thread("Issues chat");
const issueChat = () => thread("Issue #7 chat");
const inputIn = (el: HTMLElement) => within(el).getByLabelText("Message the agent") as HTMLTextAreaElement;
const crumbs = () => screen.getByRole("navigation", { name: "Chat thread" });

function typeAndSend(where: HTMLElement, text: string) {
  fireEvent.change(inputIn(where), { target: { value: text } });
  fireEvent.click(within(where).getByRole("button", { name: "Send message" }));
}

describe("ChatPanel", () => {
  beforeEach(() => {
    mainSend.mockClear();
    issuesSend.mockClear();
    issueSend.mockClear();
    storeIssue.mockClear();
    post.mockClear();
    issueState = "unknown";
    issueThreads = [];
    questionsIssue = null;
    onComposeApplied.mockClear();
    navigate.mockClear();
    chatStatus = "ready";
    chats.main = [];
    chats.issues = [];
  });
  afterEach(cleanup);

  it("on a project page, holds the main chat alone, and sends to it", () => {
    render(panel("overview"));
    expect(issuesChat()).toBeNull();
    expect(crumbs().textContent).toBe("acme›Acme Expenses");
    expect(screen.getByText(/Talking about/).textContent).toBe("Talking about the whole product.");
    expect(inputIn(mainChat()!).getAttribute("placeholder")).toBe("Tell the agent what to change…");
    typeAndSend(mainChat()!, "Add approvals");
    expect(mainSend).toHaveBeenCalledWith("shop", "Add approvals", { kind: "product" });
    expect(issuesSend).not.toHaveBeenCalled();
  });

  describe("on the Issues page", () => {
    it("holds the Issues chat in place of the main chat, sending to it", () => {
      render(panel("issues"));
      const s = issuesChat()!;
      expect(mainChat()).toBeNull();
      expect(screen.queryByText(/Main chat/)).toBeNull();
      expect(crumbs().textContent).toBe("acme›Acme Expenses›Issues");
      expect(within(s).getByText("Tell me what's broken or what you need, and I'll draft an issue.")).toBeTruthy();
      expect(within(s).getByText(/Talking about/).textContent).toBe("Talking about the project's issues.");
      expect(inputIn(s).getAttribute("placeholder")).toBe("Describe what's broken, or what you need…");
      typeAndSend(s, "Login is broken");
      expect(issuesSend).toHaveBeenCalledWith("shop", "Login is broken", { kind: "product" });
      expect(mainSend).not.toHaveBeenCalled();
    });

    it("an issue's card takes its place with that issue's chat", () => {
      issueState = "open";
      const { rerender } = render(panel("issues"));
      rerender(panel("issues", "issue"));
      expect(issuesChat()).toBeNull();
      expect(issueChat()).not.toBeNull();
    });
  });

  describe("on an issue's card", () => {
    it("holds an open issue's own chat, sending to it", () => {
      issueState = "open";
      render(panel("issues", "issue"));
      const s = issueChat()!;
      expect(mainChat()).toBeNull();
      expect(crumbs().textContent).toBe("acme›Acme Expenses›Issues›#7");
      expect(within(s).getByText(/Talking about/).textContent).toBe("Talking about issue #7.");
      expect(within(s).getByText("Ask me about this issue, or tell me what to do with it.")).toBeTruthy();
      typeAndSend(s, "Comment that it is fixed");
      expect(issueSend).toHaveBeenCalledWith("shop", "Comment that it is fixed", { kind: "product" });
      expect(storeIssue).toHaveBeenCalledWith("issue", 7);
      expect(mainSend).not.toHaveBeenCalled();
    });

    it("holds the main chat for a closed issue, or one not read yet", () => {
      issueState = "closed";
      render(panel("issues", "issue"));
      expect(issueChat()).toBeNull();
      expect(mainChat()).not.toBeNull();
      expect(crumbs().textContent).toBe("acme›Acme Expenses");
      cleanup();
      issueState = "unknown";
      render(panel("issues", "issue"));
      expect(issueChat()).toBeNull();
      expect(mainChat()).not.toBeNull();
    });

    it("when the issue turns out closed, its chat goes and the main chat says so", () => {
      issueState = "open";
      const view = render(panel("issues", "issue"));
      expect(post).not.toHaveBeenCalled();
      issueState = "closed";
      view.rerender(panel("issues", "issue"));
      expect(issueChat()).toBeNull();
      expect(mainChat()).not.toBeNull();
      expect(post).toHaveBeenCalledTimes(1);
      expect(post).toHaveBeenCalledWith("shop", "Issue #7 was closed; its chat was removed.");
      view.rerender(panel("issues", "issue"));
      expect(post).toHaveBeenCalledTimes(1);
    });

    it("stays on the Questions card answering it", () => {
      issueState = "open";
      questionsIssue = 7;
      render(panel("issues", "questions"));
      expect(issueChat()).not.toBeNull();
      expect(issuesChat()).toBeNull();
    });

    it("says nothing for an issue already closed on arrival", () => {
      issueState = "closed";
      const view = render(panel("issues", "issue"));
      view.rerender(panel("issues", "issue"));
      expect(post).not.toHaveBeenCalled();
    });

    it("keeps no draft from another thread", () => {
      issueState = "open";
      const { rerender } = render(panel("issues"));
      fireEvent.change(inputIn(issuesChat()!), { target: { value: "half a thought" } });
      rerender(panel("issues", "issue"));
      expect(inputIn(issueChat()!).value).toBe("");
    });
  });

  describe("the breadcrumb", () => {
    it("marks the thread in view as the current location, and links every crumb before it", () => {
      issueState = "open";
      render(panel("issues", "issue"));
      const current = within(crumbs()).getByText("#7");
      expect(current.getAttribute("aria-current")).toBe("location");
      expect(within(crumbs()).getAllByRole("button").map((b) => b.textContent)).toEqual(["acme", "Acme Expenses", "Issues"]);
    });

    it("the org goes to the org's page, the project to its main chat, Issues to the Issues chat", async () => {
      issueState = "open";
      render(panel("issues", "issue"));
      await act(async () => fireEvent.click(within(crumbs()).getByRole("button", { name: "acme" })));
      expect(navigate).toHaveBeenLastCalledWith({ to: "/" });
      await act(async () => fireEvent.click(within(crumbs()).getByRole("button", { name: "Acme Expenses" })));
      expect(navigate).toHaveBeenLastCalledWith({ to: "/projects/$projectName", params: { projectName: "shop" } });
      await act(async () => fireEvent.click(within(crumbs()).getByRole("button", { name: "Issues" })));
      expect(navigate).toHaveBeenLastCalledWith({ to: "/projects/$projectName/issues", params: { projectName: "shop" } });
    });

    it("on the main chat, only the org is a link", () => {
      render(panel("overview"));
      expect(within(crumbs()).getAllByRole("button").map((b) => b.textContent)).toEqual(["acme"]);
      expect(within(crumbs()).getByText("Acme Expenses").getAttribute("aria-current")).toBe("location");
    });
  });

  describe("the threads menu", () => {
    const openMenu = () => fireEvent.click(screen.getByRole("button", { name: "Threads" }));

    it("lists the main chat and, once it has messages, the Issues chat with its count", () => {
      chats.main = [{ kind: "user", id: "u1", text: "Hi", state: "sent" }];
      chats.issues = [
        { kind: "user", id: "u2", text: "/issue Save does nothing", state: "sent" },
        { kind: "agent", id: "a2", turnId: "t2", text: "Drafted it." },
      ];
      render(panel("overview"));
      openMenu();
      expect(within(screen.getByRole("menuitem", { name: /main chat/ })).getByText("Acme Expenses · main chat")).toBeTruthy();
      expect(within(screen.getByRole("menuitem", { name: /^Issues/ })).getByText("2")).toBeTruthy();
    });

    it("Issues reads summarised once the main chat has a From Issues note, and open on the Issues page", () => {
      chats.issues = [{ kind: "user", id: "u2", text: "Hello", state: "sent" }];
      chats.main = [{ kind: "note", id: "n1", text: "From Issues · 1 message", actions: [{ kind: "open-issues", label: "Reopen" }] }];
      render(panel("overview"));
      openMenu();
      expect(within(screen.getByRole("menuitem", { name: /^Issues/ })).getByText("summarised")).toBeTruthy();
      cleanup();
      render(panel("issues"));
      openMenu();
      expect(within(screen.getByRole("menuitem", { name: /^Issues/ })).getByText("open")).toBeTruthy();
    });

    it("lists only the main chat while the Issues chat is empty and not in view", () => {
      render(panel("overview"));
      openMenu();
      expect(screen.getAllByRole("menuitem")).toHaveLength(1);
    });

    it("lists the Issues chat in view, empty, as the current thread", () => {
      render(panel("issues"));
      openMenu();
      expect(screen.getByRole("menuitem", { name: /^Issues/ }).className).toContain("Mui-selected");
    });

    it("Issues goes to the Issues page", async () => {
      chats.issues = [{ kind: "user", id: "u2", text: "Hello", state: "sent" }];
      render(panel("overview"));
      openMenu();
      await act(async () => fireEvent.click(screen.getByRole("menuitem", { name: /^Issues/ })));
      expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName/issues", params: { projectName: "shop" } });
    });

    it("lists each issue's chat that holds something; it goes to the issue's card", async () => {
      issueThreads = [{ issueNumber: 7, count: 2 }];
      render(panel("overview"));
      openMenu();
      const seven = screen.getByRole("menuitem", { name: /#7/ });
      expect(within(seven).getByText("Issues › #7")).toBeTruthy();
      await act(async () => fireEvent.click(seven));
      expect(navigate).toHaveBeenCalledWith({
        to: "/projects/$projectName/issues/$number",
        params: { projectName: "shop", number: "7" },
      });
    });

    it("the main chat goes to the project's overview", async () => {
      render(panel("issues"));
      openMenu();
      await act(async () => fireEvent.click(screen.getByRole("menuitem", { name: /main chat/ })));
      expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName", params: { projectName: "shop" } });
    });
  });

  describe("a compose request", () => {
    const issuesInput = () => inputIn(issuesChat()!);
    const issue = (nonce: number): ComposeRequest => ({ text: "/issue ", view: "issues", projectName: "shop", nonce });

    it("fills the Issues chat's composer, focuses it with the cursor at the end, and sends nothing", async () => {
      render(panel("issues", null, issue(1)));
      await waitFor(() => expect(issuesInput().value).toBe("/issue "));
      await waitFor(() => expect(document.activeElement).toBe(issuesInput()));
      expect(issuesInput().selectionStart).toBe("/issue ".length);
      expect(issuesInput().selectionEnd).toBe("/issue ".length);
      expect(issuesSend).not.toHaveBeenCalled();
    });

    it("tells the shell it was applied, by nonce, so the request is single-use", async () => {
      render(panel("issues", null, issue(4)));
      await waitFor(() => expect(onComposeApplied).toHaveBeenCalledWith(4));
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("applies one request once: the same nonce again leaves what was typed", async () => {
      const view = render(panel("issues"));
      view.rerender(panel("issues", null, issue(1)));
      await waitFor(() => expect(issuesInput().value).toBe("/issue "));
      fireEvent.change(issuesInput(), { target: { value: "/issue login is broken" } });
      view.rerender(panel("issues", null, issue(1)));
      expect(issuesInput().value).toBe("/issue login is broken");
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("replaces a typed draft when the nonce is new", async () => {
      const view = render(panel("issues", null, issue(1)));
      await waitFor(() => expect(issuesInput().value).toBe("/issue "));
      fireEvent.change(issuesInput(), { target: { value: "half a thought" } });
      view.rerender(panel("issues", null, issue(2)));
      await waitFor(() => expect(issuesInput().value).toBe("/issue "));
    });

    it("does not refill after the shell has cleared it: closing and reopening the chat leaves an empty draft", async () => {
      const first = render(panel("issues", null, issue(1)));
      await waitFor(() => expect(onComposeApplied).toHaveBeenCalledWith(1));
      first.unmount();
      render(panel("issues"));
      expect(issuesInput().value).toBe("");
    });

    it("waits for its thread: the main chat's composer does not apply it", async () => {
      const view = render(panel("overview", null, issue(1)));
      await act(async () => {});
      expect(inputIn(mainChat()!).value).toBe("");
      expect(document.activeElement).not.toBe(inputIn(mainChat()!));
      expect(onComposeApplied).not.toHaveBeenCalled();
      // The move to the Issues page renders: its chat takes the request.
      view.rerender(panel("issues", null, issue(1)));
      await waitFor(() => expect(issuesInput().value).toBe("/issue "));
    });

    it("is not applied to another project's composer", async () => {
      render(panel("issues", null, { ...issue(1), projectName: "other" }));
      await act(async () => {});
      expect(issuesInput().value).toBe("");
      expect(onComposeApplied).not.toHaveBeenCalled();
    });

    it("applied while the conversation loads, focuses once the input is enabled", async () => {
      chatStatus = "loading";
      const view = render(panel("issues", null, issue(1)));
      await waitFor(() => expect(issuesInput().value).toBe("/issue "));
      expect(document.activeElement).not.toBe(issuesInput());
      chatStatus = "ready";
      view.rerender(panel("issues"));
      await waitFor(() => expect(document.activeElement).toBe(issuesInput()));
      expect(issuesInput().selectionStart).toBe("/issue ".length);
    });

    it("a main-chat request fills the main composer", async () => {
      render(panel("overview", null, { text: "Make it blue", view: "main", projectName: "shop", nonce: 1 }));
      await waitFor(() => expect(inputIn(mainChat()!).value).toBe("Make it blue"));
    });
  });
});
