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
 * runTurn — the generic, file-agnostic turn loop. Model + tools + instructions +
 * messages in; events, appended messages, usage, finishReason out. It imports
 * `ai` only, knows nothing file-specific, and WRITES NOTHING. Every consumer
 * (the SSE route, the eval) drives it identically — one waist, one stream shape.
 *
 * Tools run SERVER-SIDE (they keep `execute()`), so the model's one-step
 * self-correction (NOT_UNIQUE / NOT_FOUND / INVALID_YAML) stays inside one
 * `agent.stream()` call. The canonical bundle is already mutated when the stream
 * finishes; consumers read `bundle.snapshot()` (or reconstruct from the stream),
 * they never re-apply.
 */

import {
  ToolLoopAgent,
  isStepCount,
  type Instructions,
  type StopCondition,
  type FilePart,
  type ModelMessage,
  type LanguageModel,
  type LanguageModelUsage,
  type TelemetryOptions,
  type ToolLoopAgentSettings,
  type ToolSet,
  type UserContent,
} from "ai";
import type { StreamPart } from "@aep/agent-stream";

/** Provider-specific per-call options (`ai` doesn't export the type directly). */
export type ProviderOptions = NonNullable<ToolLoopAgentSettings["providerOptions"]>;

export interface RunTurnInput {
  model: LanguageModel;
  instructions: string;
  tools: ToolSet;
  /** The growing conversation. MUTATED IN PLACE: the user turn + the response are appended. */
  messages: ModelMessage[];
  /** This turn's user instruction text (pushed as a `user` message before streaming). */
  prompt: string;
  /**
   * Native AI SDK file parts (currently: `.pdf` reference documents, #384) to
   * ride the SAME user message as `prompt`, so Anthropic reads them as document
   * blocks instead of the model having to pull one through a tool as raw text.
   * Absent/empty → the message content stays the plain `prompt` string,
   * byte-identical to a turn without this feature.
   */
  fileParts?: FilePart[];
  /**
   * Stop conditions; defaults to `[isStepCount(maxSteps ?? 20)]`. Stays generic
   * — the main wiring passes `[isStepCount(n), hasToolCall('ask_question'),
   * hasToolCall('ask_questions')]` without runTurn ever knowing a tool name.
   */
  stopWhen?: StopCondition<ToolSet>[];
  onEvent?: (part: StreamPart) => void;
  abortSignal?: AbortSignal;
  maxSteps?: number;
  /**
   * Per-step output-token ceiling. Left unset the provider applies a low default
   * (~4096) that truncates a large file mid-`addFile`, so the tool call never
   * closes and nothing folds. Pass a generous value for spec/design generations.
   */
  maxOutputTokens?: number;
  /**
   * Provider-specific call options (e.g. Anthropic reasoning effort), built by
   * the provider-aware model seam and passed through opaquely — runTurn stays
   * provider-agnostic.
   */
  providerOptions?: ProviderOptions;
  /**
   * Provider-specific prompt-cache breakpoint, built by the model seam and
   * applied opaquely — runTurn never names a provider. Absent → nothing is
   * marked and the request is byte-identical to an uncached one.
   *
   * Applied to the system block and to the LAST message of this turn's prompt,
   * which together cover the whole stable prefix (tools + system + history).
   * The marker never reaches `input.messages`: the stored history must stay
   * clean, or every turn would leave another breakpoint behind and a long
   * conversation would blow the provider's per-request limit.
   */
  cacheBreakpoint?: ProviderOptions;
  /**
   * Trace-capture options for this turn (the `functionId` its steps are stamped
   * with), built by the telemetry seam and passed through opaquely — runTurn
   * knows nothing about which tool is capturing. Absent → the call is
   * byte-identical to an untraced one.
   */
  telemetry?: TelemetryOptions;
}

export interface RunTurnResult {
  finishReason: string;
  usage: LanguageModelUsage;
}

/**
 * The instructions as a system message carrying `breakpoint`, or the plain
 * string when there is none. `Instructions` accepts a `SystemModelMessage`,
 * which is the only place a provider option can ride on the system block.
 */
function withCacheBreakpoint(
  instructions: string,
  breakpoint: ProviderOptions | undefined,
): Instructions {
  if (!breakpoint) return instructions;
  return { role: "system", content: instructions, providerOptions: breakpoint };
}

/**
 * `messages` with `breakpoint` on its last entry — as a COPY. The caller's
 * array is the conversation that gets persisted and it must not carry cache
 * markers, so the last message is cloned rather than mutated. Everything before
 * it is shared by reference: nothing downstream mutates message objects, and
 * copying a long history every step would be the expensive part.
 */
function markLastMessage(
  messages: ModelMessage[],
  breakpoint: ProviderOptions | undefined,
): ModelMessage[] {
  if (!breakpoint || messages.length === 0) return messages;
  const last = messages[messages.length - 1]!;
  return [...messages.slice(0, -1), { ...last, providerOptions: breakpoint }];
}

