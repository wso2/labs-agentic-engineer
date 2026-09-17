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

// THE RUNTIME PORT: everything about a coding run that depends on WHICH agent
// runtime is driving it, stated once, in one file.
//
// Before this existed the answer was "`lib/runner.ts`, plus whatever else
// happened to import the SDK". The model id was a literal in the query options,
// the deny list was a Claude Code tool list, the three PreToolUse hooks were
// wired by hand to that runtime's hook grammar, and `settingSources`,
// `strictMcpConfig` and the MCP auth proxy were all there because of one
// runtime's limitations. None of that is policy about coding runs; it is one
// runtime's spelling of policy about coding runs. A second runtime would have
// had to fork the file.
//
// So the split is:
//
//   - THIS FILE says what a run may and may not do, in vocabulary no runtime
//     owns: where authored files may land, what a WebSearch query may contain,
//     which skills are reachable, which capabilities are denied outright.
//   - AN ADAPTER (`runtime/claude/`) enforces every one of those through
//     whichever mechanism its runtime actually has — a PreToolUse hook here, a
//     permission callback somewhere else — and translates that runtime's
//     messages into RUN EVENTS v2.
//   - `lib/runner.ts` becomes wiring: it builds the policy and hands it over.
//
// **There is exactly ONE implementation today: `claude-code`.** `opencode` is in
// `RuntimeName` because the organization setting's contract carries it and
// because a port with one implementation proves nothing about its own shape —
// but `runtime/registry.ts` refuses to build it, by name, with a reason. The
// design owes three live spikes (tool/permission parity, message-stream
// fidelity for the agent tree, and cost/usage reporting) before any OpenCode
// code is worth merging, and none of them has been run. A seam that says "not
// implemented" is the honest deliverable; a stub adapter would be a claim.
//
// ## Where this deviates from the design's sketch, and why
//
// The design (rev 6, §11) sketched this interface. The repo disagreed with it in
// six places and the repo won each time; each deviation is noted at its field.
// The largest is `RuntimeSession`: the sketch had `events(): AsyncIterable<
// RunEventInput>` as the session's whole output, and that shape cannot carry
// what `lib/run_loop.ts` measurably needs — see the note there.

import type { RunEventTranslator, RunStream } from "../lib/run_loop.js";

/**
 * The runtimes the organization setting can name.
 *
 * `claude-code` is the only one the platform can run. The value is the wire
 * spelling shared by the org-config contract (`AgentRuntime` in
 * `packages/contracts/api/v1/openapi.yaml`), the `AEP_AGENT_RUNTIME` env the
 * dispatcher stamps, and `lib/tool_glossary.ts`'s lookup key — one spelling, so
 * a mismatch is a type error rather than a run that silently gets no glossary.
 */
export type RuntimeName = "claude-code" | "opencode";

/** What the platform starts a run as when nobody has chosen. */
export const DEFAULT_RUNTIME: RuntimeName = "claude-code";

/** The two kinds of run this platform dispatches. */
export type TaskKind = "implementation" | "validation";

/**
 * A CLASS of tool a one-shot pod has no use for, named without naming a runtime.
 *
 * The sketch said `deniedTools: string[]` — "runtime-neutral names, mapped by
 * the adapter" — and there are no runtime-neutral tool names to write. The repo's
 * `DISALLOWED_TOOLS` is sixteen Claude Code identifiers (`ScheduleWakeup`,
 * `EnterWorktree`, …) and a second runtime's list would share none of them. What
 * the two lists WOULD share is the reason each entry is on it, which
 * `runners/AGENTS.md` already states as a sentence: the denied surface is the one
 * "that assumes an interactive user, a scheduler, a durable session or a peer to
 * talk to", none of which a one-shot pod has. So the port carries the reasons and
 * each adapter carries its own names — the same shape the tool GLOSSARY already
 * uses in the other direction, where the `aep` skill names roles and
 * `lib/tool_glossary.ts` binds them.
 *
 * This is deny-by-CLASS, not an exhaustive taxonomy of a runtime's tools:
 * anything a runtime offers that is not in one of these classes stays reachable.
 */
