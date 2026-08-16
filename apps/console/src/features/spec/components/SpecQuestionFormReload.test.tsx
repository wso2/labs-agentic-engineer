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

// A RELOAD during a pending interview (#485 live-testing round 3). Reproduced
// live: the questions render, every one is answered, and Continue stays
// disabled forever.
//
// The sequence a reload actually runs, which this file plays out:
//   1. a fresh local Y.Doc is created — the provider has not synced yet;
//   2. the chat log is available immediately (localStorage cache, then the
//      thread bootstrap), so the question mirrors into that empty local doc;
//   3. the user answers into it;
//   4. the room's persisted state — carrying the PRE-RELOAD session's write to
//      the SAME key — finally syncs in.
//
// Step 4's write is CONCURRENT with steps 2–3, and Y.Map breaks such a tie by
// clientID (random per doc), so roughly half of all reloads discarded the
// whole entry the user had been editing, answers included. The answers live in
// their own map now, which the incoming copy has nothing to say about.

import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import * as Y from "yjs";
import {
  chatKeyFor,
  replaceMessages,
  upsertQuestionMessage,
} from "../../agent-chat/chatStore";
import { projectableHistory } from "../../agent-chat/history";
import { mirrorQuestion, updateRoomAnswer } from "../../agent-chat/questionRoom";
import { applySelection } from "../../agent-chat/questionCards";
import { useRoomQuestion } from "../../agent-chat/useRoomQuestion";
import { SpecQuestionForm } from "./SpecQuestionForm";

const TOOL_CALL_ID = "call_abc";

const QUESTIONS = [
  { question: "Who signs in?", options: [{ label: "Anyone" }, { label: "Members only" }] },
  { question: "How do they pay?", options: [{ label: "Card" }, { label: "Invoice" }] },
];

/** The server thread as the BFF replays it after a reload. */
const HISTORY = [
  { role: "user" as const, content: "/start" },
  {
    role: "assistant" as const,
    content: [
      { type: "text", text: "I have a few more questions before generating the PRD." },
      {
        type: "tool-call",
        toolName: "ask_questions",
        toolCallId: TOOL_CALL_ID,
        input: { questions: QUESTIONS },
      },
    ],
  },
];

function Probe({ doc, chatKey }: { doc: Y.Doc; chatKey: string }) {
  const entry = useRoomQuestion(doc, chatKey);
  if (!entry) return <div data-testid="no-entry" />;
  return (
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SpecQuestionForm doc={doc} entry={entry} org="acme" projectName="proj1" />
    </OxygenUIThemeProvider>
  );
}

/** The provider's sync: both directions, as Hocuspocus does on connect. */
function sync(a: Y.Doc, b: Y.Doc) {
  Y.applyUpdate(b, Y.encodeStateAsUpdate(a, Y.encodeStateVector(b)));
  Y.applyUpdate(a, Y.encodeStateAsUpdate(b, Y.encodeStateVector(a)));
}

function answerEverything() {
  fireEvent.click(screen.getByText("Anyone"));
  fireEvent.click(screen.getByText("Card"));
}

const continueButton = () => screen.getByRole("button", { name: "Continue" });

let n = 0;
const freshKey = () => chatKeyFor("acme", `reload-${(n += 1)}`);

afterEach(cleanup);

