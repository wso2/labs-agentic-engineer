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
import { hasMergedWork } from "./runView";
import { taskRowState, type RunClaims } from "./taskRow";

type BuildSummary = components["schemas"]["BuildSummary"];
type TaskView = components["schemas"]["TaskView"];
type DeployStage = components["schemas"]["DeployStage"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];

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
 * Is this version parked at the deploy gate, waiting on a person (ADR-0023)?
 *
 * `status` cannot answer it: a waiting run and a running one are both
 * `in_progress`. `waitingReason` is what separates them, and the ledger read
 * carries it because the run row the summary is built from already holds it —
 * so saying this costs no request per row, and ADR-0021 §6 is not breached.
 */
export function isAwaitingValues(build: BuildSummary): boolean {
  return build.waitingReason === "external-values";
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
/**
 * A raw status value as a label — `in_progress` → "In progress". Only the
 * unknown-status fallback uses it; every value this console knows has prose of
 * its own.
 */
function humanise(status: string): string {
  const words = status.replace(/[_-]+/g, " ").trim();
  if (words === "") return "Unknown";
  return words.charAt(0).toUpperCase() + words.slice(1);
}

export function ledgerStatus(
  build: BuildSummary,
  deploy?: DeployStage | undefined,
): LedgerStatus {
  switch (build.status) {
    case "started":
    case "in_progress":
      // A version parked at the deploy gate is NOT a version an agent is
      // working. The run stopped, and only this reader (or a cancellation) can
      // restart it, so the row must say that and must not read as live: the
      // tint and the pulse mean "the moving thing", and a park is the opposite.
      // The wording is the build page's own pill, so the two surfaces agree
      // (lexicon, *A version parked at the deploy gate*). The dependency names
      // stay on that page — the row has no space for them and the ledger read
      // does not carry them.
      if (isAwaitingValues(build)) {
        return { label: "Waiting for configuration", tone: "warning", live: false };
      }
      return { label: "Running · Coding agent", tone: "info", live: true };
    case "failed":
      // The platform's terminal reason, when it left one. Without it the row
      // would say only "Failed", which tells the reader nothing to act on.
      return {
        label: build.reason ? `Failed · ${build.reason}` : "Failed",
        tone: "error",
        live: false,
      };
    case "cancelled":
      // A person stopped this increment. NEUTRAL rather than error: nothing went
      // wrong, and an error tone would say the platform failed at something a
      // reader then has to go and look into. No reason is appended either — a
      // cancel writes none, having no fault to report — so unlike Failed this
      // label stands alone by design.
      //
      // Its own case rather than the `default`, which answers "Unknown": the
      // status is a contract enum value, so falling through would be the console
      // telling a reader it does not recognise a state the API had just named.
      return { label: "Cancelled", tone: "neutral", live: false };
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
      // A status this console does not know about, which in practice means ONE
      // thing: the BFF is ahead of the bundle. The enum gained `cancelled` and
      // this branch is how that looked in between — a cancelled build rendering
      // as "Unknown" against an old bundle, which is worse than the "Failed" it
      // replaced. They deploy as separate units, so the skew window is real
      // however carefully the two are rolled.
      //
      // So show what the API actually said rather than shrugging. A future value
      // degrades to a readable label a reader can act on, and it is never live —
      // an unknown status must not tint and pulse a row as the moving thing.
      return { label: humanise(build.status), tone: "neutral", live: false };
  }
}

/**
 * Is the Duration cell still being measured against NOW?
 *
 * Deliberately keyed on the ABSENCE of `completedAt` rather than on
 * `isLedgerLive`, because that is exactly the condition under which
 * `buildDuration` falls back to `Date.now()`. Anything that renders such a
 * duration has to re-render every second or the number freezes at whatever it
 * was on first paint — which is what it did: react-query's structural sharing
 * hands back an identical `BuildSummary` on every poll, so no poll ever caused
 * a re-render and a running build's timer never moved.
 */
export function isDurationOpen(build: BuildSummary): boolean {
  return Boolean(build.startedAt) && !build.completedAt;
}

/**
 * Is this version's RUN still in flight — worth polling, and cancellable?
 *
 * Deliberately NOT the same question as `LedgerStatus.live`, which is what
 * tints and pulses a row. A version parked at the deploy gate is in flight but
 * not moving: it is still the run the build page polls (it resumes on its own
 * when the last value is saved) and still the run Cancel acts on, while its
 * row must go quiet because nothing is happening. Hence one predicate for the
 * run's flight and another for the row's motion.
 */
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
export function countTasks(tasks: TaskView[], claims?: RunClaims): TaskCounts {
  const counts: TaskCounts = {
    total: tasks.length,
    done: 0,
    inProgress: 0,
    inReview: 0,
    blocked: 0,
    pending: 0,
  };
  for (const task of tasks) {
    switch (taskRowState(task, claims)) {
      case "merged":
        counts.done += 1;
        break;
      case "in_progress":
        counts.inProgress += 1;
        break;
      case "pr_sent":
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
 * Does the Deployments link belong on this version's card yet?
 *
 * A version reaches an environment when its work MERGES — before that the
 * Deployments board has nothing to say about it, and offering the link invited
 * the reader to go look at a page that could only disappoint them, one line
 * above a note reading "v5 deploys as its tasks merge".
 *
 * So: one of the version's runs recorded a merge (`hasMergedWork` — a build
 * cycle carrying a `mergeSha`), or the deploy aggregate already names this
 * version, which settles the question outright. Both are facts the build page
 * already holds; this adds no read.
 */
export function isDeployable(
  build: BuildSummary,
  runs: MilestoneRunView[] | undefined,
  deploy?: DeployStage | undefined,
): boolean {
  if (deploy?.version === build.tag) return true;
  return hasMergedWork(runs);
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