export type DeniedCapability =
  /** Anything that stops to ask a person: prompts, plan mode, feedback. */
  | "interactive_prompt"
  /** Wakeups, crons, workflow triggers — nothing outlives this pod. */
  | "scheduling"
  /** Worktrees and other state that assumes a session comes back. */
  | "durable_session"
  /** Messaging a peer or pushing a notification; there is no peer. */
  | "peer_messaging"
  /** Publishing an artifact somewhere; a run's output is its PR. */
  | "artifact_publishing";

/** Every class, for a caller that wants the platform's whole deny list. */
export const DENIED_CAPABILITIES: readonly DeniedCapability[] = [
  "interactive_prompt",
  "scheduling",
  "durable_session",
  "peer_messaging",
  "artifact_publishing",
];

/**
 * Where authored files may land, and who hears about a refusal.
 *
 * The sketch had `allowWriteUnder: string[]` — a list of permitted prefixes —
 * and that is a decision this repo already made and reversed. `lib/workspace_guard.ts`
 * allows the temp directory plus **any dot-directory directly under `$HOME`**,
 * as ONE RULE, because every toolchain hides its cache in one (`.ballerina`,
 * `.npm`, `.m2`, `.cargo`) and an enumerated list "silently contradicts the next
 * stack skill added" — its earlier version named three and did exactly that. A
 * predicate is the shape that rule fits; a prefix list is the shape it does not.
 */
export interface WritePolicy {
  /** Absolute path of an authored file outside the project: allowed? */
  allowOutsideProject(target: string): boolean;
  /** A refusal, so the run says on its own feed that it blocked something. */
  onDenied?(reason: string): void;
}

/**
 * A gate over one tool's input. Returns the REASON to deny, or null to allow.
 *
 * Kept as the sketch had it, because it is what the repo's own predicates
 * already are underneath: `checkWebSearchQuery` and the pair
 * `isSsrfUrl` + `checkUrlForSecret` each answer "deny, and here is the sentence
 * the agent gets to read". The runtime turns that sentence into whatever its
 * hook system calls a denial.
 */
export type DenialReason = (value: string) => string | null;

/**
 * The platform's one MCP server, as a run needs it.
 *
 * `token()` is called per REQUEST, not once at startup, and that is the whole
 * point of the field: the publisher client-credentials token expires inside a
 * three-hour run. How a runtime honours that is its own problem — Claude Code's
 * HTTP MCP config only accepts a static `Authorization` header, so its adapter
 * puts a loopback proxy in front (`lib/mcp_auth_proxy.ts`); another runtime may
 * simply call the function.
 *
 * `tools` are the BARE tool names the server exposes. Namespacing is a runtime
 * convention (Claude Code renders `mcp__<server>__<tool>`), so it happens in the
 * adapter and not here.
 */
export interface McpPolicy {
  url: string;
  /** A live bearer for the next request. */
  token(): Promise<string>;
  /**
   * Discard a bearer the server just rejected, so the next `token()` mints a
   * fresh one. ABSENT when this run cannot remint — a snapshot token, which is
   * every dispatch with no publisher credentials mounted.
   *
   * "Can this run get a new token" and "is there a stale one worth discarding"
   * are the same fact, so the port states it once instead of carrying a separate
   * `canRefresh` flag that could disagree with it.
   */
  invalidate?(): void;
  /** The bare tool names the server exposes. */
  tools: readonly string[];
  /** A newly minted token, so the caller can persist and scrub it. */
  onToken?(token: string): Promise<void>;
  /** The token can no longer be renewed — this run cannot continue. */
  onFatal?(err: Error): void;
}

/** What a run may read, and what it starts with already read. */
export interface SkillsPolicy {
  /** Absolute path of the `.claude/skills/` mirror in the project clone. */
  dir: string;
  /**
   * The skills this session may load AT ALL. An allowlist, not a preload: a
   * mirrored skill absent from it is rejected rather than deferred.
   */
  allow: readonly string[];
  /**
   * The system-prompt appendix, complete and in order: the run's workflow, then
   * the skills its design pinned, then the tool glossary.
   *
   * The sketch had `preloadBodies` as the skill bodies alone, with the glossary
   * left implicit. It cannot be: the `aep` skill points at "the tool glossary at
   * the end of your instructions", so the glossary's POSITION is part of the
   * contract and nothing may be appended after it. The caller assembles the
   * whole appendix (`systemPromptAppend`, `lib/runner.ts`) using the glossary
   * this runtime supplies, and the runtime appends nothing of its own.
   */
  preloadBodies: string;
}

