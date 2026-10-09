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

import { test } from "node:test";
import assert from "node:assert/strict";
import type { StreamPart } from "@aep/agent-stream";
import { collectPart, newTurnRecord } from "../src/tracing.js";

test("a turn's usage is the sum of its steps' usage (finish-step parts)", () => {
  const rec = newTurnRecord("requirements", 1, "/start an idea");
  const parts = [
    { type: "finish-step", usage: { inputTokens: 100, outputTokens: 20 } },
    { type: "text-delta", text: "hi" },
    { type: "finish-step", usage: { inputTokens: 150, outputTokens: 5 } },
    { type: "finish-step" },
  ] as unknown as StreamPart[];
  for (const part of parts) collectPart(rec, part);
  assert.deepEqual(rec.usage, { inputTokens: 250, outputTokens: 25 });
  assert.equal(rec.agentText, "hi");
});

test("a turn with no step usage has none", () => {
  const rec = newTurnRecord("design", 1, "/design");
  collectPart(rec, { type: "text-delta", text: "x" } as unknown as StreamPart);
  assert.equal(rec.usage, undefined);
});
