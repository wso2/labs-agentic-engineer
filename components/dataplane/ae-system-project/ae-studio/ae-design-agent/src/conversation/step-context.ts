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
 * How much context a conversation held when one model step ended, read off
 * that step's `finish-step` part :
 * the step's whole prompt, cached or not, plus what it generated, which joins
 * the history the next step reads. A turn's last measure is its closing
 * context size, which auto-rotation reads on the next send
 * (`conversations/thread-book.ts`).
 */

import type { StreamPart } from "@aep/agent-stream";

interface StepUsage {
  inputTokens?: number;
  inputTokenDetails?: { noCacheTokens?: number; cacheReadTokens?: number; cacheWriteTokens?: number };
  outputTokens?: number;
}

/** The step's context size, or `undefined` for any other part, a step without usage, or a zero measure. */
export function stepContextOf(part: StreamPart): number | undefined {
  if (part.type !== "finish-step") return undefined;
  // The SDK's finish-step carries that ONE step's LanguageModelUsage, not the
  // wire TurnUsage the StreamPart type names for its `usage` field.
  const usage = part.usage as StepUsage | undefined;
  if (!usage) return undefined;
  const d = usage.inputTokenDetails ?? {};
  const input = usage.inputTokens ?? (d.noCacheTokens ?? 0) + (d.cacheReadTokens ?? 0) + (d.cacheWriteTokens ?? 0);
  const total = input + (usage.outputTokens ?? 0);
  return total > 0 ? total : undefined;
}
