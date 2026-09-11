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

// ONE agent, on one line — the wording of a section's header.
//
// Its own module because three callers compose it and none of them owns it: v2's
// `agent_settled` row, a console section header, and a terminal's end-of-run
// pass. A run that fans out to a dozen agents is read almost entirely through
// this line, so a surface inventing its own would be inventing the run's summary.

import { formatDuration, type LineTone } from "./line.js";

/** The session's top-level agent. A literal on the wire, not an id to compare. */
export const LEAD_AGENT_ID = "lead";

/**
 * What one agent has to report, whether it is still going or has settled.
 *
 * The figures are the runtime's own, reported rather than re-derived: its
 * totalDurationMs of 209158 matched a hand-measured 3m29s exactly, and nothing
 * in this feed can reconstruct a spawned agent's per-edit line counts — its
 * individual edits need not reach the feed at all.
 */
export interface AgentReport {
  /** The runtime's stable id for the agent — `lead` for the session's own. */
  id: string;
  /** Its human name: the label its parent gave it, else its role, else its id. */
  label: string;
  /**
   * What KIND of agent it is, in the runtime's vocabulary — or `inferred` when
   * the platform deduced the agent from a v1 feed rather than being told.
   */
  role?: string | undefined;
  /** AgentStatus: `running` until a settle event says otherwise. */
  status: string;
  durationMs?: number | undefined;
  toolCount?: number | undefined;
  tokens?: number | undefined;
  linesAdded?: number | undefined;
  linesRemoved?: number | undefined;
  /** Spawned detached from its parent's turn, where the runtime said. */
  background?: boolean | undefined;
  /**
   * Did an `agent_started` for this agent actually arrive, or is the agent known
   * only because something claimed to be it?
   *
   * The second case is legitimate and stays on the surface: a feed joined
   * mid-run, a feed the platform admits has gaps, and the v1 lift's `inferred`
   * agents all produce it. What it must NOT do is let the surface assume
   * anything the producer never declared — see the blocking rule in `crew.ts`,
   * where an undeclared child used to hold its parent in `waiting` forever and
   * so muted the stall report.
   */
  declared?: boolean | undefined;
  /**
   * What it says it is doing right now. Earns its place HERE and nowhere else:
   * as the live status of work the reader has chosen not to expand. A new phrase
   * REPLACES the previous one — it is a state, not a log line.
   */
  activity?: string | undefined;
  /**
   * Its closing summary of what it did. Treat it as the only copy: a spawned
   * agent's transcript does not reach this feed and dies with the pod, so a
   * surface that drops this leaves a reader nothing but counters.
   */
  report?: string | undefined;
}

/** A fresh, unsettled report for an agent nothing has been heard from yet. */
export function openAgentReport(id: string, label?: string | undefined): AgentReport {
  return { id, label: label || id, status: "running" };
}

/**
 * How an agent's ending reads: a settle is news whichever way it went, but only
 * a failure is a defect. `stopped` is a cancellation — the work was taken away
 * rather than going wrong — so it must never be toned as an error.
 */
export function agentTone(status: string): LineTone {
  if (status === "failed") return "error";
  if (status === "stopped") return "warn";
  if (status === "running") return "muted";
  return "success";
}

/** Tokens as a badge reads them: 12400 → "12.4k". */
function formatTokens(tokens: number): string {
  return tokens < 1000 ? `${String(tokens)} tokens` : `${(tokens / 1000).toFixed(1)}k tokens`;
}

/**
 * The report WITHOUT the agent's name, for a surface that already names it
 * beside this (the console puts the label in a chip, and repeating it would say
 * it twice on every section header).
 *
 * The closing `report` is deliberately absent: it is a paragraph and this is a
 * line. A surface renders it under the header — see FormattedLine.report.
 */
export function formatAgentStatus(r: AgentReport): string {
  const parts: string[] = [r.status];
  if (r.background) parts.push("background");
  if (r.durationMs) parts.push(formatDuration(r.durationMs));
  if (r.toolCount) parts.push(`${String(r.toolCount)} tool${r.toolCount === 1 ? "" : "s"}`);
  // The audit signal the user audience actually wants: how much code this
  // produced. Omitted when the runtime reported neither, rather than shown as
  // +0/−0.
  if (r.linesAdded || r.linesRemoved) {
    parts.push(`+${String(r.linesAdded ?? 0)}/−${String(r.linesRemoved ?? 0)} lines`);
  }
  if (r.tokens) parts.push(formatTokens(r.tokens));
  if (r.activity) parts.push(r.activity);
  return parts.join(" · ");
}

/**
 * The same report with the agent's name in front, for a flat row that has
 * nothing else to say which agent it is about.
 */
export function formatAgentReport(r: AgentReport): string {
  return `${r.label} ${formatAgentStatus(r)}`;
}
