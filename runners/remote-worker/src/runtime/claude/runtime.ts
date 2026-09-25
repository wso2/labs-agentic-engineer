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

// The CLAUDE CODE adapter: `RuntimePolicy` in, a running Claude Agent SDK
// session out.
//
// This is the only module in the runner that calls `query()`, and — with
// `translate.ts` (messages → run events) and `classify.ts` (messages → what the
// run loop is told) beside it — the only place the SDK's vocabulary appears at
// all. Everything in it was in
// `lib/runner.ts` before the port, mixed in with the wiring that is genuinely
// the platform's.
//
// Read it as: for each clause of `RuntimePolicy`, which Claude Code mechanism
// enforces it.
//
//   policy.write               → a PreToolUse hook on Write/Edit/NotebookEdit
//   policy.webSearch           → a PreToolUse hook on WebSearch
//   policy.webFetch            → a PreToolUse hook on WebFetch
//   policy.deniedCapabilities  → `disallowedTools`, via the mapping in tools.ts
//   policy.skills.allow        → `skills:` (an allowlist, and it preloads nothing)
//   policy.skills.preloadBodies→ the `claude_code` preset's `append`
//   policy.skills.dir          → discovered because `cwd` holds the mirror AND
//                                the project setting source is admitted
//   policy.mcp                 → an `http` server behind a loopback auth proxy
//   policy.model               → `model:`, every alias's pin and the
//                                subagent model (modelPinEnv)
//   policy.debug               → the SDK's own debug/stderr/streaming options
//   the prompt                 → a streaming input held open until the run loop
//                                ends it (see openPromptStream)
//
// Three invariants are NOT policy fields, because there is no knob and nothing
// to decide per run — they are conditions of running this platform's workload at
// all, and they are stated here so the next runtime's adapter can read what it
// has to reproduce:
//
//   1. `settingSources: ["project"]` — the ONLY filesystem source admitted. It
//      is what makes the mirrored skills discoverable AT ALL (see
//      AGENT_SETTING_SOURCES), and it must not readmit a developer's `~/.claude`.
//   2. `strictMcpConfig: true` — admitting the project source re-admits the
//      checkout's own `.mcp.json`, and a project does not get to declare servers
//      into a platform run. This used to fall out of `settingSources: []`.
//   3. `permissionMode: "bypassPermissions"` + `allowDangerouslySkipPermissions`
//      — a one-shot pod has nobody to prompt. Which is exactly why the deny list
//      and the hooks above are the boundary that actually holds.

import path from "node:path";
import { query, type HookCallback, type Query, type SDKUserMessage } from "@anthropic-ai/claude-agent-sdk";
import { debugQueryOptions, openDebugSinks, type DebugSinks } from "../../lib/logger.js";
import { startMcpAuthProxy } from "../../lib/mcp_auth_proxy.js";
import type { AccessTokenSource } from "../../lib/auth_retry.js";
import { scrubber } from "../../lib/progress/scrubber.js";
import { appendSessionContext, SESSION_CONTEXT_FILE, type SessionContextRecord } from "../../lib/run_context.js";
import { toolGlossary } from "../../lib/tool_glossary.js";
import { createWebFetchGuardHook } from "../../lib/webfetch_guard.js";
import { createWebSearchDlpHook } from "../../lib/websearch_dlp.js";
import { createWorkspaceWriteGuard } from "../../lib/workspace_guard.js";
import type { Runtime, RuntimeArtifact, RuntimePolicy, RuntimeSession } from "../port.js";
import { createClaudeClassifier } from "./classify.js";
import { buildMcpOptions, deniedTools } from "./tools.js";
import { createClaudeAdapter } from "./translate.js";

/**
 * The filesystem settings sources a dispatched run admits.
 *
 * 'project' resolves relative to `cwd` — the per-task clone the platform
 * provisioned and the BFF wrote `.claude/skills/` into. Admitting it is what
 * makes the mirrored skills discoverable AT ALL: the SDK's `skills` option is a
 * context filter over discovered skills, not a loader, so with no filesystem
 * source the pinned names match nothing and vanish without a word. That is not
 * theoretical — it shipped, and the agent compensated by grepping SKILL.md out
 * of the tree, which reads like the feature working.
 *
 * The isolation this replaces was about 'user' — a developer's ~/.claude
 * leaking into a container agent — which stays excluded, as does 'local' (a
 * personal .claude/settings.local.json has no place in a dispatched run).
 * 'project' admits only content the platform itself put in the clone. It also
 * loads that repo's CLAUDE.md, which is the project's own guidance and belongs
 * in a build of that project.
 *
 * Exported so a revert to `[]` fails a test rather than a customer's build.
 */
