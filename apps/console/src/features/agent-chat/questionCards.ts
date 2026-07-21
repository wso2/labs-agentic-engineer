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

// The pure half of the question cards (ADR-0012 / #270): parsing the
// ask_question tool-call payload off the wire, serializing the user's answer
// into the next turn's instruction, and deciding whether a card is still
// answerable. Kept free of React/store imports so it unit-tests standalone.

import type { AskQuestionInput, AskQuestionOption } from "@aep/agent-stream";
import type { ChatMessage } from "./chatStore";

/**
 * Parse an `ask_question` tool-call `input` (object, or the provider's
 * stringified JSON) into the wire shape. Anything malformed → null — the fold
 * degrades to rendering nothing for the frame and the turn's text still shows
 * the question in prose.
 */
export function parseAskQuestionInput(input: unknown): AskQuestionInput | null {
  let value = input;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (typeof value !== "object" || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.question !== "string" || !v.question) return null;
  if (!Array.isArray(v.options) || v.options.length === 0) return null;
  const options: AskQuestionOption[] = [];
  for (const raw of v.options) {
    if (typeof raw !== "object" || raw === null) return null;
    const o = raw as Record<string, unknown>;
    if (typeof o.label !== "string" || !o.label) return null;
    options.push({
      label: o.label,
      ...(typeof o.description === "string" ? { description: o.description } : {}),
      ...(o.recommended === true ? { recommended: true } : {}),
    });
  }
  return {
    question: v.question,
    options,
    ...(v.multiSelect === true ? { multiSelect: true } : {}),
  };
}

/**
 * The answer wire format (#270 decision 1): plain text, selection and free-text
 * note combined — `Answer to "<question>": <label>[, <label>] — <note>`. A
 * free-text-only answer omits the labels; the agent reads it as the response
 * either way.
 */
export function serializeAnswer(
  question: string,
  selected: string[],
  freeText?: string,
): string {
  const labels = selected.join(", ");
  const note = freeText?.trim() ?? "";
  const body = labels && note ? `${labels} — ${note}` : labels || note;
  return `Answer to "${question}": ${body}`;
}

/**
 * A card accepts input only while it is the live end of the conversation:
 * unanswered via the card AND not superseded by any later user message (typing
 * in the composer is an equally valid answer path — the card then flips
 * read-only). Derived purely from the log, so reloads and second tabs converge.
 */
export function isAnswerable(
  messages: ChatMessage[],
  msg: Extract<ChatMessage, { role: "question" }>,
): boolean {
  if (msg.answer) return false;
  const idx = messages.findIndex((m) => m.id === msg.id);
  if (idx < 0) return false;
  return messages.slice(idx + 1).every((m) => m.role !== "user");
}
