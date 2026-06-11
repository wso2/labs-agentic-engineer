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

import { streamText, stepCountIs } from "ai";
import type { Tool } from "ai";
import { createAnthropic } from "@ai-sdk/anthropic";
import { config } from "./config.js";
import { sharedTools } from "../tools/index.js";
import type { AgentConfig, AgentDefinition, AgentResult } from "./types.js";
import type { Skill } from "../skills/types.js";
import { resolveAnthropicKey } from "./anthropic-key-resolver.js";

function buildSystemPromptWithSkills(
  basePrompt: string,
  skills: Skill[],
): string {
  if (skills.length === 0) return basePrompt;

  const skillSections = skills
    .map(
      (skill) =>
        `### ${skill.name}\n${skill.description}\n\n${skill.instructions}`,
    )
    .join("\n\n");

  return `${basePrompt}\n\n## Skills\n\n${skillSections}`;
}

function collectSkillTools(skills: Skill[]): Record<string, Tool> {
  const tools: Record<string, Tool> = {};
  for (const skill of skills) {
    Object.assign(tools, skill.tools);
  }
  return tools;
}

export function createAgent<TInput, TOutput>(
  agentConfig: AgentConfig<TInput, TOutput>,
): AgentDefinition<TInput, TOutput> {
  return {
    name: agentConfig.name,
    description: agentConfig.description,

    /**
     * orgId is the OC org slug from `X-Oc-Org-Id`. The Anthropic key is
     * resolved per-call via git-service so the org's own key is used when
     * configured (otherwise the platform fallback).
     */
    run: async (input: TInput, orgId: string): Promise<AgentResult<TOutput>> => {
      const skills = agentConfig.skills ?? [];

      const systemPrompt = buildSystemPromptWithSkills(
        agentConfig.systemPrompt,
        skills,
      );

      const tools = {
        ...sharedTools,
        ...collectSkillTools(skills),
        ...(agentConfig.tools ?? {}),
      };

      const maxSteps = agentConfig.maxSteps ?? config.maxSteps;

      const { key } = await resolveAnthropicKey(orgId);
      const anthropic = createAnthropic({ apiKey: key });

      console.log(`[${agentConfig.name}] starting orgId=${orgId}`);

      const result = streamText({
        model: anthropic(config.model),
        system: systemPrompt,
        prompt: agentConfig.buildUserPrompt(input),
        tools,
        stopWhen: stepCountIs(maxSteps),
      });

      // Consume the stream to completion and collect the final text
      let fullText = "";
      for await (const chunk of result.textStream) {
        fullText += chunk;
      }

      const usage = await result.usage;

      const parsed = agentConfig.outputSchema.safeParse(JSON.parse(fullText));
      if (!parsed.success) {
        throw new Error(
          `[${agentConfig.name}] output validation failed: ${parsed.error.message}`,
        );
      }

      console.log(`[${agentConfig.name}] completed`);

      return {
        output: parsed.data,
        usage: {
          inputTokens: usage.inputTokens ?? 0,
          outputTokens: usage.outputTokens ?? 0,
        },
      };
    },
  };
}
