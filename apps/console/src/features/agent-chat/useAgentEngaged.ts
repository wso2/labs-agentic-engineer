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
 * True while the agent is mid-exchange on this project: a turn is in flight, or
 * it asked a question nobody has answered yet.
 *
 * This is the guard on INJECTED flow commands — the overview's Generate-spec
 * CTA and the `?generate=` signal it navigates with. A `/start` landing in
 * either state is destructive, and the second way invisibly so: while a turn
 * runs it is refused (409), but while a question waits the start skill reads a
 * fresh instruction as its SKIP VALVE ("no answer given — continuing with
 * defaults") and writes the PRD from its own recommended answers, tagged
 * `*assumed*`, instead of the user's. Nothing errors; the interview is simply
 * gone.
 *
 * Deliberately AUTHOR-AGNOSTIC. A teammate's unanswered question is where an
 * injected start does the MOST damage — they would never learn why their
 * questions were answered for them — and authorship does not change whether the
 * send destroys something.
 *
 * A turn that never settled (the tab closed mid-flight) rehydrates as
 * `in_flight` and reads as engaged until something settles it. Accepted: the
 * mount-time re-attach polls the real turn and settles the log within a second
 * of the panel opening, and the suppressed CTA still navigates — it withholds
 * a send, never the destination.
 *
 * Conversations are PROJECT-scoped (#430): every browser rehydrates the same
 * server thread, so a teammate's in-flight turn and unanswered questions reach
 * this log too — the predicate is project-accurate up to the freshness
 * triggers in `useAgentChat` (mount, foreign-turn poll, tab refocus).
 */
export function agentEngaged(messages: ChatMessage[]): boolean {
  if (messages.some((m) => m.role === "user" && m.status === "in_flight")) return true;
  // Answerable == unanswered AND not superseded by a later delivered user
  // message, which is exactly "the agent is still waiting on this one".
  return answerableQuestionIds(messages).size > 0;
}

/**
 * `agentEngaged` over the live chat log. The overview's spec stage reads it to
 * choose its label and whether to attach `?generate`; the chat panel reads it
 * as the backstop before firing an injected command, so a pasted URL cannot
 * inject one either.
 */
export function useAgentEngaged(
  org: string,
  projectName: string | undefined,
): boolean {
  const chatKey = projectName ? chatKeyFor(org, projectName) : null;
  return useSyncExternalStore(
    useCallback(
      (fn: () => void) => (chatKey ? subscribe(chatKey, fn) : () => {}),
      [chatKey],
    ),
    () => (chatKey ? agentEngaged(getMessages(chatKey)) : false),
  );
}
