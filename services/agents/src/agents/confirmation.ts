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
import { ASK_QUESTION_TOOL, buildAnswerInstruction, isErrorToolOutput, type AskQuestionInput } from "@aep/agent-stream";

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
 * The question card the user last saw: the input of the last `ask_question`
 * call in `messages` (a conversation's stored history) that was accepted. A
 * call the SDK rejected against the schema showed no card, and an input not
 * shaped like a question is no card either. Undefined when there is none.
 */
export function lastAskedQuestion(messages: readonly ModelMessage[]): AskQuestionInput | undefined {
  const rejected = new Set<string>();
  for (const m of messages) {
    if (typeof m.content === "string") continue;
    for (const part of m.content) {
      if (part.type === "tool-result" && part.toolName === ASK_QUESTION_TOOL && isErrorToolOutput(part.output)) {
        rejected.add(part.toolCallId);
      }
    }
  }
  let asked: AskQuestionInput | undefined;
  for (const m of messages) {
    if (typeof m.content === "string") continue;
    for (const part of m.content) {
      if (part.type !== "tool-call" || part.toolName !== ASK_QUESTION_TOOL || rejected.has(part.toolCallId)) continue;
      if (isQuestion(part.input)) asked = part.input;
    }
  }
  return asked;
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

/** Line endings and surrounding whitespace are not part of a change. */
const normalised = (s: string): string => s.replace(/\r\n/g, "\n").trim();

/**
 * Did the card the user last saw (`asked`) ask `confirmation.question` with
 * `confirmation.option` showing exactly `change` as its description? An empty
 * change (a write with no arguments) binds no description, only the question.
 */
function shownOnCard(asked: AskQuestionInput | undefined, { question, option }: Confirmation, change: string): boolean {
  if (asked?.question !== question) return false;
  const confirm = asked.options.find((o) => o.label === option);
  if (confirm === undefined) return false;
  return normalised(change) === "" || normalised(confirm.description ?? "") === normalised(change);
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
 * arguments whose change (`describe`) is the one the card the user answered
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
  describe: (input: unknown) => string,
  asked: AskQuestionInput | undefined,
  attempted: string,
): GatedTool {
  const execute = tool.execute;
  if (execute === undefined) return tool;
  const { question, option } = confirmation;
  let tried = false;
  return {
    ...tool,
    execute: async (...args: Parameters<typeof execute>) => {
      if (tried) throw new Error(attempted);
      if (!shownOnCard(asked, confirmation, describe(args[0]))) {
        throw new Error(
          `Not done: this is not the change the user confirmed. Ask "${question}" again with the exact change as the ${option} option's description.`,
        );
      }
      tried = true;
      return await execute(...args);
    },
  } as GatedTool;
}
