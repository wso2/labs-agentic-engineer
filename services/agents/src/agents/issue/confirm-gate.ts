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
 * Every change an issue's agent makes to its issue waits for the user's own
 * answer to that change's question (`../confirmation.ts`). The issue's body and
 * comments are text the model reads, so they are exactly what might try to
 * talk it into closing the issue or handing it over unasked: the write tools
 * (MCP tools, by contract name) refuse unless THIS turn's instruction is the
 * single answer to the tool's own question selecting its own option, and the
 * one tool that answer confirms runs at most once.
 */

import type { ToolSet } from "ai";
import { answeredWith, once, refusing } from "../confirmation.js";

/** The issue's write tools, by their MCP contract names. */
const WRITE_TOOLS = ["comment_issue", "edit_issue", "close_issue", "reopen_issue", "hand_to_coding_agent"] as const;

export type WriteTool = (typeof WRITE_TOOLS)[number];

/** The question each write waits on, and the option that goes ahead. */
export const CONFIRMATIONS: Record<WriteTool, { question: string; option: string }> = {
  comment_issue: { question: "Post this comment?", option: "Post it" },
  edit_issue: { question: "Apply this edit?", option: "Apply it" },
  close_issue: { question: "Close this issue?", option: "Close it" },
  reopen_issue: { question: "Reopen this issue?", option: "Reopen it" },
  hand_to_coding_agent: { question: "Hand this to the coding agent?", option: "Hand it over" },
};

/** The other option of every confirmation: change nothing. */
export const NOT_NOW = "Not now";

/** The one write `instruction` confirms: the tool whose question it answers with its option. */
export function confirmedTool(instruction: string): WriteTool | undefined {
  return WRITE_TOOLS.find((t) => answeredWith(instruction, CONFIRMATIONS[t].question, CONFIRMATIONS[t].option));
}

/**
 * Gate the issue's write tools on this turn's instruction. The write it
 * confirms runs once; every other write refuses with a tool error naming the
 * question to ask. The reads (`get_issue`, `list_components`) and any tool
 * that is not a write pass through untouched. Call it once per turn: the
 * once-state lives in the returned tools.
 */
export function gateWrites(tools: ToolSet, instruction: string): ToolSet {
  const confirmed = confirmedTool(instruction);
  const gated: ToolSet = { ...tools };
  for (const name of WRITE_TOOLS) {
    const tool = tools[name];
    if (tool === undefined) continue;
    const { question, option } = CONFIRMATIONS[name];
    gated[name] =
      name === confirmed
        ? once(tool, "Already attempted in this turn; tell the user the result and ask before trying again.")
        : refusing(
            tool,
            `Not done. Ask the user "${question}" with ask_question (options ${option} / ${NOT_NOW}) and act only after they answer ${option}.`,
          );
  }
  return gated;
}
