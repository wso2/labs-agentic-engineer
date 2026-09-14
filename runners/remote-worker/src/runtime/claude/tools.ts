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

// Claude Code's tool surface: which names this runtime answers to, and which of
// them the platform's `DeniedCapability` classes map onto.
//
// This file is the ONLY place Claude Code tool names are written down for
// policy purposes (the tool GLOSSARY, `lib/tool_glossary.ts`, writes them down
// for the agent to read, which is a different audience). `lib/runner.ts` used to
// hold both lists inline, which meant a second runtime could not be added
// without editing the module every run goes through.

import type { McpServerConfig } from "@anthropic-ai/claude-agent-sdk";
import type { DeniedCapability } from "../port.js";

// Phase 0 allowed-tools: git, gh, build/test/lint via Bash; standard file
// tools. Endpoint Spec Discovery (B2) re-introduces MCP — but only as an
// in-process remote HTTP server (see buildMcpOptions below), never the
// file-based .mcp.json — which strictMcpConfig blocks. That used to fall out
// of settingSources: [] for free; admitting the project source to discover the
// skills mirror re-admits the project's .mcp.json with it, so the exclusion is
// now stated rather than inherited.
// D9 secure search (Task 12) adds WebSearch, gated by the PreToolUse DLP
// hook this runtime wires from `policy.webSearch` (see websearch_dlp.ts for why
// PreToolUse, not canUseTool). WebFetch (external API/SDK doc + spec-URL
// fetches) is added alongside it, gated by its own PreToolUse SSRF + secret
// guard (see webfetch_guard.ts) — fail-closed, so pod egress to arbitrary
// fetched pages never reaches internal/private/link-local/metadata addresses or
// leaks a staged secret in the URL.
// Agent joins the set for the milestone run loop (docs/design §9.3): a cycle
// works several issues, and the main agent fans the big, prose-independent,
// disjoint-App-Path ones out to subagents. The main agent stays the SOLE git
// writer — subagents Edit/Write, plus the one `gh issue comment` that keeps
// their own issue's status line current while they work. That split is a SKILL
// rule, not a tool restriction: the SDK hands a subagent the same allowedTools
// as its parent, so `aep`'s deny-list is what keeps a subagent off git, and its
// fan-out section is what keeps small issues inline.
//
// The fan-out tool is `Agent`. It was `Task` until the SDK 0.2 → 0.3 bump, and
// this list still said `Task` afterwards — a name with no tool behind it in
// 0.3.220 (`sdk-tools.d.ts` declares `AgentInput` and no `TaskInput`). Nothing
// broke loudly, which is the point of the note below.
export const BASE_ALLOWED_TOOLS = [
  "Read",
  "Write",
  "Edit",
  "Bash",
  "Glob",
  "Grep",
  "WebSearch",
  "WebFetch",
  "Agent",
];