describe("the question form across a reload", () => {
  it("submits once every question is answered on the reloaded page", () => {
    const chatKey = freshKey();
    const room = new Y.Doc();

    // Before the reload: the question streams into the log and mirrors.
    upsertQuestionMessage(chatKey, {
      role: "question",
      turnId: "t1",
      toolCallId: TOOL_CALL_ID,
      questions: QUESTIONS,
      streaming: false,
    });
    render(<Probe doc={room} chatKey={chatKey} />);
    expect(screen.getByTestId("spec-question-form")).toBeInTheDocument();
    cleanup();

    // The reload: same collab room, log re-seeded from the server thread.
    replaceMessages(chatKey, projectableHistory(HISTORY));
    render(<Probe doc={room} chatKey={chatKey} />);

    answerEverything();

    expect(continueButton()).toBeEnabled();
  });

  it("keeps the answers when the room syncs in AFTER they were given", () => {
    const chatKey = freshKey();
    // The pre-reload session's room, as the collab server persisted it.
    const persisted = new Y.Doc();
    const local = new Y.Doc();
    // Y.Map settles a concurrent write to one key by clientID, and ids are
    // random per doc — pin the losing half deterministically, or this test
    // would only catch the bug on a coin flip.
    local.clientID = 1;
    persisted.clientID = 2;
    mirrorQuestion(persisted, { toolCallId: TOOL_CALL_ID, questions: QUESTIONS });

    // The reload: fresh doc, log first, provider still handshaking.
    replaceMessages(chatKey, projectableHistory(HISTORY));
    render(<Probe doc={local} chatKey={chatKey} />);
    answerEverything();
    expect(continueButton()).toBeEnabled();

    // …and the room arrives, concurrently with everything above.
    act(() => sync(local, persisted));

    expect(continueButton()).toBeEnabled();
  });

  // The narrower half of the same race: only SOME questions were answered
  // before the reload. An untouched slot must not be published — writing the
  // normalized empty default would claim a key the incoming room copy already
  // holds a real answer under, and that tie is settled by clientID again.
  it("keeps a PRE-reload answer while a different question is answered post-reload", () => {
    const chatKey = freshKey();
    const persisted = new Y.Doc();
    const local = new Y.Doc();
    local.clientID = 1;
    persisted.clientID = 2;
    mirrorQuestion(persisted, { toolCallId: TOOL_CALL_ID, questions: QUESTIONS });
    updateRoomAnswer(persisted, TOOL_CALL_ID, (live) =>
      applySelection(live.questions, live.answers, 0, "Anyone"),
    );

    // The reload: fresh doc, log first, provider still handshaking. The user
    // answers the OTHER question, not seeing the one they already answered.
    replaceMessages(chatKey, projectableHistory(HISTORY));
    render(<Probe doc={local} chatKey={chatKey} />);
    fireEvent.click(screen.getByText("Card"));

    act(() => sync(local, persisted));

    expect(continueButton()).toBeEnabled();
  });

  // The other way a form goes permanently unsubmittable without saying so: a
  // FREE-TEXT question (the agent asks for a typed answer, so it has no
  // options) reads as the optional note field beside the choice cards. Every
  // radiogroup can show a selection while a question is still unanswered.
  it("names what is still missing when Continue is disabled", () => {
    const chatKey = freshKey();
    const room = new Y.Doc();
    replaceMessages(chatKey, [
      {
        id: "q1",
        role: "question",
        turnId: "t1",
        toolCallId: "tc-freetext",
        questions: [
          { question: "Who signs in?", options: [{ label: "Anyone" }] },
          { question: "Anything else we should know?", options: [] },
        ],
      },
    ]);
    render(<Probe doc={room} chatKey={chatKey} />);

    fireEvent.click(screen.getByText("Anyone"));

    expect(continueButton()).toBeDisabled();
    expect(screen.getByText("1 question still needs an answer")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Your answer"), {
      target: { value: "we ship to the EU only" },
    });

    expect(continueButton()).toBeEnabled();
    expect(screen.queryByText(/still needs an answer/)).not.toBeInTheDocument();
  });

  it("resolves a room written before the answers split (legacy inline draft)", () => {
    const chatKey = freshKey();
    const room = new Y.Doc();
    // What the old build stored: the draft inside the question value.
    room.getMap("questions").set(TOOL_CALL_ID, {
      toolCallId: TOOL_CALL_ID,
      questions: QUESTIONS,
      answers: [{ selected: ["Anyone"] }, { selected: [] }],
      askedAt: Date.now(),
    });

    replaceMessages(chatKey, projectableHistory(HISTORY));
    render(<Probe doc={room} chatKey={chatKey} />);

    // The half-finished draft survived the upgrade; one click completes it.
    expect(continueButton()).toBeDisabled();
    fireEvent.click(screen.getByText("Card"));
    expect(continueButton()).toBeEnabled();
  });
});
