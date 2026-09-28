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

// THE ENV MAPPING, runtime × format × host × credential kind: what each
// adapter makes of the one connection and the one credential a dispatch
// mounts. Claude Code's half is the session's env (`claude/connection.ts`);
// OpenCode's is the server's env plus its provider block (`opencode/runtime.ts`,
// `opencode/config.ts`). Both halves read the same `AEP_MODEL_*` values through
// `readModelConnection`, exactly as a pod does.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readModelConnection } from "../lib/model_connection.js";
import { claudeCodeEnv } from "./claude/connection.js";
import { providerConfig } from "./opencode/config.js";
import { childEnvironment } from "./opencode/runtime.js";
import { UnsupportedRuntimeError } from "./port.js";

const KEY = "conn-key-0123456789abcdef";
const TOKEN = "sk-ant-oat01-subscription-token";

/** The connections a dispatch can stamp (the Go side is model_env.go). */
const CONNECTIONS = {
  "anthropic@api.anthropic.com": {},
  // Saved with its /v1 like every Anthropic-format URL (the SDKs append
  // `/messages` to it); Claude Code gets the root.
  "anthropic@ollama.com": {
    AEP_MODEL_FORMAT: "anthropic",
    AEP_MODEL_BASE_URL: "https://ollama.com/v1",
    AEP_MODEL_AUTH_SCHEME: "bearer",
    AEP_MODEL_CONTEXT_WINDOW: "131072",
    AEP_MODEL_OUTPUT_LIMIT: "32768",
    AEP_MODEL_WEB_SEARCH: "ollama-api",
  },
  "anthropic@llm.example.com (x-api-key)": {
    AEP_MODEL_FORMAT: "anthropic",
    AEP_MODEL_BASE_URL: "https://llm.example.com/anthropic/v1",
    AEP_MODEL_AUTH_SCHEME: "x-api-key",
    AEP_MODEL_CONTEXT_WINDOW: "200000",
    AEP_MODEL_WEB_SEARCH: "none",
  },
  "openai-compatible@ollama.com": {
    AEP_MODEL_FORMAT: "openai-compatible",
    AEP_MODEL_BASE_URL: "https://ollama.com/v1",
    AEP_MODEL_AUTH_SCHEME: "bearer",
    AEP_MODEL_CONTEXT_WINDOW: "131072",
    AEP_MODEL_OUTPUT_LIMIT: "32768",
    AEP_MODEL_WEB_SEARCH: "ollama-api",
  },
} as const;
type ConnectionName = keyof typeof CONNECTIONS;

/** The credential as each kind of dispatch mounts it. */
const CREDENTIALS = {
  // The key under Claude Code's own name, as a first-party dispatch mounts it.
  "key as ANTHROPIC_API_KEY": { ANTHROPIC_API_KEY: KEY },
  "key as AEP_MODEL_API_KEY": { AEP_MODEL_API_KEY: KEY },
  "Claude subscription": { CLAUDE_CODE_OAUTH_TOKEN: TOKEN },
} as const;
type CredentialName = keyof typeof CREDENTIALS;

function connection(name: ConnectionName) {
  return readModelConnection("claude-sonnet-5", { AEP_AGENT_MODEL: "gpt-oss:20b", ...CONNECTIONS[name] });
}

/** The credential and endpoint variables of an env, and nothing else. */
function modelVars(env: Record<string, string>): Record<string, string> {
  const names = [
    "ANTHROPIC_API_KEY",
    "ANTHROPIC_AUTH_TOKEN",
    "CLAUDE_CODE_OAUTH_TOKEN",
    "AEP_MODEL_API_KEY",
    "ANTHROPIC_BASE_URL",
    "CLAUDE_CODE_MAX_CONTEXT_TOKENS",
  ];
  return Object.fromEntries(names.filter((n) => env[n] !== undefined).map((n) => [n, env[n] as string]));
}

type Expected = Record<string, string> | "refused";

