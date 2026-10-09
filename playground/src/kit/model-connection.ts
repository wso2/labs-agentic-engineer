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
 * The engineering agent's model connection in a local run. In production the
 * pod reads the organization's connection from `AE_MODEL_CONNECTION`; here the
 * developer states it with the same `AEP_MODEL_*` variables a coding run reads
 * (`remote-worker/src/lib/model_connection.ts` parses them), so one set of
 * variables points both agents at one connection, as an organization's one
 * connection does:
 *
 *   AEP_MODEL_FORMAT=openai-compatible AEP_MODEL_BASE_URL=https://ollama.com/v1 \
 *   AEP_MODEL_AUTH_SCHEME=bearer AEP_MODEL_API_KEY=… AEP_AGENT_MODEL=gpt-oss:20b pnpm play …
 *
 * With neither `AEP_MODEL_FORMAT` nor `AEP_MODEL_BASE_URL` set, turns run on
 * Anthropic's own API with `ANTHROPIC_API_KEY`.
 */

import type { ModelCapabilities, TurnConnection } from "@aep/agent-stream";
import { loadDotenv } from "@aep/ae-design-agent/shared/env";
import { config } from "@aep/ae-design-agent/shared/config";
import { anthropicConnection, connectionFromWire, type ModelConnection } from "@aep/ae-design-agent/shared/model";
import { readModelConnection, type ModelConnection as RunnerConnection } from "remote-worker/src/lib/model_connection.js";

/** Hosts whose features are bound to them, as `modelconn` names them. */
const ANTHROPIC_HOST = "api.anthropic.com";
const OLLAMA_HOST = "ollama.com";

/**
 * What a connection supports: a mirror of aep-api's `modelconn.CapabilitiesOf`
 * (pinned against its table by `test/model-connection.test.ts`), because a
 * local run has no aep-api to compute it. The image capability is a probe
 * result in production; here nothing probes, so it is `unknown` off
 * Anthropic's own API.
 */
export function capabilitiesOf(format: TurnConnection["format"], host: string): ModelCapabilities {
  const anthropic = format === "anthropic";
  const firstParty = anthropic && host === ANTHROPIC_HOST;
  return {
    claudeCode: anthropic,
    claudeSubscription: firstParty,
    promptCache: anthropic,
    generatedAgents: true,
    nativePdf: firstParty,
    webSearch: firstParty ? "anthropic-server-tool" : host === OLLAMA_HOST ? "ollama-api" : "none",
    imageInput: firstParty ? "yes" : "unknown",
  };
}

/** The session's model connection, from the environment (and `deployments/.env`). */
export function playgroundModel(env: NodeJS.ProcessEnv = process.env): ModelConnection {
  loadDotenv();
  if (!(env.AEP_MODEL_FORMAT ?? "").trim() && !(env.AEP_MODEL_BASE_URL ?? "").trim()) {
    const anthropicKey = (env.ANTHROPIC_API_KEY ?? "").trim();
    if (anthropicKey === "") {
      throw new Error("ANTHROPIC_API_KEY is not set: export it or add it to deployments/.env, or name a connection with AEP_MODEL_*.");
    }
    return anthropicConnection(anthropicKey);
  }
  const conn: RunnerConnection = readModelConnection(config.model, env);
  const apiKey = (env.AEP_MODEL_API_KEY ?? "").trim();
  if (apiKey === "") throw new Error("AEP_MODEL_API_KEY is not set: a connection named by AEP_MODEL_* needs its key.");
  const wire: TurnConnection = {
    format: conn.format,
    baseURL: conn.baseURL,
    authScheme: conn.authScheme,
    ...(conn.contextWindow !== undefined ? { contextWindow: conn.contextWindow } : {}),
    ...(conn.outputLimit !== undefined ? { outputLimit: conn.outputLimit } : {}),
    capabilities: capabilitiesOf(conn.format, conn.host),
  };
  return connectionFromWire(wire, apiKey, conn.model);
}

/** The plain `AEP_MODEL_*` variables a coding run's connection rides on, as a dispatch stamps them. */
const CONNECTION_VARS = [
  "AEP_MODEL_FORMAT",
  "AEP_MODEL_BASE_URL",
  "AEP_MODEL_AUTH_SCHEME",
  "AEP_MODEL_CONTEXT_WINDOW",
  "AEP_MODEL_OUTPUT_LIMIT",
] as const;

/**
 * Every variable a coding run's connection arrives under, its key included: what
 * a run that names no connection must not carry, since the runner reads a bare
 * `AEP_MODEL_API_KEY` as a key for Anthropic's own API.
 */
export const CODING_CONNECTION_ENV = [...CONNECTION_VARS, "AEP_MODEL_WEB_SEARCH", "AEP_MODEL_API_KEY"] as const;

/**
 * The connection env a local CODING run is given, or undefined when no
 * `AEP_MODEL_*` names one (the run is then on Anthropic's own API).
 *
 * The developer's variables, plus the search strategy aep-api would stamp
 * (`capabilitiesOf`), unless one was set by hand: a dispatch always carries it,
 * and a run without it would take the runner's default — Anthropic's server
 * tool — on a host that cannot run it. The key is not in here: it is forwarded
 * by name, as `AEP_MODEL_API_KEY` (`engine/coding-run.ts`).
 */
export function codingConnectionEnv(env: NodeJS.ProcessEnv = process.env): Record<string, string> | undefined {
  if (!(env.AEP_MODEL_FORMAT ?? "").trim() && !(env.AEP_MODEL_BASE_URL ?? "").trim()) return undefined;
  const conn = readModelConnection(config.model, env);
  const out: Record<string, string> = {};
  for (const name of CONNECTION_VARS) {
    const value = (env[name] ?? "").trim();
    if (value !== "") out[name] = value;
  }
  out.AEP_MODEL_WEB_SEARCH = (env.AEP_MODEL_WEB_SEARCH ?? "").trim() || capabilitiesOf(conn.format, conn.host).webSearch;
  return out;
}
