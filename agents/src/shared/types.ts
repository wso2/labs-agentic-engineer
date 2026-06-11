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
import type { Skill } from "../skills/types.js";

export interface AgentDefinition<TInput, TOutput> {
  name: string;
  description: string;
  /**
   * orgId is the OC org slug (X-Oc-Org-Id). createAgent uses it to resolve
   * the effective Anthropic key per call.
   */
  run: (input: TInput, orgId: string) => Promise<AgentResult<TOutput>>;
}

export interface AgentResult<T> {
  output: T;
  usage: { inputTokens: number; outputTokens: number };
}

export interface AgentConfig<TInput, TOutput> {
  name: string;
  description: string;
  systemPrompt: string;
  buildUserPrompt: (input: TInput) => string;
  outputSchema: import("zod").ZodType<TOutput>;
  tools?: Record<string, Tool>;
  skills?: Skill[];
  maxSteps?: number;
}
