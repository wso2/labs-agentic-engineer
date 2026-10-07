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
 * What every agent module shares: the per-turn settings the runner decides
 * (`AgentRunSettings`), the one way an agent is put together from them
 * (`buildToolLoopAgent`), and the question stop every agent ends its turn on.
 *
 * An agent module (`main/agent.ts`, `issues/agent.ts`) owns WHAT the agent is —
 * its tools and instructions. `runTurn` owns what varies per turn — the model
 * built from the org's key, the step cap, the output ceiling and the prompt-
 * cache breakpoints — and hands it over as `AgentRunSettings`.
 */

import {
  ToolLoopAgent,
  isStepCount,
  type Instructions,
  type LanguageModel,
  type ModelMessage,
  type PrepareStepFunction,
  type StopCondition,
  type ToolLoopAgentSettings,
  type ToolSet,
} from "ai";
import { isErrorToolOutput, isQuestionTool } from "@aep/agent-stream";

/** Provider-specific per-call options (`ai` doesn't export the type directly). */
export type ProviderOptions = NonNullable<ToolLoopAgentSettings["providerOptions"]>;

/** An agent `runTurn` can stream: a tool loop over an untyped tool set. */
export type TurnAgent = ToolLoopAgent<never, ToolSet>;

/** What `runTurn` decides for this turn and hands to the agent's factory. */
export interface AgentRunSettings {
  /** The turn's model, built from the org's connection. */
  model: LanguageModel;
  /** The step cap; the agent stops here even without a question. */
  maxSteps: number;
  /** Per-step output-token ceiling; absent → the provider's default. */
  maxOutputTokens?: number;
  /** Retries per model call on a retryable provider error; absent → the SDK's. */
  maxRetries?: number;
  /** Provider-specific call options, passed through opaquely. */
  providerOptions?: ProviderOptions;
  /**
   * Turns the agent's system prompt into what the model receives: the plain
   * string, or a system message carrying the prompt-cache breakpoint.
   */
  instructionsWrap: (instructions: string) => Instructions;
  /** The rolling cache breakpoint; absent when caching is off. */
  prepareStep?: PrepareStepFunction<ToolSet>;
}

/**
 * Stop when the last step carries a question tool-call the schema ACCEPTED.
 * The SDK's own `hasToolCall` also matches a call whose input failed
 * validation (it stays in `step.toolCalls` flagged `invalid`), which would end
 * the turn on a question nobody can render — an empty option label was enough
 * to leave the console blank and the conversation stuck awaiting-human.
 * Skipping invalid calls lets the model read the validation error as a tool
 * error and retry in the next step.
 */
export function questionStop(): StopCondition<ToolSet> {
  return ({ steps }) =>
    steps[steps.length - 1]?.toolCalls.some((call) => !call.invalid && isQuestionTool(call.toolName)) ?? false;
}

/**
 * True when the turn ended on a HITL question tool-call (`ask_question` or
 * `ask_questions`, console ADR-0012 / #270) that RESOLVED — its placeholder
 * result is on the transcript and is not an error. Scans only the messages
 * appended THIS turn; `questionStop` guarantees an accepted call is the last
 * step, so a match means the turn is awaiting the user's answer. A call the
 * schema rejected leaves an error result instead, and a turn that then ran out
 * of steps is done, not awaiting anyone.
 */
export function endedAwaitingHuman(appended: ModelMessage[]): boolean {
  const asked = new Set<string>();
  const resolved = new Set<string>();
  for (const m of appended) {
    if (!Array.isArray(m.content)) continue;
    for (const part of m.content) {
      if (m.role === "assistant" && part.type === "tool-call" && isQuestionTool(part.toolName)) {
        asked.add(part.toolCallId);
      } else if (m.role === "tool" && part.type === "tool-result" && asked.has(part.toolCallId)) {
        if (!isErrorToolOutput(part.output)) resolved.add(part.toolCallId);
      }
    }
  }
  return resolved.size > 0;
}

/**
 * The agent an agent module describes, under this turn's settings. Every agent
 * ends its turn at the step cap or on an accepted question call (an agent
 * without question tools simply never trips the second). Optional settings
 * are left off entirely when absent, so the request is byte-identical to one
 * made without them.
 */
export function buildToolLoopAgent(
  run: AgentRunSettings,
  agent: { instructions: string; tools: ToolSet },
): TurnAgent {
  return new ToolLoopAgent({
    model: run.model,
    instructions: run.instructionsWrap(agent.instructions),
    tools: agent.tools,
    stopWhen: [isStepCount(run.maxSteps), questionStop()],
    ...(run.maxOutputTokens ? { maxOutputTokens: run.maxOutputTokens } : {}),
    ...(run.maxRetries !== undefined ? { maxRetries: run.maxRetries } : {}),
    ...(run.providerOptions ? { providerOptions: run.providerOptions } : {}),
    ...(run.prepareStep ? { prepareStep: run.prepareStep } : {}),
  });
}
