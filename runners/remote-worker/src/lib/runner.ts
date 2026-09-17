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

// Starts ONE coding run, and knows nothing about which runtime runs it.
//
// Everything here is either a fact only this layer has (the absolute workspace
// path, the child environment, the mirror's location) or a decision that is the
// PLATFORM's whatever the runtime (what may be authored where, what a WebSearch
// may contain, which skills are reachable, how long the run may take). The
// runtime-specific half — the SDK call, the deny list, the hook grammar, the
// setting sources, the MCP auth proxy, the model literal — moved behind
// `runtime/port.ts` and lives in `runtime/claude/`.
//
// The test for whether something belongs in this file is whether it would be
// written the same way for a second runtime. If it names a tool, a hook or an
// SDK option, it does not belong here.

import path from "node:path";
import type { TaskLog } from "./logger.js";
import type { DispatchRequest } from "./types.js";
import type { WorkspaceLayout } from "./workspace.js";
import { writeBearerFile } from "./workspace.js";
import { emit, primeScrubber } from "./progress/emitter.js";
import {
  consumeRun,
  createRunTerminator,
  runDeadlineFromEnv,
  type RunResult,
  type RunTerminator,
} from "./run_loop.js";
// Re-exported from where it now lives: the shape of a finished run belongs with
// the loop that decides a run is finished, but this is the module every caller
// and test already imports it from.
export type { RunResult } from "./run_loop.js";
import { createRunWatchdog } from "./progress/watchdog.js";
import { stagedSecretValues, webSearchDenial } from "./websearch_dlp.js";
import { allowsWriteOutsideProject } from "./workspace_guard.js";
import { staticTokenSource, type AccessTokenSource } from "./auth_retry.js";
import { webFetchDenial } from "./webfetch_guard.js";
import { SKILLS_MIRROR_DIR, requireWorkflowBodies } from "./skills_presence.js";
import { DENIED_CAPABILITIES, type Runtime, type RuntimePolicy, type RuntimeSession } from "../runtime/port.js";
import { createRuntime, modelFromEnv, runtimeNameFromEnv } from "../runtime/registry.js";
import { curlConfigHome } from "./endpoint_access.js";

/**
 * The mirror the BFF wrote into the project clone, as an absolute path.
 *
 * Every skill a coding session reads lives here — the run's own workflow skill
 * included. There is no second source: no plugin the runner builds, no library
 * it fetches. `.claude/skills/` sits at the root of `cwd`, which is what makes
 * a runtime discover it.
 */
function mirrorDir(workspace: string): string {
  return path.join(workspace, SKILLS_MIRROR_DIR);
}

// 128 + SIGTERM(15), the shell's own convention for "killed by a signal". The
// run reports this only when it is torn down from outside, never on its own.
const TERMINATED_EXIT_CODE = 143;

// How long the terminal dump gets to reach the pipe before the hard exit. Well
// under any sane SIGTERM grace period (Kubernetes defaults to 30s), so this
// never turns into the SIGKILL it is trying to beat.
const TERMINATE_FLUSH_MS = 50;

/**
 * The read-only tools the platform's MCP server exposes.
 *
 * BARE names: how a runtime namespaces an MCP tool is its own convention (Claude
 * Code renders `mcp__<server>__<tool>`), so the platform states what the server
 * has and the adapter states what to call it. Source of truth:
 * `services/aep-api/internal/feature/dependencies/mcp_tools.go`.
 */
