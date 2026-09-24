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
 * `modelProviderOptions` is model-aware: `@ai-sdk/anthropic` forwards `effort`
 * as `output_config.effort` without checking the model, and Haiku 4.5 /
 * Sonnet 4.5 reject it, so their turns carry no provider options at all.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { config } from "../src/shared/config.js";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";
import { isOfferedModel, modelProviderOptions, OFFERED_MODELS, resolveModelId, supportsEffort } from "../src/shared/model.js";

test("Sonnet 5 turns carry the configured reasoning effort", () => {
  assert.deepEqual(modelProviderOptions("claude-sonnet-5"), { anthropic: { effort: config.reasoningEffort } });
});

test("Haiku 4.5 and Sonnet 4.5 turns carry no effort option", () => {
  assert.equal(modelProviderOptions("claude-haiku-4-5"), undefined);
  assert.equal(modelProviderOptions("claude-haiku-4-5-20251001"), undefined);
  assert.equal(supportsEffort("claude-sonnet-4-5"), false);
  assert.equal(supportsEffort("claude-opus-5"), true);
});

test("resolveModelId: the turn's model wins; none falls back to AGENT_MODEL", () => {
  assert.equal(resolveModelId({ model: "claude-haiku-4-5" }), "claude-haiku-4-5");
  assert.equal(resolveModelId(), config.model);
});

test("OFFERED_MODELS is the contract's AgentModel enum", () => {
  const spec = parse(
    readFileSync(fileURLToPath(new URL("../../../packages/contracts/api/v1/openapi.yaml", import.meta.url)), "utf8"),
  ) as { components: { schemas: { AgentModel: { enum: string[] } } } };
  assert.deepEqual([...OFFERED_MODELS].sort(), [...spec.components.schemas.AgentModel.enum].sort());
  assert.equal(isOfferedModel("claude-opus-5"), false);
});