// allowedTools does NOT restrict anything in this run: `bypassPermissions` plus
// `allowDangerouslySkipPermissions` (see the query() options in runtime.ts)
// allows every tool the harness has, whether or not it is named above. Measured
// on a live run — the agent called `Agent` and `ScheduleWakeup`, neither of
// which was in the list, and both dispatched. So BASE_ALLOWED_TOOLS documents
// the intended surface, and the deny list below is what actually holds a
// boundary.
//
// What the deny list excludes is the harness's *session-management* surface:
// tools that assume an interactive user, a scheduler, a durable session or a
// peer to talk to, none of which a one-shot pod has. They are not merely useless
// here — a run reached for `ScheduleWakeup` to wait on its own detached
// subagents (it failed the schema and the session exited anyway), so an
// unreachable tool is a real invitation to spend a turn on a dead end.
// File/shell/search tools are deliberately absent: `aep`'s deny-list governs
// those by path and command, and blocking them wholesale would end the run.
//
// The whole TASK surface is now ALLOWED, and that took two corrections. The
// WAIT tools went first: a lead that backgrounds work has to be able to wait on
// it, and denying `TaskOutput`/`TaskStop` while the SDK's own default is to
// background a fan-out left the lead reaching for `ScheduleWakeup` instead —
// which is why that one is denied. The task LIST tools followed. They were
// denied as "a durable board's surface, and a one-shot pod has no board", and
// that reasoning had the audience wrong: the board is not for a next session, it
// is for the person watching this one. A lead's plan is the only statement of
// intent a run produces, and v2 puts it on the feed as
// `work_item {source: "plan"}` rows the console folds by item — so the plan
// being true is worth more than the turn it costs. `TaskCreate`/`TaskUpdate`
// are the writes the adapter reads; `TaskGet`/`TaskList` are the reads that let
// a lead pick its plan back up after a compaction, and they emit nothing.
//
// Each entry sits under the CLASS that explains it, which is what lets the port
// state the policy without naming a runtime (see `DeniedCapability`). A second
// runtime brings its own table; nothing about the classes moves.
const DENIED_TOOLS_BY_CAPABILITY: Record<DeniedCapability, readonly string[]> = {
  // Stops to ask a person, or waits for one to read something.
  interactive_prompt: ["AskUserQuestion", "EnterPlanMode", "ExitPlanMode", "SendFeedback"],
  // Assumes something runs after this pod is gone.
  scheduling: ["ScheduleWakeup", "Monitor", "CronCreate", "CronDelete", "CronList", "Workflow", "RemoteTrigger"],
  // Assumes the session comes back to a workspace it left behind.
  durable_session: ["EnterWorktree", "ExitWorktree"],
  // Assumes a peer on the other end. There is none.
  peer_messaging: ["SendMessage", "PushNotification"],
  // A run's output is its pull request, not a published page.
  artifact_publishing: ["Artifact"],
};

/**
 * The Claude Code tool names denied by a set of capability classes.
 *
 * Order is the classes' order and then each class's own, so the list a query
 * receives is stable — a diff on it should mean a policy change and nothing
 * else.
 */
export function deniedTools(capabilities: readonly DeniedCapability[]): string[] {
  const names: string[] = [];
  for (const capability of capabilities) {
    for (const tool of DENIED_TOOLS_BY_CAPABILITY[capability] ?? []) {
      if (!names.includes(tool)) names.push(tool);
    }
  }
  return names;
}

// The server key the platform's MCP endpoint is registered under. The SDK
// namespaces MCP tools as `mcp__<serverKey>__<toolName>` (confirmed from
// node_modules/@anthropic-ai/claude-agent-sdk/sdk.d.ts, SDKControlMcpCallRequest
// doc comment: "Fully-qualified MCP tool name, e.g. mcp__server__tool_name.").
// The convention is this runtime's, which is why the port passes BARE tool
// names and this file renders them.
export const MCP_SERVER_KEY = "aep";

/** `mcp__aep__<tool>`, the only spelling the SDK will match an allowlist on. */
export function namespacedMcpTool(tool: string): string {
  return `mcp__${MCP_SERVER_KEY}__${tool}`;
}

export interface McpQueryOptions {
  mcpServers?: Record<string, McpServerConfig>;
  allowedTools: string[];
}

/**
 * The MCP half of the query options.
 *
 * A pure seam so the presence guard is unit-testable without constructing a full
 * `query()`. Both the url and a token must be present — a URL-without-token
 * dispatch must omit the server rather than register it unauthenticated.
 *
 * The token here is the LOOPBACK placeholder in production: the SDK only accepts
 * static MCP headers, so `runtime.ts` puts an auth proxy in front and the real
 * bearer never appears in these options at all.
 */
export function buildMcpOptions(
  mcpUrl: string | undefined,
  mcpToken: string | undefined,
  tools: readonly string[] = [],
): McpQueryOptions {
  if (!mcpUrl || !mcpToken) {
    return { allowedTools: [...BASE_ALLOWED_TOOLS] };
  }
  return {
    mcpServers: {
      [MCP_SERVER_KEY]: {
        type: "http",
        url: mcpUrl,
        headers: { Authorization: `Bearer ${mcpToken}` },
      },
    },
    allowedTools: [...BASE_ALLOWED_TOOLS, ...tools.map(namespacedMcpTool)],
  };
}
