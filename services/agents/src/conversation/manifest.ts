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
 * The terminal manifest part (shared-volume-clone-architecture D14): the
 * producer half of the fold-parity gate. Emitted ONCE per successful turn (any
 * request shape), before `[DONE]`; a turn that throws emits none, so a severed
 * stream is unambiguously "do not commit" to the aep-api fold.
 */

import type { LanguageModelUsage } from "ai";
import type { FileBundle, ManifestPart, TurnUsage } from "@aep/agent-stream";
import { sha256Hex } from "../shared/hash.js";

/**
 * Project the AI SDK's whole-turn `LanguageModelUsage` onto the pinned
 * cross-runtime wire shape (#249): cache reads/writes come from
 * `inputTokenDetails`, absent counts collapse to 0 so every field is a
 * required number, and `model` is the resolved id the turn ran on (threaded
 * from the composition root — the SDK usage object does not carry it).
 */
export function toTurnUsage(usage: LanguageModelUsage, model: string): TurnUsage {
  return {
    inputTokens: usage.inputTokens ?? 0,
    outputTokens: usage.outputTokens ?? 0,
    cacheReadTokens: usage.inputTokenDetails?.cacheReadTokens ?? 0,
    cacheCreationTokens: usage.inputTokenDetails?.cacheWriteTokens ?? 0,
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
 */
export function buildManifestPart(bundle?: FileBundle, usage?: TurnUsage): ManifestPart {
  const files: Record<string, string> = {};
  const deleted: string[] = [];
  if (bundle) {
    for (const path of [...bundle.touched()].sort()) {
      const content = bundle.read(path);
      if (content === undefined) deleted.push(path);
      else files[path] = sha256Hex(content);
    }
  }
  return { type: "manifest", files, deleted, ...(usage ? { usage } : {}) };
}
