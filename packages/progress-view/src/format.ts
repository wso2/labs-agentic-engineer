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

// One v1 progress line → the text a human reads.
//
// The v1 envelope, which the TASK LOG still carries (its per-execution
// TimelineEvent stream has not moved). The run feed reads v2 RunEvents through
// `event.ts`; both render into the same FormattedLine so the two envelopes
// cannot start wording the same fact differently while both are in flight.
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

import { formatAgentReport, type AgentReport } from "./agent.js";
import {
  LIFECYCLE_LABELS,
  formatOutcome,
  type FormattedLine,
  type LineTone,
} from "./line.js";

/**
 * The subset of the runner's v1 progress envelope that rendering needs. Declared
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

// Friendly labels for phase ids. Covers both the runner's own workspace phases
// and the BFF's synthetic "dark zone" markers (agent_progress.go) that narrate
// pod scheduling / image pull / boot — the stretch before the runner writes its
// first line. An unmapped phase falls back to its summary, then the raw id, so
// nothing hides.
const PHASE_LABELS: Record<string, string> = LIFECYCLE_LABELS;

// The SDK's fan-out tool, under both names it has shipped under (`Agent` now,
// `Task` before). A tool_result naming one of these is not a step's outcome — it
// is a whole subagent's closing report.
//
// v2 needs no such inference: an `agent_settled` event says so outright.
const FANOUT_TOOLS = new Set(["Agent", "Task"]);

function isFanOutResult(e: ProgressLineView): boolean {
  return e.kind === "tool_result" && FANOUT_TOOLS.has(e.tool ?? "");
}

/**
 * A v1 fan-out result read as the subagent report it actually is. The id is the
 * tool call's, because a v1 feed has no agent ids — which is the whole reason v2
 * declares them.
 */
function subagentReportFromResult(e: ProgressLineView, id = ""): AgentReport {
  return {
    id,
    label: e.summary || "subagent",
    status: e.status || (e.ok === false ? "failed" : "completed"),
    durationMs: e.durationMs,
    toolCount: e.toolCount,
    linesAdded: e.linesAdded,
    linesRemoved: e.linesRemoved,
  };
}

/**
 * One v1 progress line → its text and weight.
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
          text: `▪ ${formatAgentReport(subagentReportFromResult(e))}`,
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
        text: `✓ commit ${e.sha?.slice(0, 7) ?? ""}${e.files ? ` · ${String(e.files)} files` : ""}`.trimEnd(),
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
