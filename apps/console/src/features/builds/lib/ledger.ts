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

type BuildSummary = components["schemas"]["BuildSummary"];
type TaskCounts = components["schemas"]["TaskCounts"];

/**
 * Pure derivations for the version ledger (ADR-0020).
 *
 * The ledger renders EVERY version at once, so everything here is a function of
 * one `BuildSummary` and nothing else — no second read, no cross-row state. That
 * constraint is why `taskCounts` and `deployedTo` are carried on the row in the
 * first place.
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
 * The qualifier after the middot is the extra fact the bare state doesn't carry:
 * who is acting, why it failed, where it landed.
 */
export function ledgerStatus(build: BuildSummary): LedgerStatus {
  switch (build.status) {
    case "queued":
      return { label: "Queued · next", tone: "neutral", live: false };
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
    case "completed":
      // A completed version that reached an environment is described by WHERE
      // it got to, which is what the reader actually wants to know. One
      // environment names itself; several are counted, because a row cannot
      // grow a list.
      if (build.deployedTo && build.deployedTo.length === 1) {
        return {
          label: `Deployed to ${build.deployedTo[0]}`,
          tone: "success",
          live: false,
        };
      }
      if (build.deployedTo && build.deployedTo.length > 1) {
        return {
          label: `Deployed to ${build.deployedTo.length} environments`,
          tone: "success",
          live: false,
        };
      }
      return { label: "Built", tone: "success", live: false };
    default:
      return { label: "Unknown", tone: "neutral", live: false };
  }
}

/** Is this version moving? Drives the row tint and the ledger's poll. */
export function isLedgerLive(build: BuildSummary): boolean {
  return build.status === "started" || build.status === "in_progress";
}

export interface TaskProgress {
  /** 0–100, for the bar. */
  percent: number;
  /** "3 of 7 done" — or the honest absence when nothing is known. */
  label: string;
  tone: StatusTone;
}

/**
 * The Tasks cell: a bar and a count.
 *
 * A version whose counts have not arrived (or whose backend predates them)
 * renders an empty bar and no claim — NOT "0 of 0 done", which reads as a
 * version with no work rather than a fact the console does not have.
 */
export function taskProgress(
  build: BuildSummary,
  counts: TaskCounts | undefined = build.taskCounts,
): TaskProgress {
  const tone: StatusTone =
    build.status === "failed"
      ? "error"
      : isLedgerLive(build)
        ? "info"
        // A queued version has done nothing yet — tinting its empty bar green
        // reads as "finished successfully" at a glance down the column.
        : build.status === "queued"
          ? "neutral"
          : "success";

  if (!counts || counts.total === 0) {
    return {
      percent: 0,
      label: counts ? "No tasks" : "—",
      tone: "neutral",
    };
  }
  const done = counts.done ?? 0;
  // Clamped: a backend that counts a task in two buckets must not produce a bar
  // wider than its track.
  const percent = Math.min(100, Math.round((done / counts.total) * 100));
  return { percent, label: `${done} of ${counts.total} done`, tone };
}

/**
 * The task breakdown as the summary card says it — "5 done · 1 in progress · 2
 * need config". Only non-zero buckets appear, so a settled build doesn't carry
 * four zeroes.
 */
export function taskBreakdown(counts: TaskCounts | undefined): string {
  if (!counts) return "";
  const parts: string[] = [];
  const push = (n: number | undefined, label: string) => {
    if (n && n > 0) parts.push(`${n} ${label}`);
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
 * a 31s deploy and a 89s one look identical. Seconds are zero-padded so the
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

/**
 * The same "18m 04s" shape from a span the server already measured.
 *
 * A deployment reports `durationSeconds` rather than two timestamps, and
 * round-tripping that through two Dates to reuse `buildDuration` was a trick,
 * not a design — both callers want one format, so the format is the shared part.
 */
export function secondsDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) {
    return "";
  }
  if (seconds < 0) return "";
  const whole = Math.floor(seconds);
  const pad = (n: number) => String(n).padStart(2, "0");
  const hours = Math.floor(whole / 3600);
  const minutes = Math.floor((whole % 3600) / 60);
  if (hours > 0) return `${hours}h ${pad(minutes)}m`;
  return `${minutes}m ${pad(whole % 60)}s`;
}

/**
 * The Duration cell. A queued version has no span to show and says so; a live
 * one counts up from `startedAt`.
 */
export function ledgerDuration(build: BuildSummary): string {
  if (build.status === "queued") return "—";
  return buildDuration(build.startedAt, build.completedAt) || "—";
}

/**
 * The Started cell. `startedAt` on a QUEUED version is its enqueue time, not a
 * start — saying "Today, 14:02" for a version that has not begun would be a
 * claim the contract explicitly warns against.
 */
export function ledgerStarted(build: BuildSummary): string | null {
  return build.status === "queued" ? null : build.startedAt;
}

/** The row's second line: `a1c9f2e · feat/approval-routing`, abbreviated. */
export function commitLine(build: BuildSummary): string {
  const sha = build.commit?.sha?.slice(0, 7);
  const branch = build.commit?.branch;
  if (sha && branch) return `${sha} · ${branch}`;
  // `||`, not `??`: the platform sends "" for an absent sha as readily as it
  // omits the key, and `??` would return the empty string as if it were a value.
  return sha || branch || "";
}
