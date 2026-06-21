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
 * Model seam — the single provider-aware module in the service.
 *
 * Every place that needs an LLM (the HTTP routes today; the eval harness
 * tomorrow) goes through `createModel`, so the rest of the codebase consumes
 * a provider-agnostic `LanguageModel` and never imports an SDK provider
 * directly. That keeps agents runnable in isolation (inject any model) and
 * keeps multi-provider an additive change: when it lands, `LlmConfig` grows a
 * `provider` discriminator and `createModel` switches on it — no call site
 * changes.
 */

import type { LanguageModel } from "ai";
import { createAnthropic } from "@ai-sdk/anthropic";
import { config } from "./config.js";
import { resolveAnthropicKey } from "./anthropic-key-resolver.js";

/**
 * Resolved LLM configuration for a single run. Today only Anthropic is
 * supported, so the provider is implicit; `provider` slots in here later.
 */
export interface LlmConfig {
  /** Provider API key. */
  apiKey: string;
  /** Model id. Defaults to the service-wide `config.model`. */
  model?: string;
  /** Optional provider base URL override (gateways / proxies / self-host). */
  baseURL?: string;
}

/**
 * Build a Vercel AI SDK `LanguageModel` from resolved credentials. This is the
 * ONLY function that knows which provider SDK to instantiate.
 */
export function createModel(cfg: LlmConfig): LanguageModel {
  const provider = createAnthropic({
    apiKey: cfg.apiKey,
    ...(cfg.baseURL ? { baseURL: cfg.baseURL } : {}),
  });
  return provider(cfg.model ?? config.model);
}

/**
 * Resolve the effective model for an OC org: per-org Anthropic key (with
 * platform fallback) → `LanguageModel`. Wraps the resolve + construct dance so
 * the routes don't each repeat it. Throws `AnthropicKeyError` (the caller maps
 * it to an HTTP status) — see `resolveAnthropicKey`.
 */
export async function resolveModelForOrg(orgId: string): Promise<LanguageModel> {
  const { key } = await resolveAnthropicKey(orgId);
  return createModel({ apiKey: key });
}