const MCP_TOOL_NAMES = [
  "list_org_component_endpoints",
  "get_remote_git_file_contents",
  "search_remote_git_code",
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

/**
 * The system prompt's appendix, in the order a session reads it.
 *
 * Three parts, and the order is the contract:
 *
 *   1. the run's WORKFLOW — the procedure everything else is read against;
 *   2. the skills the design PINNED to this work, whose rules are read against
 *      that procedure;
 *   3. the tool GLOSSARY, which binds the workflow's roles ("the fan-out tool",
 *      "the wait tool") to the names this runtime answers to.
 *
 * The glossary is last because the skill points at it there — "the tool glossary
 * at the end of your instructions" — so nothing may be appended after it. It is
 * also why the workflow can be written in roles at all: one authored library
 * steers any runtime, and only the runtime knows which names bind them.
 *
 * The glossary arrives as TEXT (`Runtime.toolGlossary()`) rather than being
 * looked up here, because the ORDER is the platform's contract and the CONTENT
 * is the runtime's. Exported so that order is a test rather than a comment.
 */
export function systemPromptAppend(workflowBodies: string, pinnedBodies: string, glossary: string): string {
  return [workflowBodies, pinnedBodies, glossary].filter((s) => s !== "").join("\n\n");
}

export interface StartedRun {
  /** The live session, for a caller that needs its artifacts or an early stop. */
  session: RuntimeSession;
  completion: Promise<RunResult>;
}

// PerTaskSkills carries the run's pinned, present skills into the runtime policy
// (built by skills_resolver.ts + skills_presence.ts).
//
// The BFF mirrors the org's coding-relevant skills into the project clone at
// `.claude/skills/`, which the runtime discovers because the workspace is the
// session's working directory. There is no per-task plugin to load. All names
// are BARE and kind-agnostic: the copies are already the filtered set, so the
// runner does no filtering of its own.
//
// The split between these two fields is the runtime's, not ours:
//
//   availableSkillNames → the ALLOWLIST. A mirrored skill absent from it is
//     rejected by the Skill tool outright, so this is every skill in the
//     mirror, not just the pinned ones. Listing only pins would leave the rest
//     of what the BFF decided this build may use as inert files on disk.
//
//   pinnedBodies → the actual preload. Nothing in the allowlist arrives in
//     context; the model gets names and descriptions, and a body only when it
//     invokes the skill. A pin says the guidance IS needed for this work, so its
//     body is appended to the system prompt instead of left to the model's
//     discretion. Empty string when nothing is pinned.
export interface PerTaskSkills {
  availableSkillNames: string[];
  pinnedBodies: string;
}

// Live access-token source for MCP (and bearer-file persistence on remint).
// canRefresh is true iff publisher CC creds are mounted — the same predicate
// local and cloud already share.
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
 * `agent-browser` is deliberately absent. It carries the browser mechanics a
 * validation run reaches for, and `acceptance-run` names it — a description
 * -triggered load is the right shape for mechanics a run may or may not need,
 * and paying for its body on every turn of every validation run is not.
 */
export function alwaysOnSkills(taskKind: DispatchRequest["taskKind"]): string[] {
  return taskKind === "validation" ? ["aep", "acceptance-run"] : ["aep"];
}

/**
 * The skills a run may LOAD on demand — the other half of the sentence above.
 *
 * The allowlist gates the Skill tool, so leaving `agent-browser` out of the
 * always-on set is only half a decision: absent from BOTH lists it is not
 * deferred, it is unreachable. That is what shipped — a validation run passed an
 * empty allowlist, so the load `acceptance-run` instructs could never succeed
 * and the agent grepped the mirror's files by hand instead.
 *
 * Named rather than "the whole mirror" as an implementation run gets: that run
 * may legitimately need any stack skill a `design.json` pinned, while a
 * validation run builds nothing and has exactly one mechanics skill to reach
 * for. Listing the mirror would readmit `go`, `ballerina` and every other stack
 * skill the checkout happens to carry. Extend this list when a validation run
 * genuinely needs something else; it is a statement of what the phase uses, not
 * a cap someone has to work around.
 */
export function onDemandSkills(taskKind: DispatchRequest["taskKind"]): string[] {
  return taskKind === "validation" ? ["agent-browser"] : [];
}

/**
 * Start a coding run and return the promise of its outcome.
 *
 * Reads as wiring on purpose: gather the run's facts, state the policy, hand it
 * to a runtime, consume the stream. Every step that used to be a Claude Code
 * detail is now one clause of `RuntimePolicy`.
 */
export async function startCodingRun(
  req: DispatchRequest,
  layout: WorkspaceLayout,
  log: TaskLog,
  perTaskSkills?: PerTaskSkills,
  mcpAuth?: McpAuthOpts,
  runtime: Runtime = createRuntime(runtimeNameFromEnv()),
): Promise<StartedRun> {
  // Spawn env. The AEP bearer is passed by FILE PATH (AEP_BEARER_FILE), so it
  // stays out of transcripts and out of a `ps` listing.
  //
  // Everything ELSE in the container environment does reach the agent: the
  // spread below is deny-nothing, so ANTHROPIC_API_KEY / CLAUDE_CODE_OAUTH_TOKEN,
  // GITHUB_TOKEN (which `gh auth git-credential` reads from env by design — see
  // gh_git_auth.ts) and the per-dependency secrets are all readable by the
  // agent's own Bash. This pod is a TRUST boundary, not a containment one, and
  // the controls sit on the way OUT rather than on concealment: the fail-closed
  // WebSearch/WebFetch DLP hooks below, and the progress scrubber primed from
  // credential_env.ts before this process logs anything.
  // F3c — surface AEP_TASK_ID and AEP_PLATFORM_URL to the agent's
  // child env so the aep skill's verification-failed shell snippet can
  // hit POST $AEP_PLATFORM_URL/api/v1/tasks/$AEP_TASK_ID/verification-failed.
  // The curl snippet reads AEP_BEARER_FILE at call time.
  const childEnv: Record<string, string> = {
    ...(process.env as Record<string, string>),
    PATH: `${layout.aepDir}:${process.env.PATH ?? ""}`,
    GH_CONFIG_DIR: layout.ghConfigDir,
    AEP_BEARER_FILE: layout.bearerFile,
    AEP_TASK_ID: req.taskId,
    AEP_PLATFORM_URL: process.env.AEP_PLATFORM_URL ?? "",
    AEP_GIT_SERVICE_URL: req.gitServiceUrl,
    AEP_CORRELATION_ID: req.correlationId ?? "",
    // A runtime's own default is typically 120s, which is under what this
    // platform's longest legitimate command takes: a Playwright spec is allowed
    // 30s, so a suite severs on a handful of them. A severed call proves nothing
    // — the command keeps running, its results are unread, and a validation run
    // that authored a full suite can end with none of it recorded. 600s is the
    // documented ceiling for a single shell call, so this raises the default to
    // the maximum already allowed rather than picking a number.
    BASH_DEFAULT_TIMEOUT_MS: "600000",
    // Where curl looks for `.curlrc`. Named explicitly rather than left to the
    // inherited HOME: a validation run writes `resolve` overrides there for its
    // deployed endpoints (endpoint_access.ts), and a config curl was never told
    // to look for is indistinguishable from no config at all. Harmless on a
    // coding run, which writes no such file.
    CURL_HOME: curlConfigHome(),
  };

  // Where the skills are, for the one skill that has to name a file inside them:
  // `acceptance-run` runs the platform's report checker rather than a copy the
  // repo committed. The runner is still the only layer that knows the path.
  childEnv.AEP_SKILLS_DIR = mirrorDir(layout.workspace);
  const skills = perTaskSkills?.availableSkillNames ?? [];
  // FATAL when the workflow is missing — see requireWorkflowBodies. This is the
  // structural guarantee that no entrypoint can start a coding run with no
  // procedure: it throws here, before a session exists, not in each caller.
  const workflowBodies = requireWorkflowBodies(layout.workspace, alwaysOnSkills(req.taskKind));

  // Secret candidates are read from childEnv, the SAME env record injected into
  // this run (see websearch_dlp.ts's stagedSecretValues doc comment): staged
  // dependency secrets (per-run K8s Secrets, mounted via envFrom) and the
  // runner's own credentials both land there before the session starts, so this
  // is the single source of truth for "what's secret in this run" without a
  // second, drift-prone channel. Both egress guards are built from it.
  const stagedSecrets = stagedSecretValues(childEnv);

  const deadline = runDeadlineFromEnv(process.env);
  // The one way anything in this file ends a run early. It is handed to the MCP
  // policy below and to the loop further down, and the loop is what acts on it
  // — see RunTerminator, and `onFatal` for the settle that used to be written
  // here instead.
  const terminator = createRunTerminator();

  const policy: RuntimePolicy = {
    workspace: layout.workspace,
    env: childEnv,
    // The organization's setting, stamped onto the Workload by the dispatcher.
    // Absent for a dispatch made before the setting existed, and for the
    // playground — both then get exactly the run they had.
    model: modelFromEnv(runtime.defaultModel),
    taskKind: req.taskKind,
    // Absent on a normal run, which is what keeps a prompt-bearing debug log out
    // of the cluster — see DispatchRequest.debug.
    debug: req.debug === true,
    logDir: log.dir,
    write: {
      // Authored files land in the project — see workspace_guard.ts. A run once
      // built a whole component into the run directory and finished green, so
      // the skill's "everything you produce goes inside it" needs an enforcer.
      allowOutsideProject: allowsWriteOutsideProject,
      onDenied: (reason) =>
        emit({ kind: "notice", level: "warn", code: "workspace_guard", detail: `[workspace] ${reason}` }),
    },
    // The whole platform list: a one-shot pod has no interactive user, no
    // scheduler, no durable session and no peer, whatever the runtime.
    deniedCapabilities: DENIED_CAPABILITIES,
    webSearch: { deny: webSearchDenial(stagedSecrets) },
    webFetch: { deny: webFetchDenial(stagedSecrets) },
    skills: {
      dir: mirrorDir(layout.workspace),
      allow: skills,
      // The whole appendix, in the one order the `aep` skill can be read in —
      // see systemPromptAppend. The glossary is the runtime's text, appended
      // last, and nothing may follow it.
      preloadBodies: systemPromptAppend(
        workflowBodies,
        perTaskSkills?.pinnedBodies ?? "",
        runtime.toolGlossary(),
      ),
    },
    ...buildMcpPolicy(req, layout, terminator, mcpAuth),
  };

  const session = await runtime.start(
    promptWithProjectRoot(req.prompt, layout.workspace, contractReferencePath(layout.workspace)),
    policy,
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
    emit({
      kind: "notice",
      level: "error",
      code: "terminated",
      detail: `[watchdog] terminated by ${signal} — ${watchdog.describe()}`,
    });
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
      // The loop is its own module (run_loop.ts) because its rule is the whole
      // point of it: the run settles when the STREAM closes, not on the first
      // `result` — a lead can end its turn with background subagents still
      // working, and returning there killed the pod with their work unread.
      return await consumeRun(session.stream, {
        translate: session.translate,
        watchdog,
        emit,
        record: (m) => log.write(m),
        requestedSkills: skills,
        deadline,
        terminator,
      });
    } finally {
      deadline?.cancel();
      stopWatchdog();
      process.removeListener("SIGTERM", onTerminate);
      process.removeListener("SIGINT", onTerminate);
      log.close();
      await session.close();
    }
  })();

  return { session, completion };
}

