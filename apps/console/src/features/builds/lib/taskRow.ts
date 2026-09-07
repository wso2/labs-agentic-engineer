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

import type { StatusTone } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { statusLine } from "../../tasks/lib/statusLine";
import { buildCycles } from "./runView";

type TaskView = components["schemas"]["TaskView"];
type ExecutionView = components["schemas"]["ExecutionView"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];

/**
 * What the RUN says about each of this version's tasks.
 *
 * `TaskView.executions` is empty for agent work — the platform records the
 * agent's pull request on the run's cycle instead — so a task's progress is
 * only knowable from here. One cycle dispatches ONE pull request that claims a
 * SET of issues (`resolves`), so every issue in that set shares the pull
 * request's fate: that is why one cycle's state lands on several rows.
 */
export interface RunClaims {
  /** issue number → the newest cycle that claimed it. */
  byIssue: ReadonlyMap<number, CycleClaim>;
  /** A live session with no recorded claims yet — presume it works the open
   *  issues. Never set once claims exist: a recorded set outranks a guess. */
  presumeOpenWork: boolean;
  /** When the open session started, for the row's elapsed time. */
  openCycleStartedAt: string | undefined;
}

/** One cycle's pull request, as the rows it claims see it. */
export interface CycleClaim {
  /** Its pull request, once a webhook has taught the platform about one. */
  prNumber: number | undefined;
  /** The merge, recorded by SHA — the platform's only record of one. */
  merged: boolean;
  /** Is the session still open? */
  open: boolean;
  /** When the session started. */
  startedAt: string;
}

/**
 * Read every cycle of every run of this version.
 *
 * Deliberately NOT just the open cycle, and not just the newest run. A cycle
 * ends the moment its pull request settles, so reading only the open one threw
 * away the answer at exactly the moment it became final — a task whose work had
 * merged fell back to `Pending` unless GitHub had also closed its issue. And a
 * version is often worked by SEVERAL runs (a `task` run reworking what a `dev`
 * run delivered), so the newest run alone does not hold its history.
 *
 * Runs arrive newest-first and cycles oldest-first, so this walks both
 * backwards: a later claim on the same issue overwrites an earlier one.
 */
export function runClaims(runs: MilestoneRunView[] | undefined): RunClaims {
  const byIssue = new Map<number, CycleClaim>();
  let openCycleStartedAt: string | undefined;
  let openClaimed = 0;

  for (const run of [...(runs ?? [])].reverse()) {
    for (const cycle of buildCycles(run.cycles ?? [])) {
      const claim: CycleClaim = {
        prNumber: cycle.prNumber || undefined,
        merged: Boolean(cycle.mergeSha),
        open: !cycle.endedAt,
        startedAt: cycle.createdAt,
      };
      if (claim.open) {
        openCycleStartedAt = cycle.createdAt;
        openClaimed = cycle.resolves?.length ?? 0;
      }
      for (const issue of cycle.resolves ?? []) {
        byIssue.set(issue, claim);
      }
    }
  }

  return {
    byIssue,
    presumeOpenWork: openCycleStartedAt !== undefined && openClaimed === 0,
    openCycleStartedAt,
  };
}

const NO_CLAIMS: RunClaims = {
  byIssue: new Map<number, CycleClaim>(),
  presumeOpenWork: false,
  openCycleStartedAt: undefined,
};

/**
 * The state a task row renders in (design arrangement 2b, ADR-0021 §3).
 *
 * DERIVED, not read off a field. `derivedStatus` is deliberately a two-value
 * vocabulary — the issue is open, or it is closed (tasks/api/status.ts) — and
 * ADR-0015 §4, which ADR-0021 keeps in force, is explicit that anything richer
 * is a derivation the console makes and must be able to defend.
 *
 * The order below is the precedence, and it matters: a task can be both held
 * and mid-execution, and "blocked" is the one the reader must act on.
 */
export type TaskRowState =
  | "merged"
  | "blocked"
  | "in_progress"
  | "pr_sent"
  | "pending";

const RUNNING_EXECUTION = /^(running|in_progress|started|active)$/i;

/** The newest execution recorded against a task, or undefined if none. */
export function latestExecution(task: TaskView): ExecutionView | undefined {
  const executions = Object.values(task.executions ?? {});
  if (executions.length === 0) return undefined;
  return executions.reduce((newest, e) =>
    new Date(e.createdAt).getTime() > new Date(newest.createdAt).getTime()
      ? e
      : newest,
  );
}

