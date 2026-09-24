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
import { DEFAULT_RUNTIME, runtimeNameFromEnv, UnsupportedRuntimeError } from "./port.js";
import { createRuntime, envOr } from "./registry.js";

test("createRuntime: the default is Claude Code, and it is what an unset env resolves to", () => {
  assert.equal(DEFAULT_RUNTIME, "claude-code");
  assert.equal(createRuntime().name, "claude-code");
  assert.equal(runtimeNameFromEnv({}), "claude-code");
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "" }), "claude-code");
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "  " }), "claude-code");
});

// The second adapter. Building it is cheap and starts nothing — `start` is where
// a server is spawned — so the registry can be asserted without a binary.
test("createRuntime: OpenCode builds its own adapter, with the platform's default model", () => {
  const runtime = createRuntime("opencode");
  assert.equal(runtime.name, "opencode");
  assert.equal(runtime.defaultModel, "claude-sonnet-5");
  assert.match(runtime.toolGlossary(), /## Tool glossary \(OpenCode\)/);
});


test("runtimeNameFromEnv: a name the platform has never heard of is an error, not a default", () => {
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "claude-code" }), "claude-code");
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "opencode" }), "opencode");
  assert.throws(() => runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "cursor" }), UnsupportedRuntimeError);
  // The underscore spelling the glossary table used before the port. One
  // spelling now, across the contract, the env and the lookup key.
  assert.throws(() => runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "claude_code" }), UnsupportedRuntimeError);
});

test("envOr(AEP_AGENT_MODEL): the org's setting wins, and an absent one changes nothing", () => {
  assert.equal(envOr("AEP_AGENT_MODEL", "claude-sonnet-5", {}), "claude-sonnet-5");
  assert.equal(envOr("AEP_AGENT_MODEL", "claude-sonnet-5", { AEP_AGENT_MODEL: "" }), "claude-sonnet-5");
  assert.equal(envOr("AEP_AGENT_MODEL", "claude-sonnet-5", { AEP_AGENT_MODEL: "  " }), "claude-sonnet-5");
  assert.equal(envOr("AEP_AGENT_MODEL", "claude-sonnet-5", { AEP_AGENT_MODEL: "claude-haiku-4-5" }), "claude-haiku-4-5");
  // Trimmed, because a stamped env var picks up whitespace from a YAML block
  // scalar and a model id with a trailing space resolves to nothing.
  assert.equal(envOr("AEP_AGENT_MODEL", "claude-sonnet-5", { AEP_AGENT_MODEL: " claude-haiku-4-5 " }), "claude-haiku-4-5");
});

