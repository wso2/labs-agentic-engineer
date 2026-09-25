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

import type { DeniedCapability } from "../port.js";
import {
  deniedTools,
  MCP_SERVER_KEY,
  opencodeModel,
  PRIMARY_AGENT,
  PROVIDER_ID,
  SUBAGENT,
} from "./tools.js";

/** A permission rule: one action, or a pattern → action map evaluated in order. */
export type PermissionRule = "allow" | "deny" | Record<string, "allow" | "deny">;

/** What the builder needs; every field comes off `RuntimePolicy` or the runtime's own files. */
export interface OpencodeConfigInput {
  /** The org's model, platform spelling (`claude-sonnet-5`). */
  model: string;
  /** Absolute path of the system-prompt appendix (workflow → pins → glossary). */
  instructionsPath: string;
  /** Absolute path of the guard plugin's package DIRECTORY. */
  pluginDir: string;
  /** The skills this session may load (`RuntimePolicy.skills.allow`). */
  skillAllow: readonly string[];
  deniedCapabilities: readonly DeniedCapability[];
  /** The loopback auth proxy's URL, when the run has the platform's MCP server. */
  mcpUrl?: string;
  debug: boolean;
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
  const model = opencodeModel(input.model);
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
    websearch: "allow",
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
    provider: { [PROVIDER_ID]: { options: { apiKey: "{env:ANTHROPIC_API_KEY}" } } },
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
    ...(input.mcpUrl
      ? {
          mcp: {
            [MCP_SERVER_KEY]: {
              type: "remote",
              url: input.mcpUrl,
              headers: { Authorization: `Bearer ${LOOPBACK_TOKEN}` },
              oauth: false,
            },
          },
        }
      : {}),
  };
}
