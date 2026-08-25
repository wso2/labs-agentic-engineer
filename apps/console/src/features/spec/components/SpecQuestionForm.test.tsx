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

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { Doc } from "yjs";
import type { AskQuestionInput } from "@aep/agent-stream";
import { chatKeyFor, consumePendingSeed } from "../../agent-chat/chatStore";
import { mirrorQuestion, readRoomQuestions, type RoomQuestion } from "../../agent-chat/questionRoom";
import { SpecQuestionForm } from "./SpecQuestionForm";

const ORG = "acme";
const PROJECT = "expenses";
const KEY = chatKeyFor(ORG, PROJECT);
const CALL_ID = "call-1";

const QUESTIONS: AskQuestionInput[] = [
  {
    question: "Who approves an expense?",
    options: [
      { label: "Any manager", recommended: true },
      { label: "The submitter's own manager" },
    ],
  },
  { question: "What is the approval limit?", options: [] },
];

/** A room holding one live (unanswered, fully-streamed) question entry. */
function room(questions: AskQuestionInput[] = QUESTIONS): { doc: Doc; entry: RoomQuestion } {
  const doc = new Doc();
  mirrorQuestion(doc, { toolCallId: CALL_ID, questions });
  return { doc, entry: readRoomQuestions(doc)[0]! };
}

function renderForm(doc: Doc, entry: RoomQuestion) {
  return render(<SpecQuestionForm doc={doc} entry={entry} org={ORG} projectName={PROJECT} />);
}

