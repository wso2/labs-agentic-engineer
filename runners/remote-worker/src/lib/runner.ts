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

import fs from "node:fs";
import path from "node:path";
import { query, type McpServerConfig, type Query } from "@anthropic-ai/claude-agent-sdk";
import { debugQueryOptions, openDebugSinks, type DebugSinks, type TaskLog } from "./logger.js";
// Re-exported from where they now live: `debugQueryOptions` is entirely about
// the sinks, so it sits beside them in `logger.ts`. Still exported here because
// this is the module every existing caller and test imports it from, and
// because a consumer that only wants the options should not have to pull in
// this module's whole dependency graph to get them.
export { debugQueryOptions, type DebugQueryOptions } from "./logger.js";
import type { DispatchRequest } from "./types.js";
import type { WorkspaceLayout } from "./workspace.js";
import { writeBearerFile } from "./workspace.js";
import { emit, primeScrubber } from "./progress/emitter.js";
import { createSdkTranslator } from "./progress/from-sdk.js";
import { createRunWatchdog } from "./progress/watchdog.js";
import { apiRetryLine, isStreamFrame, readApiRetry, readStallSignal } from "./progress/diagnostics.js";
import { scrubber } from "./progress/scrubber.js";
import { createWebSearchDlpHook, stagedSecretValues } from "./websearch_dlp.js";
import { createForegroundFanOutHook } from "./fanout_foreground.js";
import { createWorkspaceWriteGuard } from "./workspace_guard.js";
import { createValidationProgressTracker } from "./validation_progress.js";
import { startMcpAuthProxy } from "./mcp_auth_proxy.js";
import { staticTokenSource, type AccessTokenSource } from "./auth_retry.js";
import { createWebFetchGuardHook } from "./webfetch_guard.js";
import { checkPreload, preloadWarning } from "./skills_preload_check.js";
import { SKILLS_MIRROR_DIR, requireWorkflowBodies } from "./skills_presence.js";
import { curlConfigHome, playwrightCliConfigPath } from "./endpoint_access.js";

/**
 * The mirror the BFF wrote into the project clone, as an absolute path.
 *
 * Every skill a coding session reads lives here — the run's own workflow skill
 * included. There is no second source: no plugin the runner builds, no library
 * it fetches. `.claude/skills/` sits at the root of `cwd`, which is what makes
 * the SDK discover it (with AGENT_SETTING_SOURCES admitting the project source).
 */
function mirrorDir(workspace: string): string {
  return path.join(workspace, SKILLS_MIRROR_DIR);
}

// Phase 0 allowed-tools: git, gh, build/test/lint via Bash; standard file
// tools. Endpoint Spec Discovery (B2) re-introduces MCP — but only as an
// in-process remote HTTP server (see buildMcpOptions below), never the
// file-based .mcp.json — which strictMcpConfig blocks. That used to fall out
// of settingSources: [] for free; admitting the project source to discover the
// skills mirror re-admits the project's .mcp.json with it, so the exclusion is
// now stated rather than inherited.
// D9 secure search (Task 12) adds WebSearch, gated by the PreToolUse DLP
// hook wired in runClaudeQuery below (see websearch_dlp.ts for why
// PreToolUse, not canUseTool, and .superpowers/sdd/task-12-report.md for
// the spike evidence). WebFetch (external API/SDK doc + spec-URL fetches)
// is added alongside it, gated by its own PreToolUse SSRF + secret guard
// (see webfetch_guard.ts) — fail-closed, so pod egress to arbitrary
// fetched pages never reaches internal/private/link-local/metadata
// addresses or leaks a staged secret in the URL.
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
const BASE_ALLOWED_TOOLS = ["Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch", "Agent"];

