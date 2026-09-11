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

// One v2 RunEvent → the text a human reads.
//
// The difference from v1 that shows on screen: agents are DECLARED. v1 stamped a
// line `main` or `subagent` and a reader inferred the rest, which cannot
// describe a run that fans out to a dozen agents at three depths. Here
// `agent_started` opens an agent and `agent_settled` closes it, carrying the
// label, role, depth, background flag and — the thing no v1 surface could show
// at all — the agent's own closing REPORT.
//
// Every kind of the contract's RunEventKind has a case below. That is the point
// of the exhaustiveness: an unhandled kind renders as a blank row, and a blank
// row in a progress feed is indistinguishable from a run that stopped talking.

import { formatAgentReport, agentTone, type AgentReport, LEAD_AGENT_ID } from "./agent.js";
import {
  LIFECYCLE_LABELS,
  formatBytes,
  formatDuration,
  formatOutcome,
  type FormattedLine,
  type LineTone,
} from "./line.js";

/**
 * The subset of the contract's RunEvent that rendering needs. Declared
 * structurally rather than imported so this package stays free of both the
 * generated OpenAPI types and the runner's own — the console's generated
 * `RunEvent` and the runner's NDJSON line both already satisfy it.
 *
 * Every field is optional but `kind` and `agentId`, which the contract requires
 * of every kind: a producer one version ahead must degrade to a readable row,
 * never to a crash.
 */
export interface RunEventView {
  kind: string;
  agentId: string;
  parentAgentId?: string | undefined;
  seq?: number | undefined;
  ts?: string | undefined;
  /** `agent_started`/`agent_progress`/`agent_settled`: the parent's description. */
  label?: string | undefined;
  /** `agent_started`: the runtime's own kind for the agent (`inferred` when deduced). */
  role?: string | undefined;
  depth?: number | undefined;
  background?: boolean | undefined;
  model?: string | undefined;
  runtime?: string | undefined;
  taskKind?: string | undefined;
  /** `agent_progress`: what the agent says it is doing, as a REPLACING state. */
  phrase?: string | undefined;
  /** `agent_settled`/`task_settled`: AgentStatus. */
  status?: string | undefined;
  /** `agent_settled`: the agent's closing summary — the only copy of it. */
  report?: string | undefined;
  durationMs?: number | undefined;
  toolCount?: number | undefined;
  tokens?: number | undefined;
  linesAdded?: number | undefined;
  linesRemoved?: number | undefined;
  tool?: string | undefined;
  toolUseId?: string | undefined;
  summary?: string | undefined;
  command?: string | undefined;
  ok?: boolean | undefined;
  exitCode?: number | undefined;
  /** `task_started`/`task_settled`: the backgrounded command's own id. */
  taskId?: string | undefined;
  outputBytes?: number | undefined;
  sha?: string | undefined;
  files?: number | undefined;
  branch?: string | undefined;
  /** `work_item`: which named unit of work, and where it now stands. */
  itemId?: string | undefined;
  itemStatus?: string | undefined;
  source?: string | undefined;
  title?: string | undefined;
  ownerAgentId?: string | undefined;
  /** `heartbeat`: what the run is blocked on, and for how long. */
  waitingOn?: string | undefined;
  ref?: string | undefined;
  elapsedMs?: number | undefined;
  /** `notice`: how loudly, which condition, and the readable half. */
  level?: string | undefined;
  code?: string | undefined;
  detail?: string | undefined;
  /** `turn_ended`/`run_settled`. */
  outcome?: string | undefined;
  error?: string | undefined;
}

/** The kinds that carry no row at all — see `isSilentKind`. */
const SILENT_KINDS = new Set(["agent_progress", "work_item", "heartbeat", "task_started"]);

/**
 * Kinds that are STATE, not rows: a surface repaints something it already drew
 * from them and prints nothing.
 *
 * `agent_progress` replaces its agent's live phrase, `work_item` repaints one
 * named unit of work's row (one criterion moving through five statuses is one
 * row repainted, not five rows printed), and `heartbeat` says only that the
 * silence is explained. Exported because a consumer that folds them still wants
 * to know, without duplicating the list, that formatting them is a no-op.
 *
 * `task_started` is here for a different reason and it is the one worth
 * spelling out: it is a DUPLICATE, not a state. One backgrounded command puts
 * three events on the wire — the `tool_use` (or `gh_action`) carrying the
 * command, then a `task_started` carrying the same command plus the id the
 * runtime minted for it, then a `task_settled`. Printing all three made 47
 * background launches 141 rows in one live run (2026-09-08), two of them
 * identical text. The fold belongs HERE rather than in the producer because the
 * event is not redundant on the wire: `buildCrew` opens its background-task row
 * from `task_started`, so a task that is merely still running would otherwise
 * be invisible until it settled. What is redundant is the ROW, and a row is a
 * renderer's word. The action is announced once by the `tool_use` and the
 * outcome once by `task_settled` — one action, one outcome, the same shape every
 * other tool call has.
 */
