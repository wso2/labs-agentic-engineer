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

// One progress line → the text a human reads.
//
// The console and the playground watch the SAME run: identical events, from the
// same runner, through the same emitter. Only the presentation differs (React
// on a log surface vs a terminal), so only the presentation belongs to them —
// the wording is shared, and lives here.
//
// It was not always. Two independent formatters drifted into rendering the same
// event differently (`⚙ <summary>` against `$ <tool> <summary>`), which meant a
// wording defect could be invisible in the fast local loop and only show up in a
// cluster run — the slowest possible place to notice it.

/**
 * The subset of the runner's progress envelope that rendering needs. Declared
 * structurally rather than imported so this package stays free of both the
 * generated OpenAPI types and the runner's own — every caller's line type
 * already satisfies it.
 */
export interface ProgressLineView {
  kind: string;
  phase?: string | undefined;
  itemId?: string | undefined;
  tool?: string | undefined;
  summary?: string | undefined;
  command?: string | undefined;
  step?: string | undefined;
  sha?: string | undefined;
  files?: number | undefined;
  branch?: string | undefined;
  status?: string | undefined;
  error?: string | undefined;
  level?: string | undefined;
  message?: string | undefined;
  ok?: boolean | undefined;
  durationMs?: number | undefined;
  exitCode?: number | undefined;
  toolCount?: number | undefined;
  linesAdded?: number | undefined;
  linesRemoved?: number | undefined;
}

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
}

// Friendly labels for phase ids. Covers both the runner's own workspace phases
// and the BFF's synthetic "dark zone" markers (agent_progress.go) that narrate
// pod scheduling / image pull / boot — the stretch before the runner writes its
// first line. An unmapped phase falls back to its summary, then the raw id, so
// nothing hides.
const PHASE_LABELS: Record<string, string> = {
  runner_scheduling: "Waiting for a runner to be scheduled…",
  runner_unschedulable: "No capacity to schedule the runner on the cluster…",
  runner_pulling_image: "Pulling the agent image…",
  runner_image_pull_backoff: "Still pulling the agent image (retrying)…",
  runner_config_error: "Waiting on runner configuration and secrets…",
  runner_starting: "Starting the agent…",
  workspace_provisioning: "Setting up the workspace…",
  workspace_ready: "Workspace ready",
};

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

