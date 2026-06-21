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
 * Headless architect runner — the agent's orchestration with NO transport
 * coupling. The HTTP route injects an SSE-backed sink; the eval harness
 * injects a collecting sink. Both drive identical model behaviour, which is
 * what makes the architect self-contained and evaluable in isolation.
 */

import { streamText, stepCountIs, type LanguageModel } from "ai";
import { DesignDoc } from "./doc.js";
import { buildTools, type SseSink, type FinalizeResolver } from "./tools.js";
import { systemPrompt, buildUserPrompt } from "./prompt.js";
import { validate, type ValidationIssue } from "./validator.js";
import type { ArchitectInput, ArchitectOutput } from "./schema.js";

export interface ArchitectRunResult {
  /** True iff the model called finalize() and the validator passed. */
  finalized: boolean;
  /** The materialized design (authoritative, read from the DesignDoc). */
  design: ArchitectOutput;
  /** Validator issues against the final doc — empty when finalized. */
  issues: ValidationIssue[];
  usage: { inputTokens: number; outputTokens: number };
}

export interface ArchitectRunOpts {
  model: LanguageModel;
  input: ArchitectInput;
  /** Where tools push progress events (HTTP SSE in prod, collector in evals). */
  sink: SseSink;
  /** finalize() flips `finalized` and calls resolve() to tear the loop down. */
  finalizer: FinalizeResolver;
  /** Aborted by the route on client disconnect, and by finalize() on success. */
  abortSignal: AbortSignal;
}

/**
 * Drive the architect agent to completion. Mutations land in a DesignDoc via
 * tool calls; the design is read back with `materialize()` regardless of how
 * the run ended, so callers can inspect partial output on a non-finalized run.
 */
export async function runArchitect(
  opts: ArchitectRunOpts,
): Promise<ArchitectRunResult> {
  const { model, input, sink, finalizer, abortSignal } = opts;

  const doc = DesignDoc.fromPrevious(input.previousDesign);
  const tools = buildTools(doc, sink, finalizer, input.wireframes ?? {});

  let usage = { inputTokens: 0, outputTokens: 0 };

  const result = streamText({
    model,
    system: systemPrompt,
    prompt: buildUserPrompt(input, doc),
    tools,
    // 64-step runaway-loop guard. finalize() is the real terminator.
    stopWhen: stepCountIs(64),
    abortSignal,
    onError: ({ error }) => {
      console.error("[architect] streamText error:", error);
    },
    onFinish: (ev) => {
      usage = {
        inputTokens: ev.usage?.inputTokens ?? 0,
        outputTokens: ev.usage?.outputTokens ?? 0,
      };
      console.log(
        `[architect] finish=${ev.finishReason} steps=${ev.steps?.length ?? 0} in=${usage.inputTokens} out=${usage.outputTokens}`,
      );
    },
  });

  try {
    // Drive the loop. Tools emit progress via the sink as side effects; the
    // text chunks themselves have no UI surface.
    for await (const _chunk of result.textStream) {
      if (sink.isClosed()) break;
    }
  } catch (err) {
    // finalize() aborts the controller to tear the loop down cleanly — that
    // abort is expected, not an error. Anything else propagates.
    const expectedAbort =
      abortSignal.aborted &&
      finalizer.finalized &&
      (err instanceof Error
        ? err.name === "AbortError" || /aborted/i.test(err.message)
        : false);
    if (!expectedAbort) throw err;
  }

  return {
    finalized: finalizer.finalized,
    design: doc.materialize(),
    issues: validate(doc),
    usage,
  };
}
