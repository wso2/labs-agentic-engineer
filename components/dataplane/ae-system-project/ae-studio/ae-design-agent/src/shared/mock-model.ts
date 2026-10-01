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
 * Test-only mock `LanguageModel` factory. Drives the real `ToolLoopAgent` /
 * SSE route / eval without spending tokens, so every phase gates deterministically.
 *
 * It returns ONE provider stream per model call (`steps` in order): a step with
 * a `toolCall` finishes `tool-calls` (the SDK then runs the tool's server-side
 * `execute()` and calls the model again for the next step); a `text` step
 * finishes `stop`. The provider-level shapes (`@ai-sdk/provider` V4) are derived
 * from the mock's own constructor type, so there is no extraneous import to
 * resolve under pnpm's strict layout.
 */

import { MockLanguageModelV4, convertArrayToReadableStream, simulateReadableStream } from "ai/test";

// Derive the provider stream-result + part types from the mock itself.
type DoStreamArg = NonNullable<NonNullable<ConstructorParameters<typeof MockLanguageModelV4>[0]>["doStream"]>;
type StreamResult = Extract<DoStreamArg, { stream: unknown }>;
type StreamPartV4 = StreamResult["stream"] extends ReadableStream<infer P> ? P : never;
type UsageV4 = Extract<StreamPartV4, { type: "finish" }>["usage"];

const USAGE: UsageV4 = {
  inputTokens: { total: 10, noCache: 10, cacheRead: 0, cacheWrite: 0 },
  outputTokens: { total: 5, text: 5, reasoning: 0 },
};

export type MockStep =
  | { kind: "text"; text: string }
  | { kind: "toolCall"; toolCallId: string; toolName: string; input: unknown; text?: string };

function streamForStep(step: MockStep, i: number, delayMs?: number): StreamResult {
  const parts: StreamPartV4[] = [{ type: "stream-start", warnings: [] }];
  const textId = `t${i}`;
  const pushText = (text: string): void => {
    parts.push({ type: "text-start", id: textId });
    parts.push({ type: "text-delta", id: textId, delta: text });
    parts.push({ type: "text-end", id: textId });
  };

  if (step.kind === "toolCall") {
    if (step.text) pushText(step.text);
    parts.push({
      type: "tool-call",
      toolCallId: step.toolCallId,
      toolName: step.toolName,
      input: JSON.stringify(step.input), // provider spec: stringified JSON args
    });
    parts.push({
      type: "finish",
      finishReason: { unified: "tool-calls", raw: "tool-calls" },
      usage: USAGE,
    });
  } else {
    pushText(step.text);
    parts.push({ type: "finish", finishReason: { unified: "stop", raw: "stop" }, usage: USAGE });
  }

  // delayMs keeps the turn in-flight (the HTTP 409 concurrency test relies on it).
  const stream = delayMs
    ? simulateReadableStream({ chunks: parts, initialDelayInMs: delayMs, chunkDelayInMs: 0 })
    : convertArrayToReadableStream(parts);
  return { stream };
}

/**
 * Build a mock `LanguageModel` that replays `steps`, one provider stream per
 * model call. Returns the concrete `MockLanguageModelV4` (not the widened
 * `LanguageModel` union) so callers can inspect `.doStreamCalls` — the exact
 * `tools`/`prompt` the caller handed the model for that step — to assert on
 * the ASSEMBLED tool set/instructions rather than only on side effects.
 *
 * @knipkeep deliberate published test seam (`package.json` exports); playground
 * + unit tests drive ToolLoopAgent without tokens. Knip --production ignores
 * *.test.ts consumers, so the file looks unused without this keep.
 */
export function mockModel(
  steps: MockStep[],
  opts: { delayMs?: number; provider?: string } = {},
): MockLanguageModelV4 {
  return new MockLanguageModelV4({
    // `provider` is the SDK provider string the model reports. Nothing may
    // decide on it (an Anthropic-format model on another host reports
    // "anthropic.messages" too); tests pass it to prove nothing does.
    ...(opts.provider ? { provider: opts.provider } : {}),
    doStream: steps.map((s, i) => streamForStep(s, i, opts.delayMs)),
  });
}
