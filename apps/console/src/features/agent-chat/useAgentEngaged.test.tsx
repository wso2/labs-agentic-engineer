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

// The guard on injected flow commands. Its two true-states are the two ways a
// `/start` does damage: refused outright while a turn runs, and — the invisible
// one — read as the start skill's skip valve while a question waits, which
// answers the user's questions on their behalf and tags the guesses `*assumed*`.
// Everything else must stay FALSE, or the CTA stops working in exactly the
// state where the user needs it most: after a failed attempt.

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import { addMessage, chatKeyFor, getMessages, replaceMessages } from "./chatStore";
import { agentEngaged, useAgentEngaged } from "./useAgentEngaged";

const ORG = "acme";
const PROJECT = "proj1";
const KEY = chatKeyFor(ORG, PROJECT);

const QUESTIONS: AskQuestionInput[] = [
  { question: "Who signs in?", options: [{ label: "Anyone" }, { label: "Invited only" }] },
];

const engaged = () => agentEngaged(getMessages(KEY));

function askQuestion(toolCallId = "tc1") {
  addMessage(KEY, { role: "question", turnId: "t1", toolCallId, questions: QUESTIONS });
}

describe("agentEngaged", () => {
  beforeEach(() => replaceMessages(KEY, []));

  it("is false on an untouched project", () => {
    expect(engaged()).toBe(false);
  });

  it("is true while a turn is in flight", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "in_flight" });
    expect(engaged()).toBe(true);
  });

  it("is true while a question waits for an answer", () => {
    askQuestion();
    expect(engaged()).toBe(true);
  });

  it("is false once the question is answered on the card", () => {
    askQuestion();
    replaceMessages(
      KEY,
      getMessages(KEY).map((m) =>
        m.role === "question" ? { ...m, answers: [{ selected: ["Anyone"] }] } : m,
      ),
    );
    expect(engaged()).toBe(false);
  });

  it("is false once a later message supersedes the question", () => {
    askQuestion();
    // ADR-0012: a composer reply is an equally valid answer path, and it makes
    // the card unanswerable — the agent is no longer waiting on it.
    addMessage(KEY, { role: "user", content: "anyone can sign in", turnId: "t2", status: "completed" });
    expect(engaged()).toBe(false);
  });

  // The retry path. A CTA that stayed suppressed after a failure would strand
  // the user with no way to start at all.
  it("is false when the only turn FAILED", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "failed" });
    addMessage(KEY, { role: "error", content: "Failed to reach the agent." });
    expect(engaged()).toBe(false);
  });

  it("stays true when the send that followed a question failed to leave", () => {
    askQuestion();
    addMessage(KEY, { role: "user", content: "anyone", turnId: "t2", status: "failed" });
    expect(engaged()).toBe(true);
  });

  // Author-agnostic on purpose: a teammate's open interview is where an
  // injected start does the most damage, and they would never learn why their
  // questions were answered for them.
  it("is true for a question from a teammate's turn", () => {
    addMessage(KEY, {
      role: "user",
      content: "/start",
      turnId: "t1",
      status: "completed",
      author: { id: "u2", displayName: "Ada" },
    });
    askQuestion();
    expect(engaged()).toBe(true);
  });
});

describe("useAgentEngaged", () => {
  beforeEach(() => replaceMessages(KEY, []));

  it("tracks the log as the exchange opens and closes", () => {
    const { result } = renderHook(() => useAgentEngaged(ORG, PROJECT));
    expect(result.current).toBe(false);

    act(() => askQuestion());
    expect(result.current).toBe(true);

    act(() =>
      addMessage(KEY, { role: "user", content: "anyone", turnId: "t2", status: "completed" }),
    );
    expect(result.current).toBe(false);
  });

  it("is false with no project selected", () => {
    const { result } = renderHook(() => useAgentEngaged(ORG, undefined));
    expect(result.current).toBe(false);
  });

  it("ignores another project's exchange", () => {
    askQuestion();
    const { result } = renderHook(() => useAgentEngaged(ORG, "other-project"));
    expect(result.current).toBe(false);
  });
});
