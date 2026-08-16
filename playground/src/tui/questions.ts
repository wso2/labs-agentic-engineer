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
 * Render a HITL question card in the terminal and collect the user's answer
 * (console ADR-0012 / #270). The pure `parseSelection` maps a typed line to a
 * `QuestionAnswer` (number picks → option labels, anything else → free text);
 * `collectAnswers` drives it over a readline interface and returns the serialized
 * next-turn instruction (or null if the user backs out).
 */

import type readline from "node:readline/promises";
import { stdout } from "node:process";
import {
  buildAnswerInstruction,
  buildAnswersInstruction,
  type AskQuestionInput,
  type AskQuestionOption,
  type QuestionAnswer,
  type QuestionSessionInfo,
} from "@aep/agent-stream";
import type { PendingQuestions } from "../engine/questions.js";

const color = (code: string) => (s: string): string => (stdout.isTTY ? `\x1b[${code}m${s}\x1b[0m` : s);
const dim = color("2");
const bold = color("1");
const cyan = color("36");

/** The console's done-ticked / now-highlighted / todo-outlined chips, in text. */
const AREA_MARK: Record<string, string> = { done: "✔", now: "▸", todo: "·" };

/**
 * Render a grilling-session round's area checklist above its questions (#486) —
 * the terminal's half of the console's checklist header, so a session is as
 * visible in `pnpm play` as it is in the console. One-form interviews carry no
 * `session` and print nothing.
 */
export function renderSessionHeader(session: QuestionSessionInfo): string {
  const title = session.title?.trim();
  const head = `\n${cyan("§")} ${bold(`Grilling session${title ? ` — ${title}` : ""}`)}\n`;
  if (session.areas.length === 0) return head;
  const areas = session.areas
    .map((a) => {
      const mark = AREA_MARK[a.state] ?? "·";
      const name = a.state === "now" ? bold(a.name) : a.state === "done" ? dim(a.name) : a.name;
      return `${mark} ${name}`;
    })
    .join("   ");
  return `${head}    ${areas}\n`;
}

/** Render one question and its numbered options to a string block. */
function renderQuestion(q: AskQuestionInput, ordinal?: number): string {
  const head = ordinal ? `Q${ordinal}. ${q.question}` : q.question;
  const lines = [`\n${cyan("?")} ${bold(head)}`];
  q.options.forEach((o, i) => {
    const rec = o.recommended ? dim(" (recommended)") : "";
    const desc = o.description ? dim(` — ${o.description}`) : "";
    lines.push(`    ${i + 1}) ${o.label}${desc}${rec}`);
  });
  const hint = q.multiSelect ? "pick one or more (e.g. 1,3)" : "pick one (e.g. 1)";
  lines.push(dim(`    ${hint}, or type your own answer; add a note after '—'`));
  return lines.join("\n") + "\n";
}

/**
 * Map a typed line to a `QuestionAnswer`. A line of 1-based option numbers
 * (comma/space separated), optionally followed by ` — <note>`, resolves to the
 * chosen labels plus the note. Anything that isn't a clean number pick is taken
 * verbatim as free text (selection empty) — the "Other" path.
 */
export function parseSelection(raw: string, options: AskQuestionOption[]): QuestionAnswer {
  const trimmed = raw.trim();
  let selectionPart = trimmed;
  let note: string | undefined;
  const dash = trimmed.match(/\s+(?:—|--)\s+(.+)$/);
  if (dash && dash.index !== undefined) {
    note = dash[1]?.trim();
    selectionPart = trimmed.slice(0, dash.index).trim();
  }

  const tokens = selectionPart.split(/[,\s]+/).filter(Boolean);
  const selected: string[] = [];
  let cleanPick = tokens.length > 0;
  for (const tok of tokens) {
    const idx = Number(tok);
    if (Number.isInteger(idx) && idx >= 1 && idx <= options.length) {
      const label = options[idx - 1]!.label;
      if (!selected.includes(label)) selected.push(label);
    } else {
      cleanPick = false;
      break;
    }
  }

  if (cleanPick && selected.length > 0) {
    return note ? { selected, freeText: note } : { selected };
  }
  // Not a number pick → the whole line is a free-text answer.
  return { selected: [], freeText: trimmed };
}

/**
 * Prompt for every question in `pending` and return the serialized instruction
 * for the next turn, or null if the user typed `/skip` or closed stdin (the chat
 * loop then falls back to a normal prompt). A single question serializes via
 * `buildAnswerInstruction`; a batch via `buildAnswersInstruction`.
 */
export async function collectAnswers(
  rl: readline.Interface,
  pending: PendingQuestions,
): Promise<string | null> {
  const numbered = pending.questions.length > 1;
  if (pending.session) stdout.write(renderSessionHeader(pending.session));
  const answers: Array<{ question: string } & QuestionAnswer> = [];
  for (let i = 0; i < pending.questions.length; i++) {
    const q = pending.questions[i]!;
    stdout.write(renderQuestion(q, numbered ? i + 1 : undefined));
    let raw: string;
    try {
      raw = await rl.question("  answer ❯ ");
    } catch {
      return null; // stdin closed
    }
    if (raw.trim() === "/skip") return null;
    answers.push({ question: q.question, ...parseSelection(raw, q.options) });
  }

  if (pending.batch) return buildAnswersInstruction(answers);
  const a = answers[0]!;
  return buildAnswerInstruction(a.question, a.selected, a.freeText);
}
