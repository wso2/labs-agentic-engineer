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
 * The Issues chat's own tool set: the report classifier and the two
 * human-in-the-loop question tools. The agent has no spec files and no skill
 * loader; the tools that search and file issues are the BFF's MCP tools, merged
 * in per turn by `runConversationTurn` from the turn's `mcp` block.
 */

import type { Tool } from "ai";
import { ASK_QUESTION_TOOL, ASK_QUESTIONS_TOOL } from "@aep/agent-stream";
import { askQuestionTool, askQuestionsTool } from "../main/tools/files.js";
import { buildClassifyReportTool, CLASSIFY_REPORT, type JevOptions } from "./classify.js";

export function buildIssuesTools(jev: JevOptions): Record<string, Tool> {
  return {
    [CLASSIFY_REPORT]: buildClassifyReportTool(jev),
    [ASK_QUESTION_TOOL]: askQuestionTool,
    [ASK_QUESTIONS_TOOL]: askQuestionsTool,
  };
}