// allowedTools does NOT restrict anything in this run: `bypassPermissions` plus
// `allowDangerouslySkipPermissions` (see the query() options below) allows every
// tool the harness has, whether or not it is named above. Measured on a live run
// — the agent called `Agent` and `ScheduleWakeup`, neither of which was in the
// list, and both dispatched. So BASE_ALLOWED_TOOLS documents the intended
// surface, and this is the list that actually holds a boundary.
//
// What it excludes is the harness's *session-management* surface: tools that
// assume an interactive user, a scheduler, or a durable session, none of which a
// one-shot pod has. They are not merely useless here — a run reached for
// `ScheduleWakeup` to wait on its own detached subagents (it failed the schema
// and the session exited anyway), so an unreachable tool is a real invitation to
// spend a turn on a dead end. File/shell/search tools are deliberately absent
// from this list: `aep`'s deny-list governs those by path and command, and
// blocking them wholesale would end the run.
export const DISALLOWED_TOOLS = [
  "ScheduleWakeup",
  "Monitor",
  "CronCreate",
  "CronDelete",
  "CronList",
  "TaskCreate",
  "TaskUpdate",
  "TaskGet",
  "TaskList",
  "TaskStop",
  "TaskOutput",
  "Workflow",
  "Artifact",
  "AskUserQuestion",
  "EnterPlanMode",
  "ExitPlanMode",
  "SendMessage",
  "PushNotification",
  "RemoteTrigger",
  "SendFeedback",
  "EnterWorktree",
  "ExitWorktree",
];

// 128 + SIGTERM(15), the shell's own convention for "killed by a signal". The
// run reports this only when it is torn down from outside, never on its own.
const TERMINATED_EXIT_CODE = 143;

// How long the terminal dump gets to reach the pipe before the hard exit. Well
// under any sane SIGTERM grace period (Kubernetes defaults to 30s), so this
// never turns into the SIGKILL it is trying to beat.
const TERMINATE_FLUSH_MS = 50;

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

/**
 * Prepends the two absolute paths a run cannot derive to the caller's prompt.
 *
 * The `aep` skill says "the current working directory **is** the project" and
 * never names it, because static skill text cannot. Neither prompt builder can
 * either: the playground's is a TS literal and the platform's is a Go one
 * (`delivery/codingagent/coding_executor.go`), and the paths are decided here,
 * after `provisionWorkspace`. So this is the only place
 * that both knows the values and reaches every run — stating them in two prompt
 * builders would duplicate facts across a language boundary neither owns.
 *
 * **The project root** is worth stating because the alternative was measured:
 * with only relative framing, a run inferred the run directory was the project
 * root and built a whole component there.
 *
 * **The contract path** is stated for the same reason, one level down. A fan-out
 * subagent gets no skill of its own, so the lead has to hand it
 * `references/component-contract.md` as an absolute path — and a lead that has to
 * transcribe one gets it wrong: in the first playground run of the split, the
 * lead pasted `/run/base-plugin/…` to one of two subagents, dropping the
 * workspace prefix. That subagent's read failed and it fell to scanning `/` for
 * the file, which the deny-list forbids. Handing the lead the exact string to
 * copy removes the class. (It now sits under the workspace, so a dropped prefix
 * would resolve to nothing at all — but the lead still cannot derive it, because
 * nothing in the skill text names the mirror.)
 */
export function promptWithProjectRoot(prompt: string, workspaceRoot: string, contractPath?: string): string {
  const contract =
    contractPath === undefined
      ? ""
      : `The component contract every implementer follows is ${contractPath} — ` +
        `hand that exact path to every subagent you fan out to.\n`;
  return (
    `Your project root — the current working directory — is ${workspaceRoot}. ` +
    `Every file you author lives under it; nothing else on this filesystem is a project root.\n` +
    `${contract}\n${prompt}`
  );
}

/**
 * Where the `aep` skill's component contract sits, for the lead to hand on.
 *
 * The mirror, like everything else — so a fan-out subagent reads the same bytes
 * the lead does, and a developer who clones the project repo can read them too.
 */
export function contractReferencePath(workspace: string): string {
  return path.join(mirrorDir(workspace), "aep", "references", "component-contract.md");
}

export interface McpQueryOptions {
  mcpServers?: Record<string, McpServerConfig>;
  allowedTools: string[];
}

