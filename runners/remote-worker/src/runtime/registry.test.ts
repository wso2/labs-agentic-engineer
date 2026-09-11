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
import { DEFAULT_RUNTIME, UnsupportedRuntimeError } from "./port.js";
import { createRuntime, modelFromEnv, runtimeNameFromEnv } from "./registry.js";

test("createRuntime: the default is Claude Code, and it is what an unset env resolves to", () => {
  assert.equal(DEFAULT_RUNTIME, "claude-code");
  assert.equal(createRuntime().name, "claude-code");
  assert.equal(runtimeNameFromEnv({}), "claude-code");
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "" }), "claude-code");
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "  " }), "claude-code");
});

// The org setting's contract carries `opencode` because the design does, and
// this build ships no adapter for it. The failure has to be LOUD: a silent
// fallback would bill an org for a runtime it did not choose, and the org would
// never learn it had not got the one it picked.
test("createRuntime: OpenCode is refused by name, with the reason", () => {
  assert.throws(
    () => createRuntime("opencode"),
    (err: unknown) => {
      assert.ok(err instanceof UnsupportedRuntimeError);
      assert.equal(err.runtime, "opencode");
      assert.match(err.message, /no OpenCode adapter/);
      // The three spikes the design owes before an adapter is worth merging.
      assert.match(err.message, /tool\/permission parity/);
      assert.match(err.message, /declares an agent's id, depth and parent/);
      assert.match(err.message, /usage is reported for cost stamping/);
      return true;
    },
  );
});

test("runtimeNameFromEnv: a name the platform has never heard of is an error, not a default", () => {
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "claude-code" }), "claude-code");
  // Recognised, but still unbuildable — the registry is where that is decided,
  // so the two failures stay distinguishable.
  assert.equal(runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "opencode" }), "opencode");
  assert.throws(() => runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "cursor" }), UnsupportedRuntimeError);
  // The underscore spelling the glossary table used before the port. One
  // spelling now, across the contract, the env and the lookup key.
  assert.throws(() => runtimeNameFromEnv({ AEP_AGENT_RUNTIME: "claude_code" }), UnsupportedRuntimeError);
});

test("modelFromEnv: the org's setting wins, and an absent one changes nothing", () => {
  assert.equal(modelFromEnv("claude-sonnet-5", {}), "claude-sonnet-5");
  assert.equal(modelFromEnv("claude-sonnet-5", { AEP_AGENT_MODEL: "" }), "claude-sonnet-5");
  assert.equal(modelFromEnv("claude-sonnet-5", { AEP_AGENT_MODEL: "  " }), "claude-sonnet-5");
  assert.equal(modelFromEnv("claude-sonnet-5", { AEP_AGENT_MODEL: "claude-haiku-4-5" }), "claude-haiku-4-5");
  // Trimmed, because a stamped env var picks up whitespace from a YAML block
  // scalar and a model id with a trailing space resolves to nothing.
  assert.equal(modelFromEnv("claude-sonnet-5", { AEP_AGENT_MODEL: " claude-haiku-4-5 " }), "claude-haiku-4-5");
});
