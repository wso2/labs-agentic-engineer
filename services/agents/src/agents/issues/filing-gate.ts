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
 * "Draft first, file on the user's go-ahead", enforced in code. The prompt asks
 * the model to ask "File this issue?" and file only on File it, but anything the
 * model reads (an issue body returned by search_issues, text the user pasted)
 * can try to talk it into calling create_issue without that answer. So the
 * filing tool is withheld unless THIS turn's instruction is the user's own File
 * it answer, which only the console's question card produces.
 */

import type { ModelMessage, ToolSet } from "ai";
import { buildAnswerInstruction } from "@aep/agent-stream";

/** The confirmation question the agent asks, and the option that files. */
export const FILE_QUESTION = "File this issue?";
export const FILE_IT = "File it";

/** The MCP tool the gate guards. */
const CREATE_ISSUE = "create_issue";

/** What follows the label when the user added a note to their answer. */
const NOTE_SEPARATOR = " — ";

const SINGLE_ANSWER = buildAnswerInstruction(FILE_QUESTION, [FILE_IT]);

/**
 * Is `instruction` the user's answer to FILE_QUESTION selecting exactly
 * FILE_IT? Only the single-answer serialization (`buildAnswerInstruction`),
 * anchored at the START of the instruction, with nothing after the label or a
 * note. The batch form is deliberately not accepted: its lines (and any note,
 * which may span lines) can be forged from text the user pastes, so a gate that
 * scanned for them would disagree with the serializer. A batch answer simply
 * gets the refusal, and the agent re-asks with ask_question. Another label
 * ("File it later", "Change it") or chat that merely contains the words does
 * not count.
 */
export function filingConfirmed(instruction: string): boolean {
  const text = instruction.trim();
  return text === SINGLE_ANSWER || text.startsWith(SINGLE_ANSWER + NOTE_SEPARATOR);
}

/**
 * Guard `create_issue`. Unconfirmed, it keeps its description and schema but
 * refuses with a tool error telling the model what to do. Confirmed, it runs at
 * most ONCE per turn: the user's go-ahead covers one filing, and injected text
 * in an earlier tool result (or a retry after a timeout that did create the
 * issue) must not file a second. A failed first attempt counts, so the agent
 * tells the user and asks again. Call it once per turn: the one-filing state
 * lives in the returned tools. When the set has no create_issue, `tools` comes
 * back unchanged.
 */
export function gateCreateIssue(tools: ToolSet, confirmed: boolean): ToolSet {
  const create = tools[CREATE_ISSUE];
  if (create === undefined) return tools;
  if (!confirmed) {
    return {
      ...tools,
      [CREATE_ISSUE]: {
        ...create,
        execute: async () => {
          throw new Error(
            `Not filed. Ask the user "${FILE_QUESTION}" with ask_question (options ${FILE_IT} / Change it) and file only after they answer ${FILE_IT}.`,
          );
        },
      },
    } as ToolSet;
  }
  const execute = create.execute;
  if (execute === undefined) return tools;
  let attempted = false;
  return {
    ...tools,
    [CREATE_ISSUE]: {
      ...create,
      execute: async (...args: Parameters<typeof execute>) => {
        if (attempted) {
          throw new Error(
            "A filing was already attempted in this turn; tell the user the result and ask before trying again.",
          );
        }
        attempted = true;
        return await execute(...args);
      },
    },
  } as ToolSet;
}

/**
 * The issue this turn filed, as `Filed #<number>: <title>`: the last
 * `create_issue` call whose result names the new issue's number, with the
 * title the call filed. Read defensively — the result is the MCP server's text
 * (`{"number":15,"url":…}`) and a refused, failed or unreadable call is no
 * filing. Undefined when the turn filed nothing.
 */
export function filedIssue(messages: readonly ModelMessage[]): string | undefined {
  const titles = new Map<string, string>();
  let filed: string | undefined;
  for (const m of messages) {
    if (typeof m.content === "string") continue;
    for (const part of m.content) {
      if (part.type === "tool-call" && part.toolName === CREATE_ISSUE) {
        const title = (part.input as { title?: unknown } | null)?.title;
        if (typeof title === "string" && title.trim() !== "") titles.set(part.toolCallId, title.trim());
      } else if (part.type === "tool-result" && part.toolName === CREATE_ISSUE) {
        const number = issueNumber(part.output);
        const title = titles.get(part.toolCallId);
        if (number !== undefined && title !== undefined) filed = `Filed #${number}: ${title}`;
      }
    }
  }
  return filed;
}

/** The `number` a create_issue result names, when it is a positive integer. */
function issueNumber(output: unknown): number | undefined {
  const o = output as { type?: unknown; value?: unknown } | null;
  let value: unknown;
  if (o?.type === "json") value = o.value;
  else if (o?.type === "text" && typeof o.value === "string") {
    try {
      value = JSON.parse(o.value);
    } catch {
      return undefined;
    }
  } else return undefined;
  const number = (value as { number?: unknown } | null)?.number;
  return typeof number === "number" && Number.isInteger(number) && number > 0 ? number : undefined;
}
