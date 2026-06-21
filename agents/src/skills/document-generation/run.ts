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
 * Headless document-generation runner — runs a skill to its full text with an
 * injected model, no HTTP/SSE. Used by evals to score the generated document.
 * The live route streams instead (and applies `postProcess` for DSL skills);
 * this collects the final text, which is the same artifact the route persists
 * for skills without a post-processor.
 */

import { streamText, type LanguageModel } from "ai";
import type { DocumentGenerationSkill, SkillInput } from "./types.js";

export interface DocGenRunResult {
  text: string;
  usage: { inputTokens: number; outputTokens: number };
}

export async function runDocGenSkill(
  model: LanguageModel,
  skill: DocumentGenerationSkill,
  input: SkillInput,
): Promise<DocGenRunResult> {
  const result = streamText({
    model,
    system: skill.systemPrompt,
    prompt: skill.buildUserPrompt(input),
  });
  const text = await result.text;
  const u = await result.usage;
  return {
    text,
    usage: { inputTokens: u.inputTokens ?? 0, outputTokens: u.outputTokens ?? 0 },
  };
}
