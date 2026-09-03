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

// Versioned NDJSON envelope emitted by the coding-agent runner to stdout, and
// the SOURCE OF TRUTH for the progress contract — there is no design doc above
// it. `console.*` output is on the same fd and goes through the same envelope
// (see console_scrub.ts), so every byte on that stream is one of these.
// The Go mirror is services/aep-api/internal/contracts/progress.go, and the
// console-facing shapes (ProgressEvent / RunProgressLine / TimelineEvent) live
// in packages/contracts/api/v1/openapi.yaml — change this file and all four
// move together, contract-first (`make gen-api`).

export const PROGRESS_SCHEMA_VERSION = 1 as const;

export type ProgressKind =
  | "phase"
  | "tool_use"
  | "activity"
  | "tool_result"
  | "git_commit"
  | "git_push"
  | "gh_action"
  | "log"
  | "progress_item"
  | "result";

// Who produced the event: the main agent, or one of the subagents it fans out
// to with the Task tool. Absent means "main" — the field only appears on
// subagent lines, so an older consumer reads an unlabelled feed exactly as
// before. The SDK's `parent_tool_use_id` is the discriminator: it is non-null
// precisely on messages forwarded from inside a Task tool call.
export type ProgressEmitter = "main" | "subagent";

interface ProgressEnvelope {
  schemaVersion: typeof PROGRESS_SCHEMA_VERSION;
  ts: string;
  seq: number;
  kind: ProgressKind;
  emitter?: ProgressEmitter;

  // WHICH subagent, for the runs that fan out to several at once. `emitter`
  // alone collapses them: a milestone cycle routinely runs two or three
  // concurrently and their lines interleave, so a reader cannot tell one
  // component's work from another's. emitterId is the fan-out tool call's id
  // (stable for that subagent's whole life) and emitterLabel is the
  // description the main agent gave it ("Implement todo-api service (issue
  // #3)"). Both are absent on main-agent lines, same rule as `emitter`.
  emitterId?: string;
  emitterLabel?: string;

  // The tool call this line is about, for the kinds derived from one
  // (tool_use, tool_result, and the git_*/gh_action rewrites of a Bash call).
  // A tool_result carries the id of the tool_use it answers, which is the only
  // way to pair a call with its outcome once subagents interleave the feed.
  toolUseId?: string;
}

export interface PhaseEvent extends ProgressEnvelope {
  kind: "phase";
  phase: string;
}

export interface ToolUseEvent extends ProgressEnvelope {
  kind: "tool_use";
  tool: string;
  summary: string;
}

// What a subagent says it is doing right now — the SDK's own sentence off a
// task_progress message ("Writing todo-api/service.bal"), plus how many tool
// calls it has made so far.
//
// It is NOT a row. The phrase earns its place in exactly one spot: the live
// status of a collapsed subagent section, work the reader has chosen not to
// look at. Inline beside the raw commands it is status text rather than
// progress — "Running List project root contents" next to `ls` says nothing the
// command didn't. Renderers must therefore format it to no text; only the
// section header reads it.
export interface ActivityEvent extends ProgressEnvelope {
  kind: "activity";
  summary: string;
  toolCount?: number;
}

// The outcome of a tool call, paired to its tool_use by toolUseId. `ok: false`
// is the SDK's own is_error — without this event a failed call is invisible in
// the feed and reads as a success. durationMs is measured runner-side between
// the two messages, so it includes the model's turnaround, not just the tool.
// summary is populated only on failure: the error text is the diagnostic
// payload, whereas successful output is bulky, uninteresting, and the more
// likely place for a secret to surface.
//
// exitCode is the process status of a failed shell call, parsed from the SDK's
// own `Exit code N` first line. It is the honest per-step failure signal: a
// non-zero code says THIS command is what broke, where the surrounding prose
// only says the agent is unhappy. Absent when the tool was not a shell (those
// report `<tool_use_error>` with no code) — absence means "no code was
// reported", never "exited 0".
//
// The three totals below appear ONLY on a fan-out call's result, where the SDK
// hands us authoritative figures for the whole subagent (its own duration, tool
// count, and the lines its edits added and removed). They are what a settled
// subagent section reports, and they cannot be derived from this feed: a
// subagent's per-edit line counts are never in its own events.
export interface ToolResultEvent extends ProgressEnvelope {
  kind: "tool_result";
  tool?: string;
  ok: boolean;
  durationMs?: number;
  summary?: string;
  exitCode?: number;
  /** The SDK's own verdict word, when it gave one ("completed", "failed", …). */
  status?: string;
  toolCount?: number;
  linesAdded?: number;
  linesRemoved?: number;
}

export interface GitCommitEvent extends ProgressEnvelope {
  kind: "git_commit";
  sha?: string;
  files?: number;
  summary?: string;
}

export interface GitPushEvent extends ProgressEnvelope {
  kind: "git_push";
  sha?: string;
  branch?: string;
  summary?: string;
}

