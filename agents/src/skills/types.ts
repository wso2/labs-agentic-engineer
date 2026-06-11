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

import type { Tool } from "ai";

/**
 * A skill is a composable capability that bundles related tools with
 * system prompt instructions. Skills differ from raw tools: a tool is a
 * single function the model can call, while a skill is a higher-level
 * capability that may combine multiple tools with guidance on when and
 * how to use them.
 */
export interface Skill {
  name: string;
  description: string;
  /** Prompt text appended to the agent's system prompt. */
  instructions: string;
  /** Tools this skill provides to the agent. */
  tools: Record<string, Tool>;
}
