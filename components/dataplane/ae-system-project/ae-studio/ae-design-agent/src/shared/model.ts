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
 * Model seam — the single provider-aware module. Everything that needs an LLM
 * goes through `createModel`, so the rest of the code consumes a
 * provider-agnostic `LanguageModel` and never imports a provider SDK directly.
 * A turn's model is described by a `ModelConnection` (format, base URL, auth
 * scheme, key, model id, limits, capabilities) and `createModel` switches on
 * its `format`, so a new format is one new branch here — no call-site changes.
 *
 * In the AE Studio pod the organization's one connection comes from the
 * pod env (`connection-env.ts`: `AE_MODEL_CONNECTION` and the
 * `ANTHROPIC_API_KEY` secret, both rendered by aep-api), and
 * `connectionFromWire` builds it from the wire shape plus that key. What a connection supports is aep-api's
 * `capabilities`, read here and never re-derived from the host.
 * `config.model` (`AGENT_MODEL`) is only the default for a caller that sends
 * no model. Every provider request goes out through `guardedFetch`,
 * which refuses a host that resolves to a non-public address.
 */

import type { LanguageModel } from "ai";
import { anthropic, createAnthropic, type AnthropicLanguageModelOptions } from "@ai-sdk/anthropic";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { ModelAuthScheme, ModelCapabilities, ModelFormat, TurnConnection } from "@aep/agent-stream";
import type { ProviderOptions } from "../agents/main/run-turn.js";
import { config } from "./config.js";
import { guardedFetch } from "./guarded-fetch.js";
import { watchProviderLimits, type ProviderLimitLog } from "./provider-limit.js";

export type { ModelAuthScheme, ModelCapabilities, ModelFormat };

/** The resolved connection a turn's model is built from: the wire's connection plus the key and the model. */
export interface ModelConnection extends TurnConnection {
  /** Provider API key. */
  apiKey: string;
  /** Model id. */
  model: string;
}

/** Anthropic's own API. */
const ANTHROPIC_BASE_URL = "https://api.anthropic.com/v1";

/**
 * What Anthropic's own API supports, as aep-api's `CapabilitiesOf` states it
 * for `anthropic@api.anthropic.com`. Used only for a turn that names no
 * connection, which is always that one.
 */
const ANTHROPIC_CAPABILITIES: ModelCapabilities = {
  claudeCode: true,
  claudeSubscription: true,
  promptCache: true,
  generatedAgents: true,
  nativePdf: true,
  webSearch: "anthropic-server-tool",
  imageInput: "yes",
};

/**
 * A connection's identity for history replay, `format@host`: stored parts a
 * provider can replay (signed reasoning, provider-executed tool calls) are
 * tied to the format and the host that produced them. Stamped on each turn's
 * journal entry and compared by `historyFor`.
 *
 * The model is not part of it (ADR-0038 §9).
 */
export function connectionFingerprint(conn: Pick<ModelConnection, "format" | "baseURL">): string {
  return `${conn.format}@${new URL(conn.baseURL).host}`;
}

/** The host a connection's requests go to, as its errors and log lines name it. */
export function connectionHost(conn: Pick<ModelConnection, "baseURL">): string {
  return new URL(conn.baseURL).host;
}

/**
 * The connection a turn that names none runs on: `model` on Anthropic's API
 * with the key as `x-api-key`. An absent model resolves to the service default
 * (`resolveModelId`).
 */
export function anthropicConnection(apiKey: string, model?: string): ModelConnection {
  return {
    format: "anthropic",
    baseURL: ANTHROPIC_BASE_URL,
    authScheme: "x-api-key",
    capabilities: ANTHROPIC_CAPABILITIES,
    apiKey,
    model: resolveModelId(model !== undefined ? { model } : {}),
  };
}

/**
 * A model id's shape: printable ASCII without spaces, as every host's ids are
 * (`claude-sonnet-5`, `gpt-oss:20b`, `vendor/model:tag`). Whether the host
 * serves it is the host's answer, not this service's.
 */
export function isModelId(model: string): boolean {
  return /^[\x21-\x7e]{1,200}$/.test(model);
}

/**
 * A connection from its wire shape (`TurnConnection`, validated by
 * `isTurnConnection`: `AE_MODEL_CONNECTION`, or the legacy turn body's
 * `connection`) with the key and the model. Rebuilt field by field, so
 * nothing beside the known fields rides along.
 */
