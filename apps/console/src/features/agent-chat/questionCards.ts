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
// ask_question / ask_questions tool-call payloads off the wire into a uniform
// list of questions, and deciding which cards are still answerable. The wire
// tool names and answer serialization live in @aep/agent-stream (the contract);
// this module stays free of React/store imports so it unit-tests standalone.

import {
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  isQuestionTool,
  buildAnswerInstruction,
  buildAnswersInstruction,
  type AskQuestionInput,
  type QuestionAnswer,
  type AskQuestionOption,
  type QuestionOptionAction,
} from "@aep/agent-stream";
import type { ChatMessage } from "./chatStore";

/**
 * Parse one question object; null only when the QUESTION is malformed. Options
 * degrade individually: one the card cannot render (no label) or cannot tell
 * apart (a repeated label — labels are the selection identity on the card AND
 * in the serialized answer) is dropped and the rest still show. The turn ended
 * on this call and waits for the user, so a dropped card would leave them
 * facing a blank panel; a card short one option is answerable, and the form
 * always offers free text for whatever the missing option meant.
 */
/** A well-formed typed action, or undefined — a malformed one is dropped, the option stays. */
function parseOptionAction(raw: unknown): QuestionOptionAction | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const a = raw as Record<string, unknown>;
  if (typeof a.dependency !== "string" || !a.dependency) return undefined;
  if (a.kind === "accept-assumption" || a.kind === "upload-interface") {
    return { kind: a.kind, dependency: a.dependency };
  }
  return undefined;
}

function parseOneQuestion(value: unknown): AskQuestionInput | null {
  if (typeof value !== "object" || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.question !== "string" || !v.question) return null;
  // Empty options is a valid FREE-TEXT question (the form renders only the
  // text field); a missing/non-array options is malformed.
  if (!Array.isArray(v.options)) return null;
  const options: AskQuestionOption[] = [];
  const seen = new Set<string>();
  for (const raw of v.options) {
    if (typeof raw !== "object" || raw === null) continue;
    const o = raw as Record<string, unknown>;
    if (typeof o.label !== "string" || !o.label || seen.has(o.label)) continue;
    seen.add(o.label);
    const action = parseOptionAction(o.action);
    options.push({
      label: o.label,
      ...(typeof o.description === "string" ? { description: o.description } : {}),
      ...(o.recommended === true ? { recommended: true } : {}),
      ...(o.freeText === true ? { freeText: true } : {}),
      ...(action ? { action } : {}),
    });
  }
  return {
    question: v.question,
    ...(typeof v.detail === "string" && v.detail ? { detail: v.detail } : {}),
    options,
    ...(v.multiSelect === true ? { multiSelect: true } : {}),
  };
}

/**
 * Parse an `ask_question` (single) or `ask_questions` (batch) tool-call input
 * — object or the provider's stringified JSON — into a uniform, non-empty list
 * of questions. Malformed parts drop individually — an option in
 * `parseOneQuestion`, a question in a batch here — and the card renders what
 * is left; null only when nothing renderable remains.
 */
export function parseQuestionsInput(toolName: string, input: unknown): AskQuestionInput[] | null {
  let value = input;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (toolName === ASK_QUESTION_TOOL) {
    const one = parseOneQuestion(value);
    return one ? [one] : null;
  }
  if (toolName === ASK_QUESTIONS_TOOL) {
    if (typeof value !== "object" || value === null) return null;
    const list = (value as Record<string, unknown>).questions;
    if (!Array.isArray(list)) return null;
    // Same rule one level up: a malformed question drops, the form still
    // renders the rest; only a form with nothing left in it is no card.
    const out: AskQuestionInput[] = [];
    for (const q of list) {
      const parsed = parseOneQuestion(q);
      if (parsed) out.push(parsed);
    }
    return out.length > 0 ? out : null;
  }
  return null;
}

export { isQuestionTool };

/**
 * Incrementally extract the COMPLETE question objects from a PARTIAL
 * `ask_questions` input buffer, so the form can render questions one by one
 * while the batch is still streaming (#270 latency follow-up: ~3/4 of the
 * new-project wait is this JSON streaming — the first question is on the wire
 * long before the tool-call frame closes).
 *
 * A string-aware brace scanner walks the `"questions": [...]` array and
 * JSON-parses each element the moment its object closes; elements that fail
 * `parseOneQuestion` are skipped (the final complete `tool-call` parse remains
 * the authority — its upsert replaces whatever streamed). Batch tool only: a
 * single `ask_question` input closes its one object only at the very end, so
 * there is nothing to reveal early.
 */
