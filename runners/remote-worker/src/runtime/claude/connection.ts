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

// The model connection as Claude Code reads it: which `ANTHROPIC_*` variables
// the CLI's environment carries, so it calls the connection's endpoint with the
// connection's credential. Pure, so the whole mapping is a table test.
//
//   on Anthropic's own API — the environment the run was given: the
//     subscription as CLAUDE_CODE_OAUTH_TOKEN, else the key as
//     ANTHROPIC_API_KEY (from AEP_MODEL_API_KEY when the dispatch named it so);
//   anywhere else — ANTHROPIC_BASE_URL is the saved URL minus a trailing `/v1`
//     (the CLI appends `/v1/messages` itself, where the AI SDK and OpenCode
//     take the `/v1` base), the key is ANTHROPIC_AUTH_TOKEN for Bearer or
//     ANTHROPIC_API_KEY for x-api-key, and CLAUDE_CODE_MAX_CONTEXT_TOKENS is the
//     window, which the CLI otherwise assumes to be 200k for a model it does not
//     know (measured).
//
// Off Anthropic's API every OTHER Anthropic credential is removed: Claude Code
// ranks ANTHROPIC_API_KEY above ANTHROPIC_AUTH_TOKEN, so a stray Anthropic key
// in the pod's (or a developer's) environment would be the one presented — to
// another host. The key never follows the host.

import { connectionKey, onAnthropicAPI, type ModelConnection } from "../../lib/model_connection.js";
import { UnsupportedRuntimeError } from "../port.js";

/** Every variable a Claude Code session could present a model credential under. */
const CREDENTIAL_VARS = ["ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "AEP_MODEL_API_KEY"] as const;

/** The CLI's endpoint and limit variables, which only a connection may set. */
const ENDPOINT_VARS = ["ANTHROPIC_BASE_URL", "CLAUDE_CODE_MAX_CONTEXT_TOKENS"] as const;

/**
 * Claude Code's root URL for a saved base URL: the URL minus a trailing `/v1`.
 * The org enters one URL, ending where the SDKs expect it; this CLI wants the
 * root (Alibaba's docs give `…/apps/anthropic` for Claude Code and
 * `…/apps/anthropic/v1` for the SDKs).
 */
export function claudeCodeBaseURL(baseURL: string): string {
  return baseURL.replace(/\/+$/, "").replace(/\/v1$/, "");
}

/**
 * The session's environment for a connection, from the run's.
 *
 * Throws, before any spawn, for a connection this runtime cannot run: the
 * OpenAI-compatible format (the CLI speaks only Anthropic's Messages API), and
 * a Claude subscription off Anthropic's API (the token authenticates nowhere
 * else, and the dispatcher never pairs them).
 */
export function claudeCodeEnv(connection: ModelConnection, env: Record<string, string>): Record<string, string> {
  if (connection.format !== "anthropic") {
    throw new UnsupportedRuntimeError(
      "claude-code",
      `Claude Code speaks only the Anthropic format, and this connection is ${connection.format} (${connection.host})`,
    );
  }
  const out = { ...env };
  const key = connectionKey(env);

  if (onAnthropicAPI(connection)) {
    // A key the dispatch named AEP_MODEL_API_KEY is presented under the name the
    // CLI reads; nothing else moves.
    if (out.AEP_MODEL_API_KEY !== undefined) {
      delete out.AEP_MODEL_API_KEY;
      if (key !== undefined && out.CLAUDE_CODE_OAUTH_TOKEN === undefined) out.ANTHROPIC_API_KEY = key;
    }
    return out;
  }

  if ((env.CLAUDE_CODE_OAUTH_TOKEN ?? "") !== "") {
    throw new UnsupportedRuntimeError(
      "claude-code",
      `a Claude subscription authenticates only against Anthropic's API, and this connection is ${connection.host}`,
    );
  }
  for (const name of [...CREDENTIAL_VARS, ...ENDPOINT_VARS]) delete out[name];
  out.ANTHROPIC_BASE_URL = claudeCodeBaseURL(connection.baseURL);
  if (key !== undefined) {
    out[connection.authScheme === "bearer" ? "ANTHROPIC_AUTH_TOKEN" : "ANTHROPIC_API_KEY"] = key;
  }
  if (connection.contextWindow !== undefined) {
    out.CLAUDE_CODE_MAX_CONTEXT_TOKENS = String(connection.contextWindow);
  }
  return out;
}