// buildMcpOptions is a pure seam so the env-presence guard is unit-testable
// without constructing a full query(). Both mcpUrl and mcpToken must be
// present — AEP_MCP_URL plus the publisher CC token the runner minted.
// A URL-without-token dispatch must still omit the server rather than register
// it unauthenticated.
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

export interface RunResult {
  exitCode: number;
  error?: string;
}

export interface StartedRun {
  query: Query;
  completion: Promise<RunResult>;
}

// PerTaskSkills carries the run's pinned, present skills into the SDK query
// options (built by skills_resolver.ts + skills_presence.ts).
//
// The BFF mirrors the org's coding-relevant skills into the project clone at
// `.claude/skills/`, which the SDK discovers because `cwd: layout.workspace`
// puts it at the working-directory root AND the project setting source is
// admitted (see AGENT_SETTING_SOURCES — `cwd` alone is not enough, which cost
// us a release). There is no per-task plugin to load. All names are BARE and kind-agnostic: the copies are
// already the filtered set, so the runner does no filtering of its own.
//
// The split between these two fields is the SDK's, not ours:
//
//   availableSkillNames → the `skills:` ALLOWLIST. A mirrored skill absent from
//     it is rejected by the Skill tool outright, so this is every skill in the
//     mirror, not just the pinned ones. Listing only pins would leave the rest
//     of what the BFF decided this build may use as inert files on disk.
//
//   pinnedBodies → the actual preload. Nothing in `skills:` arrives in context;
//     the model gets names and descriptions, and a body only when it invokes
//     the skill. A pin says the guidance IS needed for this work, so its body
//     is appended to the system prompt instead of left to the model's
//     discretion. Empty string when nothing is pinned.
export interface PerTaskSkills {
  availableSkillNames: string[];
  pinnedBodies: string;
}

// Live access-token source for MCP (and bearer-file persistence on remint).
// canRefresh is true iff publisher CC creds are mounted — the same predicate
// local and cloud already share. The SDK only accepts static MCP headers, so
// runClaudeQuery puts a loopback proxy in front of AEP_MCP_URL.
export interface McpAuthOpts {
  source: AccessTokenSource;
  canRefresh: boolean;
}

/**
 * The skills a run is steered by whatever its design says — read from the mirror
 * like every other skill, but not optional and not the design's to choose.
 *
 * `aep` is the run's procedure. `aep-validation` REPLACES its run section for a
 * validation task, and a validation run cannot afford the agent declining a
 * description-triggered load of the workflow it is supposed to follow.
 *
 * Everything else a component needs is a `skillsPinned` entry in its
 * `design.json`, and that is the design's call. This list is not: no design
 * decides whether a coding run follows the coding workflow.
 *
 * `playwright-cli` is deliberately absent. It carries the browser mechanics a
 * validation run reaches for, and `aep-validation` names it — a description
 * -triggered load is the right shape for mechanics a run may or may not need,
 * and paying for its body on every turn of every validation run is not.
 */
export function alwaysOnSkills(taskKind: DispatchRequest["taskKind"]): string[] {
  return taskKind === "validation" ? ["aep", "aep-validation"] : ["aep"];
}

/**
 * The skills a run may LOAD on demand — the other half of the sentence above.
 *
 * `skills:` is an allowlist, so leaving `playwright-cli` out of the always-on
 * set is only half a decision: absent from BOTH lists it is not deferred, it is
 * unreachable, and the Skill tool answers "not in this session's skills
 * allowlist". That is what shipped — a validation run passed an empty allowlist,
 * so the load `aep-validation` instructs could never succeed and the agent
 * grepped the mirror's files by hand instead.
 *
 * Named rather than "the whole mirror" as an implementation run gets: that run
 * may legitimately need any stack skill a `design.json` pinned, while a
 * validation run builds nothing and has exactly one mechanics skill to reach
 * for. Listing the mirror would readmit `go`, `ballerina` and every other stack
 * skill the checkout happens to carry, which is the thing the `skills:` comment
 * below warns against. Extend this list when a validation run genuinely needs
 * something else; it is a statement of what the phase uses, not a cap someone
 * has to work around.
 */
