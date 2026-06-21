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
 * Headless tech-lead PLAN runner (phase 1). Same orchestration the route used
 * inline — the streamObject seal-rule (emit element i once i is no longer the
 * trailing element being typed) — but with the SSE coupling lifted out behind
 * an `onSealed` callback. The route passes a callback that writes SSE frames;
 * the eval passes none and just reads the returned items + validator issues.
 *
 * The phase-2 (detail) fan-out is prose and stays in the route for now.
 */

import { streamObject, type LanguageModel } from "ai";
import {
  PlanArraySchema,
  PlanItemSchema,
  type TechLeadPlanInput,
  type PlanIssue,
} from "./schema.js";
import { planSystemPrompt, buildPlanUserPrompt } from "./prompt.js";
import {
  validatePlan,
  type DiffContext,
  type PlanItemWithTempId,
} from "./validator.js";

/** Thrown when a sealed plan element fails PlanItemSchema. Carries the wire
 * fields the route emits as a `malformed-plan-item` error frame. */
export class MalformedPlanItemError extends Error {
  constructor(
    public readonly index: number,
    public readonly issues: unknown,
  ) {
    super(`malformed plan item at index ${index}`);
    this.name = "MalformedPlanItemError";
  }
}

export interface TechLeadPlanRunResult {
  items: PlanItemWithTempId[];
  issues: PlanIssue[];
  usage: { inputTokens: number; outputTokens: number };
}

export interface TechLeadPlanRunOpts {
  model: LanguageModel;
  input: TechLeadPlanInput;
  /** Pre-computed diff context for incremental coverage rules (BFF supplies). */
  diff?: DiffContext;
  /** Invoked as each plan element seals — route → SSE. Omit in evals. */
  onSealed?: (item: PlanItemWithTempId) => void;
  /** True once the client is gone; stops sealing early. */
  isClosed?: () => boolean;
  abortSignal?: AbortSignal;
}

export async function runTechLeadPlan(
  opts: TechLeadPlanRunOpts,
): Promise<TechLeadPlanRunResult> {
  const { model, input, diff, onSealed, isClosed, abortSignal } = opts;

  const result = streamObject({
    model,
    system: planSystemPrompt,
    prompt: buildPlanUserPrompt(input),
    schema: PlanArraySchema,
    abortSignal,
    onError: ({ error }) => {
      console.error("[tech-lead/plan] streamObject error:", error);
    },
  });

  const sealed: PlanItemWithTempId[] = [];
  let sealedThrough = -1;

  const sealOne = (i: number, raw: unknown): void => {
    const parsedItem = PlanItemSchema.safeParse(raw);
    if (!parsedItem.success) {
      throw new MalformedPlanItemError(i, parsedItem.error.format());
    }
    const item: PlanItemWithTempId = { tempId: `p-${i}`, ...parsedItem.data };
    sealed.push(item);
    sealedThrough = i;
    onSealed?.(item);
  };

  // Seal-rule: emit element i when the partial array length ≥ i+2 (so element
  // i is no longer the trailing one still being typed).
  for await (const partial of result.partialObjectStream) {
    if (isClosed?.()) break;
    if (!Array.isArray(partial)) continue;
    const sealedTo = partial.length - 2;
    for (let i = sealedThrough + 1; i <= sealedTo; i++) sealOne(i, partial[i]);
  }

  // Stream ended — flush the trailing element(s).
  const final = await result.object;
  for (let i = sealedThrough + 1; i < final.length; i++) sealOne(i, final[i]);

  const issues = validatePlan({
    items: sealed,
    design: input.slimDesign,
    existingTasks: input.existingTasks ?? [],
    mode: input.mode,
    diff,
  });

  const u = await result.usage;
  return {
    items: sealed,
    issues,
    usage: { inputTokens: u.inputTokens ?? 0, outputTokens: u.outputTokens ?? 0 },
  };
}
