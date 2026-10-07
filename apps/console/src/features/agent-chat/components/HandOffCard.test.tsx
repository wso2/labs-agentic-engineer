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

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ChatItem } from "../chatLog";
import type { ProjectChat } from "../chatStore";

// The main chat's announcement that a request belongs in Issues: New Issue
// moves it there (sent to the Issues chat, or into its composer while that
// chat is busy), Stay here keeps the user where they are, and either choice
// reads back after a reload.

const REQUEST = "The Save button on the expense form does nothing";
const ANNOUNCEMENT = "This belongs in Issues. I'll open it and draft the issue, in its own chat on top of this one.";

let issues: ProjectChat;
const store = {
  open: vi.fn(async () => undefined),
  get: vi.fn(() => issues),
  send: vi.fn(async () => true),
};
vi.mock("../useProjectChat", () => ({
  useProjectChat: () => issues,
  chatStoreFor: () => store,
}));
const navigate = vi.fn(async () => undefined);
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
const open = vi.fn();
const compose = vi.fn();
vi.mock("../../shell/chatPanel", () => ({ useChatPanel: () => ({ open, compose }) }));

const { HandOffCard } = await import("./HandOffCard");

const item: Extract<ChatItem, { kind: "handoff" }> = {
  kind: "handoff",
  id: "t1:h:c1",
  turnId: "t1",
  toolCallId: "c1",
  view: "issues",
  request: REQUEST,
};

const idleIssues = (items: ProjectChat["items"] = []): ProjectChat => ({
  status: "ready",
  error: null,
  items,
  turn: { phase: "idle" },
});

function renderCard() {
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <HandOffCard projectName="acme" item={item} />
    </OxygenUIThemeProvider>,
  );
}

const ISSUES_PAGE = { to: "/projects/$projectName/issues", params: { projectName: "acme" } };

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.clearAllMocks();
});

describe("the hand-off announcement", () => {
  it("says where the request belongs and offers New Issue or Stay here", () => {
    issues = idleIssues();
    renderCard();
    expect(document.body.textContent).toContain(ANNOUNCEMENT);
    expect(screen.getByText("Issues", { selector: "strong" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "New Issue" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Stay here" })).toBeTruthy();
  });

  it("New Issue opens Issues and sends the request to its chat", async () => {
    issues = idleIssues();
    renderCard();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "New Issue" })));
    expect(navigate).toHaveBeenCalledWith(ISSUES_PAGE);
    expect(open).toHaveBeenCalled();
    expect(store.send).toHaveBeenCalledWith("acme", REQUEST, { kind: "product" });
    expect(compose).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain("Continued in Issues · Open");
    expect(screen.queryByRole("button", { name: "New Issue" })).toBeNull();
  });

  it("New Issue while the Issues chat is busy puts the request in its composer and sends nothing", async () => {
    issues = { ...idleIssues(), turn: { phase: "running", turnId: "t9" } };
    renderCard();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "New Issue" })));
    expect(store.send).not.toHaveBeenCalled();
    expect(compose).toHaveBeenCalledWith(REQUEST, { view: "issues", projectName: "acme" });
  });

  it("Stay here sends nothing and goes nowhere", () => {
    issues = idleIssues();
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: "Stay here" }));
    expect(store.send).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
    expect(compose).not.toHaveBeenCalled();
    expect(screen.getByText("Stayed here instead of opening Issues")).toBeTruthy();
  });

  it("reads back what the user chose after a reload", () => {
    issues = idleIssues();
    const first = renderCard();
    fireEvent.click(screen.getByRole("button", { name: "Stay here" }));
    first.unmount();
    renderCard();
    expect(screen.getByText("Stayed here instead of opening Issues")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New Issue" })).toBeNull();
  });

  it("reads as continued once the request is a message on the Issues thread, and Open goes there", () => {
    issues = idleIssues([{ kind: "user", id: "h0", text: REQUEST, state: "sent" }]);
    renderCard();
    expect(document.body.textContent).toContain("Continued in Issues · Open");
    expect(screen.queryByRole("button", { name: "New Issue" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(navigate).toHaveBeenCalledWith(ISSUES_PAGE);
    expect(store.send).not.toHaveBeenCalled();
  });
});
