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
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { QuestionItem } from "../chatLog";

// The chat's side of the agent's questions (ADR-0002 / #879): a pointer to
// the Questions card while they are open, their texts once they are not.

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

const { QuestionsPointer } = await import("./QuestionsPointer");

afterEach(() => {
  cleanup();
  navigate.mockReset();
});

const Q = (question: string): AskQuestionInput => ({ question, options: [{ label: "Yes" }, { label: "No" }] });

function show(questions: AskQuestionInput[], opts: { open?: boolean; streaming?: boolean; answered?: boolean } = {}) {
  const item: QuestionItem = {
    kind: "question",
    id: "t1:q:c1",
    turnId: "t1",
    toolCallId: "c1",
    questions,
    streaming: opts.streaming ?? false,
    ...(opts.answered ? { answers: questions.map(() => ({ selected: ["Yes"] })) } : {}),
  };
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <QuestionsPointer projectName="acme" item={item} open={opts.open ?? true} />
    </OxygenUIThemeProvider>,
  );
}

describe("QuestionsPointer", () => {
  it("points a batch at the Questions card, and opens it on a click only", () => {
    show([Q("Who approves?"), Q("Which channels?")]);
    expect(navigate).not.toHaveBeenCalled();
    const pointer = screen.getByRole("button", { name: /The agent has 2 questions.*Answer them/ });
    expect(screen.queryByText("Who approves?")).toBeNull();
    fireEvent.click(pointer);
    expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName/questions", params: { projectName: "acme" } });
  });

  it("speaks of one question in the singular", () => {
    show([Q("Who approves?")]);
    expect(screen.getByRole("button", { name: /The agent has a question.*Answer it/ })).toBeTruthy();
  });

  it("says the agent is still asking while the batch arrives", () => {
    show([Q("Who approves?")], { streaming: true });
    expect(screen.getByRole("button", { name: /The agent is asking questions/ })).toBeTruthy();
  });

  it("folds to the question texts once answered or superseded", () => {
    show([Q("Who approves?"), Q("Which channels?")], { answered: true });
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("Who approves?")).toBeTruthy();
    cleanup();
    show([Q("Who approves?")], { open: false });
    expect(screen.queryByRole("button")).toBeNull();
  });
});