export const AGENT_SETTING_SOURCES = ["project"] as const;

/**
 * The model a Claude Code run bills to when the organization has not chosen.
 *
 * It is also the model the platform seeds `model_rates` with, which is not a
 * coincidence: a default the platform cannot price would blank the cost of every
 * run made by every org that never opened the setting.
 */
export const CLAUDE_CODE_DEFAULT_MODEL = "claude-sonnet-5";

/**
 * The CLI's model pins, every one bound to the organization's one model.
 *
 * Left alone, the CLI resolves each alias (`sonnet`, `haiku`, `opus`, `fable`)
 * to its own release default and runs its helper calls through the `haiku`
 * alias, so a run would bill models the org never chose, its key may not reach
 * and the platform cannot price (one unpriced slice blanks the cycle's cost,
 * `modelcost.SumCost`). Pinning every alias, plus `CLAUDE_CODE_SUBAGENT_MODEL`
 * for the fan-out tool, makes the lead, its subagents and the CLI's helper
 * calls all run on `model` whatever alias a call names.
 */
export function modelPinEnv(model: string): Record<string, string> {
  return {
    ANTHROPIC_DEFAULT_SONNET_MODEL: model,
    ANTHROPIC_DEFAULT_HAIKU_MODEL: model,
    ANTHROPIC_DEFAULT_OPUS_MODEL: model,
    ANTHROPIC_DEFAULT_FABLE_MODEL: model,
    CLAUDE_CODE_SUBAGENT_MODEL: model,
  };
}

/** Placeholder header the SDK sends to the loopback proxy; never sent upstream. */
const LOOPBACK_TOKEN = "loopback";

/** The lead's name in the session-context record: the feed's own word for it. */
const LEAD_SESSION = "lead";

/**
 * The session-context record (`lib/run_context.ts`) from this runtime's hooks:
 * a subagent from `SubagentStart`, a skill load from `PreToolUse` on `Skill`
 * (`agent_id` is absent on the main thread, which is the lead). A subagent's
 * `appendix` is `false` by construction — the preset `append` is the main
 * thread's system prompt only — where OpenCode's is observed per session.
 *
 * Exported for its test.
 */
export function sessionContextHook(record: (r: SessionContextRecord) => void): HookCallback {
  return async (input) => {
    const h = input as {
      hook_event_name?: string;
      agent_id?: string;
      agent_type?: string;
      tool_name?: string;
      tool_input?: { skill?: unknown };
    };
    if (h.hook_event_name === "SubagentStart" && h.agent_id) {
      record({ session: h.agent_id, agent: h.agent_type || "subagent", appendix: false });
    } else if (h.hook_event_name === "PreToolUse" && h.tool_name === "Skill" && typeof h.tool_input?.skill === "string") {
      record({ session: h.agent_id || LEAD_SESSION, skill: h.tool_input.skill });
    }
    return {};
  };
}

/**
 * The prompt as a stream the SDK cannot close on its own.
 *
 * A plain string prompt is a "single user turn" to the SDK, and on the FIRST
 * `result` it closes the CLI's stdin — the channel every SDK-side hook answers
 * on. A lead that fans out in the background ends its turn early, so from that
 * point the workspace guard and the egress guards are "cancelled (control
 * stream closed)", which the CLI reports to the agent as a user denial: on
 * 2026-09-07 both agents of a run stopped at their first Edit with a green
 * result to show for it. Fed as a stream instead, stdin stays open until this
 * generator returns — and it returns when `release()` is called, which is the
 * run loop's decision (`RunStream.endInput`) or the session's `close()`.
 *
 * Exported for its test; nothing else builds one.
 */
