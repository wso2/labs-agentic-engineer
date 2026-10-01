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
 * `toTurnUsage` sends the UNCACHED input as `inputTokens`, the shape the coding
 * runner sends, so aep-api prices each cached token once (from its own field).
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { LanguageModelUsage } from "ai";
import { toTurnUsage } from "../src/conversation/manifest.js";

function usage(input: number, details: LanguageModelUsage["inputTokenDetails"]): LanguageModelUsage {
  return {
    inputTokens: input,
    inputTokenDetails: details,
    outputTokens: 50,
    outputTokenDetails: { textTokens: 50, reasoningTokens: 0 },
    totalTokens: input + 50,
  } as LanguageModelUsage;
}

test("toTurnUsage: inputTokens excludes cache reads and writes", () => {
  const u = toTurnUsage(usage(1300, { noCacheTokens: 100, cacheReadTokens: 1000, cacheWriteTokens: 200 }), "m");
  assert.deepEqual(u, { inputTokens: 100, outputTokens: 50, cacheReadTokens: 1000, cacheCreationTokens: 200, model: "m" });
});

test("toTurnUsage: without a noCache count, the uncached input is derived from the total", () => {
  const u = toTurnUsage(usage(1300, { noCacheTokens: undefined, cacheReadTokens: 1000, cacheWriteTokens: 200 }), "m");
  assert.equal(u.inputTokens, 100);
});

test("toTurnUsage: a provider reporting no cache sends its whole input", () => {
  const u = toTurnUsage(usage(40, { noCacheTokens: undefined, cacheReadTokens: undefined, cacheWriteTokens: undefined }), "m");
  assert.deepEqual(u, { inputTokens: 40, outputTokens: 50, cacheReadTokens: 0, cacheCreationTokens: 0, model: "m" });
});
