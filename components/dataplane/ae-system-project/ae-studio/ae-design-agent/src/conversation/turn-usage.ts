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
 * A turn's token usage on the pinned cross-runtime wire shape (#249): what the
 * turn's usage record (07 §7) carries.
 */

import type { LanguageModelUsage } from "ai";
import type { TurnUsage } from "@aep/agent-stream";

/**
 * Project the AI SDK's whole-turn `LanguageModelUsage` onto the pinned
 * cross-runtime wire shape (#249): cache reads/writes come from
 * `inputTokenDetails`, absent counts collapse to 0 so every field is a
 * required number, and `model` is the resolved id the turn ran on (threaded
 * from the composition root — the SDK usage object does not carry it).
 *
 * `inputTokens` on the wire is the UNCACHED input, as the coding runner sends
 * it (Anthropic's `input_tokens`); aep-api prices cache reads and writes from
 * their own fields. The SDK's `inputTokens` is the total including both, so
 * sending it would bill every cached token twice.
 */
export function toTurnUsage(usage: LanguageModelUsage, model: string): TurnUsage {
  const cacheReadTokens = usage.inputTokenDetails?.cacheReadTokens ?? 0;
  const cacheCreationTokens = usage.inputTokenDetails?.cacheWriteTokens ?? 0;
  const uncached =
    usage.inputTokenDetails?.noCacheTokens ??
    Math.max(0, (usage.inputTokens ?? 0) - cacheReadTokens - cacheCreationTokens);
  return {
    inputTokens: uncached,
    outputTokens: usage.outputTokens ?? 0,
    cacheReadTokens,
    cacheCreationTokens,
    model,
  };
}
