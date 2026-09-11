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

// What a rendered row IS, independent of which envelope version produced it.
//
// Two envelopes now feed this package — the v1 progress line the task log still
// carries, and the v2 RunEvent the run feed carries — and they render into the
// same thing: a piece of text with a semantic weight, plus whatever its outcome
// added. Keeping that vocabulary here rather than in either formatter is what
// stops the second one drifting into its own tone set or its own duration
// wording, which is the exact defect this package exists to prevent.

/**
 * Semantic weight of a line — never a theme token. The console maps these to
 * Oxygen palette entries and the terminal maps them to nothing (or to ANSI);
 * leaking `grey.400` into a package a TUI imports would make one surface's
 * design system everyone's problem.
 */
export type LineTone = "default" | "muted" | "info" | "success" | "warn" | "error";

export interface FormattedLine {
  text: string;
  tone: LineTone;
  /**
   * Prose that belongs to this row but not ON it — an agent's closing report,
   * which runs to a paragraph. A surface renders it under the row, wrapped, and
   * a surface with one line to spend drops it; both are correct, which is why it
   * is a separate field rather than something appended to `text`.
   *
   * Only `agent_settled` carries one. It is the field the flat form gained in
   * v2 and the one thing the old UI could not show at all.
   */
  report?: string | undefined;
}

// Below this, a call is fast enough that its duration is noise on every line.
// Above it, the number is the point: it is what tells a slow build apart from a
// wedged one.
export const SLOW_CALL_MS = 3_000;

export function formatDuration(ms: number): string {
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  // Round to whole seconds FIRST, then split. Rounding the two components
  // independently is what printed 353s as "6m53s" — the minutes rounded up
  // while the seconds kept the remainder — and could also produce "5m60s".
  const seconds = Math.round(ms / 1000);
  return `${Math.floor(seconds / 60)}m${seconds % 60}s`;
}

/** Bytes as a reader reads them. Used for a backgrounded task's output size. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${String(bytes)} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * The fields an OUTCOME is judged on, in either envelope. Declared as its own
 * shape so one `formatOutcome` serves both: a v1 tool_result and a v2
 * `tool_result` differ in their attribution, never in how a failure reads.
 */
export interface CallOutcome {
  ok?: boolean | undefined;
  status?: string | undefined;
  summary?: string | undefined;
  durationMs?: number | undefined;
  exitCode?: number | undefined;
  /** `task_settled` only — a backgrounded command's ending is judged on its
   *  status, having no `ok` of its own. */
  kind?: string | undefined;
}

/**
 * What a call's OUTCOME adds to what its action already said — split into the
 * diagnosis and the duration so each surface can place them independently (the
 * console right-aligns the duration in its own column; a terminal pads).
 *
 * Both empty means the outcome is deliberately silent: a fast successful call
 * carries nothing the next action appearing doesn't already prove. That is the
 * feed's governing rule — an action always earns a line, an outcome only when it
 * carries something the action didn't.
 */
export interface OutcomeView {
  /** "exit 1 · cannot resolve module" | "failed · File does not exist" | "" */
  detail: string;
  /** "10.6s" when abnormally slow, else "" — normal timings are noise. */
  duration: string;
  tone: LineTone;
}

export function formatOutcome(e: CallOutcome | undefined): OutcomeView {
  if (!e) return { detail: "", duration: "", tone: "muted" };
  const duration = e.durationMs && e.durationMs >= SLOW_CALL_MS ? formatDuration(e.durationMs) : "";

  // A BACKGROUNDED command's ending always speaks, including when it succeeded.
  // That is the one exception to the silent-success rule above it, and it earns
  // the exception: the action row said the command was detached, so "it is done"
  // is news the action did not carry. A foreground call proves it finished by
  // the next action appearing; a background one does not.
  if (e.kind === "task_settled") {
    const status = e.status ?? "completed";
    const tone: LineTone = status === "failed" ? "error" : status === "completed" ? "success" : "warn";
    return { detail: status, duration, tone };
  }

  if (e.ok !== false) return { detail: "", duration, tone: "muted" };

  // A SEVERED call is not a failure and must not be called one. The command
  // blew its timeout and was detached, so it neither succeeded nor failed — it
  // has no outcome yet, and it may still be running. Saying "failed" would name
  // a defect that did not happen, on the one line a reader consults to tell
  // those apart. Warned rather than errored for the same reason.
  if (e.status === "severed") {
    return {
      detail: e.summary ? `severed · ${e.summary}` : "severed",
      duration,
      tone: "warn",
    };
  }

  // A failure always speaks. `exit N` is the honest per-step signal — it names
  // THIS command as what broke. Tools that are not a shell report no code at
  // all, and inventing one would be worse than the bare word: "failed" is
  // exactly as much as is known.
  const cause = e.exitCode === undefined ? "failed" : `exit ${e.exitCode}`;
  return {
    detail: e.summary ? `${cause} · ${e.summary}` : cause,
    duration,
    tone: "error",
  };
}

/**
 * The dark zone: everything before the run's first model turn.
 *
 * Six are read off pod truth by the platform, two reported by the runner once it
 * has a process but no session yet. They are the slowest stretch of a run, so
 * they are also the part a reader is most likely to be staring at.
 *
 * They live HERE, in the vocabulary both envelopes share, because both render
 * them: v1 carries them as a `phase` kind, v2 as a `notice` code. Two copies of
 * one sentence is how a reader comes to see the wording change when nothing but
 * the envelope did — which is the defect these strings were moved out of the
 * producers to fix in the first place.
 */
export const LIFECYCLE_LABELS: Record<string, string> = {
  runner_scheduling: "Waiting for a runner to be scheduled…",
  runner_unschedulable: "No capacity to schedule the runner on the cluster…",
  runner_pulling_image: "Pulling the agent image…",
  runner_image_pull_backoff: "Still pulling the agent image (retrying)…",
  runner_config_error: "Waiting on runner configuration and secrets…",
  runner_starting: "Starting the agent…",
  workspace_provisioning: "Setting up the workspace…",
  workspace_ready: "Workspace ready",
};
