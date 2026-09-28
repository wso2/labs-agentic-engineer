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

// THE MODEL CONNECTION as a coding run reads it: which endpoint the run's one
// model is served from, in which wire format, and what the platform resolved
// about it. The organization's connection reaches the pod as `AEP_MODEL_*` env,
// copied onto the Job at dispatch (ADR-0028) like the runtime and the model, so
// a run in flight keeps the connection it was launched with.
//
// This is the one place those variables are read. Each runtime adapter maps the
// connection to its own spelling (Claude Code's `ANTHROPIC_*` env, OpenCode's
// provider block), so the port carries the connection and no runtime's
// vocabulary.
//
// The CREDENTIAL is not part of it. The dispatch mounts exactly one model
// credential as a secret ref, and which variable a runtime presents it under is
// the adapter's business; it reaches the session through `RuntimePolicy.env`.
//
// Absent variables mean Anthropic's own API, the key sent as `x-api-key`, and no
// limits stated, because on `api.anthropic.com` both runtimes already know
// Claude's. A variable that IS set but names nothing this build knows is an
// error, never a default: running a connection the org did not choose is the
// same silent substitution the runtime registry refuses.

/** The wire format a connection speaks. */
export type ModelFormat = "anthropic" | "openai-compatible";

/** How the connection key is presented: Anthropic's own header, or `Authorization: Bearer`. */
export type ModelAuthScheme = "x-api-key" | "bearer";

/**
 * How a run on this connection searches the web: Anthropic's server-side tool
 * (only `api.anthropic.com` runs it), Ollama's search API, or not at all. The
 * platform decides it once, from the connection's capabilities, and the runner
 * only follows it.
 */
export type WebSearchStrategy = "anthropic-server-tool" | "ollama-api" | "none";

const FORMATS: readonly ModelFormat[] = ["anthropic", "openai-compatible"];
const AUTH_SCHEMES: readonly ModelAuthScheme[] = ["x-api-key", "bearer"];
const WEB_SEARCH_STRATEGIES: readonly WebSearchStrategy[] = ["anthropic-server-tool", "ollama-api", "none"];

/** Anthropic's own API: the host features are bound to, and the connection a run gets when the dispatch stated none. */
const ANTHROPIC_HOST = "api.anthropic.com";
const DEFAULT_BASE_URL = `https://${ANTHROPIC_HOST}/v1`;

/**
 * The context window a connection off Anthropic's API runs with when the
 * dispatch stated none: the platform's own fallback when a provider publishes
 * no figure (aep-api resolves it at save), applied here for a connection named
 * by hand — the playground's. Without a window, OpenCode turns auto-compaction
 * off and Claude Code assumes 200k for a model it does not know, and either
 * ends a long open-model run at its provider's real limit.
 */
export const FALLBACK_CONTEXT_WINDOW = 128_000;

/** A coding run's model connection. */
export interface ModelConnection {
  format: ModelFormat;
  /**
   * The URL as the organization saved it, ending in `/v1` where the format's
   * SDKs expect it. A runtime that wants the root (Claude Code's
   * `ANTHROPIC_BASE_URL`) derives it; the org enters one URL.
   */
  baseURL: string;
  /** `baseURL`'s host: what "on Anthropic's own API" is decided by. */
  host: string;
  authScheme: ModelAuthScheme;
  /**
   * The ONE model of the run (lead, subagents, helper calls), from
   * `AEP_AGENT_MODEL`.
   *
   * Pinned rather than left to the runtime's default, which drifts across
   * releases (seen live: an unpinned run resolved to `claude-sonnet-4-6`).
   */
  model: string;
  /**
   * Resolved at save; absent on `api.anthropic.com`, where the runtimes know
   * Claude's, and always present anywhere else (`FALLBACK_CONTEXT_WINDOW`).
   */
  contextWindow?: number;
  /** Resolved at save; absent on `api.anthropic.com`. */
  outputLimit?: number;
  webSearch: WebSearchStrategy;
}

/**
 * Thrown when a dispatched `AEP_MODEL_*` value names nothing this build can run.
 *
 * `shown` is what the message may echo: this pod's output reaches the
 * user-visible build log, so a URL is never echoed whole (it could carry
 * userinfo), only its host when it has one.
 */
