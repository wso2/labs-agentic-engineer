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

// Server history → display log for rehydrate (#130 multi-user threads):
// user/assistant text — plus question cards reconstructed from ask_question /
// ask_questions tool-calls (ADR-0012), without which an awaiting-human
// conversation would rehydrate in a fresh browser with the pending question
// invisible and unanswerable. File-tool parts stay dropped (the shared spec
// doc already reflects them). A user message carries `author` when the server
// payload has one, so a teammate's turn is distinguishable from the signed-in
// user's once rehydrated (pure mapping — kept out of useAgentChat.ts so it's
// independently testable). The recorded selection itself is local display
// state and does not survive a cross-browser move; answered-ness derives from
// the later user messages via answerableQuestionIds.

import type { ChatMessage } from "./chatStore.js";
import { isErrorToolOutput } from "@aep/agent-stream";
import { isQuestionTool, parseQuestionsInput } from "./questionCards.js";
import type { ConversationMessage } from "./api/turns.js";

// Ids are POSITION-STABLE (`h<n>`, and `h<n>:q-<toolCallId>` for question
// cards): the same server history projects to the same ids every time, so the
// D6 rehydrate — which REPLACES the log on mount, foreign turn, and refocus —
// neither remounts unchanged React rows nor re-arms the panel's
// one-per-question auto-navigation. A thread is append-only, so a row's
// position never moves under it.
export function projectableHistory(history: ConversationMessage[]): ChatMessage[] {
  const out: ChatMessage[] = [];
  const rejected = rejectedToolCallIds(history);
  for (const m of history) {
    const text = contentText(m.content);
    if (m.role === "user") {
      if (!text) continue;
      const attachments = attachmentNamesOf(m);
      out.push({
        id: `h${out.length}`,
        role: "user",
        content: text,
        status: "completed",
        ...(m.author ? { author: m.author } : {}),
        ...(attachments.length > 0 ? { attachments } : {}),
        ...(m.anchor ? { anchor: m.anchor } : {}),
      });
    } else if (m.role === "assistant") {
      if (text) out.push({ id: `h${out.length}`, role: "assistant", turnId: "history", content: text });
      for (const q of questionCardsOf(m.content, out.length, rejected)) out.push(q);
    }
  }
  return out;
}

/**
 * Attachment names on a rehydrated user row (#428).
 *
 * Sourced from the turn JOURNAL, which the display projection serves alongside
 * the raw text — NOT from the stored transcript's file parts. Those two are not
 * interchangeable: `projectDisplayHistory` replaces a user row's content with
 * the journal's text precisely because the transcript version is a composed
 * model prompt, so by the time the row reaches this function its parts are gone.
 * Without the journal carrying names, a reload would show the agent discussing a
 * document that appears nowhere in the thread.
 *
 * Names only, never bytes (ADR-0019) — there is nothing to render from, and a
 * chip is not a download.
 *
 * `mapConversationMessage` has already dropped malformed entries, so this only
 * has to decide present-or-absent.
 */
function attachmentNamesOf(m: ConversationMessage): string[] {
  return m.attachments ?? [];
}

/**
 * Tool-call ids whose result on the transcript is an error. A question call the
 * SDK rejected against its schema still sits on the assistant message with the
 * input the model sent, followed by an error result; the model then retried
 * with a call that resolved. Only the resolved one is a question the user was
 * asked — replaying the rejected one would put a second, near-identical card on
 * the log.
 */
function rejectedToolCallIds(history: ConversationMessage[]): Set<string> {
  const ids = new Set<string>();
  for (const m of history) {
    if (m.role !== "tool" || !Array.isArray(m.content)) continue;
    for (const part of m.content) {
      if (typeof part !== "object" || part === null) continue;
      const p = part as { type?: string; toolCallId?: string; output?: unknown };
      if (p.type !== "tool-result" || !p.toolCallId) continue;
      if (isErrorToolOutput(p.output)) ids.add(p.toolCallId);
    }
  }
  return ids;
}

/** Reconstruct question cards from an assistant message's tool-call parts. */
function questionCardsOf(
  content: unknown,
  at: number,
  rejected: ReadonlySet<string>,
): Extract<ChatMessage, { role: "question" }>[] {
  if (!Array.isArray(content)) return [];
  const cards: Extract<ChatMessage, { role: "question" }>[] = [];
  for (const part of content) {
    if (typeof part !== "object" || part === null) continue;
    const p = part as { type?: string; toolName?: string; input?: unknown; toolCallId?: string };
    if (p.type !== "tool-call" || !isQuestionTool(p.toolName)) continue;
    if (p.toolCallId && rejected.has(p.toolCallId)) continue;
    const questions = parseQuestionsInput(p.toolName!, p.input);
    if (!questions) continue;
    cards.push({
      id: `h${at + cards.length}:q-${p.toolCallId ?? ""}`,
      role: "question",
      turnId: "history",
      toolCallId: p.toolCallId ?? "",
      questions,
    });
  }
  return cards;
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
