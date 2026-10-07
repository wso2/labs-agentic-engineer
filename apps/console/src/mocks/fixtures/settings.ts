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

import type { components } from "../../generated/aep-api";

type AgentsProjection = components["schemas"]["AgentsProjection"];
type GitProviderProjection = components["schemas"]["GitProviderProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];
type LLMFormatOption = components["schemas"]["LLMFormatOption"];
type AgentRuntime = components["schemas"]["AgentRuntime"];
type ApiError = components["schemas"]["Error"];

// Scenario switch for onboarding. Toggle in devtools:
//   localStorage.setItem('aep:mock:settings', 'empty' | 'connected')
// "empty": nothing connected yet (the default — triggers the onboarding
// gate).
// "connected": GitHub + a model connection on Anthropic's API (no
// onboarding), Claude Code, no subscription.
// A connection saved in mock mode persists under `aep:mock:connection:v2` and
// wins over the scenario; remove that key to start over.
//
// The model probe (Test connection, and every save that changes the
// connection) is simulated from what is typed; see the MODEL_PROBE_* sentinels
// below and `probeConnection` in handlers/settings.ts.
export type SettingsScenario = "empty" | "connected";

// Typing this exact value into a PAT/API-key field simulates the BFF's
// synchronous probe-before-persist validation failing against the real
// provider (issue #96: PATCH /config validates before persisting).
export const INVALID_CREDENTIAL_VALUE = "invalid";

export const githubConnectedFixture: GitProviderProjection = {
  kind: "github",
  mode: "pat",
  status: "connected",
  githubLogin: "acme-dev",
  identityLogin: "acme-dev",
  identityName: "Acme Dev",
  identityEmail: "dev@acme.example",
  connectedAt: "2026-06-01T12:00:00Z",
  lastValidatedAt: "2026-07-01T09:00:00Z",
  selectedRepos: ["acme-dev/demo-shop"],
};

// Model probe sentinels, read from what the reader types (host, key, model):
// - a key containing "invalid" is rejected by the endpoint (401);
// - a key containing "ratelimit" answers Test connection with 429
//   `llm_test_rate_limited` (the platform's own 10-a-minute limit);
// - a key containing "limit" proves the key but hits the provider's usage
//   limit (`warning: provider_limit`);
// - a host containing "internal", "localhost" or ending ".local" is refused
//   as private; a host containing "unreachable" does not answer;
// - a model missing from a known host's listing is `modelListed: no`, and a
//   host with no listing here (anything but the three below) is `unknown`.
export const MODEL_PROBE_REJECTED_KEY = "invalid";
export const MODEL_PROBE_TEST_RATE_LIMIT_KEY = "ratelimit";
export const MODEL_PROBE_PROVIDER_LIMIT_KEY = "limit";

/** Each host's model listing, as its `GET /models` would answer. */
export const modelListings: Record<string, string[]> = {
  "api.anthropic.com": ["claude-sonnet-5-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-opus-5"],
  "ollama.com": ["glm-5.3", "kimi-k3", "deepseek-v4-pro:0813", "gpt-oss:20b", "gpt-oss:120b"],
  "openrouter.ai": ["z-ai/glm-5.3", "moonshotai/kimi-k3", "anthropic/claude-sonnet-5"],
};

/** Models Ollama's `/api/show` reports with `vision`. */
export const ollamaVisionModels = ["kimi-k3"];

/** The (host, model) pairs the platform holds a rate for. */
export const pricedModels: Record<string, string[]> = {
  "api.anthropic.com": ["claude-sonnet-5-5", "claude-sonnet-5", "claude-haiku-4-5"],
};

const ANTHROPIC_URL = "https://api.anthropic.com/v1";

/**
 * The formats a connection may speak on an installation that runs
 * `availableRuntimes`: Claude Code takes only the Anthropic format.
 */
export function llmFormatsFor(availableRuntimes: AgentRuntime[]): LLMFormatOption[] {
  return [
    {
      kind: "anthropic",
      defaultBaseURL: ANTHROPIC_URL,
      defaultModel: "claude-sonnet-5-5",
      runtimes: availableRuntimes.filter((r) => r === "claude-code" || r === "opencode"),
    },
    {
      kind: "openai-compatible",
      defaultBaseURL: null,
      defaultModel: "glm-5.3",
      runtimes: availableRuntimes.filter((r) => r === "opencode"),
    },
  ];
}

export const llmConnectedFixture: LLMProjection = {
  kind: "anthropic",
  baseURL: ANTHROPIC_URL,
  model: "claude-sonnet-5",
  connectedAt: "2026-06-01T12:05:00Z",
  updatedAt: "2026-09-25T13:53:00Z",
  updatedBy: "dev@acme.example",
  priced: true,
  capabilities: {
    claudeSubscription: true,
    webSearch: "anthropic-server-tool",
    imageInput: "yes",
    nativePdf: true,
    generatedAgents: true,
  },
};

// Every org has an effective runtime, so this section is never absent —
// unlike the connection. Null updatedAt/updatedBy is the platform-defaults
// state: nobody has ever chosen, which the console must not render as
// somebody having picked these very values.
export const agentsDefaultsFixture: AgentsProjection = {
  runtime: "claude-code",
  availableRuntimes: ["claude-code", "opencode"],
  subscription: null,
  updatedAt: null,
  updatedBy: null,
};

/** A refusal on one `/config` section, as the server shapes it. */
export function sectionError(section: "llm" | "agents", code: string, message: string): ApiError {
  return { code, message, details: [{ field: `body.${section}`, message }] };
}

export const gitProviderValidationError: ApiError = {
  code: "validation_failed",
  message: "the provided PAT could not be validated against GitHub",
  details: [
    {
      field: "body.gitProvider",
      message: "the provided PAT could not be validated against GitHub",
    },
  ],
};

export const subscriptionValidationError: ApiError = {
  code: "anthropic_key_invalid",
  message: "Anthropic rejected the key (401 Unauthorized)",
  details: [
    {
      field: "body.agents",
      message: "Anthropic rejected the key (401 Unauthorized)",
    },
  ],
};

export const gitProviderDisconnectRejected: ApiError = {
  code: "validation_failed",
  message:
    "use POST /config/git-provider/disconnect to disconnect the git provider",
  details: [
    {
      field: "body.gitProvider",
      message:
        "use POST /config/git-provider/disconnect to disconnect the git provider",
    },
  ],
};