/**
 * Watchers, never deciders.
 *
 * Both of these existed before the port and neither is a guard: the validation
 * run derives per-criterion progress from the calls it sees going out, and
 * settles a criterion from the same `ok` the feed reports. They are on the
 * policy because they are the platform's, and they are separate from the guards
 * above because a progress feature that could block a write would be a worse
 * bargain than no progress feature.
 */
export interface RuntimeObservers {
  /**
   * A tool call, before it runs. The return value is ignored — but a promise
   * is AWAITED, so a watcher that has to reach the outside world lands before
   * the call it describes. The validation status line does: its whole value is
   * that the line explaining a twenty-minute silence is posted before the
   * silence, not after it.
   */
  toolUse?(toolName: string, toolInput: unknown, toolUseId: string): void | Promise<void>;
  /** A plain tool call settling, with the same `ok` that reaches the feed. */
  toolOutcome?(toolUseId: string, ok: boolean): void;
}

/**
 * Everything a runtime needs to start this platform's kind of run.
 *
 * Read it as the sentence "a coding run may author files under X, may not do Y,
 * may search for Z, may load these skills, bills to this model" — and note that
 * no field names a tool, a hook, an SDK option or a runtime. That is the test
 * for whether something belongs here.
 */
export interface RuntimePolicy {
  /** Absolute project root. It is also the session's working directory. */
  workspace: string;
  /**
   * The child process environment for the session.
   *
   * Not in the sketch and not optional: `PATH`, `GH_CONFIG_DIR`,
   * `AEP_BEARER_FILE`, `AEP_SKILLS_DIR`, `BASH_DEFAULT_TIMEOUT_MS`, `CURL_HOME`
   * and the credential the run bills to all reach the agent this way, and it is
   * also the list the DLP predicates were built from. A runtime that could not
   * be handed an environment could not run this platform's workloads at all.
   */
  env: Record<string, string>;
  /**
   * The model this run bills to — the organization's setting, reaching the pod
   * as `AEP_AGENT_MODEL`.
   *
   * Pinned rather than left to the runtime's default, which drifts across
   * releases (seen live: an unpinned run resolved to `claude-sonnet-4-6`). The
   * platform can only stamp a cost for a model it has a `model_rates` row for,
   * so the settable list is narrower than the list a runtime can serve.
   */
  model: string;
  taskKind: TaskKind;
  /**
   * Developer diagnostics: the runtime's own debug log, its stderr, and whatever
   * per-token detail it can produce. Files under `logDir`, never the feed.
   */
  debug: boolean;
  /** Where a debug run's developer files go. Beside `claude.log`. */
  logDir: string;
  write: WritePolicy;
  deniedCapabilities: readonly DeniedCapability[];
  /** Deny a WebSearch query — a staged secret in it, today. */
  webSearch: { deny: DenialReason };
  /** Deny a WebFetch URL — SSRF, or a staged secret in it. */
  webFetch: { deny: DenialReason };
  skills: SkillsPolicy;
  /** Absent when the dispatch carried no MCP url or no token to present. */
  mcp?: McpPolicy;
  observe?: RuntimeObservers;
}

/** One runtime-owned file worth keeping, once the run is over. */
export interface RuntimeArtifact {
  /** The agent it belongs to; absent for a file about the run as a whole. */
  agentId?: string;
  path: string;
  /**
   * `transcript` — a spawned agent's own message log.
   * `log` — a developer file about the run (the runtime's debug log, stderr).
   *
   * The sketch also listed `task_output`. Nothing produces one: a task's output
   * reaches the feed as `agent_settled.report`, and a kind with no producer is a
   * claim about a shape nobody has seen.
   */
  kind: "transcript" | "log";
}

