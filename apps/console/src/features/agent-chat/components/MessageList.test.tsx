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

  // Live-testing round 2: interview answers and the finish valve are sent as
  // plain-text INSTRUCTIONS. The feed printed them whole, so the thread showed
  // a raw `Answers:` dump — and, after the finish valve, three paragraphs of
  // directives — as the user's chat message. What is sent is unchanged; only
  // the reading is.
  describe("machinery instructions read as one line", () => {
    const ANSWERS = 'Answers:\n- "Who signs in?": Anyone\n- "Photo uploads?": Yes';

    function userMessage(content: string): ChatMessage {
      return { id: "u1", role: "user", content, status: "completed" };
    }

    it("summarizes a batch of answers instead of dumping it", () => {
      renderFeed([userMessage(ANSWERS)]);

      expect(screen.getByText("Answered 2 questions")).toBeInTheDocument();
      expect(screen.queryByText(/Who signs in\?/)).not.toBeInTheDocument();
    });

    it("reveals the verbatim instruction on demand", () => {
      renderFeed([userMessage(ANSWERS)]);
      expect(screen.queryByTestId("user-message-detail")).not.toBeInTheDocument();

      fireEvent.click(screen.getByRole("button", { name: "Show details" }));

      expect(screen.getByTestId("user-message-detail")).toHaveTextContent(
        "Who signs in?",
      );
      fireEvent.click(screen.getByRole("button", { name: "Hide details" }));
      expect(screen.queryByTestId("user-message-detail")).not.toBeInTheDocument();
    });

    it("summarizes the finish valve by what it decided", () => {
      renderFeed([
        userMessage(
          [
            "Finish — use recommendations.",
            'Unanswered — apply your recommended answer to each and tag the decision *assumed* where it lands:\n- "Retention period?"\n- "Who can delete?"',
            "Stop asking. Apply your recommended answer to every remaining undecided area the same way, each tagged *assumed*.",
          ].join("\n\n"),
        ),
      ]);

      expect(
        screen.getByText("Finished — applied recommendations to 2 remaining questions"),
      ).toBeInTheDocument();
      expect(screen.queryByText(/Stop asking/)).not.toBeInTheDocument();
    });

    it("leaves a human message exactly as typed", () => {
      renderFeed([userMessage("add a returns policy section")]);

      expect(screen.getByText("add a returns policy section")).toBeInTheDocument();
      expect(screen.queryByTestId("user-message-summary")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Show details" }),
      ).not.toBeInTheDocument();
    });
  });

  it("renders the tail Working indicator only when asked", () => {
    renderFeed(
      [{ id: "u1", role: "user", content: "go", status: "in_flight", turnId: "t1" }],
      true,
    );
    expect(screen.getByTestId("working")).toBeInTheDocument();
  });
});
