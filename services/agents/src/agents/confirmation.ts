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
 * "Draft first, act on the user's go-ahead", enforced in code. An agent that
 * writes somewhere the user can see (an issue filed, a comment posted) asks
 * first with a question card, and the prompt says to act only on the answer.
 * But anything the model reads (an issue's body, a comment, text the user
 * pasted) can try to talk it past the question. So a write tool is gated on
 * THIS turn's instruction being the user's own answer, which only the console's
 * question card produces, and on the call writing exactly the change that card
 * showed: the agent puts the change, rendered one canonical way, in the confirm
 * option's `description`, and a call whose arguments render to anything else is
 * refused. The card is read from the conversation the service stored before
 * this turn, never from the instruction or this turn's model output. A
 * confirmed write runs at most once.
 *
 * The gates themselves (`issues/filing-gate.ts`, `issue/confirm-gate.ts`) say
 * which tool waits for which answer and how its change is rendered; this
 * module is how every gate reads an answer and a card and holds a tool.
 */

import type { ModelMessage, ToolSet } from "ai";
import {
  ASK_QUESTION_TOOL,
  buildAnswerInstruction,
  isErrorToolOutput,
  isQuestionTool,
  type AskQuestionInput,
} from "@aep/agent-stream";

type GatedTool = ToolSet[string];

/** A write's question and the option that goes ahead with it. */
export interface Confirmation {
  question: string;
  option: string;
}

/** What follows the label when the user added a note to their answer. */
const NOTE_SEPARATOR = " — ";

/**
 * Is `instruction` the user's answer to `question` selecting exactly `option`?
 * Only the single-answer serialization (`buildAnswerInstruction`), anchored at
 * the START of the instruction, with nothing after the label or a note. The
 * batch form is deliberately not accepted: its lines (and any note, which may
 * span lines) can be forged from text the user pastes, so a gate that scanned
 * for them would disagree with the serializer. A batch answer simply gets the
 * refusal, and the agent re-asks with ask_question. Another label ("File it
 * later"), another question, or chat that merely contains the words does not
 * count.
 */
export function answeredWith(instruction: string, question: string, option: string): boolean {
  const single = buildAnswerInstruction(question, [option]);
  const text = instruction.trim();
  return text === single || text.startsWith(single + NOTE_SEPARATOR);
}

/**
 * The question card the user is answering, as the console decides it: the one
 * card the agent asked since the user's last message in `messages` (a
 * conversation's stored history), the only card the console lets them answer
 * (its `answerableQuestionId`: the last card, and none once anything was said
 * after it). A call the SDK rejected against the schema showed no card. An
 * `ask_questions` batch of one question is that card (its answer takes the
 * single form). Undefined, so nothing is confirmed, when there is no such card
 * or it is not exactly one readable single question: two cards asked in one
 * turn, a batch of several, an input not shaped like a question. Never an
 * earlier card: the user did not answer that one.
 */
export function answerableQuestion(messages: readonly ModelMessage[]): AskQuestionInput | undefined {
  let lastUser = -1;
  messages.forEach((m, i) => {
    if (m.role === "user") lastUser = i;
  });
  const turn = messages.slice(lastUser + 1);
  const rejected = new Set<string>();
  const cards: { toolCallId: string; toolName: string; input: unknown }[] = [];
  for (const m of turn) {
    if (typeof m.content === "string") continue;
    for (const part of m.content) {
      if (part.type === "tool-result" && isQuestionTool(part.toolName) && isErrorToolOutput(part.output)) {
        rejected.add(part.toolCallId);
      } else if (part.type === "tool-call" && isQuestionTool(part.toolName)) {
        cards.push(part);
      }
    }
  }
  const shown = cards.filter((c) => !rejected.has(c.toolCallId));
  if (shown.length !== 1) return undefined;
  const { toolName, input } = shown[0]!;
  const question = toolName === ASK_QUESTION_TOOL ? input : onlyQuestionOf(input);
  return isQuestion(question) ? question : undefined;
}

/** The one question of an `ask_questions` input, when it holds exactly one. */
function onlyQuestionOf(batch: unknown): unknown {
  const questions = (batch as { questions?: unknown } | null)?.questions;
  return Array.isArray(questions) && questions.length === 1 ? questions[0] : undefined;
}

function isQuestion(input: unknown): input is AskQuestionInput {
  const q = input as { question?: unknown; options?: unknown } | null;
  return (
    typeof q?.question === "string" &&
    Array.isArray(q.options) &&
    q.options.every((o: unknown) => {
      const opt = o as { label?: unknown; description?: unknown } | null;
      return typeof opt?.label === "string" && (opt.description === undefined || typeof opt.description === "string");
    })
  );
}

/**
 * How a write's change is shown on its card: its arguments in order, each
 * rendered by its own function when present. An argument the layout does not
 * name is still shown (`<name>: <value>`, after the named ones, by name), so
 * nothing reaches the tool that the card did not show.
 */
export type ChangeLayout = readonly (readonly [arg: string, render: (value: string) => string])[];

