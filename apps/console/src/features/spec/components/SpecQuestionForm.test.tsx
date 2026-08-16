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

// The shared question form's header copy (#485 live-testing round): the title
// says what the questions are FOR, and the collaborative subtitle stays.

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import * as Y from "yjs";
import type { RoomQuestion } from "../../agent-chat/questionRoom";
import { SpecQuestionForm } from "./SpecQuestionForm";

const ENTRY: RoomQuestion = {
  toolCallId: "tc-1",
  questions: [
    {
      question: "Who signs in?",
      options: [{ label: "Anyone" }, { label: "Members only" }],
    },
  ],
  answers: null,
};

function renderForm() {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SpecQuestionForm doc={new Y.Doc()} entry={ENTRY} org="acme" projectName="proj1" />
    </OxygenUIThemeProvider>,
  );
}

afterEach(cleanup);

describe("SpecQuestionForm — header", () => {
  it("titles the form with what the answers are for, keeping the shared-answer subtitle", () => {
    renderForm();

    expect(
      screen.getByText("A few clarifications to write your spec"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Quick questions")).not.toBeInTheDocument();
    expect(
      screen.getByText(/Everyone on this project can answer together/),
    ).toBeInTheDocument();
  });

  it("still renders the questions and their options", () => {
    renderForm();

    expect(screen.getByText("Who signs in?")).toBeInTheDocument();
    expect(screen.getByText("Anyone")).toBeInTheDocument();
    expect(screen.getByText("Members only")).toBeInTheDocument();
  });
});