export function extractStreamingQuestions(
  toolName: string | undefined,
  buf: string,
): AskQuestionInput[] {
  if (toolName !== ASK_QUESTIONS_TOOL) return [];
  const arr = buf.match(/"questions"\s*:\s*\[/);
  if (!arr) return [];
  const out: AskQuestionInput[] = [];
  const n = buf.length;
  let i = (arr.index ?? 0) + arr[0].length;
  while (i < n) {
    while (i < n && /[\s,]/.test(buf[i]!)) i++;
    if (i >= n || buf[i] !== "{") break;
    let depth = 0;
    let inStr = false;
    let esc = false;
    let end = -1;
    for (let j = i; j < n; j++) {
      const c = buf[j]!;
      if (inStr) {
        if (esc) esc = false;
        else if (c === "\\") esc = true;
        else if (c === '"') inStr = false;
      } else if (c === '"') inStr = true;
      else if (c === "{") depth++;
      else if (c === "}" && --depth === 0) {
        end = j;
        break;
      }
    }
    if (end < 0) break; // this element is still streaming
    try {
      const q = parseOneQuestion(JSON.parse(buf.slice(i, end + 1)));
      if (q) out.push(q);
    } catch {
      // skip an unparseable element; later ones may still be fine
    }
    i = end + 1;
  }
  return out;
}

/**
 * Options whose selection implies TYPING the real answer, when the agent
 * didn't set the explicit `freeText` flag — "Other", "Something else",
 * "Type my own answer"… Heuristic drives FOCUS and the answered-check both:
 * a bare label like "Something else" with no typed text answers nothing.
 */
export function isFreeTextOption(opt: AskQuestionOption): boolean {
  return (
    opt.freeText === true ||
    /\b(other|something else|type (in|my)|describe|own answer|specify|custom)\b/i.test(opt.label)
  );
}

/**
 * One question's answered-ness — the submit gate. Free text always answers.
 * A selection answers UNLESS every selected option is a free-text escape
 * hatch (explicit flag or heuristic): "Something else" without the something
 * else is not an answer.
 */
export function isQuestionAnswered(q: AskQuestionInput, answer: QuestionAnswer | undefined): boolean {
  if ((answer?.freeText ?? "").trim().length > 0) return true;
  const selected = answer?.selected ?? [];
  if (selected.length === 0) return false;
  const byLabel = new Map(q.options.map((o) => [o.label, o] as const));
  return selected.some((label) => {
    const opt = byLabel.get(label);
    return opt !== undefined && !isFreeTextOption(opt);
  });
}

/**
 * Answers aligned 1:1 with `questions`, padding slots the stored array lacks.
 * LOAD-BEARING while a batch streams (#335): the room's answers array is sized
 * to the questions that existed when the user first touched an answer, but the
 * batch keeps growing — every read and write must re-align to the CURRENT
 * question count, or edits to later questions silently vanish (the array is
 * mapped over, so an out-of-range index is simply never visited).
 */
export function normalizeAnswers(
  questions: AskQuestionInput[],
  answers: QuestionAnswer[] | null | undefined,
): QuestionAnswer[] {
  return questions.map((_, i) => answers?.[i] ?? { selected: [] });
}

/**
 * Toggle one option on question `qi`, returning the full re-aligned answers
 * array. Single-select replaces (and re-clicking clears); multi-select adds
 * and removes.
 */
export function applySelection(
  questions: AskQuestionInput[],
  answers: QuestionAnswer[] | null | undefined,
  qi: number,
  label: string,
): QuestionAnswer[] {
  const multi = questions[qi]?.multiSelect === true;
  return normalizeAnswers(questions, answers).map((a, i) => {
    if (i !== qi) return a;
    const has = a.selected.includes(label);
    const selected = multi
      ? has
        ? a.selected.filter((l) => l !== label)
        : [...a.selected, label]
      : has
        ? []
        : [label];
    return { ...a, selected };
  });
}

/** Set the free-text note on question `qi`, re-aligned to the question count. */
export function applyNote(
  questions: AskQuestionInput[],
  answers: QuestionAnswer[] | null | undefined,
  qi: number,
  freeText: string,
): QuestionAnswer[] {
  return normalizeAnswers(questions, answers).map((a, i) => (i === qi ? { ...a, freeText } : a));
}

/**
 * Serialize a card's answer(s) into the next turn's plain-text instruction —
 * one definition shared by the chat hook and the collab banner. Single question
 * → `Answer to "…"`, batch → an `Answers:` list (the wire contract's builders).
 */
export function serializeQuestionAnswer(
  questions: AskQuestionInput[],
  answers: QuestionAnswer[],
): string {
  if (questions.length === 1) {
    return buildAnswerInstruction(
      questions[0]!.question,
      answers[0]?.selected ?? [],
      answers[0]?.freeText,
    );
  }
  return buildAnswersInstruction(
    questions.map((q, i) => ({
      question: q.question,
      selected: answers[i]?.selected ?? [],
      ...(answers[i]?.freeText ? { freeText: answers[i]!.freeText } : {}),
    })),
  );
}

/**
 * The ids of question cards that still accept input: unanswered via the card
 * AND not superseded by any later user message that actually reached the server
 * (a `failed` send supersedes nothing — the agent never saw it). Single
 * backward pass, computed once per render; derived purely from the log, so
 * reloads and second tabs converge.
 */
export function answerableQuestionIds(messages: ChatMessage[]): Set<string> {
  const ids = new Set<string>();
  let superseded = false;
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]!;
    if (m.role === "user" && m.status !== "failed") superseded = true;
    else if (m.role === "question" && !superseded && !m.answers) ids.add(m.id);
  }
  return ids;
}

/** Newest chat-log question that still accepts an answer, if any. */
export function pendingAnswerableQuestion(
  messages: ChatMessage[],
): Extract<ChatMessage, { role: "question" }> | undefined {
  const ids = answerableQuestionIds(messages);
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]!;
    if (m.role === "question" && m.questions.length && ids.has(m.id)) return m;
  }
  return undefined;
}