export function connectionFromWire(wire: TurnConnection, apiKey: string, model: string): ModelConnection {
  return {
    format: wire.format,
    baseURL: wire.baseURL,
    authScheme: wire.authScheme,
    ...(wire.contextWindow !== undefined ? { contextWindow: wire.contextWindow } : {}),
    ...(wire.outputLimit !== undefined ? { outputLimit: wire.outputLimit } : {}),
    capabilities: {
      claudeCode: wire.capabilities.claudeCode,
      claudeSubscription: wire.capabilities.claudeSubscription,
      promptCache: wire.capabilities.promptCache,
      generatedAgents: wire.capabilities.generatedAgents,
      nativePdf: wire.capabilities.nativePdf,
      webSearch: wire.capabilities.webSearch,
      imageInput: wire.capabilities.imageInput,
    },
    apiKey,
    model,
  };
}

/**
 * The model id a turn runs on: `cfg.model`, else `AGENT_MODEL`. Exported so
 * the composition root can thread the SAME id it instantiates into the turn
 * (usage attribution on the terminal manifest, #249) instead of re-deriving the
 * default elsewhere.
 */
export function resolveModelId(cfg: { model?: string } = {}): string {
  return cfg.model ?? config.model;
}

/**
 * The OpenAI-compatible provider's name, which is also the key it reads its
 * per-call options under (`providerOptions.aep`).
 */
const OPENAI_COMPATIBLE_PROVIDER = "aep";

/** Per-call overrides of how `createModel` reaches the provider. */
interface CreateModelOptions {
  /**
   * The fetch provider requests go out through. Defaults to `guardedFetch`;
   * tests and cassette replays pass a recorder or reach a local server the
   * guard would refuse.
   */
  fetch?: typeof globalThis.fetch;
  /** The organization the turn runs for: named on its `model_provider_429` lines. */
  orgId?: string;
  /** Where `model_provider_429` lines go (the service log unless a test captures them). */
  providerLimitLog?: ProviderLimitLog;
  /** The clock the 429 budget counts on (tests). */
  now?: () => number;
  /** Told when a model call waits out a short 429 (`ProviderLimitWatch.onWait`). */
  onProviderWait?: (host: string) => void;
  /** Stable conversation ID for providers that route requests by session. */
  conversationId?: string;
}

/** OpenCode Go requires a session header on every model request in a conversation. */
function openCodeGoHeaders(conn: ModelConnection, conversationId: string | undefined): Record<string, string> | undefined {
  if (conn.format !== "openai-compatible") return undefined;
  const url = new URL(conn.baseURL);
  if (url.protocol !== "https:" || url.hostname !== "opencode.ai" || url.pathname.replace(/\/+$/, "") !== "/zen/go/v1") {
    return undefined;
  }
  if (!conversationId) throw new Error("OpenCode Go requires a conversation ID for x-opencode-session");
  return { "x-opencode-session": conversationId, "User-Agent": "aep-agents/1.0" };
}

/**
 * Build a Vercel AI SDK `LanguageModel` from a connection. This is the ONLY
 * function that knows which provider SDK to instantiate. Build one per turn:
 * the fetch it wraps carries the turn's 429 budget (`watchProviderLimits`).
 */
export function createModel(conn: ModelConnection, options: CreateModelOptions = {}): LanguageModel {
  const fetch = watchProviderLimits(options.fetch ?? guardedFetch, {
    apiKey: conn.apiKey,
    host: connectionHost(conn),
    format: conn.format,
    model: conn.model,
    ...(options.orgId ? { org: options.orgId } : {}),
    ...(options.providerLimitLog ? { log: options.providerLimitLog } : {}),
    ...(options.now ? { now: options.now } : {}),
    ...(options.onProviderWait ? { onWait: options.onProviderWait } : {}),
  });
  // Trace capture is NOT wrapped around the model: a capturing object's
  // lifetime became the trace's run identity, and this object is rebuilt every
  // turn (the key is per-request), which split one conversation across N runs.
  // Capture registers once at the composition root and is stamped per turn —
  // see shared/telemetry.ts.
  if (conn.format === "openai-compatible") {
    // `includeUsage` asks for the usage chunk a stream otherwise omits, which
    // is what the turn's cost capture reads.
    const headers = openCodeGoHeaders(conn, options.conversationId);
    return createOpenAICompatible({
      name: OPENAI_COMPATIBLE_PROVIDER,
      baseURL: conn.baseURL,
      apiKey: conn.apiKey,
      includeUsage: true,
      ...(headers ? { headers } : {}),
      fetch,
    })(conn.model);
  }
  const auth = conn.authScheme === "bearer" ? { authToken: conn.apiKey } : { apiKey: conn.apiKey };
  return createAnthropic({ baseURL: conn.baseURL, ...auth, fetch })(conn.model);
}

