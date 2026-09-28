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
import {
  FALLBACK_CONTEXT_WINDOW,
  InvalidModelConnectionError,
  connectionKey,
  onAnthropicAPI,
  readModelConnection,
} from "./model_connection.js";

test("readModelConnection: no AEP_MODEL_* is Anthropic's own API on the runtime's default model", () => {
  assert.deepEqual(readModelConnection("claude-sonnet-5", {}), {
    format: "anthropic",
    baseURL: "https://api.anthropic.com/v1",
    host: "api.anthropic.com",
    authScheme: "x-api-key",
    model: "claude-sonnet-5",
    webSearch: "anthropic-server-tool",
  });
});

test("readModelConnection: the dispatched connection, when every variable is set", () => {
  const env = {
    AEP_AGENT_MODEL: "gpt-oss:20b",
    AEP_MODEL_FORMAT: "openai-compatible",
    AEP_MODEL_BASE_URL: "https://ollama.com/v1",
    AEP_MODEL_AUTH_SCHEME: "bearer",
    AEP_MODEL_CONTEXT_WINDOW: "131072",
    AEP_MODEL_OUTPUT_LIMIT: "32768",
    AEP_MODEL_WEB_SEARCH: "ollama-api",
  };
  assert.deepEqual(readModelConnection("claude-sonnet-5", env), {
    format: "openai-compatible",
    baseURL: "https://ollama.com/v1",
    host: "ollama.com",
    authScheme: "bearer",
    model: "gpt-oss:20b",
    contextWindow: 131072,
    outputLimit: 32768,
    webSearch: "ollama-api",
  });
});

// A stamped env var picks up whitespace from a YAML block scalar, and a
// dispatcher may send "" for an unset setting: neither may pin anything to
// nothing.
test("readModelConnection: values are trimmed, and a blank one is the same as none", () => {
  const blank = { AEP_AGENT_MODEL: "  ", AEP_MODEL_FORMAT: "", AEP_MODEL_BASE_URL: " ", AEP_MODEL_CONTEXT_WINDOW: "" };
  assert.deepEqual(readModelConnection("claude-sonnet-5", blank), readModelConnection("claude-sonnet-5", {}));
  const padded = readModelConnection("claude-sonnet-5", {
    AEP_AGENT_MODEL: " claude-haiku-4-5 ",
    AEP_MODEL_FORMAT: " anthropic ",
  });
  assert.equal(padded.model, "claude-haiku-4-5");
  assert.equal(padded.format, "anthropic");
});

// Running a connection the org did not choose is the silent substitution the
// runtime registry refuses too.
test("readModelConnection: a value this build cannot read is an error, never a default", () => {
  const refused: Record<string, string>[] = [
    { AEP_MODEL_FORMAT: "gemini" },
    { AEP_MODEL_AUTH_SCHEME: "basic" },
    { AEP_MODEL_WEB_SEARCH: "bing" },
    { AEP_MODEL_CONTEXT_WINDOW: "128k" },
    { AEP_MODEL_OUTPUT_LIMIT: "0" },
    { AEP_MODEL_BASE_URL: "ollama.com/v1" },
    { AEP_MODEL_BASE_URL: "http://ollama.com/v1" },
  ];
  for (const env of refused) {
    assert.throws(() => readModelConnection("claude-sonnet-5", env), InvalidModelConnectionError, JSON.stringify(env));
  }
});

// The refusal reaches the user-visible build log; a URL's userinfo must not.
test("readModelConnection: a refused base URL is named by its host, never echoed whole", () => {
  assert.throws(
    () => readModelConnection("claude-sonnet-5", { AEP_MODEL_BASE_URL: "http://user:hunter2@ollama.com/v1" }),
    (err: Error) => err.message.includes('"ollama.com"') && !err.message.includes("hunter2"),
  );
});

// A connection named without a window (the playground's) still states one off
// Anthropic's API, so neither runtime runs an open model with no compaction.
test("readModelConnection: off Anthropic's API a missing context window is the platform's fallback", () => {
  const ollama = { AEP_MODEL_FORMAT: "openai-compatible", AEP_MODEL_BASE_URL: "https://ollama.com/v1" };
  assert.equal(readModelConnection("m", ollama).contextWindow, FALLBACK_CONTEXT_WINDOW);
  assert.equal(readModelConnection("m", { ...ollama, AEP_MODEL_CONTEXT_WINDOW: "40000" }).contextWindow, 40000);
  // Anthropic's format on another host is not Anthropic's API.
  assert.equal(
    readModelConnection("m", { AEP_MODEL_FORMAT: "anthropic", AEP_MODEL_BASE_URL: "https://ollama.com" }).contextWindow,
    FALLBACK_CONTEXT_WINDOW,
  );
  assert.equal(readModelConnection("m", {}).contextWindow, undefined, "on Anthropic's API the runtimes know Claude's");
});

test("onAnthropicAPI: the Anthropic format on api.anthropic.com, and nothing else", () => {
  assert.equal(onAnthropicAPI({ format: "anthropic", host: "api.anthropic.com" }), true);
  assert.equal(onAnthropicAPI({ format: "anthropic", host: "ollama.com" }), false);
  assert.equal(onAnthropicAPI({ format: "openai-compatible", host: "api.anthropic.com" }), false);
});

// A key on Anthropic's own API is mounted as ANTHROPIC_API_KEY (ADR-0038 §6).
test("connectionKey: AEP_MODEL_API_KEY, else the ANTHROPIC_API_KEY an older dispatch mounts", () => {
  assert.equal(connectionKey({ ANTHROPIC_API_KEY: "sk-ant-api03-old-dispatch" }), "sk-ant-api03-old-dispatch");
  assert.equal(connectionKey({ AEP_MODEL_API_KEY: "ollama-key-0123456789" }), "ollama-key-0123456789");
  assert.equal(connectionKey({ AEP_MODEL_API_KEY: " ", CLAUDE_CODE_OAUTH_TOKEN: "sk-ant-oat01-token" }), undefined);
});
