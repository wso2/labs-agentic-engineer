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

/**
 * Display projection of a conversation (#463). The stored transcript's user
 * messages are COMPOSED MODEL PROMPTS (eager skill bodies + inlined files +
 * framing) — correct as the model's memory, wrong as a chat bubble. The
 * get-conversation read serves this projection instead: user rows carry the
 * turn journal's raw client-sent text + author; assistant/tool rows pass
 * through untouched (the UI projects their text and question tool-calls).
 *
 * Pairing is BY STATED INDEX: each entry carries the `messages` index of the
 * user message its turn appended, stamped at the append site — so a turn with
 * no entry (pre-journal history, an older caller mid-rolling-deploy) falls
 * back to its raw stored content WITHOUT shifting any other turn's pairing.
 */

import type { ModelMessage } from "ai";
import type { Conversation, TurnJournalEntry } from "../store/conversation-store.js";

/** A transcript message as served on the wire: user rows may carry an author. */
export type DisplayMessage =
  | ModelMessage
  | {
      role: "user";
      content: string;
      author?: TurnJournalEntry["author"];
      /** File NAMES that rode this message (#428) — never bytes. */
      attachments?: string[];
      /** What this message was aimed at (#666) — names, never content. */
      anchor?: TurnJournalEntry["anchor"];
    };

export function projectDisplayHistory(conv: Conversation): DisplayMessage[] {
  const byIndex = new Map<number, TurnJournalEntry>();
  for (const entry of conv.turns ?? []) byIndex.set(entry.messageIndex, entry);
  return conv.messages.map((m, index): DisplayMessage => {
    if (m.role !== "user") return m;
    const entry = byIndex.get(index);
    if (!entry) return m;
    return {
      role: "user",
      content: entry.text,
      ...(entry.author ? { author: entry.author } : {}),
      ...(entry.attachments && entry.attachments.length > 0
        ? { attachments: entry.attachments }
        : {}),
      ...(entry.anchor ? { anchor: entry.anchor } : {}),
    };
  });
}
