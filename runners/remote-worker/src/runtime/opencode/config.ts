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

// The ONE config object an OpenCode run is started with, built from
// `RuntimePolicy` alone (ADR-0015). It reaches the server as
// `OPENCODE_CONFIG_CONTENT`; the checkout's own config is switched off
// (`OPENCODE_DISABLE_PROJECT_CONFIG`, runtime.ts), so a project cannot add to
// it. Pure, so every clause is a test.

import { FALLBACK_CONTEXT_WINDOW, type ModelConnection } from "../../lib/model_connection.js";
import type { DeniedCapability, WebSearchServer } from "../port.js";
import { deniedTools, MCP_SERVER_KEY, opencodeModel, PRIMARY_AGENT, providerId, SUBAGENT } from "./tools.js";

/** A permission rule: one action, or a pattern → action map evaluated in order. */
export type PermissionRule = "allow" | "deny" | Record<string, "allow" | "deny">;

/** What the builder needs; every field comes off `RuntimePolicy` or the runtime's own files. */
export interface OpencodeConfigInput {
  /** The run's model connection; its `model` is the org's, platform spelling (`claude-sonnet-5`). */
  connection: ModelConnection;
  /** Absolute path of the system-prompt appendix (workflow → pins → glossary). */
  instructionsPath: string;
  /** Absolute path of the guard plugin's package DIRECTORY. */
  pluginDir: string;
  /** The skills this session may load (`RuntimePolicy.skills.allow`). */
  skillAllow: readonly string[];
  deniedCapabilities: readonly DeniedCapability[];
  /** The loopback auth proxy's URL, when the run has the platform's MCP server. */
  mcpUrl?: string;
  /** The platform's web search server, when the connection's strategy needs it (`RuntimePolicy.webSearch.server`). */
  webSearchServer?: WebSearchServer;
  debug: boolean;
}

/**
 * The server-env variable the connection key is read from — the adapter puts
 * it there (`runtime.ts`, `childEnvironment`) — so the config names a reference
 * and never a value.
 */
export const CONNECTION_KEY_ENV = "AEP_MODEL_API_KEY";
const CONNECTION_KEY_REF = `{env:${CONNECTION_KEY_ENV}}`;

/**
 * A model's output limit when the connection states none. OpenCode requires one
 * beside the context window for a model it has no catalog entry for, and caps
 * every request's output at 32,000 tokens in any case, so its own ceiling is
 * the limit that changes nothing.
 */
export const FALLBACK_OUTPUT_LIMIT = 32_000;

/**
 * The provider block for a connection.
 *
 * On Anthropic's own API: provider `anthropic` with the key and nothing else,
 * so models.dev's catalog (shipped in the image) keeps supplying Claude's
 * limits.
 *
 * Anywhere else: provider `aep`, which matches no catalog entry, so the
 * connection's figures are the only ones — and a model with no context limit
 * is one OpenCode never compacts. The SDK package follows the format; both
 * ship inside OpenCode, so a pod installs nothing. An Anthropic-format host
 * that takes the key as a Bearer token gets it as `authToken`.
 */
export function providerConfig(connection: ModelConnection): Record<string, unknown> {
  const id = providerId(connection);
  if (id === "anthropic") return { anthropic: { options: { apiKey: CONNECTION_KEY_REF } } };
  const openaiCompatible = connection.format === "openai-compatible";
  const credential =
    !openaiCompatible && connection.authScheme === "bearer" ? { authToken: CONNECTION_KEY_REF } : { apiKey: CONNECTION_KEY_REF };
  return {
    [id]: {
      npm: openaiCompatible ? "@ai-sdk/openai-compatible" : "@ai-sdk/anthropic",
      name: `AEP model connection (${connection.host})`,
      options: {
        baseURL: connection.baseURL,
        ...credential,
        // Streamed usage is what a run's cost capture reads.
        ...(openaiCompatible ? { includeUsage: true } : {}),
      },
      models: {
        [connection.model]: {
          name: connection.model,
          limit: {
            context: connection.contextWindow ?? FALLBACK_CONTEXT_WINDOW,
            output: connection.outputLimit ?? FALLBACK_OUTPUT_LIMIT,
          },
        },
      },
    },
  };
}

