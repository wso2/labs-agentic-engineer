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

// The session chrome + finish valve (#486): the area checklist header on a
// session round, the "Finish — use recommendations" valve carrying partial
// answers, and resume-after-park keeping a draft (the room entry is the
// persistence — the form only renders it).

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { Doc } from "yjs";
import { FINISH_PREFIX } from "@aep/agent-stream";
import { SpecQuestionForm } from "./SpecQuestionForm";
import { chatKeyFor, consumePendingSeed } from "../../agent-chat/chatStore";
import { mirrorQuestion, readRoomQuestions, updateRoomAnswer } from "../../agent-chat/questionRoom";
import { applySelection } from "../../agent-chat/questionCards";

const ORG = "acme";
const PROJECT = "proj";

const QS = [
  { question: "Who owns a round?", options: [{ label: "The opener", recommended: true }, { label: "Anyone" }] },
  { question: "Any limits?", options: [{ label: "None" }, { label: "Cap it" }] },
];

const SESSION = {
  title: "Grilling Favorites",
  areas: [
    { name: "ownership", state: "done" as const },
    { name: "limits", state: "now" as const },
    { name: "privacy", state: "todo" as const },
  ],
};

function roomWith(session?: typeof SESSION) {
  const doc = new Doc();
  mirrorQuestion(doc, { toolCallId: "tc-1", questions: QS, ...(session ? { session } : {}) });
  return { doc, entry: readRoomQuestions(doc)[0]! };
}

function renderForm(doc: Doc, entry: ReturnType<typeof roomWith>["entry"]) {
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SpecQuestionForm doc={doc} entry={entry} org={ORG} projectName={PROJECT} />
    </OxygenUIThemeProvider>,
  );
}

afterEach(() => {
  cleanup();
  consumePendingSeed(chatKeyFor(ORG, PROJECT)); // drain between tests
});

describe("SpecQuestionForm — session chrome (#486)", () => {
  it("shows the session title and the area checklist with per-state chips", () => {
    const { doc, entry } = roomWith(SESSION);
    renderForm(doc, entry);
    expect(screen.getByText("Grilling Favorites")).toBeInTheDocument();
    const checklist = screen.getByTestId("session-area-checklist");
    for (const name of ["ownership", "limits", "privacy"]) {
      expect(checklist).toHaveTextContent(name);
    }
  });

  it("keeps the plain header on a one-form interview (no session)", () => {
    const { doc, entry } = roomWith();
    renderForm(doc, entry);
    expect(screen.getByText("Quick questions")).toBeInTheDocument();
    expect(screen.queryByTestId("session-area-checklist")).not.toBeInTheDocument();
  });
});

describe("SpecQuestionForm — finish valve (#486)", () => {
  it("sends given answers as decisions and lists the unanswered for *assumed* recommendations", () => {
    const { doc } = roomWith(SESSION);
    updateRoomAnswer(doc, "tc-1", (live) => applySelection(live.questions, live.answers, 0, "The opener"));
    renderForm(doc, readRoomQuestions(doc)[0]!);

    fireEvent.click(screen.getByRole("button", { name: "Finish — use recommendations" }));

    const seed = consumePendingSeed(chatKeyFor(ORG, PROJECT));
    expect(seed).not.toBeNull();
    expect(seed!.startsWith(FINISH_PREFIX)).toBe(true);
    expect(seed).toContain('- "Who owns a round?": The opener');
    expect(seed).toMatch(/Unanswered[\s\S]*- "Any limits\?"/);
    expect(seed).toContain("*assumed*");
    // The valve closes the round for the whole room.
    expect(readRoomQuestions(doc)[0]!.submitted).toBe(true);
  });
});

describe("SpecQuestionForm — park/resume", () => {
  it("a re-render from the persisted room entry restores the draft selection (resume)", () => {
    const { doc } = roomWith(SESSION);
    updateRoomAnswer(doc, "tc-1", (live) => applySelection(live.questions, live.answers, 0, "The opener"));

    // Park: unmount (navigate away). Resume: render fresh from the same room.
    const first = renderForm(doc, readRoomQuestions(doc)[0]!);
    first.unmount();
    renderForm(doc, readRoomQuestions(doc)[0]!);

    expect(screen.getByRole("radio", { name: /The opener/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("session-area-checklist")).toBeInTheDocument();
  });
});