export function taskRowState(
  task: TaskView,
  claims: RunClaims = NO_CLAIMS,
): TaskRowState {
  const claim = claims.byIssue.get(task.issueNumber);

  // Closed is closed, and so is a recorded merge. Either is final, so nothing
  // below can override it — and the two are checked together because they can
  // disagree in one direction: a pull request merges before GitHub's close
  // event lands, and for that moment the issue is still open.
  if (task.derivedStatus === "merged" || claim?.merged) return "merged";

  // A hold is what the reader has to act on, so it outranks whatever was
  // happening when the dependency was hit.
  if (task.hold || (task.blockedBy && task.blockedBy.length > 0)) {
    return "blocked";
  }

  // A GATE keeps an execution row, and it is the honest source for one.
  const execution = latestExecution(task);
  if (execution && !execution.endedAt && RUNNING_EXECUTION.test(execution.status)) {
    return "in_progress";
  }

  // AGENT WORK has no execution — the run's build sessions are what know. A
  // recorded claim is a FACT; the presumption covers the stretch before the
  // pull request exists (ADR-0015 §4, kept in force by ADR-0021 §3).
  if (claim) {
    // A pull request exists and nothing merged it. That is true whether its
    // session is still open or has ended — an ended session with an unmerged
    // pull request is precisely the case that used to fall back to `Pending`,
    // throwing away the most useful thing the row could say.
    if (claim.prNumber !== undefined) return "pr_sent";
    return claim.open ? "in_progress" : "pending";
  }
  if (claims.presumeOpenWork) return "in_progress";

  return "pending";
}

export interface TaskRowChip {
  label: string;
  tone: StatusTone;
  /** Show elapsed time and a shimmer rather than a timestamp. */
  live: boolean;
}

export function taskRowChip(state: TaskRowState): TaskRowChip {
  switch (state) {
    case "merged":
      return { label: "Merged", tone: "success", live: false };
    case "blocked":
      return { label: "Blocked", tone: "warning", live: false };
    case "in_progress":
      return { label: "In progress", tone: "info", live: true };
    case "pr_sent":
      return { label: "PR sent", tone: "warning", live: false };
    default:
      return { label: "Pending", tone: "neutral", live: false };
  }
}

/**
 * The row's second line — the agent's latest note.
 *
 * The note ITSELF is `statusLine`, shared with the Validation page so the two
 * surfaces cannot disagree about which comment counts or where the line ends.
 * What is local to this row is the FALLBACK ladder: the platform's own rationale,
 * then the dependency a hold is waiting on. Returns null rather than a
 * placeholder when there is nothing to say — an empty second line is quieter
 * than "No updates yet" repeated down a list of eleven tasks.
 */
export function taskRowNote(task: TaskView): string | null {
  const line = statusLine(task);
  if (line) return line;
  if (task.blockedBy && task.blockedBy.length > 0) {
    return `Waiting on ${task.blockedBy.join(", ")}`;
  }
  return task.rationale || null;
}

/**
 * How long the agent has been on this task, from the running execution's start.
 * Null for anything that is not currently running.
 *
 * The running test is the SAME one `taskRowState` uses, and must stay that way:
 * a queued execution has no `endedAt` either, so testing only for that made a
 * row `taskRowState` calls `pending` render a counting-up elapsed time.
 */
export function taskElapsedFrom(
  task: TaskView,
  claims: RunClaims = NO_CLAIMS,
): string | null {
  const execution = latestExecution(task);
  if (execution && !execution.endedAt && RUNNING_EXECUTION.test(execution.status)) {
    return execution.startedAt ?? execution.createdAt;
  }
  // Agent work counts from when its build session started — there is no
  // per-task start to count from, and the session's is the honest stand-in.
  if (execution) return null;
  return taskRowState(task, claims) === "in_progress"
    ? (claims.byIssue.get(task.issueNumber)?.startedAt ??
      claims.openCycleStartedAt ??
      null)
    : null;
}

/** The stamp a settled row shows on its right — when it finished. */
export function taskSettledAt(task: TaskView): string | null {
  const execution = latestExecution(task);
  return execution?.endedAt ?? null;
}

export interface TaskTally {
  total: number;
  done: number;
  attention: number;
}

/**
 * The Tasks header's counts: "11 in this build · 5 done · 2 need your
 * attention". Attention is blocked plus in-review — both are waiting on the
 * reader, which is what makes them one number rather than two.
 */
export function taskTally(tasks: TaskView[], claims: RunClaims = NO_CLAIMS): TaskTally {
  let done = 0;
  let attention = 0;
  for (const task of tasks) {
    const state = taskRowState(task, claims);
    if (state === "merged") done += 1;
    if (state === "blocked" || state === "pr_sent") attention += 1;
  }
  return { total: tasks.length, done, attention };
}

/** Is any task in this build being worked right now? Drives the header pulse. */
export function anyTaskRunning(
  tasks: TaskView[],
  claims: RunClaims = NO_CLAIMS,
): boolean {
  return tasks.some((t) => taskRowState(t, claims) === "in_progress");
}
