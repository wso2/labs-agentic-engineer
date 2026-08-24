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
import { cleanup, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { MessageList } from "./MessageList";
import { buildFeed } from "../feed";
import type { ChatMessage } from "../chatStore";

const ME = "me@aep.dev";

function renderFeed(messages: ChatMessage[], showWorkingTail = false) {
  const feed = buildFeed(messages, { currentUserId: ME });
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <MessageList
        feed={feed}
        expandedGroups={new Set()}
        onToggleGroup={vi.fn()}
        onOpenSpec={vi.fn()}
        showWorkingTail={showWorkingTail}
      />
    </OxygenUIThemeProvider>,
  );
}

afterEach(cleanup);

describe("MessageList", () => {
  it("labels the current user's message 'You' and a teammate by name", () => {
    renderFeed([
      {
        id: "u1",
        role: "user",
        content: "mine",
        status: "completed",
        author: { id: ME, displayName: "Me" },
      },
      {
        id: "u2",
        role: "user",
        content: "theirs",
        status: "completed",
        author: { id: "u-sarah", displayName: "Sarah Perera" },
      },
    ]);
    expect(screen.getByText("You")).toBeInTheDocument();
    expect(screen.getByText("Sarah Perera")).toBeInTheDocument();
  });

  it("falls back to 'You' for an author-less (legacy) message", () => {
    renderFeed([{ id: "u1", role: "user", content: "legacy", status: "completed" }]);
    expect(screen.getByText("You")).toBeInTheDocument();
  });

  it("renders the tail Working indicator only when asked", () => {
    renderFeed(
      [{ id: "u1", role: "user", content: "go", status: "in_flight", turnId: "t1" }],
      true,
    );
    expect(screen.getByTestId("working")).toBeInTheDocument();
  });

  // --- Chat attachments (#428) ---------------------------------------------

  it("shows a chip per attachment on the sent message", () => {
    renderFeed([
      {
        id: "m1",
        role: "user",
        content: "what is wrong here?",
        status: "completed",
        attachments: ["error.png", "rows.csv"],
      },
    ]);
    const chips = screen.getByTestId("user-message-attachments");
    expect(chips).toHaveTextContent("error.png");
    expect(chips).toHaveTextContent("rows.csv");
  });

  it("renders no attachment row for a message without any", () => {
    renderFeed([{ id: "m1", role: "user", content: "hello", status: "completed" }]);
    expect(screen.queryByTestId("user-message-attachments")).not.toBeInTheDocument();
  });

  it("keeps the chips on a failed message, dimmed with its text", () => {
    // The user still needs to see what they tried to send.
    renderFeed([
      {
        id: "m1",
        role: "user",
        content: "look at this",
        status: "failed",
        attachments: ["error.png"],
      },
    ]);
    expect(screen.getByTestId("user-message-attachments")).toHaveTextContent("error.png");
  });
});
