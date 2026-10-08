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

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { AskQuestionInput } from "@aep/agent-stream";
import { QuestionBlock } from "./QuestionBlock";

// A confirmation card shows the exact change in its confirm option's
// description: a comment's paragraphs, an issue's "Title: …\n\nBody:\n…". The
// user confirms what they read there, so its line breaks must survive.

afterEach(cleanup);

const CHANGE = "Title: Save fails offline\n\nBody:\nSteps:\n1. Go offline\n2. Save";

const q: AskQuestionInput = {
  question: "Apply this edit?",
  options: [{ label: "Apply it", recommended: true, description: CHANGE }, { label: "Not now" }],
};

describe("QuestionBlock", () => {
  it("shows an option's description with its line breaks", () => {
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <QuestionBlock q={q} answer={{ selected: [] }} disabled={false} onSelect={() => {}} onNote={() => {}} />
      </OxygenUIThemeProvider>,
    );
    const description = screen.getByText((_, el) => el?.textContent === CHANGE && el.children.length === 0);
    expect(getComputedStyle(description).whiteSpace).toBe("pre-wrap");
  });
});