/**
 * The MCP clause of the policy, or nothing.
 *
 * Both the url and a token must be present — a URL-without-token dispatch must
 * omit the server rather than register it unauthenticated. Split out because the
 * bearer's lifecycle is the platform's (persist it to the bearer file, enrol it
 * with the scrubber, end the run when it can no longer be renewed) while the
 * mechanism that keeps a static header fresh is the runtime's.
 */
export function buildMcpPolicy(
  req: DispatchRequest,
  layout: WorkspaceLayout,
  terminator: RunTerminator,
  mcpAuth?: McpAuthOpts,
): Pick<RuntimePolicy, "mcp"> {
  if (!req.mcpUrl || !req.mcpToken) return {};
  const source = mcpAuth?.source ?? staticTokenSource(req.mcpToken);
  const canRefresh = mcpAuth?.canRefresh ?? false;
  let lastBearer = req.mcpToken;
  return {
    mcp: {
      url: req.mcpUrl,
      tools: MCP_TOOL_NAMES,
      token: () => source.getToken(),
      // Present only when this run can actually remint — see McpPolicy.
      ...(canRefresh ? { invalidate: () => source.invalidate() } : {}),
      onToken: async (token) => {
        lastBearer = await writeBearerFile(layout.bearerFile, token, lastBearer);
        primeScrubber([token]);
      },
      // The run is over — but saying so is the LOOP's job, not this callback's.
      // This used to emit its own `run_settled` while `consumeRun` was still
      // reading, so a fatal put two settles on one run's feed: the loop wrote
      // its own when the stream then closed, and every consumer treats the
      // first as terminal (`buildCrew` in `@aep/progress-view` settles every
      // agent it never heard close on one). Tripping the terminator states the
      // reason and lets the loop do the ending — stop the subagents still
      // running, name the cause in a `terminated` notice, write the single
      // settle, and return the exit code that follows it.
      //
      // No `process.exit` here any more either. Both entrypoints already exit
      // on the completion promise (`.then((code) => process.exit(code))`), so
      // the loop returning IS the exit; and a hard exit from this callback
      // could fire AFTER a healthy run had settled, because the proxy can fail
      // a straggling request while the session is closing. Tripping a
      // terminator nobody is racing any more is the right no-op there.
      onFatal: (err) => {
        terminator.terminate({
          source: "mcp auth",
          why: `this run's platform credential can no longer be renewed: ${err.message}`,
          error: `mcp auth: ${err.message}`,
        });
      },
    },
  };
}