export function isSilentKind(kind: string): boolean {
  return SILENT_KINDS.has(kind);
}

// A fan-out has NO tool row in v2, and this renderer does not look for one.
//
// The contract's rule: "Runtime names never appear. `tool` carries the runtime's
// tool name for a step (that is what the row prints), but no consumer branches
// on it: fan-out is `agent_started`, not a `tool_result` whose tool is called
// Agent." This module used to hold exactly that forbidden branch, suppressing a
// `tool_use`/`tool_result` whose tool was `Agent` or `Task`. Nothing produces
// those: the claude adapter emits no call for a spawn (the `agent_started` IS
// its row) and the v1 lift turns a fan-out result into `agent_settled`. The
// branch was kept alive only by a mock fixture that invented the pair.
// `format.ts` keeps its own copy, because that renders the V1 envelope, where a
// fan-out result genuinely is the subagent's report.

// `notice.code` is a closed set, so it gets sentences rather than snake_case.
// An unmapped code still prints — as its raw code — because a condition the
// platform bothered to raise must never render as an empty row.
//
// No producer ships its own sentence: that is how the console and the playground
// began describing one fact two ways. The eight dark-zone codes come from
// `LIFECYCLE_LABELS`, which v1's phase renderer reads too, so one envelope
// cannot drift from the other.
const NOTICE_LABELS: Record<string, string> = {
  api_retry: "retrying after a model error",
  compaction: "context compacted",
  refusal: "the model declined to answer",
  rate_limit: "rate limited by the provider",
  permission_denied: "a tool call was denied",
  terminated: "the run was terminated from outside",
  workspace_guard: "a write outside the workspace was denied",
  // Deliberately "missing" and not "lost": the platform raises this code the
  // moment it notices a hole, with the pod still running and the recorder about
  // to ask for that stretch of log again. Whether the events are GONE is the
  // producer's to say in `detail` — "not captured yet" while they can still be
  // fetched, "nothing can recover it now" once the recording closes short.
  gap: "events are missing from this feed",
  artifact_failed: "an artifact could not be stored",
  ...LIFECYCLE_LABELS,
};

const NOTICE_GLYPHS: Record<string, string> = { info: "ℹ", warn: "⚠", error: "✗" };

// What the run is waiting on, said as a wait rather than as a field value.
const WAITING_LABELS: Record<string, string> = {
  tool: "a tool call",
  model: "the model",
  agent: "a spawned agent",
};

/**
 * An agent's human name. The label its parent gave it, else the reusable role,
 * else the runtime's opaque id — which is a poor name but a true one, and better
 * than a section titled nothing.
 */
export function agentName(e: {
  agentId: string;
  label?: string | undefined;
  role?: string | undefined;
}): string {
  if (e.label) return e.label;
  if (e.role) return e.role;
  return e.agentId === LEAD_AGENT_ID ? "lead agent" : e.agentId;
}

/** The report an `agent_started` opens, before anything has settled it. */
export function agentReportFromStarted(e: RunEventView): AgentReport {
  return {
    id: e.agentId,
    label: agentName(e),
    role: e.role,
    status: "running",
    background: e.background,
    // The producer declared this agent. Everything else that opens a report —
    // a first sighting, a settle with no start — leaves it unset, which is what
    // stops a surface treating an unannounced author as an announced one.
    declared: true,
  };
}

/** Fold an `agent_settled` onto the report its `agent_started` opened. */
export function settleAgentReport(open: AgentReport, e: RunEventView): AgentReport {
  return {
    ...open,
    // A label can arrive on the settle when the start was never seen (a feed
    // joined mid-run); take a real one over a placeholder id.
    label: open.label === open.id ? agentName(e) : open.label,
    status: e.status ?? "completed",
    durationMs: e.durationMs,
    toolCount: e.toolCount,
    tokens: e.tokens,
    linesAdded: e.linesAdded,
    linesRemoved: e.linesRemoved,
    report: e.report,
    // The live phrase describes work that is over; keeping it would leave a
    // settled agent claiming to still be writing a file.
    activity: undefined,
  };
}