/**
 * Models that reject Anthropic's `effort` parameter. `@ai-sdk/anthropic`
 * forwards `effort` as `output_config.effort` without checking the model, and
 * the API answers a 400 on these rather than ignoring it, so the option is
 * omitted for them. Every other Claude model (Sonnet 5, Opus 4.5 and later)
 * takes it. A quirk of these model ids, so it applies on the Anthropic format
 * only.
 */
const MODELS_WITHOUT_EFFORT = ["claude-haiku-4-5", "claude-sonnet-4-5"] as const;

/** Whether `modelId` accepts the `effort` option. Prefix match covers dated ids. */
export function supportsEffort(modelId: string): boolean {
  return !MODELS_WITHOUT_EFFORT.some((prefix) => modelId.startsWith(prefix));
}

/**
 * Provider-specific per-call options for the turn's model, built here so the
 * reasoning-effort knob (like the provider SDK itself) stays inside this seam.
 * The generic turn loop passes the returned object through untouched.
 *
 * - Anthropic format: `effort`, except on a model that rejects it, whose turns
 *   carry no options at all (no `output_config`).
 * - OpenAI-compatible: the same level as `reasoning_effort`.
 */
export function modelProviderOptions(conn: Pick<ModelConnection, "format" | "model">): ProviderOptions | undefined {
  if (conn.format === "openai-compatible") {
    return { [OPENAI_COMPATIBLE_PROVIDER]: { reasoningEffort: config.reasoningEffort } };
  }
  if (!supportsEffort(conn.model)) return undefined;
  return {
    anthropic: { effort: config.reasoningEffort } satisfies AnthropicLanguageModelOptions,
  };
}

/**
 * The per-step output-token ceiling: `AGENT_MAX_OUTPUT_TOKENS`, lowered to the
 * connection's own output limit when it states one (a model that emits past
 * its limit is refused by its host, not truncated).
 */
export function maxOutputTokensFor(conn: Pick<ModelConnection, "outputLimit">): number {
  return conn.outputLimit !== undefined ? Math.min(config.maxOutputTokens, conn.outputLimit) : config.maxOutputTokens;
}

/**
 * The provider-specific PROMPT-CACHE breakpoint, or undefined when caching is
 * off or the connection does not take markers (`capabilities.promptCache`:
 * other hosts cache on their own, measured on Ollama). Lives in this seam for
 * the same reason `modelProviderOptions` does: the marker is Anthropic's
 * (`cacheControl`), and the turn loop stays provider-agnostic by passing
 * whatever this returns through opaquely.
 *
 * Anthropic caches the prompt prefix UP TO AND INCLUDING the marked block, so a
 * caller marks the last stable block rather than every block — the API allows
 * only a handful of breakpoints, and marking history messages individually
 * would exhaust them as a conversation grows.
 */
export function modelCacheBreakpoint(conn: Pick<ModelConnection, "capabilities">): ProviderOptions | undefined {
  if (!config.promptCache || !conn.capabilities.promptCache) return undefined;
  return { anthropic: { cacheControl: { type: "ephemeral" } } };
}

/**
 * Anthropic's provider-executed `web_search` tool (external-dependency-
 * discovery #252), the `anthropic-server-tool` strategy: Anthropic's own API
 * runs the search, so the tool has no `execute` here. `maxUses` bounds the
 * per-turn search budget. Registered only on a connection whose
 * `capabilities.webSearch` names this strategy (`tools/web-search.ts`), since
 * any other host answers the tool with an error.
 */
export function anthropicWebSearchTool(maxUses: number): ReturnType<typeof anthropic.tools.webSearch_20250305> {
  return anthropic.tools.webSearch_20250305({ maxUses });
}
