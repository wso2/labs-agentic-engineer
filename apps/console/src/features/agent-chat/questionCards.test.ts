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

import { describe, expect, it } from "vitest";
import { ASK_QUESTION_TOOL, ASK_QUESTIONS_TOOL } from "@aep/agent-stream";
import {
  applyNote,
  applySelection,
  extractStreamingQuestions,
  isFreeTextOption,
  isQuestionAnswered,
  normalizeAnswers,
  parseQuestionsInput,
  serializeQuestionAnswer,
} from "./questionCards";
import type { QuestionAnswer } from "@aep/agent-stream";

// Copied from the old console's agent-chat/questionCards.test.ts, less the typed
// option actions (not parsed here) and the log-level answerability tests,
// which moved with that logic to chatLog.test.ts.

const SINGLE = {
  question: "Which auth flow?",
  options: [
    { label: "OIDC", description: "Platform default", recommended: true },
    { label: "API keys" },
  ],
};

describe("parseQuestionsInput — ask_question (single)", () => {
  it("wraps a single question as a one-element list", () => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, SINGLE)).toEqual([SINGLE]);
  });

  it("accepts the provider's stringified JSON", () => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, JSON.stringify(SINGLE))).toEqual([SINGLE]);
  });

  it("keeps multiSelect only when explicitly true", () => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { ...SINGLE, multiSelect: true })![0]!.multiSelect).toBe(true);
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { ...SINGLE, multiSelect: "yes" })![0]!.multiSelect).toBeUndefined();
  });

  it("keeps detail only when a non-empty string", () => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { ...SINGLE, detail: "Why I ask." })![0]!.detail).toBe("Why I ask.");
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { ...SINGLE, detail: "" })![0]!.detail).toBeUndefined();
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { ...SINGLE, detail: 42 })![0]!.detail).toBeUndefined();
  });

  it("accepts empty options as a free-text question", () => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { question: "q", options: [] })).toEqual([
      { question: "q", options: [] },
    ]);
  });

  it.each([
    ["missing question", { options: SINGLE.options }],
    ["missing options", { question: "q" }],
    ["malformed JSON string", "{nope"],
    ["null", null],
  ])("rejects %s", (_name, value) => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, value)).toBeNull();
  });

  // A card degrades per OPTION, never per card: the agent's turn ends on the
  // question call, so a dropped card leaves the user staring at nothing while
  // the conversation waits on them.
  it("drops an option without a label and keeps the card", () => {
    expect(
      parseQuestionsInput(ASK_QUESTION_TOOL, {
        question: "q",
        options: [{ label: "A" }, { label: "", freeText: true }, { description: "x" }, "not-an-object"],
      }),
    ).toEqual([{ question: "q", options: [{ label: "A" }] }]);
  });

  it("a card whose every option is unlabeled becomes a free-text question", () => {
    expect(parseQuestionsInput(ASK_QUESTION_TOOL, { question: "q", options: [{ label: "" }] })).toEqual([
      { question: "q", options: [] },
    ]);
  });

  it("keeps the first of two options with the same label", () => {
    expect(
      parseQuestionsInput(ASK_QUESTION_TOOL, {
        question: "q",
        options: [{ label: "a" }, { label: "b" }, { label: "a", description: "d" }],
      }),
    ).toEqual([{ question: "q", options: [{ label: "a" }, { label: "b" }] }]);
  });
});

describe("parseQuestionsInput — ask_questions (batch)", () => {
  const BATCH = { questions: [SINGLE, { question: "Web or mobile?", options: [{ label: "Web" }, { label: "Mobile" }] }] };

  it("returns the full list", () => {
    expect(parseQuestionsInput(ASK_QUESTIONS_TOOL, BATCH)).toEqual(BATCH.questions);
  });

  it("rejects an empty questions list", () => {
    expect(parseQuestionsInput(ASK_QUESTIONS_TOOL, { questions: [] })).toBeNull();
  });

  it("drops a malformed question and keeps the rest of the form", () => {
    expect(parseQuestionsInput(ASK_QUESTIONS_TOOL, { questions: [SINGLE, { question: "q" }] })).toEqual([SINGLE]);
  });

  it("a form whose every question is malformed is no card", () => {
    expect(parseQuestionsInput(ASK_QUESTIONS_TOOL, { questions: [{ question: "q" }, { options: [] }] })).toBeNull();
  });

  it("rejects an unknown tool name", () => {
    expect(parseQuestionsInput("addFile", SINGLE)).toBeNull();
  });
});

