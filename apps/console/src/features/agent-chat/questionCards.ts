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

// The pure half of the question cards (ADR-0012 / #270), copied from the
// console's agent-chat/questionCards.ts: parsing the ask_question /
// ask_questions tool-call payloads off the wire into a uniform list of
// questions, and the answer form's rules. The wire tool names and the answer
// serialization live in @aep/agent-stream (the contract). Not copied: the
// options' typed `action`s (ADR-0028), which act on dependencies this app does
// not show yet; an option carrying one renders as an ordinary option.

import {
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  buildAnswerInstruction,
  buildAnswersInstruction,
  type AskQuestionInput,
  type AskQuestionOption,
  type QuestionAnswer,
} from "@aep/agent-stream";

/**
 * Parse one question object; null only when the QUESTION is malformed. An
 * option the card cannot render (no label) or cannot tell apart (a repeated
 * label: labels are the selection identity on the card and in the answer) is
 * dropped and the rest still show, since the turn ended on this call and waits
 * for the user; the free answer covers whatever the missing option meant.
 */
function parseOneQuestion(value: unknown): AskQuestionInput | null {
  if (typeof value !== "object" || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.question !== "string" || !v.question) return null;
  // Empty options is a valid free-text question; a missing one is malformed.
  if (!Array.isArray(v.options)) return null;
  const options: AskQuestionOption[] = [];
  const seen = new Set<string>();
  for (const raw of v.options) {
    if (typeof raw !== "object" || raw === null) continue;
    const o = raw as Record<string, unknown>;
    if (typeof o.label !== "string" || !o.label || seen.has(o.label)) continue;
    seen.add(o.label);
    options.push({
      label: o.label,
      ...(typeof o.description === "string" ? { description: o.description } : {}),
      ...(o.recommended === true ? { recommended: true } : {}),
      ...(o.freeText === true ? { freeText: true } : {}),
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
 * Parse an `ask_question` (single) or `ask_questions` (batch) tool-call input,
 * an object or the provider's stringified JSON, into a non-empty list of
 * questions; null when nothing renderable remains.
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
    const out: AskQuestionInput[] = [];
    for (const q of list) {
      const parsed = parseOneQuestion(q);
      if (parsed) out.push(parsed);
    }
    return out.length > 0 ? out : null;
  }
  return null;
}

/**
 * The COMPLETE question objects in a PARTIAL `ask_questions` input buffer, so
 * a batch renders question by question while it streams (#270). A
 * string-aware brace scanner walks the `"questions": [...]` array and parses
 * each element as its object closes. The complete `tool-call` stays the
 * authority: its card replaces whatever streamed. Batch tool only: a single
 * `ask_question` closes its one object only at the very end.
 */
export function extractStreamingQuestions(toolName: string | undefined, buf: string): AskQuestionInput[] {
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
 * An option whose selection means TYPING the real answer, when the agent did
 * not set the explicit `freeText` flag: "Other", "Something else"… A bare
 * "Something else" with nothing typed answers nothing. Only a label that
 * OPENS with the escape is one: "Fixed list: Laptop, Projector, Camera,
 * Other" is a real answer that happens to contain the word.
 */
export function isFreeTextOption(opt: AskQuestionOption): boolean {
  return (
    opt.freeText === true ||
    /^\s*(other|something else|let me (type|describe)|type (in|my)|(my|your) own( answer)?|describe (it|my)|specify|custom)\b/i.test(opt.label)
  );
}

/**
 * One question's answered-ness, the submit gate. Typed text always answers; a
 * selection answers unless every selected option is a free-text escape hatch.
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
 * Answers aligned 1:1 with `questions`. Load-bearing while a batch streams
 * (#335): the list grows after the user starts answering, and every read and
 * write must re-align to the current count or later answers vanish.
 */
export function normalizeAnswers(
  questions: AskQuestionInput[],
  answers: QuestionAnswer[] | null | undefined,
): QuestionAnswer[] {
  return questions.map((_, i) => answers?.[i] ?? { selected: [] });
}

/** Toggle one option on question `qi`. Single-select replaces (re-clicking clears); multi-select adds and removes. */
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

/** Set the free-text answer on question `qi`. */
export function applyNote(
  questions: AskQuestionInput[],
  answers: QuestionAnswer[] | null | undefined,
  qi: number,
  freeText: string,
): QuestionAnswer[] {
  return normalizeAnswers(questions, answers).map((a, i) => (i === qi ? { ...a, freeText } : a));
}

/**
 * A card's answers as the next turn's plain-text instruction, through the
 * contract's builders: one question → `Answer to "…": …`, several → an
 * `Answers:` list. The agent reads it as an ordinary user message.
 */
export function serializeQuestionAnswer(questions: AskQuestionInput[], answers: QuestionAnswer[]): string {
  const cleaned = normalizeAnswers(questions, answers).map((a) => ({
    selected: a.selected,
    ...(a.freeText?.trim() ? { freeText: a.freeText.trim() } : {}),
  }));
  if (questions.length === 1) {
    return buildAnswerInstruction(questions[0]!.question, cleaned[0]!.selected, cleaned[0]!.freeText);
  }
  return buildAnswersInstruction(questions.map((q, i) => ({ question: q.question, ...cleaned[i]! })));
}
