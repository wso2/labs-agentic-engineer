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

import type { ToolSet } from "ai";
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
 * Withhold `create_issue` until the user has confirmed: unconfirmed, it keeps
 * its description and schema but refuses with a tool error telling the model
 * what to do. Confirmed, or when the set has no create_issue, `tools` comes
 * back unchanged.
 */
export function gateCreateIssue(tools: ToolSet, confirmed: boolean): ToolSet {
  const create = tools[CREATE_ISSUE];
  if (confirmed || create === undefined) return tools;
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
