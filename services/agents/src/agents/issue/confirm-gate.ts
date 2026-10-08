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
 * talk it into closing the issue or handing it over unasked, or into writing
 * something else than what the user confirmed: the write tools (MCP tools, by
 * contract name) refuse unless THIS turn's instruction is the single answer to
 * the tool's own question selecting its own option, and the one tool that
 * answer confirms runs at most once, only with the change its card showed
 * (`describeChange`).
 */

import type { AskQuestionInput } from "@aep/agent-stream";
import type { ToolSet } from "ai";
import { answeredWith, describeArgs, onceAsShown, refusing, type ChangeLayout, type Confirmation } from "../confirmation.js";

/** The issue's read tools, by their MCP contract names: never gated. */
const READ_TOOLS = ["get_issue", "list_components"] as const;

/** The issue's write tools, by their MCP contract names. */
const WRITE_TOOLS = ["comment_issue", "edit_issue", "close_issue", "reopen_issue", "hand_to_coding_agent"] as const;

export type WriteTool = (typeof WRITE_TOOLS)[number];

/** Every MCP tool an issue's agent takes: its reads and its gated writes. */
export const ISSUE_MCP_TOOLS: readonly string[] = [...READ_TOOLS, ...WRITE_TOOLS];

/** The question each write waits on, and the option that goes ahead. */
export const CONFIRMATIONS: Record<WriteTool, Confirmation> = {
  comment_issue: { question: "Post this comment?", option: "Post it" },
  edit_issue: { question: "Apply this edit?", option: "Apply it" },
  close_issue: { question: "Close this issue?", option: "Close it" },
  reopen_issue: { question: "Reopen this issue?", option: "Reopen it" },
  hand_to_coding_agent: { question: "Hand this to the coding agent?", option: "Hand it over" },
};

/** How each write's change is shown on its card (reopening has none to show). */
const CHANGES: Record<WriteTool, ChangeLayout> = {
  comment_issue: [["body", (body) => body]],
  edit_issue: [
    ["title", (title) => `Title: ${title}`],
    ["body", (body) => `Body:\n${body}`],
  ],
  close_issue: [["reason", (reason) => reason]],
  reopen_issue: [],
  hand_to_coding_agent: [["component", (component) => `Component: ${component}`]],
};

/**
 * The change a call of `tool` with `input` makes, as its confirm option's
 * description must show it: the comment; "Title: <title>" and/or
 * "Body:\n<body>"; the reason; "Component: <component>"; nothing to reopen.
 */
export function describeChange(tool: WriteTool, input: unknown): string {
  return describeArgs(input, CHANGES[tool]);
}

/** The other option of every confirmation: change nothing. */
export const NOT_NOW = "Not now";

/** The one write `instruction` confirms: the tool whose question it answers with its option. */
export function confirmedTool(instruction: string): WriteTool | undefined {
  return WRITE_TOOLS.find((t) => answeredWith(instruction, CONFIRMATIONS[t].question, CONFIRMATIONS[t].option));
}

/**
 * Gate the issue's write tools on this turn's instruction and the card the
 * user answered (`asked`, `answerableQuestion` over the stored history). The
 * write the instruction confirms runs once, only with the change `asked`
 * showed; every other write refuses with a tool error naming the question to
 * ask. The reads and any tool that is not a write pass through untouched.
 * Call it once per turn: the once-state lives in the returned tools.
 */
export function gateWrites(tools: ToolSet, instruction: string, asked: AskQuestionInput | undefined): ToolSet {
  const confirmed = confirmedTool(instruction);
  const gated: ToolSet = { ...tools };
  for (const name of WRITE_TOOLS) {
    const tool = tools[name];
    if (tool === undefined) continue;
    const { question, option } = CONFIRMATIONS[name];
    gated[name] =
      name === confirmed
        ? onceAsShown(
            tool,
            CONFIRMATIONS[name],
            CHANGES[name],
            asked,
            "Already attempted in this turn; tell the user the result and ask before trying again.",
          )
        : refusing(
            tool,
            `Not done. Ask the user "${question}" with ask_question (options ${option} / ${NOT_NOW}) and act only after they answer ${option}.`,
          );
  }
  return gated;
}
