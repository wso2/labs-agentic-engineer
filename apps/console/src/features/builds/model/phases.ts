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

import { formatDuration, formatEvent } from "@aep/progress-view";
import type { components } from "../../../generated/aep-api";
import type { RunProgressCycle, StampedRunEvent } from "../hooks/useRunProgress";

// A run's steps as the Build card's phase strip shows them, read from what today's run
// progress stream already carries: Plan, then each entry of the coding
// agent's own plan ("Foundation · API", "F1 Submit expenses · Web app"), then
// Validate. Pure, so the card and its tests read the same answer.
//
//   Plan      the lead agent's work before its first plan entry starts: it
//             reads the spec and writes the plan.
//   entries   the `work_item` events with `source: plan`, in the order the
//             agent first named them; an entry is live from `in_progress` to
//             `completed`, and its log is its owner's events in that window.
//   Validate  the run's validation cycle; its count is the criteria judged so
//             far (`work_item`, `source: criterion`, reaching pass or fail).

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

export type PhaseState = "done" | "live" | "queued" | "failed";

export interface RunPhase {
  key: string;
  label: string;
  state: PhaseState;
  /** Validate only: criteria judged so far, of all it planned. */
  count: { done: number; total: number } | null;
  /** The phase's log, formatted, oldest first; silent events are left out. */
  log: string[];
  /** First to last event of a finished phase; null while it runs or before it starts. */
  durationMs: number | null;
}

export interface RunSteps {
  phases: RunPhase[];
  /** Index of the phase running now; null before the run starts and once it is over. */
  current: number | null;
  /** How much of the run is done, 0 to 1. */
  fraction: number;
}

const LEAD = "lead";
const JUDGED = new Set(["pass", "fail"]);

interface PlanEntry {
  id: string;
  title: string;
  owner: string;
  status: string;
  /** Positions in the coding events where the entry started and finished. */
  start: number | null;
  end: number | null;
}

function lines(events: StampedRunEvent[]): string[] {
  return events.map((e) => formatEvent(e).text).filter(Boolean);
}

function span(events: StampedRunEvent[]): number | null {
  const first = events[0];
  const last = events.at(-1);
  if (!first || !last) return null;
  return Date.parse(last.ts) - Date.parse(first.ts);
}

function planEntries(events: StampedRunEvent[]): PlanEntry[] {
  const entries = new Map<string, PlanEntry>();
  events.forEach((e, i) => {
    if (e.kind !== "work_item" || e.source !== "plan" || !e.itemId) return;
    const entry = entries.get(e.itemId) ?? {
      id: e.itemId,
      title: e.title ?? e.itemId,
      owner: e.ownerAgentId ?? LEAD,
      status: "pending",
      start: null,
      end: null,
    };
    if (e.title) entry.title = e.title;
    if (e.ownerAgentId) entry.owner = e.ownerAgentId;
    entry.status = e.itemStatus ?? entry.status;
    if (e.itemStatus === "in_progress" && entry.start === null) entry.start = i;
    if (e.itemStatus === "completed") entry.end = i;
    entries.set(e.itemId, entry);
  });
  return [...entries.values()].filter((e) => e.status !== "deleted");
}

function criteria(events: StampedRunEvent[]): { done: number; total: number } {
  const status = new Map<string, string>();
  for (const e of events) {
    if (e.kind === "work_item" && e.source === "criterion" && e.itemId) status.set(e.itemId, e.itemStatus ?? "planned");
  }
  const all = [...status.values()];
  return { done: all.filter((s) => JUDGED.has(s)).length, total: all.length };
}