describe("extractStreamingQuestions", () => {
  const BATCH_JSON = JSON.stringify({
    questions: [
      SINGLE,
      { question: "Which platform?", detail: "Sets the UI stack.", options: [{ label: "Web" }, { label: "Mobile" }] },
    ],
  });

  it("returns [] before the first question object closes", () => {
    const cut = BATCH_JSON.indexOf("}") - 1; // inside the first option object
    expect(extractStreamingQuestions(ASK_QUESTIONS_TOOL, BATCH_JSON.slice(0, cut))).toEqual([]);
  });

  it("returns each question as soon as its object closes", () => {
    // Cut right after the first question's closing brace (before the comma).
    const firstClose = BATCH_JSON.indexOf('},{"question"') + 1;
    const got = extractStreamingQuestions(ASK_QUESTIONS_TOOL, BATCH_JSON.slice(0, firstClose));
    expect(got).toEqual([SINGLE]);
  });

  it("returns all questions from a complete (or fully-buffered) input", () => {
    expect(extractStreamingQuestions(ASK_QUESTIONS_TOOL, BATCH_JSON)).toHaveLength(2);
    // Also when the closing ]} hasn't arrived yet.
    const noTail = BATCH_JSON.slice(0, BATCH_JSON.lastIndexOf("]"));
    expect(extractStreamingQuestions(ASK_QUESTIONS_TOOL, noTail)).toHaveLength(2);
  });

  it("is not confused by braces and escaped quotes inside strings", () => {
    const tricky = JSON.stringify({
      questions: [
        { question: 'Use "brace {} style" config?', options: [{ label: "Yes", description: 'It means {"a":1} literally \\ everywhere' }] },
        SINGLE,
      ],
    });
    const firstClose = tricky.indexOf('},{"question"') + 1;
    const got = extractStreamingQuestions(ASK_QUESTIONS_TOOL, tricky.slice(0, firstClose));
    expect(got).toHaveLength(1);
    expect(got[0]!.question).toBe('Use "brace {} style" config?');
  });

  it("skips a malformed question object but keeps later valid ones", () => {
    const buf = JSON.stringify({ questions: [{ options: [{ label: "orphan" }] }, SINGLE] });
    expect(extractStreamingQuestions(ASK_QUESTIONS_TOOL, buf)).toEqual([SINGLE]);
  });

  it("returns [] for the single-question tool and unknown tools", () => {
    expect(extractStreamingQuestions(ASK_QUESTION_TOOL, JSON.stringify(SINGLE))).toEqual([]);
    expect(extractStreamingQuestions("addFile", BATCH_JSON)).toEqual([]);
    expect(extractStreamingQuestions(undefined, BATCH_JSON)).toEqual([]);
  });

  it("returns [] for garbage before the questions array", () => {
    expect(extractStreamingQuestions(ASK_QUESTIONS_TOOL, '{"other": [')).toEqual([]);
    expect(extractStreamingQuestions(ASK_QUESTIONS_TOOL, "")).toEqual([]);
  });
});

describe("isQuestionAnswered / isFreeTextOption", () => {
  const OPTS = {
    question: "q",
    options: [
      { label: "Web" },
      { label: "Something else", description: "Type it in." },
      { label: "Escape", freeText: true },
    ],
  };

  it("free text always answers — including option-less questions", () => {
    expect(isQuestionAnswered({ question: "q", options: [] }, { selected: [], freeText: "my answer" })).toBe(true);
    expect(isQuestionAnswered({ question: "q", options: [] }, { selected: [] })).toBe(false);
  });

  it("a concrete selection answers; nothing selected does not", () => {
    expect(isQuestionAnswered(OPTS, { selected: ["Web"] })).toBe(true);
    expect(isQuestionAnswered(OPTS, undefined)).toBe(false);
  });

  it("a free-text escape hatch alone (flag or heuristic label) does NOT answer until text is typed", () => {
    expect(isQuestionAnswered(OPTS, { selected: ["Escape"] })).toBe(false);
    expect(isQuestionAnswered(OPTS, { selected: ["Something else"] })).toBe(false);
    expect(isQuestionAnswered(OPTS, { selected: ["Something else"], freeText: "custom roles: admin only" })).toBe(true);
    // A concrete option alongside the hatch still answers.
    expect(isQuestionAnswered(OPTS, { selected: ["Web", "Escape"] })).toBe(true);
  });

  // Seen on the live walk: "Fixed list: Laptop, Projector, Camera, Other"
  // contains the word but is a real answer, and the card never let it send.
  it("is an escape hatch only when the label opens with one", () => {
    expect(isFreeTextOption({ label: "Other" })).toBe(true);
    expect(isFreeTextOption({ label: "Something else — tell me" })).toBe(true);
    expect(isFreeTextOption({ label: "Fixed list: Laptop, Projector, Camera, Other" })).toBe(false);
    expect(isFreeTextOption({ label: "Per role, as the policy describes" })).toBe(false);
    const fixed = { question: "q", options: [{ label: "Fixed list: Laptop, Projector, Camera, Other" }] };
    expect(isQuestionAnswered(fixed, { selected: ["Fixed list: Laptop, Projector, Camera, Other"] })).toBe(true);
  });

  it("keeps the parsed freeText flag off the wire", () => {
    const parsed = parseQuestionsInput(ASK_QUESTION_TOOL, {
      question: "q",
      options: [{ label: "A" }, { label: "B", freeText: true }],
    })![0]!;
    expect(parsed.options[1]!.freeText).toBe(true);
    expect(parsed.options[0]!.freeText).toBeUndefined();
  });
});