export function onDemandSkills(taskKind: DispatchRequest["taskKind"]): string[] {
  return taskKind === "validation" ? ["playwright-cli"] : [];
}

/**
 * `PLAYWRIGHT_MCP_CONFIG`, but only when there is a config to point at.
 *
 * Spread into the child env so the variable is absent rather than empty when the
 * preflight wrote nothing: playwright-cli reads it eagerly and its daemon dies
 * on a path that does not resolve, so an unset variable is the only safe way to
 * say "no override needed". Synchronous on purpose — this runs once, at spawn,
 * after the preflight that writes the file has already returned.
 */
function playwrightCliConfigEnv(): Record<string, string> {
  const file = playwrightCliConfigPath();
  return fs.existsSync(file) ? { PLAYWRIGHT_MCP_CONFIG: file } : {};
}

export async function runClaudeQuery(
  req: DispatchRequest,
  layout: WorkspaceLayout,
  log: TaskLog,
  perTaskSkills?: PerTaskSkills,
  mcpAuth?: McpAuthOpts,
): Promise<StartedRun> {
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
    // Where curl looks for `.curlrc`. Named explicitly rather than left to the
    // inherited HOME: a validation run writes `resolve` overrides there for its
    // deployed endpoints (endpoint_access.ts), and a config curl was never told
    // to look for is indistinguishable from no config at all. Harmless on a
    // coding run, which writes no such file.
    CURL_HOME: curlConfigHome(),
    // And where playwright-cli looks for its own — the browser half of the same
    // endpoint override (endpoint_access.ts). Set from the file's EXISTENCE, not
    // unconditionally like CURL_HOME above: curl treats a missing `.curlrc` as
    // no config, but this variable is fatal when it points at nothing (the
    // daemon exits on ENOENT), so a coding run and a cloud validation run — both
    // of which write no such file — must not see it at all.
    ...playwrightCliConfigEnv(),
  };

  // NO plugins. Every skill this session reads is a directory in the project's
  // `.claude/skills/` mirror, which Claude Code discovers natively because the
  // mirror sits at the root of `cwd` and the project setting source is admitted
  // (AGENT_SETTING_SOURCES). The runner used to load two plugins — one it
  // assembled from the library and one it materialised per task — and both are
  // gone: the workflow skill it selected is now mirrored like any other, so what
  // reaches a build is decided in exactly one place, by the BFF.
  //
  // Related-issue discovery/cross-linking moved to the SRE agent's handoff
  // stage (a "## Related issues" section in the issue body; GitHub #N
  // mentions back-link automatically) — issues arrive pre-linked, so the
  // former aep:related-issues preload is gone. See AE-HANDOFF-DESIGN.md in
  // openchoreo/agents/sre-agent.
  //
  // Where the skills are, for the one skill that has to name a file inside them:
  // `aep-validation` runs the platform's report generator rather than a copy the
  // repo committed. The runner is still the only layer that knows the path, so it
  // still stamps it — the value is now the mirror rather than the image's library.
  childEnv.AEP_SKILLS_DIR = mirrorDir(layout.workspace);
  // An ALLOWLIST, not a preload: a mirrored skill absent from it is rejected by
  // the Skill tool outright, so this is the whole mirror. The BFF already decided
  // what this build may use; listing only the pins would leave the rest as inert
  // files on disk.
  const skills = perTaskSkills?.availableSkillNames ?? [];
  // FATAL when the workflow is missing — see requireWorkflowBodies. This is the
  // structural guarantee that no entrypoint can start a coding run with no
  // procedure: it throws here, before a session exists, not in each caller.
  const workflowBodies = requireWorkflowBodies(layout.workspace, alwaysOnSkills(req.taskKind));

  // Endpoint Spec Discovery (B2) — register the BFF's MCP server through a
  // loopback proxy so the bearer can rotate. The SDK only accepts static
  // Authorization headers; the proxy calls getToken() per request (5-minute
  // CC buffer) and on HTTP 401 remints once. A second 401 or a remint
  // failure kills the run. Dummy "loopback" is never sent upstream.
  let mcpProxy: { url: string; close: () => Promise<void> } | undefined;
  let mcpUrl = req.mcpUrl;
  let mcpToken = req.mcpToken;
  if (mcpUrl && mcpToken) {
    const source = mcpAuth?.source ?? staticTokenSource(mcpToken);
    const canRefresh = mcpAuth?.canRefresh ?? false;
    let lastBearer = mcpToken;
    mcpProxy = await startMcpAuthProxy({
      upstreamUrl: mcpUrl,
      source,
      canRefresh,
      onToken: async (token) => {
        lastBearer = await writeBearerFile(layout.bearerFile, token, lastBearer);
        primeScrubber([token]);
      },
      onFatal: (err) => {
        emit({ kind: "result", status: "failure", error: `mcp auth: ${err.message}` });
        setTimeout(() => process.exit(1), TERMINATE_FLUSH_MS);
      },
    });
    mcpUrl = mcpProxy.url;
    mcpToken = "loopback";
  }
  const { mcpServers, allowedTools } = buildMcpOptions(mcpUrl, mcpToken);
  // tool. Secret candidates are read from childEnv, the SAME env record
  // injected into this run (see websearch_dlp.ts's stagedSecretValues doc
  // comment): staged dependency secrets (Tasks 9-11's per-run K8s Secrets,
  // mounted via envFrom) and the runner's own credentials both land there
  // before the query() call below, so this is the single source of truth
  // for "what's secret in this run" without a second, drift-prone channel.
  const stagedSecrets = stagedSecretValues(childEnv);
  const webSearchDlpHook = createWebSearchDlpHook(stagedSecrets);

  // WebFetch SSRF + secret-leak guard (see webfetch_guard.ts) — built from
  // the SAME staged-secret list as the WebSearch hook above, one source of
  // truth for "what's secret in this run".
  const webFetchGuardHook = createWebFetchGuardHook(stagedSecrets);

  // Fan-out stays in the foreground — see fanout_foreground.ts. Backgrounding a
  // subagent detaches it, and the SDK then forwards none of its messages, so the
  // run's whole implementation phase reaches the feed as an empty section.
  const foregroundFanOutHook = createForegroundFanOutHook((label) => {
    emit({
      kind: "log",
      level: "info",
      summary: `[fan-out] ${label} — running in the foreground so its steps stay on the feed`,
    });
  });

  // Authored files land in the project — see workspace_guard.ts. A run once built
  // a whole component into the run directory and finished green, so the skill's
  // "everything you produce goes inside it" needs an enforcer too.
  const workspaceWriteGuard = createWorkspaceWriteGuard(layout.workspace, (reason) => {
    emit({ kind: "log", level: "warn", summary: `[workspace] ${reason}` });
  });

  // Per-criterion progress — see validation_progress.ts. Validation only: a
  // coding run has no validation criteria to report on, and registering the
  // matchers anyway would put a hook on every Write, Edit and Bash call of every
  // run to derive nothing.
  const validationProgress =
    req.taskKind === "validation"
      ? createValidationProgressTracker((update) => {
          emit({ kind: "progress_item", itemId: update.itemId, status: update.status });
        })
      : undefined;

  // The SDK auto-discovers the bundled native binary — no
  // pathToClaudeCodeExecutable needed. See settingSources below for why the
  // project source — and only the project source — is admitted.
  //
  // The workflow body and any pinned bodies ride in on the system prompt, because
  // `skills:` does not carry them: membership buys a name and a description, and
  // the body arrives only when the model invokes the skill. Appended to the
  // claude_code preset rather than replacing it — the preset is what makes the
  // harness's own tools and conventions work, and this is additional context, not
  // a different agent. The workflow goes FIRST: it is the procedure, and a pinned
  // stack skill's rules are read against it.
  const appended = [workflowBodies, perTaskSkills?.pinnedBodies ?? ""].filter((s) => s !== "").join("\n\n");

  // Opened before the session so the sinks exist for its first byte, and closed
  // in the run loop's finally. Absent on a normal run, which is what keeps the
  // prompt-bearing debug log out of the cluster — see DispatchRequest.debug.
  const debugSinks = req.debug ? openDebugSinks(log.dir, (line) => scrubber.scrub(line)) : undefined;

  let q: Query;
  try {
    q = query({
    prompt: promptWithProjectRoot(req.prompt, layout.workspace, contractReferencePath(layout.workspace)),
    options: {
      cwd: layout.workspace,
      systemPrompt: { type: "preset" as const, preset: "claude_code" as const, append: appended },
      // Pinned rather than left to the SDK's own default, which drifts across
      // SDK releases (seen live: an unpinned run resolved to claude-sonnet-4-6).
      model: "claude-sonnet-5",
      // An ALLOWLIST, not a preload — a name absent here cannot be invoked at
      // all. Do NOT replace with 'all': the point of naming them is that the BFF
      // already decided which skills this build may use, and 'all' would readmit
      // whatever else a checkout happens to carry.
      skills: skills as unknown as string[],
      allowedTools,
      // The boundary that actually holds under bypassPermissions — see
      // DISALLOWED_TOOLS.
      disallowedTools: DISALLOWED_TOOLS,
      ...(mcpServers ? { mcpServers } : {}),
      permissionMode: "bypassPermissions",
      allowDangerouslySkipPermissions: true,
      persistSession: false,
      settingSources: [...AGENT_SETTING_SOURCES],
      // The project source above admits the clone's .claude/, and a project
      // repo can carry a .mcp.json declaring arbitrary servers. The runner's
      // MCP surface is the platform's to decide, not a checkout's, so only
      // servers from buildMcpOptions are honoured — the isolation that
      // settingSources: [] used to give implicitly, kept explicitly.
      strictMcpConfig: true,
      env: childEnv,
      ...debugQueryOptions(debugSinks),
      // NOT canUseTool — the Task 12 spike found canUseTool is never
      // invoked for the server-executed WebSearch tool (confirmed under
      // bypassPermissions above too). PreToolUse is the mechanism that
      // actually gates it pre-dispatch. See websearch_dlp.ts. WebFetch is
      // a genuine local dispatch (it actually dials out), but is gated the
      // same way for consistency and because PreToolUse is still the
      // earliest point to deny before any egress happens. See
      // webfetch_guard.ts.
      hooks: {
        PreToolUse: [
          { matcher: "WebSearch", hooks: [webSearchDlpHook] },
          { matcher: "WebFetch", hooks: [webFetchGuardHook] },
          // Not a guard: this one rewrites the call rather than gating it. Two
          // entries rather than one alternation, because the matcher's grammar
          // is unspecified in the SDK's types and a pattern that silently failed
          // to match would take the feed down with it. The hook re-checks the
          // tool name itself, so a matcher that over-matches is harmless.
          { matcher: "Agent", hooks: [foregroundFanOutHook] },
          { matcher: "Task", hooks: [foregroundFanOutHook] },
          // One matcher per authoring tool, same reasoning as the pair above: the
          // matcher grammar is unspecified, and the hook re-checks the tool name
          // itself, so over-matching is harmless and a silent non-match is not.
          { matcher: "Write", hooks: [workspaceWriteGuard] },
          { matcher: "Edit", hooks: [workspaceWriteGuard] },
          { matcher: "NotebookEdit", hooks: [workspaceWriteGuard] },
          // Neither a guard nor a rewrite: this one only watches, and returns an
          // empty decision. One matcher per tool for the same reason as every
          // entry above, and `Bash` as well because a validation run's per-spec
          // `npm test` call is what says a criterion is running.
          ...(validationProgress
            ? [
                { matcher: "Write", hooks: [validationProgress.hook] },
                { matcher: "Edit", hooks: [validationProgress.hook] },
                { matcher: "NotebookEdit", hooks: [validationProgress.hook] },
                { matcher: "Bash", hooks: [validationProgress.hook] },
              ]
            : []),
        ],
      },
    },
    });
  } catch (err) {
    await mcpProxy?.close();
    throw err;
  }

  // One translator per run — it carries this run's subagent labels and
  // in-flight tool calls (see createSdkTranslator).
  // The tracker settles a criterion from the SAME `ok` the feed reports, rather
  // than re-deriving success from the tool result a second time.
  const translate = createSdkTranslator(
    validationProgress ? { onToolOutcome: validationProgress.settle } : undefined,
  );
  // …and one watchdog, so a silent stretch says what it is waiting on rather
  // than looking identical to a dead run.
  const watchdog = createRunWatchdog();
  const stopWatchdog = watchdog.start();

  // A killed run must still explain itself. Without this, SIGTERM (what the
  // playground's Ctrl-C and a Job eviction both send) tears the process down
  // mid-tool and leaves nothing behind — the state that would have named the
  // culprit dies with it. Handling the signal means we now own the exit, so
  // this MUST terminate: a handler that only logged would convert a kill into
  // the very hang it exists to diagnose.
  const onTerminate = (signal: NodeJS.Signals): void => {
    emit({ kind: "log", level: "error", summary: `[watchdog] terminated by ${signal} — ${watchdog.describe()}` });
    stopWatchdog();
    // stdout is a PIPE here (a pod's log stream; the playground's child stdio),
    // and pipe writes are asynchronous on POSIX — exiting on this tick can
    // truncate the dump just written, losing the one line the handler exists
    // to produce. Give the fd a bounded moment to drain and then exit hard: a
    // blocked reader must not turn a kill into a hang.
    setTimeout(() => process.exit(TERMINATED_EXIT_CODE), TERMINATE_FLUSH_MS);
  };
  process.once("SIGTERM", onTerminate);
  process.once("SIGINT", onTerminate);

  const completion = (async (): Promise<RunResult> => {
    try {
      for await (const message of q) {
        // Streaming frames arrive per token and are pure watchdog fuel: they
        // reach neither the feed nor claude.log, because writing them to a
        // JSON-per-message file would turn a diagnostic into the hang it exists
        // to report. Only present under `debug` at all.
        if (isStreamFrame(message)) {
          watchdog.observeStream();
          continue;
        }
        log.write(message);
        // A retryable API failure is the answer to "waiting on the model" —
        // see progress/diagnostics.ts. It is recorded and reported but NOT
        // passed to observe(): a retry means the run failed to progress, and
        // counting it as activity would suppress the very report it explains.
        // The translator drops this message, so emitting here adds a line
        // rather than duplicating one.
        const retry = readApiRetry(message);
        if (retry) {
          watchdog.observeRetry(retry);
          emit({ kind: "log", level: "warn", summary: apiRetryLine(retry) });
          continue;
        }
        // The other system messages that explain a silence or an ending — a
        // compaction, a refusal, a denied tool, a worker going away. Dropped
        // with every other unrecognised subtype until now, which is how a run
        // that was compacting and a run that was wedged looked identical.
        // Deliberately NOT fed to the watchdog: none of them is the agent making
        // progress, and firing the idle report slightly early is the safe
        // direction for a diagnostic.
        const signal = readStallSignal(message);
        if (signal) {
          emit({ kind: "log", level: signal.level, summary: signal.summary });
          continue;
        }
        const events = translate(message);
        watchdog.observe(events);
        for (const event of events) {
          emit(event);
        }
        // The SDK reports what it actually resolved; a preload that matched
        // nothing is dropped in silence (see skills_preload_check.ts for the
        // run this cost us). Warn rather than fail: the guidance is missing,
        // not the build, and a run that can still produce something useful
        // should — but it must not look clean while doing it.
        if (message.type === "system" && message.subtype === "init") {
          const { missing } = checkPreload(skills, message.skills ?? []);
          if (missing.length > 0) {
            emit({ kind: "log", level: "warn", summary: preloadWarning(missing) });
          }
        }
        if (message.type === "result") {
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
      stopWatchdog();
      process.removeListener("SIGTERM", onTerminate);
      process.removeListener("SIGINT", onTerminate);
      log.close();
      debugSinks?.close();
      await mcpProxy?.close();
    }
  })();

  return { query: q, completion };
}