export function openPromptStream(prompt: string): { stream: AsyncIterable<SDKUserMessage>; release: () => void } {
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  async function* stream(): AsyncGenerator<SDKUserMessage, void, undefined> {
    yield { type: "user", message: { role: "user", content: prompt }, parent_tool_use_id: null, session_id: "" };
    await released;
  }
  return { stream: stream(), release };
}

/** The token source the loopback proxy drives, built from the port's two members. */
function tokenSource(mcp: NonNullable<RuntimePolicy["mcp"]>): AccessTokenSource {
  return {
    getToken: () => mcp.token(),
    invalidate: () => mcp.invalidate?.(),
  };
}

/**
 * Build the Claude Code runtime.
 *
 * Stateless: every per-run object — the adapter, the proxy, the debug sinks —
 * is created inside `start`, because each describes ONE run and sharing any of
 * them across two would mislabel lines rather than merely lose detail.
 */
export function createClaudeCodeRuntime(): Runtime {
  return {
    name: "claude-code",
    defaultModel: CLAUDE_CODE_DEFAULT_MODEL,
    toolGlossary: () => toolGlossary("claude-code"),
    start: startClaudeCodeSession,
  };
}

async function startClaudeCodeSession(prompt: string, policy: RuntimePolicy): Promise<RuntimeSession> {
  // Endpoint Spec Discovery (B2) — register the platform's MCP server through a
  // loopback proxy so the bearer can rotate. The SDK only accepts static
  // Authorization headers; the proxy calls the policy's `token()` per request
  // and, on HTTP 401, invalidates and remints once. A second 401 or a remint
  // failure is fatal, and the policy owns what fatal means.
  let mcpProxy: { url: string; close: () => Promise<void> } | undefined;
  let mcpUrl = policy.mcp?.url;
  const mcpToken = policy.mcp ? LOOPBACK_TOKEN : undefined;
  if (policy.mcp) {
    mcpProxy = await startMcpAuthProxy({
      upstreamUrl: policy.mcp.url,
      source: tokenSource(policy.mcp),
      // "This run can get a fresh token" and "there is a stale one to discard"
      // are the same fact, so the port states it once.
      canRefresh: policy.mcp.invalidate !== undefined,
      onToken: policy.mcp.onToken,
      onFatal: (err) => policy.mcp?.onFatal?.(err),
    });
    mcpUrl = mcpProxy.url;
  }
  const { mcpServers, allowedTools } = buildMcpOptions(mcpUrl, mcpToken, policy.mcp?.tools ?? []);

  // Opened before the session so the sinks exist for its first byte, and closed
  // by `close()`. Absent on a normal run, which is what keeps the prompt-bearing
  // debug log out of the cluster — see RuntimePolicy.debug.
  const debugSinks: DebugSinks | undefined = policy.debug
    ? openDebugSinks(policy.logDir, (line) => scrubber.scrub(line))
    : undefined;

  const workspaceWriteGuard = createWorkspaceWriteGuard(
    policy.workspace,
    policy.write.onDenied,
    policy.write.allowOutsideProject,
  );
  const webSearchHook = createWebSearchDlpHook(policy.webSearch.deny);
  const webFetchHook = createWebFetchGuardHook(policy.webFetch.deny);
  const sessionContextFile = path.join(policy.logDir, SESSION_CONTEXT_FILE);
  const recordContext = (r: SessionContextRecord): void => appendSessionContext(sessionContextFile, r);
  recordContext({ session: LEAD_SESSION, agent: LEAD_SESSION, appendix: policy.skills.preloadBodies !== "" });
  const contextHook = sessionContextHook(recordContext);

  // One adapter per run — it carries this run's agent registry and in-flight
  // tool calls (see createClaudeAdapter).
  const adapter = createClaudeAdapter({ taskKind: policy.taskKind });

  const input = openPromptStream(prompt);
  let q: Query;
  try {
    q = query({
      prompt: input.stream,
      options: {
        cwd: policy.workspace,
        // The whole appendix — workflow, pins, glossary — arrives assembled, and
        // this runtime appends nothing after it: the `aep` skill points at "the
        // tool glossary at the end of your instructions". Appended to the
        // `claude_code` preset rather than replacing it: the preset is what makes
        // the harness's own tools and conventions work, and this is additional
        // context, not a different agent.
        systemPrompt: { type: "preset" as const, preset: "claude_code" as const, append: policy.skills.preloadBodies },
        // Pinned by the organization's setting rather than left to the SDK's own
        // default, which drifts across releases (seen live: an unpinned run
        // resolved to claude-sonnet-4-6).
        model: policy.model,
        // An ALLOWLIST, not a preload — a name absent here cannot be invoked at
        // all. Do NOT replace with 'all': the point of naming them is that the
        // BFF already decided which skills this build may use, and 'all' would
        // readmit whatever else a checkout happens to carry.
        skills: [...policy.skills.allow],
        allowedTools,
        // The boundary that actually holds under bypassPermissions — see tools.ts.
        disallowedTools: deniedTools(policy.deniedCapabilities),
        ...(mcpServers ? { mcpServers } : {}),
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
        persistSession: false,
        settingSources: [...AGENT_SETTING_SOURCES],
        strictMcpConfig: true,
        // Every alias and the subagent model, bound to the org's one model —
        // see modelPinEnv. Spread last so a stray pin in the pod's own env
        // cannot point an alias at a model the org did not choose.
        env: { ...policy.env, ...modelPinEnv(policy.model) },
        ...debugQueryOptions(debugSinks),
        // NOT canUseTool — the Task 12 spike found canUseTool is never invoked
        // for the server-executed WebSearch tool (confirmed under
        // bypassPermissions too). PreToolUse is the mechanism that actually gates
        // it pre-dispatch. See websearch_dlp.ts. WebFetch is a genuine local
        // dispatch (it actually dials out), but is gated the same way for
        // consistency and because PreToolUse is still the earliest point to deny
        // before any egress happens. See webfetch_guard.ts.
        hooks: {
          // A spawned agent's transcript is the one artefact the feed will never
          // carry — narration is deliberately off the wire, and the file is far
          // larger than a line. The hook records WHERE it is and nothing else:
          // the design's storage decision is that transcripts stay on the local
          // plane and are never uploaded from a pod, so this is what
          // `artifacts()` reads, not a second channel.
          SubagentStart: [{ hooks: [contextHook] }],
          SubagentStop: [
            {
              hooks: [
                async (input): Promise<Record<string, never>> => {
                  const h = input as { agent_id?: string; agent_transcript_path?: string };
                  adapter.noteTranscript(h.agent_id ?? "", h.agent_transcript_path ?? "");
                  return {};
                },
              ],
            },
          ],
          PreToolUse: [
            { matcher: "WebSearch", hooks: [webSearchHook] },
            { matcher: "WebFetch", hooks: [webFetchHook] },
            // One matcher per authoring tool, same reasoning as the pair above:
            // the matcher grammar is unspecified, and each hook re-checks the
            // tool name itself, so over-matching is harmless and a silent
            // non-match is not.
            { matcher: "Write", hooks: [workspaceWriteGuard] },
            { matcher: "Edit", hooks: [workspaceWriteGuard] },
            { matcher: "NotebookEdit", hooks: [workspaceWriteGuard] },
            { matcher: "Skill", hooks: [contextHook] },
          ],
        },
      },
    });
  } catch (err) {
    input.release();
    await mcpProxy?.close();
    debugSinks?.close();
    throw err;
  }

  const debugArtifacts: RuntimeArtifact[] = debugSinks
    ? [
        { path: debugSinks.debugFilePath, kind: "log" },
        { path: debugSinks.stderrFilePath, kind: "log" },
      ]
    : [];
  const contextArtifact: RuntimeArtifact = { path: sessionContextFile, kind: "log" };

  return {
    stream: { messages: q, stopTask: (taskId) => q.stopTask(taskId), endInput: input.release },
    translate: adapter.translate,
    // Per session, like the adapter: it remembers the last rate-limit line.
    classify: createClaudeClassifier(),
    usage: adapter.usage,
    artifacts: async () => [...adapter.artifacts(), contextArtifact, ...debugArtifacts],
    close: async () => {
      // A closed session must not leave the prompt stream pending: the SDK
      // would otherwise wait on a generator nothing will ever resume.
      input.release();
      debugSinks?.close();
      await mcpProxy?.close();
    },
  };
}