/**
 * A started run.
 *
 * **This is the sketch's biggest casualty.** It had one member —
 * `events(): AsyncIterable<RunEventInput>` — and the repo cannot express the run
 * that way without changing what the watchdog is told, which is load-bearing and
 * measured:
 *
 *   - `watchdog.observe([])` RESETS the idle clock (`progress/watchdog.ts`), and
 *     a heartbeat dropped by the rate limiter produces no event at all. So "route
 *     on the events that came back" silently converts every rate-limited wait
 *     into activity, which is the exact stall the heartbeat exists to report.
 *     `run_loop.ts` therefore routes by MESSAGE TYPE, and says so.
 *   - `observeRetry` and `observeStream` are two DIFFERENT non-activity signals,
 *     and the watchdog needs both to name a stall's cause. A flat event stream
 *     collapses them into "a notice arrived".
 *   - every raw message is written to `claude.log`, which a flat event stream no
 *     longer carries.
 *
 * So the session exposes the run at the level the loop actually reads it: the
 * runtime's `messages`, plus the `translate` that turns each one into canonical
 * events. `lib/run_loop.ts` is unchanged and stays the neutral owner of the one
 * rule that matters — a run settles when the stream CLOSES — and the classifiers
 * it consults (`progress/diagnostics.ts`) sit beside the adapter that knows the
 * same message shapes. Collapsing those into a flat event stream is a real
 * improvement and a real risk; it is a change to the watchdog's contract, not to
 * this port, and it is not this phase's to make.
 */
export interface RuntimeSession {
  /** The message stream and the task-stop, as `consumeRun` reads them. */
  readonly stream: RunStream;
  /** One runtime message → the canonical run events it produced, in order. */
  readonly translate: RunEventTranslator;
  /**
   * The files this run produced that outlive its messages.
   *
   * Async because a runtime may have to look: this one already knows, because
   * the adapter records each transcript from the runtime's own stop hook rather
   * than walking a session directory afterwards.
   *
   * Nothing uploads these. The design's storage decision is that transcripts
   * stay on the local plane, so this is the seam the recorder reads, not a
   * second channel out of the pod.
   */
  artifacts(): Promise<RuntimeArtifact[]>;
  /**
   * Release everything the session holds — the auth proxy, the debug sinks.
   *
   * `stream.messages` has ended by the time a caller reaches this; the sketch's
   * "events() ends after this" describes a cancel, which no caller wants: a run
   * that must be cut short is stopped through `stream.stopTask` so it can settle
   * and explain itself first (`run_loop.ts`'s deadline guard).
   */
  close(): Promise<void>;
}

/**
 * A coding-agent runtime.
 *
 * Stateless and cheap to build — `start` is where a run begins, and one process
 * starts exactly one.
 */
export interface Runtime {
  readonly name: RuntimeName;
  /**
   * The model a run uses when the organization has not chosen one.
   *
   * On the runtime rather than in the registry because "what does this thing run
   * by default" is the runtime's fact, and because the platform's default has to
   * be a model the runtime can actually serve.
   */
  readonly defaultModel: string;
  /**
   * The tool glossary for this runtime, ready to append to a system prompt.
   *
   * A STRING, not the sketch's `{fanOut, wait, stop, edit, write, shell}` record.
   * The repo already had this working (`lib/tool_glossary.ts`) and its content is
   * more than a name per role: it carries the model aliases a lead picks from and
   * the argument that makes each role work (`run_in_background: true`,
   * `block: true`). A record of bare names would drop exactly the part that
   * stopped leads guessing, and the caller would have to render it back into
   * prose anyway. Reformatting a working artefact to match a sketch is churn.
   */
  toolGlossary(): string;
  start(prompt: string, policy: RuntimePolicy): Promise<RuntimeSession>;
}

/**
 * Thrown when an organization names a runtime this build cannot run.
 *
 * A distinct type so a caller can report "the platform does not run that" rather
 * than a generic startup crash — and so the one place that refuses is
 * greppable when the second adapter does land.
 */
export class UnsupportedRuntimeError extends Error {
  readonly runtime: string;
  constructor(runtime: string, reason: string) {
    super(`coding-agent runtime ${JSON.stringify(runtime)} is not available: ${reason}`);
    this.name = "UnsupportedRuntimeError";
    this.runtime = runtime;
  }
}