export interface GhActionEvent extends ProgressEnvelope {
  kind: "gh_action";
  command: string;
  summary?: string;
}

export interface LogEvent extends ProgressEnvelope {
  kind: "log";
  level?: "info" | "warn" | "error";
  summary: string;
}

// What a `progress_item` may say about one item. The terminal two are
// report.json's OWN words (`pass`/`fail`, see aep-validation's
// generate-report.mjs), deliberately not `passed`/`failed`: the console
// overlays these live statuses onto the same rows it later fills from that
// report, and a second spelling for one fact would need a translation table
// between them. `not_run` is absent because only the report can conclude it —
// nothing observed DURING a run proves a criterion was never attempted.
//
// `healing` means a spec that once passed has broken, matching what
// aep-validation's healing.md scopes a heal to. An edit before an item's first
// pass is still `authoring`: authoring.md requires a spec to pass twice
// consecutively, so failing on the way there is the normal path, and calling it
// healing would make a healthy run read as a struggling one.
export type ProgressItemStatus =
  | "planned"
  | "exploring"
  | "authoring"
  | "running"
  | "healing"
  | "pass"
  | "fail";

// One named unit of work whose status changes over a run — as opposed to every
// other kind here, which reports something that happened once at a moment.
// `itemId` is the stable key a consumer folds on: many of these lines describe
// the SAME item, and a reader wants one row repainted rather than seven rows
// printed.
//
// Validation is the first caller and binds items to acceptance criteria
// (`AC-003-a`). Nothing here says so, and nothing needs to: `cycleKind` is
// already stamped on every line the BFF serves, so the same kind carries a
// coding cycle's issues or a build's steps without a second field repeating
// what the envelope already said.
//
// Renderers format this to no text — the structure IS the payload, and the
// surface that folds it (the console's Validation page) shows it as row state
// rather than as log rows. See @aep/progress-view's formatLine.
export interface ProgressItemEvent extends ProgressEnvelope {
  kind: "progress_item";
  itemId: string;
  status: ProgressItemStatus;
}

// TurnModelUsage is one model's slice of the run's usage, read off the SDK
// result's per-model `modelUsage` record (#291). The model id is the SDK's
// canonicalModel where it reports one (versioned release ids collapse onto the
// alias aep-api's model_rates table is keyed by), else the record key.
export interface TurnModelUsage {
  model: string;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
}

// TurnUsage is the run's token usage off the SDK result (#291) — the cross-
// runtime wire shape aep-api parses (usageFromLog → contracts.CapturedUsage),
// so the camelCase field names must match byte-for-byte. model is "" when the
// run spanned multiple models (or the SDK reported none); `models` is the
// per-model split that lets aep-api price a multi-model run — without it a
// single blanked model id made the whole run unpriceable and the console fell
// back to token counts.
export interface TurnUsage {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  model: string;
  models?: TurnModelUsage[];
}

export interface ResultEvent extends ProgressEnvelope {
  kind: "result";
  status: "success" | "failure";
  summary?: string;
  error?: string;
  // Token usage for the whole coding run (#291), present on a successful
  // result; aep-api stamps its USD cost onto the execution row.
  usage?: TurnUsage;
}

export type ProgressEvent =
  | PhaseEvent
  | ToolUseEvent
  | ActivityEvent
  | ToolResultEvent
  | GitCommitEvent
  | GitPushEvent
  | GhActionEvent
  | LogEvent
  | ProgressItemEvent
  | ResultEvent;

// Attribution the translator knows and emit() cannot: which agent produced the
// line and which tool call it belongs to. Carried alongside every payload
// below rather than per-kind, because a Bash call rewritten into a git_commit
// is still the same subagent's same tool call.
export interface ProgressAttribution {
  emitter?: ProgressEmitter;
  emitterId?: string;
  emitterLabel?: string;
  toolUseId?: string;
}

// Discriminated union of payloads (no envelope fields except the attribution
// above). The emitter stamps schemaVersion / ts / seq itself so callers cannot
// forget.
export type ProgressEventInput = (
  | { kind: "phase"; phase: string }
  | { kind: "tool_use"; tool: string; summary: string }
  | { kind: "activity"; summary: string; toolCount?: number }
  | {
      kind: "tool_result";
      tool?: string;
      ok: boolean;
      durationMs?: number;
      summary?: string;
      exitCode?: number;
      status?: string;
      toolCount?: number;
      linesAdded?: number;
      linesRemoved?: number;
    }
  | { kind: "git_commit"; sha?: string; files?: number; summary?: string }
  | { kind: "git_push"; sha?: string; branch?: string; summary?: string }
  | { kind: "gh_action"; command: string; summary?: string }
  | { kind: "log"; level?: "info" | "warn" | "error"; summary: string }
  | { kind: "progress_item"; itemId: string; status: ProgressItemStatus }
  | { kind: "result"; status: "success" | "failure"; summary?: string; error?: string; usage?: TurnUsage }
) & ProgressAttribution;