/** The counters and prose an `agent_settled` carries, as one row. */
function settledLine(e: RunEventView): FormattedLine {
  const status = e.status ?? "completed";
  return {
    text: `▪ ${formatAgentReport(settleAgentReport(agentReportFromStarted(e), e))}`,
    tone: agentTone(status),
    report: e.report,
  };
}

/**
 * One v2 event → its text and weight.
 *
 * An empty `text` means the event is deliberately silent: it exists on the wire
 * for a machine reader but has nothing worth a row (a fast successful
 * tool_result, or one of the three state kinds). Renderers drop those rather
 * than emitting a blank line.
 */
export function formatEvent(e: RunEventView): FormattedLine {
  switch (e.kind) {
    case "run_started": {
      // What this attempt IS: what it was dispatched to do, on which runtime and
      // model. All three are read back long after the org's settings moved on,
      // which is why the event records them rather than a reader looking them up.
      const parts = [e.taskKind ? `${e.taskKind} run` : "run", e.runtime, e.model].filter(Boolean);
      return { text: `▸ ${parts.join(" · ")}`, tone: "info" };
    }
    case "agent_started": {
      // The section header. `background` is printed when the runtime said TRUE:
      // this agent was spawned DETACHED, so its parent kept working and the
      // section will fill in interleaved with everyone else's. That is the
      // ordinary shape of a build — fan-out is backgrounded by default
      // (ADR-0014) and the skill, not the platform, decides it — so the word is
      // context for reading an interleaved feed, not an alarm. `false` is the
      // notable case and is never printed here, because a parent blocked inside
      // a child is already said where it reads better: the crew view shows the
      // parent WAITING ON that child by name. Absence is neither answer; the
      // runtime simply did not say.
      const parts = [agentName(e)];
      if (e.background) parts.push("background");
      if (e.model) parts.push(e.model);
      return { text: `⑂ ${parts.join(" · ")}`, tone: "info" };
    }
    case "agent_progress":
    case "work_item":
    case "heartbeat":
      // State, never a row — see isSilentKind.
      return { text: "", tone: "muted" };
    case "agent_settled":
      return settledLine(e);
    case "tool_use": {
      // A tool call carries a bare argument ("src/App.tsx"), meaningless without
      // the verb, so the tool name is printed. An EMPTY tool means the summary
      // is already a whole sentence containing its own verb.
      const summary = e.summary ?? e.command ?? "";
      const tool = e.tool ?? "";
      if (!summary) return { text: `$ ${tool || "tool"}`, tone: "muted" };
      // For Bash the `$` prompt already says "shell", so its name is noise.
      if (!tool || tool === "Bash") return { text: `$ ${summary}`, tone: "muted" };
      return { text: `$ ${tool} ${summary}`, tone: "muted" };
    }
    case "tool_result": {
      // The standalone form of an outcome, for a surface that cannot go back and
      // rewrite the action row it belongs to (a terminal). The console merges
      // the same OutcomeView onto that row instead; both read the same source,
      // so neither can drift into saying something the other doesn't.
      const { detail, duration, tone } = formatOutcome(e);
      if (!detail && !duration) return { text: "", tone: "muted" };
      const glyph = e.ok === false ? "✗" : "↳";
      // The tool is named because the outcome may be several rows below its
      // action once agents interleave the feed.
      const parts = [glyph, e.tool || "tool", e.ok !== false ? e.summary : "", detail, duration];
      return { text: parts.filter(Boolean).join(" "), tone };
    }
    case "task_started":
      // State, never a row — see isSilentKind, where the reasoning is: the
      // `tool_use` for the call that launched this task already printed the
      // command, and repeating it is the same sentence twice. The id this event
      // carries is what a surface needs; the text is not.
      return { text: "", tone: "muted" };
    case "task_settled": {
      // Always a row: nothing else on the feed reveals how a backgrounded
      // command ended, and by the time it does the run has usually moved on.
      const status = e.status ?? "completed";
      const parts = [`background ${e.summary ?? e.taskId ?? "task"}`, status];
      if (e.outputBytes) parts.push(`${formatBytes(e.outputBytes)} output`);
      const failed = status === "failed";
      return { text: `${failed ? "✗" : "↳"} ${parts.join(" · ")}`, tone: agentTone(status) };
    }
    case "git_commit":
      return {
        text: `✓ commit ${e.sha?.slice(0, 7) ?? ""}${e.files ? ` · ${String(e.files)} files` : ""}`.trimEnd(),
        tone: "success",
      };
    case "git_push":
      return { text: `↑ push${e.branch ? ` ${e.branch}` : ""}`, tone: "success" };
    case "gh_action":
      return {
        text: `⚙ ${e.summary ?? e.command ?? "gh"}`,
        tone: e.ok === false ? "error" : "info",
      };
    case "notice": {
      const glyph = (e.level && NOTICE_GLYPHS[e.level]) ?? "ℹ";
      const tone: LineTone = e.level === "error" ? "error" : e.level === "warn" ? "warn" : "info";
      // Neither a code nor a detail is a notice about nothing, and an empty row
      // is indistinguishable from a run that stopped talking — so it still says
      // that something was raised.
      return { text: `${glyph} ${noticeSentence(e) || "notice"}`, tone };
    }
    case "turn_ended": {
      // A turn ending is not news: the next action appearing proves it. A turn
      // that ended BADLY is the whole point of the kind — a run can lose several
      // turns and still succeed, and those are the rows that explain why it took
      // so long.
      if (!e.outcome || e.outcome === "success") return { text: "", tone: "muted" };
      const cancelled = e.outcome === "cancelled";
      return {
        text: `${cancelled ? "▪" : "✗"} turn ${e.outcome}${e.error ? ` — ${e.error}` : ""}`,
        tone: cancelled ? "warn" : "error",
      };
    }
    case "run_settled": {
      // The run's terminal line. `cancelled` is deliberately not a failure: the
      // work was taken away rather than going wrong, and a run someone stopped
      // on purpose must not be reported as broken.
      const outcome = e.outcome ?? "finished";
      return {
        text: `■ run ${outcome}${e.error ? ` — ${e.error}` : ""}`,
        tone: outcome === "failure" ? "error" : outcome === "cancelled" ? "warn" : "success",
      };
    }
    default: {
      // A kind this build has never heard of. It still prints whatever the
      // producer put in a human field, because a silent row is how a newer
      // runner's news disappears against an older console.
      const tone: LineTone = e.level === "error" ? "error" : e.level === "warn" ? "warn" : "default";
      return { text: e.summary ?? e.detail ?? "", tone };
    }
  }
}

