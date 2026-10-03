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
 * A local run's engineering-agent connection (`kit/model-connection.ts`): the
 * `AEP_MODEL_*` variables become the in-process design agent's connection,
 * and with none set it is Anthropic's own API. The capabilities mirror aep-api's
 * `modelconn.CapabilitiesOf`; the table is `modelconn_test.go`'s.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { anthropicConnection } from "@aep/ae-design-agent/shared/model";
import { capabilitiesOf, playgroundModel } from "../src/kit/model-connection.js";

test("capabilitiesOf mirrors modelconn.CapabilitiesOf's table", () => {
  // What every connection off Anthropic's own API shares: generated agents run
  // on any format, the first-party features on none.
  const base = { claudeCode: false, claudeSubscription: false, promptCache: false, generatedAgents: true, nativePdf: false };
  assert.deepEqual(capabilitiesOf("anthropic", "api.anthropic.com"), {
    claudeCode: true,
    claudeSubscription: true,
    promptCache: true,
    generatedAgents: true,
    nativePdf: true,
    webSearch: "anthropic-server-tool",
    imageInput: "yes",
  });
  assert.deepEqual(capabilitiesOf("anthropic", "ollama.com"), {
    ...base,
    claudeCode: true,
    promptCache: true,
    webSearch: "ollama-api",
    imageInput: "unknown",
  });
  assert.deepEqual(capabilitiesOf("openai-compatible", "ollama.com"), { ...base, webSearch: "ollama-api", imageInput: "unknown" });
  assert.deepEqual(capabilitiesOf("openai-compatible", "openrouter.ai"), { ...base, webSearch: "none", imageInput: "unknown" });
  assert.deepEqual(capabilitiesOf("openai-compatible", "api.anthropic.com"), { ...base, webSearch: "none", imageInput: "unknown" });
});

test("AEP_MODEL_* names the connection, its key and its model", () => {
  const got = playgroundModel({
    AEP_MODEL_FORMAT: "openai-compatible",
    AEP_MODEL_BASE_URL: "https://ollama.com/v1",
    AEP_MODEL_AUTH_SCHEME: "bearer",
    AEP_MODEL_API_KEY: "ollama-test-key-0000000000",
    AEP_MODEL_CONTEXT_WINDOW: "131072",
    AEP_AGENT_MODEL: "gpt-oss:20b",
  });
  assert.deepEqual(got, {
    format: "openai-compatible",
    baseURL: "https://ollama.com/v1",
    authScheme: "bearer",
    contextWindow: 131072,
    capabilities: capabilitiesOf("openai-compatible", "ollama.com"),
    apiKey: "ollama-test-key-0000000000",
    model: "gpt-oss:20b",
  });
});

test("a connection named without its key is refused", () => {
  assert.throws(() => playgroundModel({ AEP_MODEL_BASE_URL: "https://ollama.com/v1" }), /AEP_MODEL_API_KEY is not set/);
});

test("no AEP_MODEL_* names no connection: Anthropic's own API on ANTHROPIC_API_KEY", () => {
  assert.deepEqual(playgroundModel({ ANTHROPIC_API_KEY: "sk-ant-test" }), anthropicConnection("sk-ant-test"));
  assert.throws(() => playgroundModel({}), /ANTHROPIC_API_KEY is not set/);
});