describe("SpecQuestionForm", () => {
  beforeEach(() => {
    consumePendingSeed(KEY);
  });

  it("gates Continue until every question is answered, then sends the answers", () => {
    const { doc, entry } = room();
    const { rerender } = renderForm(doc, entry);

    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();

    fireEvent.click(screen.getByRole("radio", { name: /Any manager/ }));
    fireEvent.change(screen.getByLabelText("Your answer"), { target: { value: "5000 USD" } });
    rerender(<SpecQuestionForm doc={doc} entry={readRoomQuestions(doc)[0]!} org={ORG} projectName={PROJECT} />);

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    const seed = consumePendingSeed(KEY)?.message ?? "";
    expect(seed).toContain("Any manager");
    expect(seed).toContain("5000 USD");
    expect(readRoomQuestions(doc)[0]!.submitted).toBe(true);
  });

  // #578: the exit is not "skip" — it makes the agent decide, and the decisions
  // land in the document flagged, so the control says so rather than leaving
  // the user to discover they signed the agent's assumptions.
  describe("the recommended-answers exit", () => {
    it("says what it does — the agent decides, and each decision stays reversible", () => {
      const { doc, entry } = room();
      renderForm(doc, entry);

      expect(screen.getByRole("button", { name: "Use recommended answers" })).toBeEnabled();
      expect(screen.queryByRole("button", { name: /skip/i })).toBeNull();
      // Read off the whole form: the note italicises `assumed`, so the
      // sentence is split across elements and no single node carries it.
      expect(screen.getByTestId("spec-question-form").textContent).toMatch(
        /lands flagged assumed in your spec, so you can change it later/i,
      );
    });

    it("asks the agent to decide and flag — never to stop interviewing", () => {
      const { doc, entry } = room();
      renderForm(doc, entry);

      fireEvent.click(screen.getByRole("button", { name: "Use recommended answers" }));

      const seed = consumePendingSeed(KEY)?.message ?? "";
      expect(seed).toMatch(/recommended answer/i);
      expect(seed).toContain("`*assumed*`");
      expect(seed).not.toMatch(/stop interviewing/i);
      expect(readRoomQuestions(doc)[0]!.submitted).toBe(true);
    });

    // The caption invites partial use — answer the ones you have opinions
    // about, let the agent take the rest. Handing every question back would
    // overwrite real decisions with guesses and flag them `*assumed*`, which
    // reads as the agent's invention rather than the user's lost answer. The
    // `grilling` skill draws the same line: "every REMAINING decision".
    it("keeps the answers the room already made, and defers only the rest", () => {
      const { doc, entry } = room();
      const { rerender } = renderForm(doc, entry);

      fireEvent.click(screen.getByRole("radio", { name: /submitter's own manager/i }));
      rerender(<SpecQuestionForm doc={doc} entry={readRoomQuestions(doc)[0]!} org={ORG} projectName={PROJECT} />);

      fireEvent.click(screen.getByRole("button", { name: "Use recommended answers" }));

      const seed = consumePendingSeed(KEY)?.message ?? "";
      // The decision survives, serialized exactly as Continue would send it.
      expect(seed).toContain("The submitter's own manager");
      // Only the unanswered question is handed back.
      expect(seed).toMatch(/Decide the rest yourself/i);
      expect(seed).toContain("What is the approval limit?");
      expect(seed).toContain("`*assumed*`");
    });

    it("defers nothing when the room answered everything", () => {
      // The button stays live once every question has an answer, and asking the
      // agent to decide "the rest" of an empty set invites it to invent
      // decisions and flag them `*assumed*`. It behaves as Continue instead.
      const { doc, entry } = room();
      const { rerender } = renderForm(doc, entry);

      fireEvent.click(screen.getByRole("radio", { name: /submitter's own manager/i }));
      rerender(<SpecQuestionForm doc={doc} entry={readRoomQuestions(doc)[0]!} org={ORG} projectName={PROJECT} />);
      fireEvent.change(screen.getByLabelText("Your answer"), { target: { value: "5000" } });
      rerender(<SpecQuestionForm doc={doc} entry={readRoomQuestions(doc)[0]!} org={ORG} projectName={PROJECT} />);

      fireEvent.click(screen.getByRole("button", { name: "Use recommended answers" }));

      const seed = consumePendingSeed(KEY)?.message ?? "";
      expect(seed).toContain("The submitter's own manager");
      expect(seed).toContain("5000");
      expect(seed).not.toMatch(/Decide the rest yourself/i);
      expect(seed).not.toMatch(/assumed/i);
      expect(seed).toBe(seed.trimEnd());
    });

    it("hands back every question when the room answered none", () => {
      const { doc, entry } = room();
      renderForm(doc, entry);

      fireEvent.click(screen.getByRole("button", { name: "Use recommended answers" }));

      const seed = consumePendingSeed(KEY)?.message ?? "";
      expect(seed).toMatch(/^Use your recommended answers for these questions/);
      expect(seed).not.toMatch(/Decide the rest yourself/i);
    });
  });

  it("gates both exits while the batch is still streaming", () => {
    const doc = new Doc();
    mirrorQuestion(doc, { toolCallId: CALL_ID, questions: QUESTIONS, streaming: true });
    renderForm(doc, readRoomQuestions(doc)[0]!);

    expect(screen.getByRole("button", { name: "Use recommended answers" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
  });
});

// This message is the instruction closest to the act of writing an assumption,
// and the path that produces most of them. Saying only "flag each one as
// assumed" left the agent to invent a rendering: it wrote `*(assumed)*` about
// one time in four, which the reader did not recognise — no count in the rail,
// no Settle control on the line, so the judgment could not be challenged.
describe("the seeded command names the assumption tag literally", () => {
  const KEY2 = chatKeyFor(ORG, PROJECT);

  it.each([
    ["every question handed back", false],
    ["only the remaining ones", true],
  ])("%s", (_name, answerOne) => {
    const { doc, entry } = room();
    const { rerender } = renderForm(doc, entry);
    if (answerOne) {
      fireEvent.click(screen.getByRole("radio", { name: /submitter's own manager/i }));
      rerender(
        <SpecQuestionForm doc={doc} entry={readRoomQuestions(doc)[0]!} org={ORG} projectName={PROJECT} />,
      );
    }

    fireEvent.click(screen.getByRole("button", { name: "Use recommended answers" }));

    const seed = consumePendingSeed(KEY2)?.message ?? "";
    // The exact token the reader matches — not a description of it.
    expect(seed).toContain("`*assumed*`");
    // The bare word alone is what drifted; it must not stand on its own.
    expect(seed).not.toMatch(/flag each one as assumed\b/i);
  });
});
