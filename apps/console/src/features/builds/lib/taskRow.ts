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

type TaskView = components["schemas"]["TaskView"];
type ExecutionView = components["schemas"]["ExecutionView"];
type IssueComment = components["schemas"]["IssueComment"];

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
  | "done"
  | "blocked"
  | "in_progress"
  | "in_review"
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

export function taskRowState(task: TaskView): TaskRowState {
  // Closed is closed: a merged pull request is what closes a task, so nothing
  // below can override it.
  if (task.derivedStatus === "merged") return "done";

  // A hold is what the reader has to act on, so it outranks whatever the agent
  // was doing when it hit the dependency.
  if (task.hold || (task.blockedBy && task.blockedBy.length > 0)) {
    return "blocked";
  }

  const execution = latestExecution(task);
  if (execution && !execution.endedAt && RUNNING_EXECUTION.test(execution.status)) {
    return "in_progress";
  }

  // The agent finished and the issue is still open — the pull request is up and
  // waiting on a human. This is a derivation, not a platform state: there is no
  // ready-for-review field on the contract, and "the work ended but nothing
  // merged" is the only honest reading of that pair.
  if (execution?.endedAt) return "in_review";

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
    case "done":
      return { label: "Done", tone: "success", live: false };
    case "blocked":
      return { label: "Blocked", tone: "warning", live: false };
    case "in_progress":
      return { label: "In progress", tone: "info", live: true };
    case "in_review":
      return { label: "Review", tone: "warning", live: false };
    default:
      return { label: "Pending", tone: "neutral", live: false };
  }
}

/**
 * The issue's NEWEST comment.
 *
 * `comments` arrives OLDEST FIRST and is never an empty array — the contract
 * omits the field entirely for every empty case — so the newest is the last
 * element. The platform's own machine comments are already excluded server-side;
 * what is left is the coding agent's progress notes and whatever a human wrote.
 */
export function latestComment(task: TaskView): IssueComment | undefined {
  return task.comments?.at(-1);
}

/**
 * The row's second line — the agent's latest note.
 *
 * Falls back to the platform's own rationale, then to the dependency a hold is
 * waiting on. Returns null rather than a placeholder when there is nothing to
 * say: an empty second line is quieter than "No updates yet" repeated down a
 * list of eleven tasks.
 *
 * A comment body is markdown over an unbounded textarea, and this is one line of
 * a dense row — so it is flattened to its first non-empty line and the row
 * clamps what is left. The full thread is one click away on the task page.
 */
export function taskRowNote(task: TaskView): string | null {
  const comment = latestComment(task);
  const firstLine = comment?.body
    .split("\n")
    .map((l) => l.trim())
    .find((l) => l.length > 0);
  if (firstLine) return firstLine;
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
export function taskElapsedFrom(task: TaskView): string | null {
  const execution = latestExecution(task);
  if (!execution || execution.endedAt) return null;
  if (!RUNNING_EXECUTION.test(execution.status)) return null;
  return execution.startedAt ?? execution.createdAt;
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
export function taskTally(tasks: TaskView[]): TaskTally {
  let done = 0;
  let attention = 0;
  for (const task of tasks) {
    const state = taskRowState(task);
    if (state === "done") done += 1;
    if (state === "blocked" || state === "in_review") attention += 1;
  }
  return { total: tasks.length, done, attention };
}

/** Is any task in this build being worked right now? Drives the header pulse. */
export function anyTaskRunning(tasks: TaskView[]): boolean {
  return tasks.some((t) => taskRowState(t) === "in_progress");
}
