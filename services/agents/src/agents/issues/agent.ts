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
 * The Issues agent: the agent behind the Issues view's chat. It has no spec
 * bundle, no file tools and no skill loader — the report classifier and the
 * question tools are its own, and the tools that search and file issues are
 * the BFF's, loaded from the turn's MCP block and handed in as `mcpTools`.
 */

import type { Surface } from "@aep/agent-stream";
import type { ToolSet } from "ai";
import type { SkillSource } from "../main/skill-source.js";
import { buildToolLoopAgent, type AgentRunSettings, type TurnAgent } from "../run-settings.js";
import type { JevOptions } from "./classify.js";
import { filingConfirmed, gateCreateIssue } from "./filing-gate.js";
import { buildIssuesInstructions } from "./prompt.js";
import { buildIssuesTools } from "./tools.js";

export interface IssuesAgentDeps {
  /** How the report classifier reaches its model. */
  jev: JevOptions;
  /** The MCP tools this turn loaded (`{}` when it has none). */
  mcpTools: ToolSet;
  /** This turn's raw user instruction: the filing gate reads it. */
  instruction: string;
  skills?: SkillSource | undefined;
  surface?: Surface | undefined;
}

export function createIssuesAgent(deps: IssuesAgentDeps, run: AgentRunSettings): TurnAgent {
  const builtIns = buildIssuesTools(deps.jev);
  // The agent's own tools spread LAST — the shadow-guard — so an MCP tool can
  // never stand in for the classifier or a question tool of the same name.
  const merged: ToolSet = Object.keys(deps.mcpTools).length > 0 ? { ...deps.mcpTools, ...builtIns } : builtIns;
  // It files only on the user's own File it answer: until then create_issue
  // (an MCP tool) refuses. The prompt asks for the same; this is what holds
  // when text the model read tries to talk it past the question.
  const tools = gateCreateIssue(merged, filingConfirmed(deps.instruction));
  return buildToolLoopAgent(run, {
    instructions: buildIssuesInstructions(deps.skills, deps.surface),
    tools,
  });
}