/**
 * What a notice SAYS, without the glyph that decorates its row.
 *
 * A coded notice reads as its LABEL, which this package owns, plus whatever
 * specifics the producer could add — both, because the specifics alone would
 * lose which condition it was. A notice with only `detail` and no code is the
 * other shape and it stays: it is how a line the run itself printed (scrubbed
 * `console.*`, the watchdog's sentence) reaches a reader, and its prose stands
 * alone — prefixing it with the word "notice" would narrate the envelope instead
 * of the news.
 *
 * Exported because the same sentence has two homes: a row on the feed, and the
 * caption of an agent whose silence that notice is the explanation for.
 */
export function noticeSentence(e: RunEventView): string {
  const label = e.code ? (NOTICE_LABELS[e.code] ?? e.code) : "";
  return [label, e.detail].filter(Boolean).join(" · ");
}

/**
 * "waiting on the model for 2m10s" — the ONE wording for a wait, whether the
 * producer declared it in a heartbeat or a reader derived it from an agent that
 * has not come back yet. Two spellings of one fact is how a surface ends up
 * saying a run is idle in one place and blocked in another.
 */
export function waitingPhrase(on: string, elapsedMs?: number | undefined): string {
  return `waiting on ${on}${elapsedMs ? ` for ${formatDuration(elapsedMs)}` : ""}`;
}

/**
 * What a `heartbeat` says, for the ONE surface that shows it: a status line
 * beside the feed, not a row in it. Exported separately for that reason — a
 * heartbeat formatted as a row is what turns a quiet run into a wall of "still
 * waiting", which is why `formatEvent` renders it silent.
 *
 * `nameOf` resolves `ref` when the wait is on a spawned AGENT: "waiting on
 * Build onboarding-webapp React SPA" names the thing a reader can go and look
 * at, where "waiting on a spawned agent" makes them hunt for which one. A
 * caller with no registry passes nothing and gets the generic wording.
 */
export function formatHeartbeat(
  e: RunEventView,
  nameOf?: (agentId: string) => string | undefined,
): string {
  const named = e.waitingOn === "agent" && e.ref ? nameOf?.(e.ref) : undefined;
  const on = named ?? (e.waitingOn && WAITING_LABELS[e.waitingOn]) ?? e.waitingOn ?? "something";
  return waitingPhrase(on, e.elapsedMs);
}
