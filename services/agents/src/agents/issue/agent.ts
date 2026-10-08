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
 * An issue's own agent: the agent behind the chat on one filed issue's card.
 * It has no spec bundle, no file tools, no skill loader and no classifier —
 * the question tools are its own, and the tools that read and change the issue
 * are the BFF's, loaded from the turn's MCP block (whose token names the issue)
 * and handed in as `mcpTools`.
 */

import { ASK_QUESTION_TOOL, ASK_QUESTIONS_TOOL, type Surface } from "@aep/agent-stream";
import type { ToolSet } from "ai";
import { askQuestionTool, askQuestionsTool } from "../main/tools/files.js";
import type { SkillSource } from "../main/skill-source.js";
import { buildToolLoopAgent, type AgentRunSettings, type TurnAgent } from "../run-settings.js";
import { CONFIRMATIONS, gateWrites } from "./confirm-gate.js";
import { buildIssueInstructions } from "./prompt.js";

/**
 * The MCP tools an issue's agent takes, by contract name: the two reads and
 * the gated writes. Whatever else the server lists (the Issues chat's
 * search_issues / create_issue) is left out, so an issue's thread never files
 * or searches.
 */
const ISSUE_MCP_TOOLS: readonly string[] = ["get_issue", "list_components", ...Object.keys(CONFIRMATIONS)];

export interface IssueAgentDeps {
  /** The issue this thread works on; its prompt names it. */
  issueNumber: number;
  /** The MCP tools this turn loaded (`{}` when it has none). */
  mcpTools: ToolSet;
  /** This turn's raw user instruction: the confirmation gate reads it. */
  instruction: string;
  skills?: SkillSource | undefined;
  surface?: Surface | undefined;
}

export function createIssueAgent(deps: IssueAgentDeps, run: AgentRunSettings): TurnAgent {
  const issueTools: ToolSet = {};
  for (const name of ISSUE_MCP_TOOLS) {
    const tool = deps.mcpTools[name];
    if (tool !== undefined) issueTools[name] = tool;
  }
  // The question tools spread LAST — the shadow-guard — so an MCP tool can
  // never stand in for them. Every write then refuses until the user's own
  // answer to its question: the prompt asks for the same, and this is what
  // holds when the issue's text tries to talk the model past the question.
  const tools = gateWrites(
    { ...issueTools, [ASK_QUESTION_TOOL]: askQuestionTool, [ASK_QUESTIONS_TOOL]: askQuestionsTool },
    deps.instruction,
  );
  return buildToolLoopAgent(run, {
    instructions: buildIssueInstructions(deps.issueNumber, deps.skills, deps.surface),
    tools,
  });
}
