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

import type { components } from "../../../generated/aep-api";
import { buildCycles } from "./run";

// A version's tasks as the Build card lists them, copied from the old console
// (features/builds/lib/taskRow.ts and features/tasks/lib/statusLine.ts): each
// task's state, derived from its issue and from the run's cycles, and its
// status line. The issue is the status line (runner ADR-0010): whoever works
// an issue keeps its newest comment current, so that comment's first line is
// what the task is doing now.

type TaskView = components["schemas"]["TaskView"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];

/** The newest comment's first non-empty line; null when the issue has none. Comments arrive oldest first. */
export function statusLine(task: Pick<TaskView, "comments">): string | null {
  const body = task.comments?.at(-1)?.body;
  if (!body) return null;
  return (
    body
      .split("\n")
      .map((l) => l.trim())
      .find((l) => l.length > 0) ?? null
  );
}

/** One cycle's pull request, as the issues it resolves see it. */
interface CycleClaim {
  prNumber: number | undefined;
  /** A merge is recorded by its SHA. */
  merged: boolean;
  open: boolean;
}

/**
 * What the run says about each issue. Agent work records no execution on its
 * issue: its pull request lives on the run's cycle, which names the issues it
 * resolves. Every cycle of every run counts (a later claim on an issue
 * outranks an earlier one), and a live cycle that has claimed nothing yet is
 * presumed to be working the open issues.
 */
export interface RunClaims {
  byIssue: ReadonlyMap<number, CycleClaim>;
  presumeOpenWork: boolean;
}

export function runClaims(runs: readonly MilestoneRunView[]): RunClaims {
  const byIssue = new Map<number, CycleClaim>();
  let openUnclaimed = false;
  // Runs arrive newest first, cycles oldest first: walk runs oldest first so later claims win.
  for (const run of [...runs].reverse()) {
    for (const cycle of buildCycles(run.cycles)) {
      const claim: CycleClaim = {
        prNumber: cycle.prNumber || undefined,
        merged: Boolean(cycle.mergeSha),
        open: !cycle.endedAt,
      };
      if (claim.open) openUnclaimed = (cycle.resolves?.length ?? 0) === 0;
      for (const issue of cycle.resolves ?? []) byIssue.set(issue, claim);
    }
  }
  return { byIssue, presumeOpenWork: openUnclaimed };
}

const NO_CLAIMS: RunClaims = { byIssue: new Map(), presumeOpenWork: false };

export type TaskState = "merged" | "blocked" | "in_progress" | "pr_sent" | "pending";

const RUNNING_EXECUTION = /^(running|in_progress|started|active)$/i;

/**
 * A task's state, in precedence order: a closed issue or a recorded merge is
 * final; a hold is what the reader must act on; then a running execution (a
 * provisioning gate keeps one), then the run's claim on it.
 */
export function taskState(task: TaskView, claims: RunClaims = NO_CLAIMS): TaskState {
  const claim = claims.byIssue.get(task.issueNumber);
  if (task.derivedStatus === "merged" || claim?.merged) return "merged";
  if (task.hold || (task.blockedBy?.length ?? 0) > 0) return "blocked";
  const executions = Object.values(task.executions ?? {});
  if (executions.some((e) => !e.endedAt && RUNNING_EXECUTION.test(e.status))) return "in_progress";
  if (claim) {
    if (claim.prNumber !== undefined) return "pr_sent";
    return claim.open ? "in_progress" : "pending";
  }
  return claims.presumeOpenWork ? "in_progress" : "pending";
}

const STATE_LABEL: Record<TaskState, string> = {
  merged: "Merged",
  blocked: "Blocked",
  in_progress: "In progress",
  pr_sent: "PR sent",
  pending: "Pending",
};

export function taskStateLabel(state: TaskState): string {
  return STATE_LABEL[state];
}

/**
 * The row's second line: its status line, else the dependency a hold waits
 * on, else the platform's own reason for the task; null when there is nothing
 * to say, which is quieter than a placeholder repeated down the list.
 */
export function taskNote(task: TaskView): string | null {
  const line = statusLine(task);
  if (line) return line;
  if (task.blockedBy && task.blockedBy.length > 0) return `Waiting on ${task.blockedBy.join(", ")}`;
  return task.rationale || null;
}

/** The tasks in the order the milestone planned them: by issue number. */
export function orderedTasks(tasks: readonly TaskView[]): TaskView[] {
  return [...tasks].sort((a, b) => a.issueNumber - b.issueNumber);
}

export interface TaskTally {
  total: number;
  done: number;
  /** Blocked or waiting on a review: both wait on a person. */
  attention: number;
}

export function taskTally(tasks: readonly TaskView[], claims: RunClaims = NO_CLAIMS): TaskTally {
  let done = 0;
  let attention = 0;
  for (const task of tasks) {
    const state = taskState(task, claims);
    if (state === "merged") done += 1;
    if (state === "blocked" || state === "pr_sent") attention += 1;
  }
  return { total: tasks.length, done, attention };
}
