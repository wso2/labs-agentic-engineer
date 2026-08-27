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
import { taskRowState } from "./taskRow";

type BuildSummary = components["schemas"]["BuildSummary"];
type TaskView = components["schemas"]["TaskView"];
type DeployStage = components["schemas"]["DeployStage"];

/**
 * Pure derivations for the version ledger (ADR-0021).
 *
 * EVERYTHING here comes from reads the console already makes — the ledger adds
 * no contract surface of its own. Two sources, and which one answers which cell
 * is the whole design of this file:
 *
 *   - `BuildSummary`      the version, its run state, its span.
 *   - `ProjectStatus.deploy` — which version reached an environment. Already
 *     polled by the project layout, so react-query serves it from cache.
 *
 * Task counts are NOT among them, and `countTasks` explains why: they cannot be
 * attributed to versions from any read the ledger can afford.
 */

export interface LedgerStatus {
  label: string;
  tone: StatusTone;
  /** A pulsing dot: the version is moving and the page is polling it. */
  live: boolean;
}

/**
 * What a version's row says about itself.
 *
 * The label names the reader's SITUATION rather than the state machine's name
 * for it (lexicon naming rule 6) — `Running · Coding agent`, not `in_progress`.
 *
 * `deploy` is the project's deploy aggregate. Only the version it names can be
 * described by where it reached; every other completed version says `Built`,
 * because the platform records ONE deployed version per project and inferring
 * anything about the others would be a guess.
 */
export function ledgerStatus(
  build: BuildSummary,
  deploy?: DeployStage | undefined,
): LedgerStatus {
  switch (build.status) {
    case "started":
    case "in_progress":
      return { label: "Running · Coding agent", tone: "info", live: true };
    case "failed":
      // The platform's terminal reason, when it left one. Without it the row
      // would say only "Failed", which tells the reader nothing to act on.
      return {
        label: build.reason ? `Failed · ${build.reason}` : "Failed",
        tone: "error",
        live: false,
      };
    case "completed": {
      if (deploy && deploy.version === build.tag) {
        if (deploy.status === "deployed") {
          return { label: "Deployed to development", tone: "success", live: false };
        }
        if (deploy.status === "deploying") {
          return { label: "Deploying to development", tone: "info", live: true };
        }
        if (deploy.status === "failed") {
          return { label: "Deploy failed", tone: "error", live: false };
        }
      }
      return { label: "Built", tone: "success", live: false };
    }
    default:
      return { label: "Unknown", tone: "neutral", live: false };
  }
}

/** Is this version moving? Drives the row tint and the ledger's poll. */
export function isLedgerLive(build: BuildSummary): boolean {
  return build.status === "started" || build.status === "in_progress";
}

export interface TaskCounts {
  total: number;
  done: number;
  inProgress: number;
  inReview: number;
  blocked: number;
  pending: number;
}

/**
 * Count a list of tasks that is ALREADY scoped to one version.
 *
 * Deliberately not a group-by-version helper. The obvious one — take an
 * untagged list-tasks read and group by `lineage.specTag` — cannot work: the
 * server sets that field only on a TAG-SCOPED read and leaves it empty when the
 * query spans versions (`reads.go`: *"the version tag every returned issue
 * belongs to (empty when the query spans versions)"*). Nothing else on
 * `TaskView` identifies the version either, so an untagged read cannot be
 * attributed to versions at all, and the Builds ledger has no Tasks column
 * because of it. This runs on the build page, where the read is tag-scoped.
 */
export function countTasks(tasks: TaskView[]): TaskCounts {
  const counts: TaskCounts = {
    total: tasks.length,
    done: 0,
    inProgress: 0,
    inReview: 0,
    blocked: 0,
    pending: 0,
  };
  for (const task of tasks) {
    switch (taskRowState(task)) {
      case "done":
        counts.done += 1;
        break;
      case "in_progress":
        counts.inProgress += 1;
        break;
      case "in_review":
        counts.inReview += 1;
        break;
      case "blocked":
        counts.blocked += 1;
        break;
      default:
        counts.pending += 1;
    }
  }
  return counts;
}

/**
 * The task breakdown as the summary card says it — "5 done · 1 in progress · 2
 * need config". Only non-zero buckets appear, so a settled build doesn't carry
 * four zeroes.
 */
export function taskBreakdown(counts: TaskCounts | undefined): string {
  if (!counts) return "";
  const parts: string[] = [];
  const push = (n: number, label: string) => {
    if (n > 0) parts.push(`${n} ${label}`);
  };
  push(counts.done, "done");
  push(counts.inProgress, "in progress");
  push(counts.inReview, "in review");
  push(counts.blocked, "need config");
  push(counts.pending, "pending");
  return parts.length > 0 ? parts.join(" · ") : `${counts.total} total`;
}

/**
 * "18m 04s" — the precise span the ledger and the summary card show.
 *
 * Deliberately finer than `runDuration`'s "18 min": on this surface the number
 * is a column that gets compared across rows, and rounding to the minute makes
 * a 31s build and an 89s one look identical. Seconds are zero-padded so the
 * column stays aligned under `font-variant-numeric: tabular-nums`.
 *
 * `to` omitted means "still going", measured against now.
 */
export function buildDuration(
  fromIso: string | null | undefined,
  toIso?: string | null,
): string {
  if (!fromIso) return "";
  const from = new Date(fromIso).getTime();
  const to = toIso ? new Date(toIso).getTime() : Date.now();
  if (Number.isNaN(from) || Number.isNaN(to) || to < from) return "";

  const totalSeconds = Math.floor((to - from) / 1000);
  const seconds = totalSeconds % 60;
  const totalMinutes = Math.floor(totalSeconds / 60);
  const minutes = totalMinutes % 60;
  const hours = Math.floor(totalMinutes / 60);

  const pad = (n: number) => String(n).padStart(2, "0");
  if (hours > 0) return `${hours}h ${pad(minutes)}m`;
  return `${minutes}m ${pad(seconds)}s`;
}

/** The Duration cell — a live version counts up from `startedAt`. */
export function ledgerDuration(build: BuildSummary): string {
  return buildDuration(build.startedAt, build.completedAt) || "—";
}

/** The Milestone cell. The platform records a number, not a title. */
export function milestoneLabel(build: BuildSummary): string {
  return `Milestone #${build.milestoneNumber}`;
}
