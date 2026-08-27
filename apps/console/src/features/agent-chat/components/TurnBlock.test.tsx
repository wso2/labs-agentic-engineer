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

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { TurnBlock } from "./TurnBlock";
import type { FeedBlock } from "../feed";
import type { ChatItem } from "../toolGrouping";

type Turn = Extract<FeedBlock, { kind: "turn" }>;

const narration = (content: string): ChatItem => ({
  kind: "message",
  message: { id: "a1", role: "assistant", turnId: "t1", content },
});
const errorItem = (content: string): ChatItem => ({
  kind: "message",
  message: { id: "e1", role: "error", content },
});
const toolGroup = (path: string): ChatItem => ({
  kind: "tool-group",
  id: "tg1",
  path,
  tools: [
    {
      id: "tc1",
      role: "tool",
      turnId: "t1",
      toolCallId: "tc1",
      status: "done",
      op: "add",
      path,
      ok: true,
    },
  ],
});

const turn = (over: Partial<Turn> = {}): Turn => ({
  kind: "turn",
  id: "turn-a1",
  turnId: "t1",
  attribution: { displayName: "You", isOwn: true },
  items: [],
  status: "committed",
  ...over,
});

function renderTurn(t: Turn, onOpenSpec = vi.fn(), showSpecLink?: boolean) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <TurnBlock
        turn={t}
        expandedGroups={new Set()}
        onToggleGroup={vi.fn()}
        onOpenSpec={onOpenSpec}
        {...(showSpecLink === undefined ? {} : { showSpecLink })}
      />
    </OxygenUIThemeProvider>,
  );
  return onOpenSpec;
}

afterEach(cleanup);

describe("TurnBlock", () => {
  it("renders the agent header without attribution for an own turn", () => {
    renderTurn(turn({ items: [narration("hi")] }));
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.queryByText(/· for/)).not.toBeInTheDocument();
  });

  it("attributes a teammate's turn with '· for <name>'", () => {
    renderTurn(
      turn({
        attribution: { displayName: "Sarah Perera", isOwn: false },
        items: [narration("working")],
      }),
    );
    expect(screen.getByText("· for Sarah Perera")).toBeInTheDocument();
  });

  it("renders narration as markdown (zero activity steps is fine)", () => {
    renderTurn(turn({ items: [narration("# Done\n\nAll set.")] }));
    expect(screen.getByRole("heading", { name: "Done" })).toBeInTheDocument();
    expect(screen.queryByTestId("activity-step")).not.toBeInTheDocument();
  });

  it("renders a tool call as an activity step with the file leaf", () => {
    renderTurn(turn({ items: [toolGroup("specs/requirements/prd.md")] }));
    expect(screen.getByTestId("activity-step")).toBeInTheDocument();
    expect(screen.getByText("prd.md")).toBeInTheDocument();
  });

  it("shows the committed footer with a working Open spec link", () => {
    const onOpenSpec = renderTurn(turn({ status: "committed", items: [narration("done")] }));
    expect(screen.getByTestId("turn-committed")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open spec" }));
    expect(onOpenSpec).toHaveBeenCalledOnce();
  });

  it("hides Open spec and the questions pointer when showSpecLink is off", () => {
    const question: ChatItem = {
      kind: "message",
      message: {
        id: "q1",
        role: "question",
        turnId: "t1",
        toolCallId: "tc1",
        questions: [{ question: "Auth?", options: [{ label: "PAT" }] }],
      },
    };
    renderTurn(turn({ items: [question] }), vi.fn(), false);
    expect(screen.queryByRole("button", { name: "Open spec" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("questions-pointer")).not.toBeInTheDocument();
  });

  it("shows distinct failed styling on a failed turn (not a committed footer)", () => {
    renderTurn(turn({ status: "failed", items: [narration("tried")] }));
    expect(screen.getByTestId("turn-failed")).toBeInTheDocument();
    expect(screen.queryByTestId("turn-committed")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open spec" })).not.toBeInTheDocument();
  });

  it("shows a Working indicator while the turn is running", () => {
    renderTurn(turn({ status: "running", items: [narration("thinking")] }));
    expect(screen.getByTestId("working")).toBeInTheDocument();
    expect(screen.queryByTestId("turn-committed")).not.toBeInTheDocument();
  });

  it("keeps the chat-error testid for an error message inside the turn", () => {
    renderTurn(turn({ status: "failed", items: [narration("oops"), errorItem("boom")] }));
    expect(screen.getByTestId("chat-error")).toHaveTextContent("boom");
  });
});
