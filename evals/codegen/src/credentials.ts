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
 * One credential for every session: the OAuth token in AEP_CODING_ANTHROPIC_KEY,
 * never an API key (ADR-0002). Claude Code ranks ANTHROPIC_API_KEY above all
 * other credentials, so every env here strips the competitors.
 */

import { readFileSync } from "node:fs";
import { parseEnv } from "node:util";
import { CONNECTION_ENV } from "./config.js";

/** The `.env` key the token lives under — the playground's own coding-credential name (ADR-0016). */
export const TOKEN_KEY = "AEP_CODING_ANTHROPIC_KEY";
/** What a `claude setup-token` OAuth token starts with; an API key is `sk-ant-api…`. */
export const OAUTH_PREFIX = "sk-ant-oat";

/** A refusal before anything is spent — the CLI maps it to exit 2. */
export class CredentialError extends Error {
  override name = "CredentialError";
}

/**
 * Read the token out of `envFile` without loading the file into this process.
 * Not `loadEnvFile`: that would put the file's ANTHROPIC_API_KEY into process.env.
 */
export function readOAuthToken(envFile: string): string {
  let text: string;
  try {
    text = readFileSync(envFile, "utf8");
  } catch {
    throw new CredentialError(`cannot read ${envFile} — the codegen evals take their one credential from it`);
  }
  return oauthTokenFrom(parseEnv(text));
}

/** The validation half of `readOAuthToken`, over an already-parsed file. */
export function oauthTokenFrom(parsed: Record<string, string | undefined>): string {
  const value = parsed[TOKEN_KEY]?.trim() ?? "";
  if (value === "") throw new CredentialError(`${TOKEN_KEY} is not set in deployments/.env`);
  if (!value.startsWith(OAUTH_PREFIX)) {
    // Says WHAT it is not, never what it is: the value stays out of every message.
    throw new CredentialError(
      `${TOKEN_KEY} is not a Claude OAuth token (expected the ${OAUTH_PREFIX}… prefix from \`claude setup-token\`). ` +
        "The codegen evals refuse to run on an API key.",
    );
  }
  return value;
}

/** A model connection for an `opencode` config: literal values plus the NAME of the key's variable. */
export interface ConnectionSpec {
  format: string;
  baseUrl: string;
  authScheme?: string | undefined;
  /** The env var holding the connection's key, read from `deployments/.env` (then the shell). */
  apiKeyEnv: string;
}

/** What one `configs.yaml` entry contributes to a coding run's environment. */
export interface RunEnvSpec {
  runtime: string;
  model: string;
  connection?: ConnectionSpec | undefined;
}

/**
 * The env of a `play` subprocess (`code`, `wire`). API key and connection vars
 * are blanked, not deleted: `play`'s loadEnvFile refills only absent vars. An
 * opencode config blanks the token instead.
 */
export function playEnv(
  parent: NodeJS.ProcessEnv,
  token: string,
  spec: RunEnvSpec,
  dotenv: Record<string, string | undefined> = {},
): NodeJS.ProcessEnv {
  const env = withoutClaudeSession(parent);
  delete env.ANTHROPIC_AUTH_TOKEN;
  env.ANTHROPIC_API_KEY = "";
  env.AEP_AGENT_RUNTIME = spec.runtime;
  env.AEP_AGENT_MODEL = spec.model;
  for (const name of CONNECTION_ENV) env[name] = "";
  if (!spec.connection) {
    env[TOKEN_KEY] = token;
    return env;
  }
  const key = (dotenv[spec.connection.apiKeyEnv] ?? parent[spec.connection.apiKeyEnv] ?? "").trim();
  if (key === "") {
    throw new CredentialError(
      `${spec.connection.apiKeyEnv} is not set (deployments/.env or the shell) — the ${spec.runtime} connection's key`,
    );
  }
  env[TOKEN_KEY] = "";
  env.AEP_MODEL_FORMAT = spec.connection.format;
  env.AEP_MODEL_BASE_URL = spec.connection.baseUrl;
  if (spec.connection.authScheme) env.AEP_MODEL_AUTH_SCHEME = spec.connection.authScheme;
  env.AEP_MODEL_API_KEY = key;
  return env;
}

/**
 * The env of the harness's own Agent SDK sessions (planner, walker, judge):
 * the parent's, minus every API-key form, plus the token where Claude Code
 * reads an OAuth token.
 */
export function sdkEnv(parent: NodeJS.ProcessEnv, token: string): NodeJS.ProcessEnv {
  const env = withoutClaudeSession(parent);
  delete env.ANTHROPIC_API_KEY;
  delete env.ANTHROPIC_AUTH_TOKEN;
  env.CLAUDE_CODE_OAUTH_TOKEN = token;
  return env;
}

/**
 * Drops a surrounding Claude Code session's vars, which would hand each child
 * its identity, socket and token.
 */
export function withoutClaudeSession(parent: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {};
  for (const [name, value] of Object.entries(parent)) {
    if (name === "CLAUDECODE" || name.startsWith("CLAUDE_CODE_")) continue;
    env[name] = value;
  }
  return env;
}

/**
 * `system/init`'s `apiKeySource` values that mean an API key. An OAuth token
 * reports `none` ("no API key in use"); these three are the CLI's names for a
 * key from the environment, from a helper command, or minted by a Console
 * `/login`.
 */
const API_KEY_SOURCES = new Set(["ANTHROPIC_API_KEY", "apiKeyHelper", "/login managed key"]);

/** Throws when a session authenticated with an API key — the attempt becomes a harness error. */
export function assertNotApiKey(apiKeySource: string | undefined): void {
  if (apiKeySource !== undefined && API_KEY_SOURCES.has(apiKeySource)) {
    throw new CredentialError(`an SDK session authenticated with an API key (apiKeySource=${apiKeySource}) — refused`);
  }
}
