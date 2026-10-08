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

import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ProjectChat } from "../chatStore";
import type { ChatView } from "../chatView";
import { ChatPanelContext, type ChatPanelControls, type ComposeRequest } from "../../shell/chatPanel";
import type { BranchState } from "../../shell/useChatControls";

// The chat panel always holds the project's main chat. On the Issues page the
// Issues chat is a branch of it: not started, a Start row above the main
// composer; started, a sheet over the main chat with its own thread and
// composer; minimised, a link at the end of the main thread. The threads menu
// in the header lists both.

let chatStatus: ProjectChat["status"] = "ready";
const chats: Record<ChatView, ProjectChat["items"]> = { main: [], issues: [], issue: [] };
let issuesTurn: ProjectChat["turn"] = { phase: "idle" };
const chatOf = (view: ChatView): ProjectChat => ({
  status: chatStatus,
  error: null,
  items: chats[view],
  turn: view === "issues" ? issuesTurn : { phase: "idle" },
});

const mainSend = vi.fn(() => Promise.resolve(true));
const issuesSend = vi.fn(() => Promise.resolve(true));
const stores: Record<ChatView, { send: typeof mainSend }> = {
  main: { send: mainSend },
  issues: { send: issuesSend },
  issue: { send: vi.fn(() => Promise.resolve(true)) },
};
vi.mock("../useProjectChat", () => ({
  useProjectChat: (_projectName: string, view: ChatView = "main") => chatOf(view),
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
const NOT_STARTED: BranchState = { started: false, minimised: false };
const OPEN: BranchState = { started: true, minimised: false };

/** The panel as the shell draws it: the shell keeps the branch's state. */
function Harness({
  page,
  card,
  composeRequest,
  initial,
}: {
  page: "issues" | "overview";
  card: "issue" | null;
  composeRequest: ComposeRequest | null;
  initial: BranchState;
}) {
  const [branch, setBranch] = useState(initial);
  const controls: ChatPanelControls = {
    open: vi.fn(),
    compose: vi.fn(),
    startBranch: () => setBranch(OPEN),
  };
  return (
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ChatPanelContext.Provider value={controls}>
        <ChatPanel
          projectName="shop"
          page={page}
          card={card}
          specFile={null}
          composeRequest={composeRequest}
          onComposeApplied={onComposeApplied}
          branch={branch}
          onStartBranch={() => setBranch(OPEN)}
          onMinimiseBranch={() => setBranch((b) => ({ ...b, minimised: true }))}
          onClose={() => {}}
        />
      </ChatPanelContext.Provider>
    </OxygenUIThemeProvider>
  );
}

function panel(
  page: "issues" | "overview",
  card: "issue" | null = null,
  composeRequest: ComposeRequest | null = null,
  initial: BranchState = NOT_STARTED,
) {
  return <Harness page={page} card={card} composeRequest={composeRequest} initial={initial} />;
}

const sheet = () => screen.queryByRole("region", { name: "Issues chat" });
const mainLayer = () => screen.getByTestId("main-chat");
const inputIn = (el: HTMLElement) => within(el).getByLabelText("Message the agent") as HTMLTextAreaElement;

function typeAndSend(where: HTMLElement, text: string) {
  fireEvent.change(inputIn(where), { target: { value: text } });
  fireEvent.click(within(where).getByRole("button", { name: "Send message" }));
}

describe("ChatPanel", () => {
  beforeEach(() => {
    mainSend.mockClear();
    issuesSend.mockClear();
    onComposeApplied.mockClear();
    navigate.mockClear();
    chatStatus = "ready";
    chats.main = [];
    chats.issues = [];
    issuesTurn = { phase: "idle" };
  });
  afterEach(cleanup);

  it("on the overview, holds the main chat only, and sends to it", () => {
    render(panel("overview"));
    expect(sheet()).toBeNull();
    expect(screen.queryByText(/Work on Issues here/)).toBeNull();
    expect(screen.getByText(/Talking about/).textContent).toBe("Talking about the whole product.");
    expect(inputIn(mainLayer()).getAttribute("placeholder")).toBe("Tell the agent what to change…");
    typeAndSend(mainLayer(), "Add approvals");
    expect(mainSend).toHaveBeenCalledWith("shop", "Add approvals", { kind: "product" });
    expect(issuesSend).not.toHaveBeenCalled();
  });

  describe("on the Issues page", () => {
    it("before the branch is started: the main chat, with Start above its composer, and no sheet", () => {
      render(panel("issues"));
      expect(sheet()).toBeNull();
      expect(screen.getByText("No messages yet. The conversation about this project shows here.")).toBeTruthy();
      expect(screen.getByTestId("branch-start").textContent).toBe("Work on Issues here · Start");
      typeAndSend(mainLayer(), "Add approvals");
      expect(mainSend).toHaveBeenCalledWith("shop", "Add approvals", { kind: "product" });
      expect(issuesSend).not.toHaveBeenCalled();
    });

    it("Start opens the Issues chat as a sheet over the main chat, focused, sending to the Issues chat", async () => {
      render(panel("issues"));
      fireEvent.click(screen.getByRole("button", { name: "Start" }));
      const s = sheet()!;
      expect(s.textContent).toContain("↑ Main chat");
      expect(within(s).getByTestId("branch-path").textContent).toBe("Acme Expenses └ Issues");
      expect(within(s).getByText("Tell me what's broken or what you need, and I'll draft an issue.")).toBeTruthy();
      expect(within(s).getByText(/Talking about/).textContent).toBe("Talking about the project's issues.");
      expect(inputIn(s).getAttribute("placeholder")).toBe("Describe what's broken, or what you need…");
      expect(screen.queryByTestId("branch-start")).toBeNull();
      await waitFor(() => expect(document.activeElement).toBe(inputIn(s)));
      typeAndSend(s, "Login is broken");
      expect(issuesSend).toHaveBeenCalledWith("shop", "Login is broken", { kind: "product" });
      expect(mainSend).not.toHaveBeenCalled();
    });

    it("the main chat stays under the sheet, out of reach while the sheet is up", () => {
      render(panel("issues", null, null, OPEN));
      expect(mainLayer().hasAttribute("inert")).toBe(true);
      fireEvent.click(screen.getByRole("button", { name: /Main chat/ }));
      expect(mainLayer().hasAttribute("inert")).toBe(false);
    });

    it("↑ Main chat minimises it to a link at the end of the main thread, and Open brings it back", () => {
      render(panel("issues", null, null, OPEN));
      fireEvent.click(screen.getByRole("button", { name: /Main chat/ }));
      expect(sheet()).toBeNull();
      expect(screen.getByTestId("branch-link").textContent).toBe("↳ Issues · its own chat · Open ↑");
      expect(screen.queryByTestId("branch-start")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Open ↑" }));
      expect(sheet()).not.toBeNull();
      expect(screen.queryByTestId("branch-link")).toBeNull();
    });

    it("keeps a draft in the Issues composer across minimise and open", () => {
      render(panel("issues", null, null, OPEN));
      fireEvent.change(inputIn(sheet()!), { target: { value: "half a thought" } });
      fireEvent.click(screen.getByRole("button", { name: /Main chat/ }));
      fireEvent.click(screen.getByRole("button", { name: "Open ↑" }));
      expect(inputIn(sheet()!).value).toBe("half a thought");
    });

    it("keeps the Issues draft while an issue's card is open over the page", () => {
      const { rerender } = render(panel("issues", null, null, OPEN));
      fireEvent.change(inputIn(sheet()!), { target: { value: "half a thought" } });
      rerender(panel("issues", "issue", null, OPEN));
      expect(sheet()).toBeNull();
      expect(mainLayer().hasAttribute("inert")).toBe(false);
      rerender(panel("issues", null, null, OPEN));
      expect(inputIn(sheet()!).value).toBe("half a thought");
    });

    it("keeps a minimised sheet's draft across an issue's card too", () => {
      const { rerender } = render(panel("issues", null, null, OPEN));
      fireEvent.change(inputIn(sheet()!), { target: { value: "half a thought" } });
      fireEvent.click(screen.getByRole("button", { name: /Main chat/ }));
      rerender(panel("issues", "issue", null, OPEN));
      expect(screen.queryByTestId("branch-link")).toBeNull();
      rerender(panel("issues", null, null, OPEN));
      fireEvent.click(screen.getByRole("button", { name: "Open ↑" }));
      expect(inputIn(sheet()!).value).toBe("half a thought");
    });

    it("takes focus once, when it is brought up, not again when the sheet is shown or mounted later", async () => {
      const { rerender } = render(panel("issues"));
      fireEvent.click(screen.getByRole("button", { name: "Start" }));
      await waitFor(() => expect(document.activeElement).toBe(inputIn(sheet()!)));
      inputIn(sheet()!).blur();
      rerender(panel("issues", "issue", null, OPEN));
      rerender(panel("issues", null, null, OPEN));
      expect(document.activeElement).not.toBe(inputIn(sheet()!));
      rerender(panel("overview", null, null, OPEN));
      rerender(panel("issues", null, null, OPEN));
      expect(sheet()).not.toBeNull();
      await act(async () => {});
      expect(document.activeElement).not.toBe(inputIn(sheet()!));
    });

    it("minimising moves focus to the link that brings the sheet back", async () => {
      render(panel("issues", null, null, OPEN));
      const strip = screen.getByRole("button", { name: /Main chat/ });
      strip.focus();
      fireEvent.click(strip);
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Open ↑" })));
    });

    it("the strip shows the main chat's last line", () => {
      chats.main = [{ kind: "agent", id: "a1", turnId: "t1", text: "Sure, I'll look at that." }];
      render(panel("issues", null, null, OPEN));
      expect(screen.getByRole("button", { name: /Main chat/ }).textContent).toContain("Sure, I'll look at that.");
    });

    it("on an issue's card, which is the main chat's, shows no branch", () => {
      render(panel("issues", "issue", null, OPEN));
      expect(sheet()).toBeNull();
      expect(screen.queryByTestId("branch-start")).toBeNull();
      expect(screen.queryByTestId("branch-link")).toBeNull();
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

    it("Issues reads summarised once the main chat has a From Issues note, and open while started", () => {
      chats.issues = [{ kind: "user", id: "u2", text: "Hello", state: "sent" }];
      chats.main = [{ kind: "note", id: "n1", text: "From Issues · 1 message", actions: [{ kind: "open-issues", label: "Reopen" }] }];
      render(panel("overview"));
      openMenu();
      expect(within(screen.getByRole("menuitem", { name: /^Issues/ })).getByText("summarised")).toBeTruthy();
      cleanup();
      render(panel("issues", null, null, OPEN));
      openMenu();
      expect(within(screen.getByRole("menuitem", { name: /^Issues/ })).getByText("open")).toBeTruthy();
    });

    it("lists only the main chat while the Issues chat is empty and not started", () => {
      render(panel("overview"));
      openMenu();
      expect(screen.getAllByRole("menuitem")).toHaveLength(1);
    });

    it("Issues goes to the Issues page and starts its chat there", async () => {
      chats.issues = [{ kind: "user", id: "u2", text: "Hello", state: "sent" }];
      render(panel("issues"));
      openMenu();
      await act(async () => fireEvent.click(screen.getByRole("menuitem", { name: /^Issues/ })));
      expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName/issues", params: { projectName: "shop" } });
      expect(sheet()).not.toBeNull();
    });

    it("the main chat minimises the sheet", () => {
      render(panel("issues", null, null, OPEN));
      openMenu();
      fireEvent.click(screen.getByRole("menuitem", { name: /main chat/ }));
      expect(sheet()).toBeNull();
      expect(screen.getByTestId("branch-link")).toBeTruthy();
    });
  });

  describe("a compose request", () => {
    const sheetInput = () => inputIn(sheet()!);
    const issue = (nonce: number): ComposeRequest => ({ text: "/issue ", view: "issues", projectName: "shop", nonce });

    it("fills the Issues sheet's composer, focuses it with the cursor at the end, and sends nothing", async () => {
      render(panel("issues", null, issue(1), OPEN));
      await waitFor(() => expect(sheetInput().value).toBe("/issue "));
      await waitFor(() => expect(document.activeElement).toBe(sheetInput()));
      expect(sheetInput().selectionStart).toBe("/issue ".length);
      expect(sheetInput().selectionEnd).toBe("/issue ".length);
      expect(inputIn(mainLayer()).value).toBe("");
      expect(issuesSend).not.toHaveBeenCalled();
      expect(mainSend).not.toHaveBeenCalled();
    });

    it("tells the shell it was applied, by nonce, so the request is single-use", async () => {
      render(panel("issues", null, issue(4), OPEN));
      await waitFor(() => expect(onComposeApplied).toHaveBeenCalledWith(4));
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("applies one request once: the same nonce again leaves what was typed", async () => {
      const view = render(panel("issues", null, null, OPEN));
      view.rerender(panel("issues", null, issue(1), OPEN));
      await waitFor(() => expect(sheetInput().value).toBe("/issue "));
      fireEvent.change(sheetInput(), { target: { value: "/issue login is broken" } });
      view.rerender(panel("issues", null, issue(1), OPEN));
      expect(sheetInput().value).toBe("/issue login is broken");
      expect(onComposeApplied).toHaveBeenCalledTimes(1);
    });

    it("replaces a typed draft when the nonce is new", async () => {
      const view = render(panel("issues", null, issue(1), OPEN));
      await waitFor(() => expect(sheetInput().value).toBe("/issue "));
      fireEvent.change(sheetInput(), { target: { value: "half a thought" } });
      view.rerender(panel("issues", null, issue(2), OPEN));
      await waitFor(() => expect(sheetInput().value).toBe("/issue "));
    });

    it("does not refill after the shell has cleared it: closing and reopening the chat leaves an empty draft", async () => {
      const first = render(panel("issues", null, issue(1), OPEN));
      await waitFor(() => expect(onComposeApplied).toHaveBeenCalledWith(1));
      first.unmount();
      render(panel("issues", null, null, OPEN));
      expect(sheetInput().value).toBe("");
    });

    it("is not applied by the main chat's composer, on the Issues page or after leaving it", async () => {
      render(panel("overview", null, issue(1)));
      await act(async () => {});
      expect(inputIn(mainLayer()).value).toBe("");
      expect(document.activeElement).not.toBe(inputIn(mainLayer()));
      expect(onComposeApplied).not.toHaveBeenCalled();
    });

    it("is not applied to another project's composer", async () => {
      render(panel("issues", null, { ...issue(1), projectName: "other" }, OPEN));
      await act(async () => {});
      expect(sheetInput().value).toBe("");
      expect(onComposeApplied).not.toHaveBeenCalled();
    });

    it("applied while the conversation loads, focuses once the input is enabled", async () => {
      chatStatus = "loading";
      const view = render(panel("issues", null, issue(1), OPEN));
      await waitFor(() => expect(sheetInput().value).toBe("/issue "));
      expect(document.activeElement).not.toBe(sheetInput());
      chatStatus = "ready";
      view.rerender(panel("issues", null, null, OPEN));
      await waitFor(() => expect(document.activeElement).toBe(sheetInput()));
      expect(sheetInput().selectionStart).toBe("/issue ".length);
    });

    it("a main-chat request fills the main composer", async () => {
      render(panel("overview", null, { text: "Make it blue", view: "main", projectName: "shop", nonce: 1 }));
      await waitFor(() => expect(inputIn(mainLayer()).value).toBe("Make it blue"));
    });
  });
});
