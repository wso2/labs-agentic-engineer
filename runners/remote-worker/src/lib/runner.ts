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

import path from "node:path";
import { fileURLToPath } from "node:url";
import { query, type McpServerConfig, type Query } from "@anthropic-ai/claude-agent-sdk";
import type { TaskLog } from "./logger.js";
import type { DispatchRequest } from "./types.js";
import type { WorkspaceLayout } from "./workspace.js";
import { emit } from "./progress/emitter.js";
import { progressFromSdkMessage } from "./progress/from-sdk.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PLUGIN_PATH = path.resolve(__dirname, "../../plugin");

// Phase 0 allowed-tools: git, gh, build/test/lint via Bash; standard file
// tools. Endpoint Spec Discovery (B2) re-introduces MCP — but only as an
// in-process remote HTTP server (see buildMcpOptions below), never the
// file-based .mcp.json that settingSources: [] deliberately blocks.
const BASE_ALLOWED_TOOLS = ["Read", "Write", "Edit", "Bash", "Glob", "Grep"];

// The server key the BFF MCP endpoint is registered under. The SDK
// namespaces MCP tools as `mcp__<serverKey>__<toolName>` (confirmed from
// node_modules/@anthropic-ai/claude-agent-sdk/sdk.d.ts, SDKControlMcpCallRequest
// doc comment: "Fully-qualified MCP tool name, e.g. mcp__server__tool_name.").
const MCP_SERVER_KEY = "aep";

// The three read-only tools the BFF's aep-api-mcp server exposes (see
// services/aep-api/internal/feature/dependencies/mcp_tools.go). Namespaced
// per the SDK's mcp__<server>__<tool> convention above.
const MCP_TOOL_NAMES = [
  `mcp__${MCP_SERVER_KEY}__list_org_component_endpoints`,
  `mcp__${MCP_SERVER_KEY}__get_remote_git_file_contents`,
  `mcp__${MCP_SERVER_KEY}__search_remote_git_code`,
];

export interface McpQueryOptions {
  mcpServers?: Record<string, McpServerConfig>;
  allowedTools: string[];
}

// buildMcpOptions is a pure seam so the env-presence guard is unit-testable
// without constructing a full query(). Both mcpUrl and mcpToken must be
// present — the BFF's coding-agent Job template stamps AEP_MCP_URL
// unconditionally but only stamps AEP_MCP_TOKEN when minting succeeded
// (see job_template.go), so a URL-without-token dispatch must still omit
// the server rather than register it unauthenticated.
export function buildMcpOptions(mcpUrl: string | undefined, mcpToken: string | undefined): McpQueryOptions {
  if (!mcpUrl || !mcpToken) {
    return { allowedTools: BASE_ALLOWED_TOOLS };
  }
  return {
    mcpServers: {
      [MCP_SERVER_KEY]: {
        type: "http",
        url: mcpUrl,
        headers: { Authorization: `Bearer ${mcpToken}` },
      },
    },
    allowedTools: [...BASE_ALLOWED_TOOLS, ...MCP_TOOL_NAMES],
  };
}

export interface RunResult {
  exitCode: number;
  error?: string;
}

export interface StartedRun {
  query: Query;
  completion: Promise<RunResult>;
}

// PerTaskSkills carries the materialised AgentSkills plugin into the SDK
// query options. PR 3 of docs/design/skills-system.md.
//
// `skillsPluginDir` is the absolute path to .aep/skills-plugin/. If
// set, runner.ts adds a second `{type:"local"}` plugin entry pointing
// at it. `preloadSkillNames` lists the materialised names of every
// platform-shipped (`kind: org`) skill in that plugin, which we push
// into the SDK's `skills:` array so their full bodies inject at
// startup. Custom and imported skills sit in the same plugin and
// surface via the SDK's standard skill listing (description in
// context, body on invoke) — they are NOT in the preload array. See
// the design doc's "Why skills: <built-in names> and not skills:
// 'all'" rationale.
export interface PerTaskSkills {
  skillsPluginDir?: string;
  preloadSkillNames: string[];
}

// BaseAgentConfig parameterizes the two values that were hardcoded until the
// playground's local mode (docs/design/playground.md §3): the always-loaded
// workflow plugin and the startup skill preload. Both default byte-identically
// to today's behavior (pinned in runner.test.ts); production callers pass
// nothing. A caller that overrides `basePreload` owns the FULL preload list —
// the validation-task append only applies to the default.
export interface BaseAgentConfig {
  /** The always-loaded workflow plugin dir; defaults to the shipped `plugin/`. */
  basePluginPath?: string;
  /** The startup `skills:` preload; defaults to ["aep:aep"] (+ the validation body for validation tasks). */
  basePreload?: string[];
}

// resolveBaseAgentConfig is a pure seam (the buildMcpOptions pattern) so the
// byte-identical defaults are unit-pinnable without constructing a query().
export function resolveBaseAgentConfig(
  base: BaseAgentConfig | undefined,
  taskKind: DispatchRequest["taskKind"],
): { pluginPath: string; preload: string[] } {
  const pluginPath = base?.basePluginPath ?? PLUGIN_PATH;
  if (base?.basePreload) return { pluginPath, preload: [...base.basePreload] };
  const preload = ["aep:aep"];
  if (taskKind === "validation") preload.push("aep:aep-validation");
  return { pluginPath, preload };
}

