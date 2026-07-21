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
 * Multi-provider stays additive: `LlmConfig` grows a `provider` discriminator
 * and `createModel` switches on it — no call-site changes.
 *
 * (The per-org key resolver from the legacy agents-service is intentionally NOT
 * ported here — this package is the standalone main-agent demo, which reads its
 * key from the environment. See run.ts.)
 */

import { wrapLanguageModel, type LanguageModel, type LanguageModelMiddleware } from "ai";
import { createAnthropic, type AnthropicLanguageModelOptions } from "@ai-sdk/anthropic";
import { devToolsMiddleware } from "@ai-sdk/devtools";
import type { ProviderOptions } from "../agents/main/run-turn.js";
import { config } from "./config.js";

/**
 * Make a middleware NON-FATAL: a throw in any of its hooks degrades to the
 * unwrapped behavior instead of failing the model call. DevTools' `wrapStream`
 * runs its trace-capture (`ensureRunCreated`/`createStep`, which touch
 * `.devtools/generations.json`) BEFORE its own try/catch, so a corrupt/missing
 * db or an FS error there propagates into the stream and KILLS THE TURN (seen
 * live: "Cannot read properties of undefined (reading 'find')" → "stream ended
 * without a manifest"). Trace capture is a debugging aid — it must never be able
 * to break a live generation. The `wrapStream`/`wrapGenerate` fallbacks call the
 * original `doStream`/`doGenerate` exactly once (DevTools only throws BEFORE it
 * calls them, so there is no double model call).
 */
function nonFatalMiddleware(mw: LanguageModelMiddleware): LanguageModelMiddleware {
  return {
    ...mw,
    ...(mw.transformParams
      ? {
          transformParams: async (o) => {
            try {
              return await mw.transformParams!(o);
            } catch {
              return o.params;
            }
          },
        }
      : {}),
    ...(mw.wrapGenerate
      ? {
          wrapGenerate: async (o) => {
            try {
              return await mw.wrapGenerate!(o);
            } catch {
              return o.doGenerate();
            }
          },
        }
      : {}),
    ...(mw.wrapStream
      ? {
          wrapStream: async (o) => {
            try {
              return await mw.wrapStream!(o);
            } catch {
              return o.doStream();
            }
          },
        }
      : {}),
  };
}

/** Resolved LLM configuration for a single run. */
export interface LlmConfig {
  /** Provider API key. */
  apiKey: string;
  /** Model id. Defaults to the service-wide `config.model`. */
  model?: string;
  /** Optional provider base URL override (gateways / proxies / self-host). */
  baseURL?: string;
}

/**
 * The model id `createModel` resolves for `cfg`. Exported so the composition
 * root can thread the SAME id it instantiates into the turn (usage attribution
 * on the terminal manifest, #249) instead of re-deriving the default elsewhere.
 */
export function resolveModelId(cfg: Pick<LlmConfig, "model"> = {}): string {
  return cfg.model ?? config.model;
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
  const model = provider(resolveModelId(cfg));
  // AI SDK DevTools (AGENT_DEVTOOLS): wrap the model so every LLM call/step —
  // request, response, tool calls, token usage, timing — is captured to
  // `.devtools/generations.json` (and pushed to a running viewer on
  // AI_SDK_DEVTOOLS_PORT) for the trace UI. Opt-in: the middleware writes a file
  // + best-effort-notifies per call, so it stays off unless explicitly enabled.
  if (config.devtools) {
    return wrapLanguageModel({ model, middleware: nonFatalMiddleware(devToolsMiddleware()) });
  }
  return model;
}

/**
 * Provider-specific per-call options, built here so the reasoning-effort knob
 * (like the provider SDK itself) stays inside this seam. The generic turn loop
 * passes the returned object through untouched.
 */
export function modelProviderOptions(): ProviderOptions {
  return {
    anthropic: { effort: config.reasoningEffort } satisfies AnthropicLanguageModelOptions,
  };
}