export function formatOutcome(e: ProgressLineView | undefined): OutcomeView {
  if (!e) return { detail: "", duration: "", tone: "muted" };
  const duration = e.durationMs && e.durationMs >= SLOW_CALL_MS ? formatDuration(e.durationMs) : "";
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

// The SDK's fan-out tool, under both names it has shipped under (`Agent` now,
// `Task` before). A tool_result naming one of these is not a step's outcome — it
// is a whole subagent's closing report.
const FANOUT_TOOLS = new Set(["Agent", "Task"]);

function isFanOutResult(e: ProgressLineView): boolean {
  return e.kind === "tool_result" && FANOUT_TOOLS.has(e.tool ?? "");
}

/**
 * What one subagent has to report, whether it is still going or has settled.
 * The figures come off the SDK's own Agent result — reported, not re-derived:
 * its totalDurationMs of 209158 matched a hand-measured 3m29s exactly, and
 * nothing in this feed can reconstruct a subagent's per-edit line counts.
 */
export interface SubagentReport {
  label: string;
  /** The SDK's verdict word where it gave one, else "running". */
  status: string;
  durationMs?: number | undefined;
  toolCount?: number | undefined;
  linesAdded?: number | undefined;
  linesRemoved?: number | undefined;
  /**
   * What it says it is doing right now. Earns its place HERE and nowhere else:
   * as the live status of work the reader has chosen not to expand.
   */
  activity?: string | undefined;
}

export function subagentReportFromResult(e: ProgressLineView): SubagentReport {
  return {
    label: e.summary || "subagent",
    status: e.status || (e.ok === false ? "failed" : "completed"),
    durationMs: e.durationMs,
    toolCount: e.toolCount,
    linesAdded: e.linesAdded,
    linesRemoved: e.linesRemoved,
  };
}

/**
 * One subagent, on one line. The single wording home for it: the console's
 * collapsed section header, the playground's settle row, and the playground's
 * end-of-run merged pass all render through this, so a fan-out reads the same
 * whichever surface you are watching.
 */
export function formatSubagentReport(r: SubagentReport): string {
  return `${r.label} ${formatSubagentStatus(r)}`;
}

/**
 * The same report without the label, for a surface that already names the
 * subagent beside it (the console puts the label in a chip, and repeating it
 * would say it twice on every section header).
 */
export function formatSubagentStatus(r: SubagentReport): string {
  const parts: string[] = [r.status];
  if (r.durationMs) parts.push(formatDuration(r.durationMs));
  if (r.toolCount) parts.push(`${r.toolCount} tool${r.toolCount === 1 ? "" : "s"}`);
  // The audit signal the user audience actually wants: how much code this
  // produced. Omitted when the SDK reported neither, rather than shown as +0/−0.
  if (r.linesAdded || r.linesRemoved) parts.push(`+${r.linesAdded ?? 0}/−${r.linesRemoved ?? 0} lines`);
  if (r.activity) parts.push(r.activity);
  return parts.join(" · ");
}

/**
 * One progress line → its text and weight.
 *
 * An empty `text` means the line is deliberately silent: it exists on the wire
 * for a machine reader but has nothing worth a row (a fast, successful
 * tool_result). Renderers drop those rather than emitting a blank line.
 */
export function formatLine(e: ProgressLineView): FormattedLine {
  switch (e.kind) {
    case "phase": {
      // Prefer the BFF/runner summary when present — bootstrap narration (esp.
      // unschedulable capacity detail) is more specific than the phase id label.
      const label = e.summary ?? (e.phase && PHASE_LABELS[e.phase]) ?? e.phase ?? e.message ?? "phase";
      return { text: `▸ ${label}`, tone: "info" };
    }
    case "tool_use": {
      // A tool call carries a bare argument ("src/App.tsx"), meaningless without
      // the verb, so the tool name is printed.
      //
      // An EMPTY tool is a positive fact, not missing data: it means the summary
      // is already a whole sentence containing its own verb. A runner old enough
      // to translate a subagent's narration into a tool_use still sends that
      // shape, and stamping the name on it printed "$ Read Reading src/App.tsx"
      // in a live run. Current runners send an `activity` instead, but the
      // console and a cluster can be on different images, so the rule stays.
      const summary = e.summary ?? "";
      const tool = e.tool ?? "";
      if (!summary) return { text: `$ ${tool || "tool"}`, tone: "muted" };
      // For Bash the `$` prompt already says "shell", so its name is noise.
      if (!tool || tool === "Bash") return { text: `$ ${summary}`, tone: "muted" };
      return { text: `$ ${tool} ${summary}`, tone: "muted" };
    }
    case "activity":
      // Header material, never a row — see the runner's ActivityEvent.
      return { text: "", tone: "muted" };
    case "progress_item":
      // Row STATE, never a row. The line says an item's status changed
      // (`AC-003-a` → `authoring`); the surface that folds it repaints that
      // item's existing row, and printing the transition here as well would
      // narrate the same fact twice — one criterion moving through five
      // statuses is five lines interleaved with the tool calls that caused
      // them. Silent for the same reason `activity` above is.
      //
      // Explicit rather than left to the default arm below, which would also
      // return "" today only because the payload happens to carry neither
      // `message` nor `summary`. That is an accident of the shape, not a
      // decision, and it would start printing the moment either is added.
      return { text: "", tone: "muted" };
    case "tool_result": {
      // A fan-out call's result settles a WHOLE subagent, so it reads as that
      // subagent's report rather than as one call's outcome.
      if (isFanOutResult(e)) {
        return {
          text: `▪ ${formatSubagentReport(subagentReportFromResult(e))}`,
          tone: e.ok === false ? "error" : "success",
        };
      }
      // The standalone form of an outcome, for a surface that cannot go back and
      // rewrite the action row it belongs to (a terminal). The console merges
      // the same OutcomeView onto that row instead; both read the same source,
      // so neither can drift into saying something the other doesn't.
      const { detail, duration, tone } = formatOutcome(e);
      if (!detail && !duration) return { text: "", tone: "muted" };
      const glyph = e.ok === false ? "✗" : "↳";
      // The tool is named because the outcome may be several rows below its
      // action once subagents interleave the feed.
      const parts = [glyph, e.tool || "tool", e.ok !== false ? e.summary : "", detail, duration];
      return { text: parts.filter(Boolean).join(" "), tone };
    }
    case "git_commit":
      return {
        text: `✓ commit ${e.sha?.slice(0, 7) ?? ""}${e.files ? ` · ${e.files} files` : ""}`.trimEnd(),
        tone: "success",
      };
    case "git_push":
      return { text: `↑ push${e.branch ? ` ${e.branch}` : ""}`, tone: "success" };
    case "gh_action":
    case "build_step":
      // gh_action's payload is its command; build_step's is step/summary.
      return {
        text: `⚙ ${e.step ?? e.summary ?? e.command ?? e.kind}${e.status ? ` — ${e.status}` : ""}`,
        tone: e.status === "failed" ? "error" : "info",
      };
    case "result":
      return {
        text: `■ ${e.summary ?? e.status ?? "finished"}${e.error ? ` — ${e.error}` : ""}`,
        tone: e.error || e.status === "failed" ? "error" : "success",
      };
    default: {
      const tone: LineTone = e.level === "error" ? "error" : e.level === "warn" ? "warn" : "default";
      return { text: e.message ?? e.summary ?? "", tone };
    }
  }
}