/** `input`'s change as `layout` shows it: the rendered arguments, a blank line between each. */
export function describeArgs(input: unknown, layout: ChangeLayout): string {
  const args = (typeof input === "object" && input !== null ? input : {}) as Record<string, unknown>;
  const text = (v: unknown): string => (typeof v === "string" ? v : JSON.stringify(v));
  const named = new Set(layout.map(([arg]) => arg));
  const parts = layout.flatMap(([arg, render]) => (args[arg] === undefined ? [] : [render(text(args[arg]))]));
  for (const arg of Object.keys(args).sort()) {
    if (!named.has(arg) && args[arg] !== undefined) parts.push(`${arg}: ${text(args[arg])}`);
  }
  return parts.join("\n\n");
}

/**
 * Line endings, blank lines before and whitespace after are not part of a
 * change. A first line's indentation is: it shows on the card, and in markdown
 * it can make a code block.
 */
const normalised = (s: string): string => s.replace(/\r\n/g, "\n").replace(/^(?:[ \t]*\n)+/, "").trimEnd();

/**
 * A character the card cannot show as the write will make it: a control
 * character other than a line break or tab (a lone carriage return renders as
 * a space), a format character (bidi overrides and isolates reorder what is
 * shown, zero-width ones hide), a line or paragraph separator, or anything
 * else that renders as nothing (variation selectors, fillers). CRLF line
 * endings count as line breaks.
 */
const HIDDEN = /(?![\n\t])[\p{Cc}\p{Cf}\p{Zl}\p{Zp}\p{Default_Ignorable_Code_Point}]/u;

/** The first character in `change` the card cannot show faithfully, as `U+XXXX`. */
function hiddenCharacter(change: string): string | undefined {
  const found = HIDDEN.exec(change.replace(/\r\n/g, "\n"))?.[0];
  return found === undefined ? undefined : `U+${found.codePointAt(0)!.toString(16).toUpperCase().padStart(4, "0")}`;
}

/**
 * Why the card the user answered (`asked`) does not confirm the change `input`
 * makes (`layout`); undefined when it does. It does when the card asked
 * `confirmation.question`, exactly one of its options is one the user's answer
 * would read as `confirmation.option`, that option is labelled exactly so, and
 * its description shows exactly the change. Only a write that names no
 * arguments (an empty `layout`) and is given none binds the question alone;
 * any other write, even one whose arguments are left empty, must match the
 * description. A change with a character the card cannot show is refused
 * whatever the card says.
 */
function notAsShown(
  asked: AskQuestionInput | undefined,
  { question, option }: Confirmation,
  layout: ChangeLayout,
  input: unknown,
): string | undefined {
  const described = describeArgs(input, layout);
  const hidden = hiddenCharacter(described);
  if (hidden !== undefined) {
    return `Not done: the change has a character the card cannot show faithfully (${hidden}: a control, format or invisible character). Remove it and ask "${question}" again with the change as the ${option} option's description.`;
  }
  const mismatch = `Not done: this is not the change the user confirmed. Ask "${question}" again with the exact change as the ${option} option's description.`;
  if (asked?.question !== question) return mismatch;
  // The answer names the option by its label, so every option whose answer
  // reads as the confirm one counts: a repeated label, "Post it " (the answer
  // is trimmed), "Post it — later" (read as Post it with a note). With two,
  // nobody knows which description the user read.
  const confirms = asked.options.filter((o) => answeredWith(buildAnswerInstruction(question, [o.label]), question, option));
  if (confirms.length > 1) {
    return `Not done: more than one option on the card reads as ${option}, so the answer does not say which change the user saw. Ask "${question}" again with a single ${option} option.`;
  }
  const confirm = confirms[0];
  if (confirm?.label !== option) return mismatch;
  const change = normalised(described);
  if (layout.length === 0 && change === "") return undefined;
  return normalised(confirm.description ?? "") === change ? undefined : mismatch;
}

/**
 * `tool` unconfirmed: it keeps its description and schema, so the model still
 * knows it exists, but every call fails with `message`, a tool error telling
 * the model what to ask first.
 */
export function refusing(tool: GatedTool, message: string): GatedTool {
  return {
    ...tool,
    execute: async () => {
      throw new Error(message);
    },
  } as GatedTool;
}

/**
 * `tool` confirmed by this turn's answer to `confirmation`: it runs only with
 * arguments whose change (`layout`, `describeArgs`) is the one the card the user answered
 * (`asked`) showed, and at most ONCE. A call with any other change, or with no
 * such card in the stored history, is refused and uses up nothing: the
 * confirmed change can still be made, and no other. The go-ahead covers one
 * write, so injected text in an earlier tool result (or a retry after a
 * timeout that did write) must not make a second: a failed first attempt
 * counts, so the agent tells the user and asks again; later calls fail with
 * `attempted`. The once-state lives in the returned tool, so build it per
 * turn. A tool with no `execute` comes back unchanged.
 */
export function onceAsShown(
  tool: GatedTool,
  confirmation: Confirmation,
  layout: ChangeLayout,
  asked: AskQuestionInput | undefined,
  attempted: string,
): GatedTool {
  const execute = tool.execute;
  if (execute === undefined) return tool;
  let tried = false;
  return {
    ...tool,
    execute: async (...args: Parameters<typeof execute>) => {
      if (tried) throw new Error(attempted);
      const refusal = notAsShown(asked, confirmation, layout, args[0]);
      if (refusal !== undefined) throw new Error(refusal);
      tried = true;
      return await execute(...args);
    },
  } as GatedTool;
}