const CLAUDE_CODE: Record<ConnectionName, Record<CredentialName, Expected>> = {
  "anthropic@api.anthropic.com": {
    "key as ANTHROPIC_API_KEY": { ANTHROPIC_API_KEY: KEY },
    "key as AEP_MODEL_API_KEY": { ANTHROPIC_API_KEY: KEY },
    "Claude subscription": { CLAUDE_CODE_OAUTH_TOKEN: TOKEN },
  },
  "anthropic@ollama.com": {
    "key as ANTHROPIC_API_KEY": {
      ANTHROPIC_AUTH_TOKEN: KEY,
      ANTHROPIC_BASE_URL: "https://ollama.com",
      CLAUDE_CODE_MAX_CONTEXT_TOKENS: "131072",
    },
    "key as AEP_MODEL_API_KEY": {
      ANTHROPIC_AUTH_TOKEN: KEY,
      ANTHROPIC_BASE_URL: "https://ollama.com",
      CLAUDE_CODE_MAX_CONTEXT_TOKENS: "131072",
    },
    "Claude subscription": "refused",
  },
  "anthropic@llm.example.com (x-api-key)": {
    "key as ANTHROPIC_API_KEY": {
      ANTHROPIC_API_KEY: KEY,
      ANTHROPIC_BASE_URL: "https://llm.example.com/anthropic",
      CLAUDE_CODE_MAX_CONTEXT_TOKENS: "200000",
    },
    "key as AEP_MODEL_API_KEY": {
      ANTHROPIC_API_KEY: KEY,
      ANTHROPIC_BASE_URL: "https://llm.example.com/anthropic",
      CLAUDE_CODE_MAX_CONTEXT_TOKENS: "200000",
    },
    "Claude subscription": "refused",
  },
  "openai-compatible@ollama.com": {
    "key as ANTHROPIC_API_KEY": "refused",
    "key as AEP_MODEL_API_KEY": "refused",
    "Claude subscription": "refused",
  },
};

for (const [conn, row] of Object.entries(CLAUDE_CODE) as [ConnectionName, Record<CredentialName, Expected>][]) {
  for (const [cred, want] of Object.entries(row) as [CredentialName, Expected][]) {
    test(`claude-code · ${conn} · ${cred}`, () => {
      const run = () => claudeCodeEnv(connection(conn), { PATH: "/bin", ...CREDENTIALS[cred] });
      if (want === "refused") {
        assert.throws(run, UnsupportedRuntimeError);
        return;
      }
      const env = run();
      assert.deepEqual(modelVars(env), want);
      assert.equal(env.PATH, "/bin", "the rest of the env is the run's");
    });
  }
}

// The key never follows the host: a developer's (or a stray pod) Anthropic key
// beside the connection's must not be what Claude Code presents to ollama.com,
// and a stray base URL must not redirect it.
test("claude-code · anthropic@ollama.com · a stray Anthropic key and base URL are dropped", () => {
  const env = claudeCodeEnv(connection("anthropic@ollama.com"), {
    AEP_MODEL_API_KEY: KEY,
    ANTHROPIC_API_KEY: "sk-ant-api03-developers-own-key",
    ANTHROPIC_BASE_URL: "https://proxy.example.com",
  });
  assert.deepEqual(modelVars(env), {
    ANTHROPIC_AUTH_TOKEN: KEY,
    ANTHROPIC_BASE_URL: "https://ollama.com",
    CLAUDE_CODE_MAX_CONTEXT_TOKENS: "131072",
  });
});

const OPENCODE: Record<ConnectionName, { provider: string; npm?: string; credential?: "apiKey" | "authToken" }> = {
  "anthropic@api.anthropic.com": { provider: "anthropic" },
  "anthropic@ollama.com": { provider: "aep", npm: "@ai-sdk/anthropic", credential: "authToken" },
  "anthropic@llm.example.com (x-api-key)": { provider: "aep", npm: "@ai-sdk/anthropic", credential: "apiKey" },
  "openai-compatible@ollama.com": { provider: "aep", npm: "@ai-sdk/openai-compatible", credential: "apiKey" },
};

const RUN_FILES = { secrets: "/t/s", ready: "/t/r", instructions: "/t/i", probe: "/t/p" };

for (const [conn, want] of Object.entries(OPENCODE) as [ConnectionName, (typeof OPENCODE)[ConnectionName]][]) {
  for (const cred of ["key as ANTHROPIC_API_KEY", "key as AEP_MODEL_API_KEY"] as const) {
    test(`opencode · ${conn} · ${cred}`, () => {
      const c = connection(conn);
      const env = childEnvironment({ workspace: "/w", env: { ...CREDENTIALS[cred] }, logDir: "/w/.logs" }, RUN_FILES);
      // One credential, under the name the config references.
      assert.deepEqual(modelVars(env), { AEP_MODEL_API_KEY: KEY });
      const block = providerConfig(c)[want.provider] as { npm?: string; options: Record<string, unknown> };
      assert.ok(block, `provider ${want.provider}`);
      assert.equal(block.npm, want.npm);
      if (want.credential) {
        assert.equal(block.options[want.credential], "{env:AEP_MODEL_API_KEY}");
        // The saved URL as is: `@ai-sdk/anthropic` appends `/messages` to it, so
        // a root URL here answers 405 (measured on Ollama).
        assert.equal(block.options.baseURL, c.baseURL);
      } else assert.deepEqual(block.options, { apiKey: "{env:AEP_MODEL_API_KEY}" });
    });
  }
}