export class InvalidModelConnectionError extends Error {
  readonly variable: string;
  constructor(variable: string, shown: string, reason: string) {
    super(`${variable}=${JSON.stringify(shown)} is not a model connection this runner can read: ${reason}`);
    this.name = "InvalidModelConnectionError";
    this.variable = variable;
  }
}

/**
 * The run's model connection, from the dispatch's env.
 *
 * `defaultModel` is the runtime's own default (`Runtime.defaultModel`), used when
 * the organization has chosen no model: what a runtime should default to is the
 * runtime's fact, not this module's.
 *
 * Every value is trimmed and a blank one counts as absent, because a stamped env
 * var picks up whitespace from a YAML block scalar, and a model id with a
 * trailing space resolves to nothing.
 */
export function readModelConnection(defaultModel: string, env: NodeJS.ProcessEnv = process.env): ModelConnection {
  const read = (key: string): string => (env[key] ?? "").trim();

  const baseURL = read("AEP_MODEL_BASE_URL") || DEFAULT_BASE_URL;
  const format = oneOf("AEP_MODEL_FORMAT", read("AEP_MODEL_FORMAT"), FORMATS, "anthropic");
  const host = hostOf(baseURL);
  const firstParty = format === "anthropic" && host === ANTHROPIC_HOST;
  const contextWindow =
    positiveInt("AEP_MODEL_CONTEXT_WINDOW", read("AEP_MODEL_CONTEXT_WINDOW")) ??
    (firstParty ? undefined : FALLBACK_CONTEXT_WINDOW);
  const outputLimit = positiveInt("AEP_MODEL_OUTPUT_LIMIT", read("AEP_MODEL_OUTPUT_LIMIT"));
  return {
    format,
    baseURL,
    host,
    authScheme: oneOf("AEP_MODEL_AUTH_SCHEME", read("AEP_MODEL_AUTH_SCHEME"), AUTH_SCHEMES, "x-api-key"),
    model: read("AEP_AGENT_MODEL") || defaultModel,
    ...(contextWindow !== undefined ? { contextWindow } : {}),
    ...(outputLimit !== undefined ? { outputLimit } : {}),
    webSearch: oneOf("AEP_MODEL_WEB_SEARCH", read("AEP_MODEL_WEB_SEARCH"), WEB_SEARCH_STRATEGIES, "anthropic-server-tool"),
  };
}

/**
 * Whether the connection is Anthropic's own API — the Anthropic format on
 * `api.anthropic.com`. The Anthropic format alone is not: Ollama serves it too,
 * and a key or a feature bound to Anthropic must not follow the format there.
 */
export function onAnthropicAPI(conn: Pick<ModelConnection, "format" | "host">): boolean {
  return conn.format === "anthropic" && conn.host === ANTHROPIC_HOST;
}

/**
 * The connection key's value, from the variable the dispatch mounted it under:
 * `AEP_MODEL_API_KEY`, or `ANTHROPIC_API_KEY` — the name a key on Anthropic's
 * own API keeps. Undefined when neither holds one (a Claude subscription,
 * or no credential at all). Each adapter presents it under its own spelling.
 */
export function connectionKey(env: Readonly<Record<string, string | undefined>>): string | undefined {
  const key = (env.AEP_MODEL_API_KEY ?? "").trim() || (env.ANTHROPIC_API_KEY ?? "").trim();
  return key === "" ? undefined : key;
}

function oneOf<T extends string>(variable: string, raw: string, allowed: readonly T[], fallback: T): T {
  if (raw === "") return fallback;
  const found = allowed.find((v) => v === raw);
  if (found) return found;
  throw new InvalidModelConnectionError(variable, raw, `expected one of ${allowed.join(", ")}`);
}

function positiveInt(variable: string, raw: string): number | undefined {
  if (raw === "") return undefined;
  if (!/^[1-9][0-9]*$/.test(raw)) throw new InvalidModelConnectionError(variable, raw, "expected a positive integer");
  return Number(raw);
}

function hostOf(baseURL: string): string {
  let url: URL;
  try {
    url = new URL(baseURL);
  } catch {
    throw new InvalidModelConnectionError("AEP_MODEL_BASE_URL", "", "not a URL");
  }
  if (url.protocol !== "https:") {
    throw new InvalidModelConnectionError("AEP_MODEL_BASE_URL", url.hostname, "not an https URL");
  }
  return url.hostname;
}
