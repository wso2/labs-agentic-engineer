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
 * The pod's model connection (07 §4): `AE_MODEL_CONNECTION` (rendered by
 * aep-api's `A/organization/aestudio/desired.go` as
 * `json.Marshal(modelConnectionEnv{*agentsvc.TurnConnection, Model})`) plus
 * the `ANTHROPIC_API_KEY` secret. Without a key the pod still starts, and a
 * turn answers `no_default_key`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { connectionFromEnv, ConnectionEnvError } from "../src/shared/connection-env.js";

// Byte for byte what desired.go renders (generated from modelConnectionEnv
// with agentsvc.ConnectionFor): Anthropic's own API, whose limits the runtime
// knows (no contextWindow/outputLimit), and an OpenAI-compatible host.
const ANTHROPIC_ENV =
  '{"format":"anthropic","baseURL":"https://api.anthropic.com/v1","authScheme":"x-api-key","capabilities":{"claudeCode":true,"claudeSubscription":true,"promptCache":true,"generatedAgents":true,"nativePdf":true,"webSearch":"anthropic-server-tool","imageInput":"yes"},"model":"claude-sonnet-4-6"}';
const OLLAMA_ENV =
  '{"format":"openai-compatible","baseURL":"https://ollama.com/v1","authScheme":"bearer","contextWindow":131072,"outputLimit":8192,"capabilities":{"claudeCode":false,"claudeSubscription":false,"promptCache":false,"generatedAgents":true,"nativePdf":false,"webSearch":"ollama-api","imageInput":"unknown"},"model":"gpt-oss:120b"}';

const KEY = "test-key-not-a-secret";

test("AE_MODEL_CONNECTION + ANTHROPIC_API_KEY → the connection, key and model", () => {
  assert.deepEqual(connectionFromEnv({ AE_MODEL_CONNECTION: ANTHROPIC_ENV, ANTHROPIC_API_KEY: KEY }), {
    format: "anthropic",
    baseURL: "https://api.anthropic.com/v1",
    authScheme: "x-api-key",
    capabilities: {
      claudeCode: true,
      claudeSubscription: true,
      promptCache: true,
      generatedAgents: true,
      nativePdf: true,
      webSearch: "anthropic-server-tool",
      imageInput: "yes",
    },
    apiKey: KEY,
    model: "claude-sonnet-4-6",
  });
});

test("an OpenAI-compatible connection keeps its limits", () => {
  const conn = connectionFromEnv({ AE_MODEL_CONNECTION: OLLAMA_ENV, ANTHROPIC_API_KEY: KEY });
  assert.ok(conn);
  assert.equal(conn.format, "openai-compatible");
  assert.equal(conn.authScheme, "bearer");
  assert.equal(conn.contextWindow, 131072);
  assert.equal(conn.outputLimit, 8192);
  assert.equal(conn.capabilities.webSearch, "ollama-api");
  assert.equal(conn.capabilities.imageInput, "unknown");
  assert.equal(conn.model, "gpt-oss:120b");
  assert.equal(conn.apiKey, KEY);
});

test("no ANTHROPIC_API_KEY → null (no_default_key), whatever the connection", () => {
  assert.equal(connectionFromEnv({ AE_MODEL_CONNECTION: ANTHROPIC_ENV }), null);
  assert.equal(connectionFromEnv({ AE_MODEL_CONNECTION: ANTHROPIC_ENV, ANTHROPIC_API_KEY: "" }), null);
  assert.equal(connectionFromEnv({ AE_MODEL_CONNECTION: ANTHROPIC_ENV, ANTHROPIC_API_KEY: "  " }), null);
});

test("no AE_MODEL_CONNECTION → null: there is no platform fallback connection", () => {
  assert.equal(connectionFromEnv({ ANTHROPIC_API_KEY: KEY }), null);
  assert.equal(connectionFromEnv({ ANTHROPIC_API_KEY: KEY, AE_MODEL_CONNECTION: "" }), null);
  assert.equal(connectionFromEnv({}), null);
});

test("a malformed AE_MODEL_CONNECTION is an error that never quotes the value", () => {
  const bad = [
    "{not json",
    "[]",
    '"x"',
    ANTHROPIC_ENV.replace('"model":"claude-sonnet-4-6"', '"model":""'),
    ANTHROPIC_ENV.replace(',"model":"claude-sonnet-4-6"', ""),
    ANTHROPIC_ENV.replace('"model":"claude-sonnet-4-6"', '"model":"has space"'),
    ANTHROPIC_ENV.replace("https://api.anthropic.com/v1", "http://api.anthropic.com/v1"),
    ANTHROPIC_ENV.replace('"format":"anthropic"', '"format":"other"'),
    ANTHROPIC_ENV.replace('"imageInput":"yes"', '"imageInput":""'),
  ];
  for (const raw of bad) {
    assert.throws(
      () => connectionFromEnv({ AE_MODEL_CONNECTION: raw, ANTHROPIC_API_KEY: KEY }),
      (err: unknown) => err instanceof ConnectionEnvError && !err.message.includes(raw) && !err.message.includes(KEY),
      raw,
    );
  }
});

test("fields beside the known ones do not ride along", () => {
  const raw = ANTHROPIC_ENV.replace('"format":', '"extra":"x","format":');
  const conn = connectionFromEnv({ AE_MODEL_CONNECTION: raw, ANTHROPIC_API_KEY: KEY });
  assert.ok(conn);
  assert.equal("extra" in conn, false);
});
