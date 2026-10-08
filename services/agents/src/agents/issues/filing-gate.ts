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
 * "Draft first, file on the user's go-ahead", enforced in code
 * (`../confirmation.ts`): create_issue is withheld unless THIS turn's
 * instruction is the user's own File it answer, and runs once for it.
 */

import type { ModelMessage, ToolSet } from "ai";
import { answeredWith, once, refusing } from "../confirmation.js";

/** The confirmation question the agent asks, and the option that files. */
export const FILE_QUESTION = "File this issue?";
export const FILE_IT = "File it";

/** The MCP tool the gate guards. */
const CREATE_ISSUE = "create_issue";

/**
 * Is `instruction` the user's single answer to FILE_QUESTION selecting exactly
 * FILE_IT (optionally with a note)? See `answeredWith`.
 */
export function filingConfirmed(instruction: string): boolean {
  return answeredWith(instruction, FILE_QUESTION, FILE_IT);
}

/**
 * Guard `create_issue`. Unconfirmed, it refuses with a tool error telling the
 * model what to do; confirmed, it runs at most once per turn (`once`). Call it
 * once per turn: the one-filing state lives in the returned tools. When the set
 * has no create_issue, `tools` comes back unchanged.
 */
export function gateCreateIssue(tools: ToolSet, confirmed: boolean): ToolSet {
  const create = tools[CREATE_ISSUE];
  if (create === undefined) return tools;
  const gated = confirmed
    ? once(create, "A filing was already attempted in this turn; tell the user the result and ask before trying again.")
    : refusing(
        create,
        `Not filed. Ask the user "${FILE_QUESTION}" with ask_question (options ${FILE_IT} / Change it) and file only after they answer ${FILE_IT}.`,
      );
  return gated === create ? tools : { ...tools, [CREATE_ISSUE]: gated };
}

/**
 * The issue this turn filed: the last `create_issue` call whose result names
 * the new issue's number, with the title the call filed (raw: the caller
 * sanitises it). Read defensively — the result is the MCP server's text
 * (`{"number":15,"url":…}`) and a refused, failed or unreadable call is no
 * filing. Undefined when the turn filed nothing.
 */
export function filedIssue(messages: readonly ModelMessage[]): { number: number; title: string } | undefined {
  const titles = new Map<string, string>();
  let filed: { number: number; title: string } | undefined;
  for (const m of messages) {
    if (typeof m.content === "string") continue;
    for (const part of m.content) {
      if (part.type === "tool-call" && part.toolName === CREATE_ISSUE) {
        const title = (part.input as { title?: unknown } | null)?.title;
        if (typeof title === "string") titles.set(part.toolCallId, title);
      } else if (part.type === "tool-result" && part.toolName === CREATE_ISSUE) {
        const number = issueNumber(part.output);
        const title = titles.get(part.toolCallId);
        if (number !== undefined && title !== undefined) filed = { number, title };
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
