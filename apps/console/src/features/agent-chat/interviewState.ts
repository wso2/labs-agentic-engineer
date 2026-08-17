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

import { useCallback, useSyncExternalStore } from "react";
import { chatKeyFor, getMessages, subscribe, type ChatMessage } from "./chatStore.js";
import { answerableQuestionIds } from "./questionCards.js";

/**
 * What the agent is doing on this project, read off the live chat log. Two
 * facts, because every surface that speaks for the agent — the overview's Spec
 * card, the spec view's waiting states, the injected-command guard — needs the
 * same two and would otherwise each invent its own predicate.
 */
export interface InterviewState {
  /** A turn is in flight. */
  turnRunning: boolean;
  /** Questions the agent is still waiting on (0 = none). */
  pendingQuestions: number;
}

export function interviewState(messages: ChatMessage[]): InterviewState {
  // Answerable == unanswered AND not superseded by a later delivered user
  // message, which is exactly "the agent is still waiting on this one". That
  // set holds CARDS; the count the user is told is QUESTIONS, since one form
  // routinely carries several and "1 question" over a form of four would be a
  // lie the form itself immediately contradicts.
  const answerable = answerableQuestionIds(messages);
  let pendingQuestions = 0;
  for (const m of messages) {
    if (m.role === "question" && answerable.has(m.id)) {
      pendingQuestions += m.questions?.length ?? 0;
    }
  }
  return {
    turnRunning: messages.some((m) => m.role === "user" && m.status === "in_flight"),
    pendingQuestions,
  };
}

// useSyncExternalStore compares snapshots by IDENTITY, so a fresh object per
// read is an infinite render loop. The log's array identity changes exactly
// when the log does, which makes it the right memo key.
const EMPTY_INTERVIEW: InterviewState = { turnRunning: false, pendingQuestions: 0 };
const snapshots = new WeakMap<ChatMessage[], InterviewState>();

function interviewSnapshot(messages: ChatMessage[]): InterviewState {
  const cached = snapshots.get(messages);
  if (cached) return cached;
  const state = interviewState(messages);
  snapshots.set(messages, state);
  return state;
}

/** `interviewState` over the live chat log of one project. */
export function useInterviewState(
  org: string,
  projectName: string | undefined,
): InterviewState {
  const chatKey = projectName ? chatKeyFor(org, projectName) : null;
  return useSyncExternalStore(
    useCallback(
      (fn: () => void) => (chatKey ? subscribe(chatKey, fn) : () => {}),
      [chatKey],
    ),
    () => (chatKey ? interviewSnapshot(getMessages(chatKey)) : EMPTY_INTERVIEW),
    () => EMPTY_INTERVIEW,
  );
}
