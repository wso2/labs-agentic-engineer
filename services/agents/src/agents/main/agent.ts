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
 * The main agent: the spec-editing agent behind the project's main chat. Its
 * tool set is assembled per turn by `runConversationTurn` (the file tools or the
 * task-plan tools, plus whatever MCP, web search and the register draft add),
 * and its instructions are the matching system prompt; this module makes the
 * agent out of them under the turn's settings.
 */

import type { ToolSet } from "ai";
import { buildToolLoopAgent, type AgentRunSettings, type TurnAgent } from "../run-settings.js";

export interface MainAgentDeps {
  /** The system prompt (skill catalog and surface policy already appended). */
  instructions: string;
  /** The turn's whole tool set. */
  tools: ToolSet;
}

export function createMainAgent(deps: MainAgentDeps, run: AgentRunSettings): TurnAgent {
  return buildToolLoopAgent(run, deps);
}