/**
 * An allowlist as OpenCode must read it: `"*": "deny"` FIRST. Rules are last
 * match wins, and a tool whose last matching rule is a `*` deny is hidden
 * outright, so the key order below IS the policy.
 */
export function allowlist(names: readonly string[]): Record<string, "allow" | "deny"> {
  const rules: Record<string, "allow" | "deny"> = { "*": "deny" };
  for (const name of names) rules[name] = "allow";
  return rules;
}

/** The loopback placeholder the proxy expects; never sent upstream (`lib/mcp_auth_proxy.ts`). */
const LOOPBACK_TOKEN = "loopback";

/**
 * The run's config; ADR-0015's table has the reason for each clause. The
 * provider key is an `{env:…}` reference the server resolves, so no credential
 * is ever in this object.
 */
export function buildOpencodeConfig(input: OpencodeConfigInput): Record<string, unknown> {
  const provider = providerId(input.connection);
  const model = opencodeModel(provider, input.connection.model);
  // OpenCode's own `websearch` runs only beside Anthropic's API here; on any
  // other connection it is off, and the platform's `aep-web` (below) is the
  // search where the strategy supplies one.
  const builtinSearch = input.connection.webSearch === "anthropic-server-tool" ? "allow" : "deny";
  const denied = Object.fromEntries(deniedTools(input.deniedCapabilities).map((tool) => [tool, "deny" as const]));
  const subagentPermission = { todowrite: "deny", task: "allow" } as const;

  const permission: Record<string, PermissionRule> = {
    read: "allow",
    glob: "allow",
    grep: "allow",
    list: "allow",
    edit: "allow",
    bash: "allow",
    task: allowlist([SUBAGENT]),
    todowrite: "allow",
    webfetch: "allow",
    websearch: builtinSearch,
    skill: allowlist(input.skillAllow),
    // The capability classes (`question` today), from tools.ts — never `ask`.
    ...denied,
    // Gates reads and bash paths too, with no read/write split; the guard
    // plugin is the platform's one write gate (ADR-0015).
    external_directory: "allow",
    doom_loop: "allow",
    lsp: "deny",
  };

  return {
    $schema: "https://opencode.ai/config.json",
    model,
    small_model: model,
    default_agent: PRIMARY_AGENT,
    subagent_depth: 3,
    autoupdate: false,
    share: "disabled",
    instructions: [input.instructionsPath],
    plugin: [input.pluginDir],
    snapshot: false,
    ...(input.debug ? { logLevel: "DEBUG" } : {}),
    // Only the connection's provider loads — none detected from the env joins
    // it, so no other key in the server's environment is ever presented.
    ...(provider === "anthropic" ? {} : { enabled_providers: [provider] }),
    provider: providerConfig(input.connection),
    agent: {
      [PRIMARY_AGENT]: { mode: "primary", description: "AEP coding run lead" },
      [SUBAGENT]: { mode: "subagent", model, permission: { ...subagentPermission } },
      build: { disable: true },
      plan: { disable: true },
      explore: { disable: true },
      scout: { disable: true },
    },
    permission,
    tools: { lsp: false },
    ...(input.mcpUrl || input.webSearchServer ? { mcp: mcpServers(input) } : {}),
  };
}

/** The run's MCP servers: the platform's, behind the loopback proxy, and `aep-web`. */
function mcpServers(input: OpencodeConfigInput): Record<string, unknown> {
  const servers: Record<string, unknown> = {};
  if (input.mcpUrl) {
    servers[MCP_SERVER_KEY] = {
      type: "remote",
      url: input.mcpUrl,
      headers: { Authorization: `Bearer ${LOOPBACK_TOKEN}` },
      oauth: false,
    };
  }
  if (input.webSearchServer) {
    servers[input.webSearchServer.name] = {
      type: "local",
      command: [input.webSearchServer.command, ...input.webSearchServer.args],
      enabled: true,
    };
  }
  return servers;
}
