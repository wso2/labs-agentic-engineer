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

import { formatEvent, formatLine } from "@aep/progress-view";
import type { components } from "../../../generated/aep-api";
import { isTerminalRun } from "../api/runs";
import type { StampedRunEvent } from "../hooks/useRunProgress";

// What a version's runs say, for the Build card: which cycles built it, which
// one merged (its component builds are the build logs), whether an agent is
// writing now, and whether the run is parked at the deploy gate waiting for a
// dependency's values. Copied from the old console's features/builds/lib/runView.ts
// and trimmed to what the card reads. Pure, so the card and its tests agree.

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type CycleBuild = components["schemas"]["CycleBuild"];
type TimelineEvent = components["schemas"]["TimelineEvent"];

/**
 * The cycle kinds a build owns. Validation is not one: its attempts and
 * verdict are the version's validation, shown apart from its build.
 */
const BUILD_CYCLE_KINDS: readonly string[] = ["coding", "fix", "conflict"];

/** A run's build cycles, in dispatch order. */
export function buildCycles(cycles: readonly RunCycleView[]): RunCycleView[] {
  return cycles.filter((c) => BUILD_CYCLE_KINDS.includes(c.kind));
}

/**
 * The newest build cycle whose pull request merged, across the version's runs
 * (newest first, cycles oldest first): the merge its component builds came
 * from. A validation cycle's merge is the commit it judged, so it is not one.
 */
export function mergedCycle(runs: readonly MilestoneRunView[]): RunCycleView | undefined {
  for (const run of runs) {
    const merged = buildCycles(run.cycles).reverse().find((c) => Boolean(c.mergeSha));
    if (merged) return merged;
  }
  return undefined;
}

/**
 * Is an agent writing the version now? Not the run's state: a run stays open
 * through the merge, the component builds and the rollout, long after its
 * agent stopped.
 */
export function isAgentStreaming(runs: readonly MilestoneRunView[]): boolean {
  const newest = runs[0];
  if (!newest || isTerminalRun(newest.state)) return false;
  return buildCycles(newest.cycles).some((c) => !c.endedAt);
}

/**
 * The dependencies a run is parked on at the deploy gate, waiting for their
 * values; null when it is not parked there. Empty is a real answer: parked,
 * with no dependency named (an older run row).
 */
export function externalValuesPark(run: MilestoneRunView | undefined): string[] | null {
  if (!run || run.state !== "waiting" || run.waitingReason !== "external-values") return null;
  return run.blockingDependencies ?? [];
}

export type ComponentBuildState = "running" | "succeeded" | "failed" | "other";

const SUCCEEDED = new Set(["WorkflowSucceeded", "Succeeded"]);
const FAILED = new Set(["WorkflowFailed", "Failed"]);

/**
 * A component build's outcome, from the cluster's condition reason. A reason
 * this console has not learned is neither green nor red: it shows itself.
 */
export function componentBuildState(build: Pick<CycleBuild, "completed" | "status">): ComponentBuildState {
  if (!build.completed) return "running";
  if (SUCCEEDED.has(build.status)) return "succeeded";
  if (FAILED.has(build.status)) return "failed";
  return "other";
}

/** How the run ended, beside the coding agent's log. */
export function settledLabel(state: string): string {
  switch (state) {
    case "succeeded":
      return "Run finished";
    case "failed":
      return "Run failed";
    case "cancelled":
      return "Run cancelled";
    case "blocked":
      return "Run blocked";
    default:
      return `Run ended: ${state}`;
  }
}

function clock(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/**
 * A log as the Build card prints it: newest first, each line with its time.
 * A line with nothing worth a row (a fast, successful tool result) is left out.
 */
function newestFirst(entries: readonly { ts: string; text: string }[]): string[] {
  return entries.flatMap((e) => (e.text ? [`${clock(e.ts)}  ${e.text}`] : [])).reverse();
}

/** A run's events (the coding agent's log), newest first. */
export function agentLogLines(events: readonly StampedRunEvent[]): string[] {
  return newestFirst(events.map((e) => ({ ts: e.ts, text: formatEvent(e).text })));
}

/** A task's log (its executions' timeline), newest first. */
export function taskLogLines(lines: readonly TimelineEvent[]): string[] {
  return newestFirst(lines.map((l) => ({ ts: l.ts, text: formatLine(l).text })));
}
