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
import { useQuery } from "@tanstack/react-query";
import {
  chatKeyFor,
  getMessages,
  isLocalTurnActive,
  subscribe,
  subscribeLocalTurn,
  type ChatMessage,
} from "./chatStore.js";
import { answerableQuestionIds } from "./questionCards.js";
import { getActiveTurn, getConversationMessages } from "./api/turns.js";
import { conversationKeys, fetchCurrentConversationId } from "./api/conversations.js";
import { projectableHistory } from "./history.js";

/** Questions the agent is still waiting on — summed across answerable CARDS
 *  (an ask_questions batch is one card carrying several questions, and the
 *  card count would undersell "4 questions waiting" to "1"). */
function waitingQuestionCount(messages: ChatMessage[]): number {
  const ids = answerableQuestionIds(messages);
  let n = 0;
  for (const m of messages) {
    if (m.role === "question" && ids.has(m.id)) n += m.questions?.length ?? 0;
  }
  return n;
}

/** The interview's questions were asked AND all answered/superseded — the
 *  thread has moved past the interview, so a running turn is writing content
 *  (the working-state and stage lines say "drafting", not "preparing"). */
function questionsSettled(messages: ChatMessage[]): boolean {
  const ids = answerableQuestionIds(messages);
  let asked = false;
  for (const m of messages) {
    if (m.role !== "question") continue;
    asked = true;
    if (ids.has(m.id)) return false;
  }
  return asked;
}

/**
 * The fresh project's spec-interview state (#485), readable from ANY surface —
 * the overview's Spec card in particular, which must show "interviewing…" /
 * "N questions waiting" instead of Generate spec while the BE-started `/start`
 * turn works, without the chat panel ever having been opened.
 */
export interface SpecInterviewState {
  /** A turn is streaming right now — from `GET /turns/active` (12 s poll) or,
   *  without waiting for it, from a turn this tab itself started. */
  running: boolean;
  /** Unanswered questions the agent is waiting on. */
  questionsWaiting: number;
  /** The running turn is PAST the interview — questions asked and answered,
   *  the agent is writing the document. False while it is still preparing or
   *  waiting on questions, and always false when nothing runs. */
  drafting: boolean;
}

/** Matches useAgentChat's foreign-turn poll — the turn is server-driven, so
 *  every surface learns about it at the same cadence. */
const INTERVIEW_POLL_MS = 12_000;

/**
 * `running` comes from the active-turn poll (DB-backed, works even while the
 * project repo still provisions). `questionsWaiting` prefers the live chat log
 * (already streaming when the panel attached somewhere in this tab) and falls
 * back to a server rehydrate of the current thread — a turn that ENDED at its
 * ask_questions call leaves no active turn, so on a fresh landing the log is
 * the only place the pending questions exist.
 *
 * `enabled: false` renders the idle state and issues no requests — pass the
 * fresh-project predicate (the overview passes `spec.cta`), so settled
 * projects never pay this polling.
 */
export function useSpecInterview(
  org: string,
  projectName: string,
  enabled: boolean,
): SpecInterviewState {
  const chatKey = chatKeyFor(org, projectName);
  // Primitive snapshots keep useSyncExternalStore stable across unchanged logs.
  const storeCount = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => waitingQuestionCount(getMessages(chatKey)),
  );
  const storeHasLog = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => getMessages(chatKey).length > 0,
  );
  const storeSettled = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => questionsSettled(getMessages(chatKey)),
  );
  // A turn THIS tab started (answers submitted, a typed message) is known the
  // instant it starts, where the poll below would leave every surface on its
  // pre-turn state for up to a full interval — long enough that submitting
  // interview answers looked like nothing happened (#485 live-testing round 2).
  const localRunning = useSyncExternalStore(
    useCallback((fn: () => void) => subscribeLocalTurn(chatKey, fn), [chatKey]),
    () => isLocalTurnActive(chatKey),
  );

  const active = useQuery({
    queryKey: ["agent-active-turn", projectName],
    queryFn: () => getActiveTurn(projectName),
    enabled,
    refetchInterval: INTERVIEW_POLL_MS,
    // 404s while the repo provisions are expected — the interval is the retry.
    retry: false,
  });
  const running = enabled && (active.data?.status === "running" || localRunning);

  // Rehydrate fallback: only while the local log is empty (an attached panel
  // supersedes it). Runs even while a turn streams — a running turn can't be
  // AWAITING answers, but the thread is the only place a fresh landing can
  // learn whether the running turn is still interviewing or already drafting.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName),
    queryFn: () => fetchCurrentConversationId(projectName),
    staleTime: Infinity,
    enabled: enabled && !storeHasLog,
    retry: false,
  });
  const rehydrated = useQuery({
    queryKey: ["agent-conversation-questions", projectName, conversation.data],
    queryFn: async () => {
      const history = await getConversationMessages(projectName, conversation.data!);
      if (!history) return { waiting: 0, settled: false };
      const messages = projectableHistory(history);
      return {
        waiting: waitingQuestionCount(messages),
        settled: questionsSettled(messages),
      };
    },
    enabled: enabled && !storeHasLog && conversation.data !== undefined,
    refetchInterval: INTERVIEW_POLL_MS,
    retry: false,
  });

  const settled = storeHasLog ? storeSettled : (rehydrated.data?.settled ?? false);
  return {
    running,
    questionsWaiting: !enabled
      ? 0
      : storeHasLog
        ? storeCount
        : running
          ? 0 // a running turn is never awaiting answers
          : (rehydrated.data?.waiting ?? 0),
    drafting: running && settled,
  };
}
