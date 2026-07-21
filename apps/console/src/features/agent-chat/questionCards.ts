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
// ask_question tool-call payload off the wire and deciding which cards are
// still answerable. The wire tool name and the answer serialization live in
// @aep/agent-stream (the contract); this module stays free of React/store
// imports so it unit-tests standalone.

import type { AskQuestionInput, AskQuestionOption } from "@aep/agent-stream";
import type { ChatMessage } from "./chatStore";

/**
 * Parse an `ask_question` tool-call `input` (object, or the provider's
 * stringified JSON) into the wire shape. Anything malformed — including
 * duplicate option labels, which would make a selection ambiguous — → null;
 * the fold degrades to rendering nothing for the frame and the turn's text
 * still shows the question in prose. Deliberately lenient beyond that (no
 * option-count cap): rendering whatever the server accepted beats dropping it.
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
  if (new Set(options.map((o) => o.label)).size !== options.length) return null;
  return {
    question: v.question,
    options,
    ...(v.multiSelect === true ? { multiSelect: true } : {}),
  };
}

/**
 * The ids of question cards that still accept input: unanswered via the card
 * AND not superseded by any later user message that actually reached the
 * server (a `failed` send supersedes nothing — the agent never saw it).
 * Single backward pass, computed once per render; derived purely from the
 * log, so reloads and second tabs converge.
 */
export function answerableQuestionIds(messages: ChatMessage[]): Set<string> {
  const ids = new Set<string>();
  let superseded = false;
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]!;
    if (m.role === "user" && m.status !== "failed") superseded = true;
    else if (m.role === "question" && !superseded && !m.answer) ids.add(m.id);
  }
  return ids;
}