describe("answer editing while the batch streams (#335 regression)", () => {
  // The room's answers array is sized to the questions visible when the user
  // FIRST touched an answer; later-streamed questions must still be editable.
  const Q = (label: string, multi = false) => ({
    question: label,
    options: [{ label: "A" }, { label: "B" }],
    ...(multi ? { multiSelect: true } : {}),
  });
  const FIVE = [Q("q0"), Q("q1"), Q("q2", true), Q("q3"), Q("q4")];
  const SHORT: QuestionAnswer[] = [{ selected: ["A"] }, { selected: [] }];

  it("normalizes a short answers array to the question count", () => {
    const out = normalizeAnswers(FIVE, SHORT);
    expect(out).toHaveLength(5);
    expect(out[0]).toEqual({ selected: ["A"] }); // existing answers survive
    expect(out[4]).toEqual({ selected: [] });
    expect(normalizeAnswers(FIVE, null)).toHaveLength(5);
    expect(normalizeAnswers(FIVE, undefined)).toHaveLength(5);
  });

  it("selects on a question BEYOND the stored array (the stuck case)", () => {
    const out = applySelection(FIVE, SHORT, 4, "B");
    expect(out).toHaveLength(5);
    expect(out[4]!.selected).toEqual(["B"]);
    expect(out[0]!.selected).toEqual(["A"]); // earlier answers untouched
  });

  it("types free text on a question beyond the stored array", () => {
    const out = applyNote(FIVE, SHORT, 3, "typed later");
    expect(out).toHaveLength(5);
    expect(out[3]!.freeText).toBe("typed later");
  });

  it("keeps single-select exclusive and multi-select additive", () => {
    const single = applySelection(FIVE, SHORT, 1, "A");
    expect(applySelection(FIVE, single, 1, "B")[1]!.selected).toEqual(["B"]);
    expect(applySelection(FIVE, single, 1, "A")[1]!.selected).toEqual([]); // toggle off

    const multi = applySelection(FIVE, SHORT, 2, "A");
    expect(applySelection(FIVE, multi, 2, "B")[2]!.selected).toEqual(["A", "B"]);
    expect(applySelection(FIVE, applySelection(FIVE, multi, 2, "B"), 2, "A")[2]!.selected).toEqual(["B"]);
  });
});

describe("serializeQuestionAnswer (the next turn's instruction)", () => {
  it("serializes one question's selection through the contract's builder", () => {
    expect(serializeQuestionAnswer([SINGLE], [{ selected: ["OIDC"] }])).toBe('Answer to "Which auth flow?": OIDC');
  });

  it("carries typed text, trimmed, beside or instead of a selection", () => {
    expect(serializeQuestionAnswer([SINGLE], [{ selected: [], freeText: "  SAML, sadly " }])).toBe(
      'Answer to "Which auth flow?": SAML, sadly',
    );
    expect(serializeQuestionAnswer([SINGLE], [{ selected: ["OIDC"], freeText: "via Thunder" }])).toBe(
      'Answer to "Which auth flow?": OIDC — via Thunder',
    );
  });

  it("serializes a batch as the Answers: list, one line per question", () => {
    const second = { question: "Web or mobile?", options: [{ label: "Web" }, { label: "Mobile" }] };
    expect(serializeQuestionAnswer([SINGLE, second], [{ selected: ["OIDC"] }, { selected: ["Web"] }])).toBe(
      'Answers:\n- "Which auth flow?": OIDC\n- "Web or mobile?": Web',
    );
  });
});