export function runClaudeQuery(
  req: DispatchRequest,
  layout: WorkspaceLayout,
  log: TaskLog,
  perTaskSkills?: PerTaskSkills,
  base?: BaseAgentConfig,
): StartedRun {
  // Spawn env: bearer + git-service URL passed by file path / URL only.
  // No tokens cross via env, so transcripts cannot leak credentials.
  // ANTHROPIC_API_KEY flows through from process.env (container env).
  // F3c — surface AEP_TASK_ID and AEP_PLATFORM_URL to the agent's
  // child env so the aep skill's verification-failed shell snippet can
  // hit POST $AEP_PLATFORM_URL/api/v1/tasks/$AEP_TASK_ID/verification-failed.
  // The bearer rides through a file (AEP_BEARER_FILE) so the agent's
  // SDK transcripts can't leak it; the curl snippet reads the file at
  // call time.
  const childEnv: Record<string, string> = {
    ...(process.env as Record<string, string>),
    PATH: `${layout.aepDir}:${process.env.PATH ?? ""}`,
    GH_CONFIG_DIR: layout.ghConfigDir,
    AEP_BEARER_FILE: layout.bearerFile,
    AEP_TASK_ID: req.taskId,
    AEP_PLATFORM_URL: process.env.AEP_PLATFORM_URL ?? "",
    AEP_GIT_SERVICE_URL: req.gitServiceUrl,
    AEP_CORRELATION_ID: req.correlationId ?? "",
  };

  // Two-tier plugin list: the base `aep` plugin (workflow + base
  // conventions) is always loaded; the per-task `aep-task-skills`
  // plugin (project-attached skills) is loaded conditionally when
  // workspace.ts materialised it. Per-task org skills land in the
  // `skills:` preload so the SDK injects their full bodies at startup;
  // custom + imported sit in the same plugin and surface via the SDK's
  // standard discovery (description in context, body on invoke).
  // Related-issue discovery/cross-linking moved to the SRE agent's handoff
  // stage (a "## Related issues" section in the issue body; GitHub #N
  // mentions back-link automatically) — issues arrive pre-linked, so the
  // former aep:related-issues preload is gone. See AE-HANDOFF-DESIGN.md in
  // openchoreo/agents/sre-agent.
  // Validation tasks additionally preload the validation workflow body:
  // it replaces the implementation workflow and the run cannot afford
  // the agent skipping a description-triggered load of it.
  const resolvedBase = resolveBaseAgentConfig(base, req.taskKind);
  const plugins: Array<{ type: "local"; path: string }> = [
    { type: "local", path: resolvedBase.pluginPath },
  ];
  const skillPreload: string[] = resolvedBase.preload;
  if (perTaskSkills?.skillsPluginDir) {
    plugins.push({ type: "local", path: perTaskSkills.skillsPluginDir });
    for (const name of perTaskSkills.preloadSkillNames) {
      skillPreload.push(`aep-task-skills:${name}`);
    }
  }

  // Endpoint Spec Discovery (B2) — register the BFF's MCP server in-process
  // when the dispatch carries both AEP_MCP_URL and AEP_MCP_TOKEN. Older
  // dispatches (or a failed token mint) omit one or both, in which case the
  // runner falls back to the base tool set unchanged.
  const { mcpServers, allowedTools } = buildMcpOptions(req.mcpUrl, req.mcpToken);

  // SDK v0.2.126 auto-discovers the bundled native binary — no
  // pathToClaudeCodeExecutable needed. settingSources: [] ensures no
  // host filesystem settings leak into the container agent.
  const q = query({
    prompt: req.prompt,
    options: {
      cwd: layout.workspace,
      plugins,
      // Force built-in skill bodies into context at startup. Do NOT
      // replace with 'all' — see docs/design/skills-system.md.
      skills: skillPreload as unknown as string[],
      allowedTools,
      ...(mcpServers ? { mcpServers } : {}),
      permissionMode: "bypassPermissions",
      allowDangerouslySkipPermissions: true,
      persistSession: false,
      settingSources: [],
      env: childEnv,
    },
  });

  const completion = (async (): Promise<RunResult> => {
    try {
      for await (const message of q) {
        log.write(message);
        for (const event of progressFromSdkMessage(message)) {
          emit(event);
        }
        if (message.type === "result") {
          // #249: tokens on the result event are the wire truth; the SDK's own
          // cost figure never rides it (USD is derived server-side from tokens
          // + model, console ADR-0011). Log it to stderr as a reconciliation
          // cross-check only — stdout stays the emitter's.
          if (typeof message.total_cost_usd === "number") {
            process.stderr.write(`usage cross-check: sdk total_cost_usd=${message.total_cost_usd}\n`);
          }
          if (message.subtype === "success") {
            return { exitCode: 0 };
          }
          const errors =
            "errors" in message && Array.isArray(message.errors)
              ? (message.errors as string[])
              : [];
          return {
            exitCode: 1,
            error: `agent result ${message.subtype}${errors.length ? ": " + errors.join(", ") : ""}`,
          };
        }
      }
      emit({ kind: "log", level: "warn", summary: "agent stream ended without result" });
      return { exitCode: 1, error: "agent stream ended without result" };
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log.write({ type: "worker_error", error: msg });
      emit({ kind: "result", status: "failure", error: msg });
      return { exitCode: 1, error: msg };
    } finally {
      log.close();
    }
  })();

  return { query: q, completion };
}