/**
 * A ROLLING cache breakpoint: before each step, move the marker onto the newest
 * message so the step just completed becomes part of the cached prefix.
 *
 * Marking only the turn prompt (`markLastMessage`) freezes the cache boundary
 * where the turn STARTED. Every assistant + tool-result message a tool loop
 * appends lands after it and is therefore re-prefilled, uncached, on every
 * remaining step — the tail grows all turn, so the waste is quadratic in steps.
 * Measured on a 20-step design generation: 750K uncached input tokens against
 * 445K cache reads, the per-step uncached figure climbing 27K → 50K while the
 * cache read stayed pinned at 22.5K. A single 70KB `loadSkill` result from step 1
 * was re-sent, uncached, nineteen times.
 *
 * The turn-prompt marker STAYS (`pinIndex`): it is the boundary the NEXT turn of
 * this conversation reads from, and it is stable because history is append-only.
 * The rolling marker is additionally cheap to move — Anthropic bills a cache
 * write only for blocks not already cached, which here is the one step just
 * added.
 *
 * Marker accounting matters, and it is where the published AI SDK recipe for
 * this (cookbook: "Dynamic Prompt Caching") goes wrong. That version maps over
 * the messages adding a marker to the last one and leaves every earlier marker
 * in place. Because `prepareStep`'s `messages` override carries forward as the
 * base for later steps, the markers ACCUMULATE — one per step — and the provider
 * keeps only the first handful and silently drops the rest, so on a long loop
 * the cache boundary stops advancing partway through and the recipe quietly
 * stops working. So the previous rolling marker comes OFF as the new one goes
 * on. The original message object is kept and restored rather than having its
 * options edited, which is what lets this stay provider-agnostic: the loop never
 * looks inside `breakpoint`.
 *
 * Rolling also keeps the marker within the provider's backward lookback for
 * finding a cached prefix (~20 content blocks). One step of parallel tool calls
 * is already 2N blocks — eight batched edits is sixteen — so a stationary marker
 * can fall out of range in two steps, missing the cache even on content that IS
 * cached.
 */
function rollingCacheBreakpoint(
  pinIndex: number,
  breakpoint: ProviderOptions,
): (messages: ModelMessage[]) => ModelMessage[] {
  let marked: { index: number; original: ModelMessage } | undefined;
  return (messages) => {
    const lastIndex = messages.length - 1;
    // Nothing appended since the turn prompt (step 1): its marker is the tail.
    if (lastIndex <= pinIndex) return messages;
    const next = [...messages];
    // Take the previous rolling marker off, restoring the untouched message.
    if (marked && marked.index !== pinIndex && marked.index < next.length) {
      next[marked.index] = marked.original;
    }
    const original = next[lastIndex]!;
    marked = { index: lastIndex, original };
    // Merge rather than replace: a response message may already carry provider
    // options of its own, and the marker must not be the reason they vanish.
    next[lastIndex] = {
      ...original,
      providerOptions: { ...original.providerOptions, ...breakpoint },
    };
    return next;
  };
}

export async function runTurn(input: RunTurnInput): Promise<RunTurnResult> {
  // Single source of conversation truth: push this turn, then stream({ messages })
  // ONLY (prompt + messages are mutually exclusive in v7). fileParts turns the
  // content into an array ONLY when there is at least one — an idle feature
  // must never change the shape of a turn that carries no attachments.
  const content: UserContent =
    input.fileParts && input.fileParts.length > 0
      ? [{ type: "text", text: input.prompt }, ...input.fileParts]
      : input.prompt;
  input.messages.push({ role: "user", content });

  // The turn prompt's index — the breakpoint the NEXT turn reads from, and the
  // one the rolling marker must leave alone. Captured before streaming, since
  // `input.messages` is appended to afterwards.
  const pinIndex = input.messages.length - 1;
  const roll = input.cacheBreakpoint
    ? rollingCacheBreakpoint(pinIndex, input.cacheBreakpoint)
    : undefined;

  const agent = new ToolLoopAgent({
    model: input.model,
    instructions: withCacheBreakpoint(input.instructions, input.cacheBreakpoint),
    tools: input.tools,
    stopWhen: input.stopWhen ?? [isStepCount(input.maxSteps ?? 20)],
    ...(input.maxOutputTokens ? { maxOutputTokens: input.maxOutputTokens } : {}),
    ...(input.providerOptions ? { providerOptions: input.providerOptions } : {}),
    // Absent when caching is off, so the request is byte-identical to one made
    // before any of this existed.
    ...(roll ? { prepareStep: ({ messages }) => ({ messages: roll(messages) }) } : {}),
  });

  const result = await agent.stream({
    messages: markLastMessage(input.messages, input.cacheBreakpoint),
    ...(input.telemetry ? { telemetry: input.telemetry } : {}),
    ...(input.abortSignal ? { abortSignal: input.abortSignal } : {}),
  });

  // v7: read result.stream (NOT fullStream). The async iterator yields the
  // server TextStreamPart; text-delta carries `.text`, tool-input-delta `.delta`.
  for await (const part of result.stream as AsyncIterable<StreamPart>) {
    input.onEvent?.(part);
  }

  // v7: appended messages come from result.responseMessages (accumulated across
  // ALL steps), not (await result.response).messages (last-step-only). The caller
  // reads the appended turn from its own `messages` array (mutated in place above).
  const appended = await result.responseMessages;
  input.messages.push(...appended);

  return {
    finishReason: await result.finishReason,
    usage: await result.usage, // v7: already the whole-turn sum
  };
}
