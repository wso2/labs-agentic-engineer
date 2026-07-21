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

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { ASK_QUESTION_TOOL, buildAnswerInstruction } from "@aep/agent-stream";
import {
  addMessage,
  answerQuestion,
  chatKeyFor,
  conversationIdFor,
  dropTurnOutput,
  getMessages,
  replaceMessages,
  setTurnStatus,
  subscribe,
  type ChatMessage,
} from "./chatStore.js";
import { parseAskQuestionInput } from "./questionCards.js";
import {
  getActiveTurn,
  getConversationMessages,
  startCollabTurn,
} from "./api/turns.js";
import { attachAndFoldTurn } from "./runTurn.js";

// The panel's behavior hook (#130): per-project message log, send → collab
// turn → stream fold, mount-time rehydrate + running-turn re-attach. The
// stream abort on unmount only detaches the VIEW — turns run detached
// server-side and a later mount re-attaches via replay.

export interface AgentChat {
  messages: ChatMessage[];
  isSending: boolean;
  send: (instruction: string) => void;
  /**
   * Answer a question card (ADR-0012): serializes the choice into the next
   * turn's instruction. The answer is recorded on the card only once the turn
   * actually STARTED — a failed or no-op send leaves the card answerable, so
   * an answer can never be silently lost behind a read-only card.
   */
  answer: (
    msg: Extract<ChatMessage, { role: "question" }>,
    selected: string[],
    freeText?: string,
  ) => void;
}

export function useAgentChat(org: string, projectName: string): AgentChat {
  const chatKey = chatKeyFor(org, projectName);
  const messages = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => getMessages(chatKey),
  );
  const [isSending, setIsSending] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  // Mount / project switch: rehydrate an empty log from the server history,
  // then re-attach to a still-running chat turn (replay from 0).
  useEffect(() => {
    const ac = new AbortController();
    abortRef.current = ac;
    void (async () => {
      const convId = conversationIdFor(org, projectName, { create: false });
      if (convId && getMessages(chatKey).length === 0) {
        const history = await getConversationMessages(projectName, convId);
        if (ac.signal.aborted) return;
        if (history && history.length > 0) {
          replaceMessages(chatKey, projectableHistory(history));
        }
      }
      const active = await getActiveTurn(projectName);
      if (ac.signal.aborted || !active || active.status !== "running") return;
      if (active.useCase !== "general") return; // another flow's turn
      setIsSending(true);
      dropTurnOutput(chatKey, active.turnId); // replay-from-0 re-adds it all
      try {
        await attachAndFoldTurn(chatKey, projectName, active.turnId, ac.signal);
      } catch {
        // surfaced by the fold's error handling; the view just settles
      } finally {
        if (!ac.signal.aborted) setIsSending(false);
      }
    })();
    return () => {
      ac.abort();
      setIsSending(false);
    };
  }, [chatKey, org, projectName]);

  // One turn dispatch, shared by send and answer. `onStarted` fires after
  // startCollabTurn succeeds — the earliest point the instruction is durably
  // on its way to the agent.
  const dispatch = useCallback(
    (instruction: string, onStarted?: () => void) => {
      const text = instruction.trim();
      if (!text || isSending) return;
      const convId = conversationIdFor(org, projectName, { create: true })!;
      setIsSending(true);
      void (async () => {
        let turnId: string;
        try {
          turnId = await startCollabTurn(projectName, convId, text);
        } catch (err) {
          addMessage(chatKey, { role: "user", content: text, status: "failed" });
          addMessage(chatKey, {
            role: "error",
            content: err instanceof Error ? err.message : "Failed to reach the agent.",
          });
          setIsSending(false);
          return;
        }
        onStarted?.();
        addMessage(chatKey, {
          role: "user",
          content: text,
          turnId,
          status: "in_flight",
        });
        const signal = abortRef.current?.signal ?? new AbortController().signal;
        try {
          await attachAndFoldTurn(chatKey, projectName, turnId, signal);
        } catch {
          if (!signal.aborted) {
            setTurnStatus(chatKey, turnId, "failed");
            addMessage(chatKey, {
              role: "error",
              content: "Lost the agent's stream — reopen the panel to re-attach.",
            });
          }
        } finally {
          if (!signal.aborted) setIsSending(false);
        }
      })();
    },
    [chatKey, org, projectName, isSending],
  );

  const send = useCallback((instruction: string) => dispatch(instruction), [dispatch]);

  const answer = useCallback<AgentChat["answer"]>(
    (msg, selected, freeText) => {
      dispatch(buildAnswerInstruction(msg.question, selected, freeText), () =>
        answerQuestion(chatKey, msg.id, {
          selected,
          ...(freeText?.trim() ? { freeText: freeText.trim() } : {}),
        }),
      );
    },
    [dispatch, chatKey],
  );

  return { messages, isSending, send, answer };
}

// Server history → display log: user/assistant text, plus question cards
// reconstructed from persisted ask_question tool-calls (ADR-0012) — without
// them, a conversation that is awaiting-human rehydrates in a fresh browser
// with the pending question invisible and unanswerable. File-tool parts stay
// dropped (the doc already reflects them). Answered-ness derives from the
// later user messages via answerableQuestionIds; the recorded selection
// itself is local display state and does not survive a cross-browser move.
function projectableHistory(
  history: { role: string; content: unknown }[],
): ChatMessage[] {
  const out: ChatMessage[] = [];
  for (const m of history) {
    const text = contentText(m.content);
    if (m.role === "user") {
      if (text) out.push({ id: "", role: "user", content: text, status: "completed" });
    } else if (m.role === "assistant") {
      if (text) out.push({ id: "", role: "assistant", turnId: "history", content: text });
      if (!Array.isArray(m.content)) continue;
      for (const part of m.content) {
        if (
          typeof part !== "object" ||
          part === null ||
          (part as { type?: string }).type !== "tool-call" ||
          (part as { toolName?: string }).toolName !== ASK_QUESTION_TOOL
        ) {
          continue;
        }
        const input = parseAskQuestionInput((part as { input?: unknown }).input);
        if (!input) continue;
        out.push({
          id: "",
          role: "question",
          turnId: "history",
          toolCallId: (part as { toolCallId?: string }).toolCallId ?? "",
          ...input,
        });
      }
    }
  }
  return out;
}

function contentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((p) =>
        typeof p === "object" && p !== null && (p as { type?: string }).type === "text"
          ? ((p as { text?: string }).text ?? "")
          : "",
      )
      .join("");
  }
  return "";
}
