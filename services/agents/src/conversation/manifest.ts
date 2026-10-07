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
 * The terminal manifest part (shared-workspace-volume D14): the
 * producer half of the fold-parity gate. Emitted ONCE per successful turn (any
 * request shape), before `[DONE]`; a turn that throws emits none, so a severed
 * stream is unambiguously "do not commit" to the aep-api fold.
 */

import type { LanguageModelUsage, ModelMessage } from "ai";
import { OUTCOME_MAX_CHARS, type FileBundle, type ManifestPart, type TurnUsage } from "@aep/agent-stream";
import { sha256Hex } from "../shared/hash.js";

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

/**
 * Build the manifest from the turn's bundle. Covers ONLY paths mutated THIS
 * turn (`bundle.touched()` — set on APPLIED ops only, so noop/already-applied/
 * rejected ops never appear): still-present paths map to the sha256 of their
 * final (LF-canonical) content, vanished paths land in `deleted`. Paths are
 * sorted for a deterministic wire encoding (cassette/golden friendly). No
 * bundle (chat-only or task-plan turn) → the empty manifest. `usage` (#249)
 * rides the manifest because it is the one frame every successful turn emits;
 * a failed turn emits no manifest and therefore reports no usage (v1).
 * `outcome` (an Issues turn's, see `turnOutcome`) rides it for the same reason.
 */
export function buildManifestPart(bundle?: FileBundle, usage?: TurnUsage, outcome?: string): ManifestPart {
  const files: Record<string, string> = {};
  const deleted: string[] = [];
  if (bundle) {
    for (const path of [...bundle.touched()].sort()) {
      const content = bundle.read(path);
      if (content === undefined) deleted.push(path);
      else files[path] = sha256Hex(content);
    }
  }
  return { type: "manifest", files, deleted, ...(usage ? { usage } : {}), ...(outcome ? { outcome } : {}) };
}

/**
 * What a turn came to (`ManifestPart.outcome`): the last text part of the
 * assistant messages it appended, trimmed, and cut to `OUTCOME_MAX_CHARS`
 * ending in `…` when longer. A turn that never replied in text has none.
 */
export function turnOutcome(messages: readonly ModelMessage[]): string | undefined {
  const texts = messages.flatMap((m) => {
    if (m.role !== "assistant") return [];
    return typeof m.content === "string" ? [m.content] : m.content.flatMap((p) => (p.type === "text" ? [p.text] : []));
  });
  const last = texts.map((t) => t.trim()).filter((t) => t !== "").pop();
  if (last === undefined) return undefined;
  return last.length > OUTCOME_MAX_CHARS ? `${last.slice(0, OUTCOME_MAX_CHARS - 1)}…` : last;
}