/** The run's steps, from its record and the cycles its progress stream has sent so far. */
export function runSteps(run: Pick<MilestoneRunView, "state"> | undefined, cycles: RunProgressCycle[]): RunSteps {
  const coding = cycles.filter((c) => c.cycle.kind !== "validation");
  const validation = cycles.filter((c) => c.cycle.kind === "validation").at(-1);
  const events = coding.flatMap((c) => c.events);
  const entries = planEntries(events);
  const failed = run?.state === "failed" || run?.state === "cancelled" || run?.state === "blocked";

  const firstStart = Math.min(...entries.map((e) => e.start ?? Infinity));
  const planEvents = events.slice(0, Number.isFinite(firstStart) ? firstStart : events.length);
  const planDone = Number.isFinite(firstStart) || validation !== undefined;
  const live = (state: PhaseState): PhaseState => (failed && state === "live" ? "failed" : state);

  const plan: RunPhase = {
    key: "plan",
    label: "Plan",
    state: planDone ? "done" : run ? live("live") : "queued",
    count: null,
    log: lines(planEvents),
    durationMs: planDone ? span(planEvents) : null,
  };

  const work = entries.map((entry): RunPhase => {
    const window = entry.start === null ? [] : events.slice(entry.start, (entry.end ?? events.length - 1) + 1);
    const own = window.filter((e) => e.agentId === entry.owner);
    const state: PhaseState =
      entry.status === "completed" ? "done" : entry.status === "in_progress" ? live("live") : "queued";
    return {
      key: entry.id,
      label: entry.title,
      state,
      count: null,
      log: lines(own),
      durationMs: state === "done" ? span(window) : null,
    };
  });

  const judged = validation ? criteria(validation.events) : { done: 0, total: 0 };
  const validated = Boolean(validation?.cycle.endedAt);
  const validate: RunPhase = {
    key: "validate",
    label: "Validate",
    state: validated ? "done" : validation ? live("live") : "queued",
    count: validation ? judged : null,
    log: lines(validation?.events ?? []),
    durationMs: validated ? span(validation?.events ?? []) : null,
  };

  const phases = [plan, ...work, validate];
  const at = phases.findIndex((p) => p.state === "live" || p.state === "failed");
  const done = phases.filter((p) => p.state === "done").length;
  const partial = validate.state === "live" && judged.total > 0 ? judged.done / judged.total : 0;
  return {
    phases,
    current: at === -1 ? null : at,
    fraction: Math.min(1, (done + partial) / phases.length),
  };
}

export type RunTone = "primary" | "success" | "warning" | "error" | null;

/**
 * The run's state in one line, above its progress bar: starting, which step
 * of how many (or how far validation is), then its result.
 */
export function runStatus(
  state: MilestoneRunView["state"] | undefined,
  steps: RunSteps,
  outcome: { passed: number; total: number; failing: { story: string | null; name: string }[] } | null,
): { text: string; tone: RunTone } {
  if (state === "failed") return { text: "Failed", tone: "error" };
  if (state === "cancelled") return { text: "Cancelled", tone: null };
  if (state === "blocked") return { text: "Blocked", tone: "warning" };
  if (state === "succeeded") {
    if (!outcome) return { text: "Built", tone: null };
    if (outcome.failing.length === 0) return { text: `Built · ${outcome.passed}/${outcome.total} passing`, tone: "success" };
    const which = outcome.failing.map((f) => f.story ?? f.name).join(", ");
    return { text: `Built · ${outcome.passed}/${outcome.total} · ${which} failing`, tone: "warning" };
  }
  if (!state || steps.current === null) return { text: "Starting", tone: "primary" };
  const phase = steps.phases[steps.current];
  if (phase?.key === "validate" && phase.count) {
    return { text: `Validating · ${phase.count.done} of ${phase.count.total}`, tone: "primary" };
  }
  return { text: `Running · ${steps.current + 1} of ${steps.phases.length}`, tone: "primary" };
}

/** A phase as the Build card's strip says it, for its tooltip and a screen reader: where it is, in words. */
export function phaseStripLabel(phase: RunPhase): string {
  switch (phase.state) {
    case "done":
      return `${phase.label}: done${phase.durationMs !== null ? ` in ${formatDuration(phase.durationMs)}` : ""}`;
    case "live":
      return phase.count && phase.count.total > 0
        ? `${phase.label}: ${phase.count.done} of ${phase.count.total}`
        : `${phase.label}: running`;
    case "failed":
      return `${phase.label}: failed`;
    default:
      return `${phase.label}: queued`;
  }
}
