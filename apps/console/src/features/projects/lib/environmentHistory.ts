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

// PAST DEPLOYMENTS for one environment (the design, §4.3 / §6 section 4).
//
// The platform records no deployment history. `Deployment` is the CURRENT
// binding and nothing else, so the only environment with a past the console
// can read is the pipeline's ENTRY environment: every completed build
// auto-deploys there, and the version ledger is the record of which ones
// there were. Every other environment gets what runs now and an admission —
// a build reaching the entry environment says nothing whatever about any
// other, and folding the ledger onto Staging would invent a past that never
// happened.

import type { components } from "../../../generated/aep-api";
import type { EnvironmentRow } from "./deploymentLedger";
import type { EnvironmentInfo } from "./environments";

type BuildSummary = components["schemas"]["BuildSummary"];

export interface HistoryRow {
  /** Stable key for the row — a version tag, or the binding when none. */
  key: string;
  /** The version that ran; absent when nothing names it. */
  version?: string;
  /** The milestone its work lived in — the version ledger's, so the entry
   *  environment's rows only. */
  milestoneNumber?: number;
  /**
   * When it started running here — the BINDING's stamp, the one deploy time
   * the platform actually recorded. Only the live row has one: a superseded
   * version's binding is gone, and its build's finish time is a different
   * fact, so the row carries no time rather than a guess dressed as one.
   */
  deployedAt?: string;
  current: boolean;
}

export interface HistoryView {
  rows: HistoryRow[];
  /** The platform records nothing for this environment beyond what runs now. */
  unrecorded: boolean;
  /** The read the rows come from has not answered — so an empty `rows` here
   *  means "not known yet", never "nothing ran". */
  pending: boolean;
}

/** A build that produced a version the environment actually ran. A failed or
 *  cancelled build never reached it, and an unfinished one has not yet. */
function deployed(build: BuildSummary): boolean {
  return build.status === "completed";
}

/** Newest first — the order the version ledger is read in. */
function newestFirst(a: BuildSummary, b: BuildSummary): number {
  return (b.startedAt ?? "").localeCompare(a.startedAt ?? "");
}

function bound(row: EnvironmentRow | undefined): boolean {
  return row?.cards.some((c) => c.deployment) ?? false;
}

/**
 * What has run on this environment, newest first.
 *
 * `builds` is the version ledger; `undefined` means the read has not answered
 * yet, which is reported as `pending` rather than folded into an empty ledger.
 */
export function historyFor(
  env: EnvironmentInfo,
  row: EnvironmentRow | undefined,
  builds: BuildSummary[] | undefined,
): HistoryView {
  if (env.position !== 0) {
    // Nothing downstream of the entry environment has a recorded past. All it
    // can say is what is bound to it now — and only when something is.
    if (!bound(row) || !row) return { rows: [], unrecorded: true, pending: false };
    return {
      rows: [
        {
          key: row.version ?? `${row.environment}:current`,
          ...(row.version ? { version: row.version } : {}),
          ...(row.deployedAt ? { deployedAt: row.deployedAt } : {}),
          current: true,
        },
      ],
      unrecorded: true,
      pending: false,
    };
  }

  if (!builds) return { rows: [], unrecorded: false, pending: true };

  const rows: HistoryRow[] = [];
  const seen = new Set<string>();
  for (const build of [...builds].filter(deployed).sort(newestFirst)) {
    if (seen.has(build.tag)) continue;
    seen.add(build.tag);
    const current = Boolean(row?.version) && row?.version === build.tag;
    // Only the live row is dated, and only by its BINDING — the one deploy
    // stamp the platform actually kept. A superseded row's binding is gone
    // and the platform kept nothing in its place, so the row is left
    // undated: the build's finish time is when the BUILD ended, not when
    // that version started or stopped serving.
    const deployedAt = current ? row?.deployedAt : undefined;
    rows.push({
      key: build.tag,
      version: build.tag,
      milestoneNumber: build.milestoneNumber,
      ...(deployedAt ? { deployedAt } : {}),
      current,
    });
  }

  // A version the ledger does not list as completed — one still building, or
  // one the ledger lost — still IS what runs here. Its milestone comes from
  // the ledger all the same: the build row exists whatever its status, and
  // section 1 reads it the same way, so the two must not disagree.
  if (row?.version && !seen.has(row.version)) {
    const build = builds.find((b) => b.tag === row.version);
    rows.unshift({
      key: row.version,
      version: row.version,
      ...(build ? { milestoneNumber: build.milestoneNumber } : {}),
      ...(row.deployedAt ? { deployedAt: row.deployedAt } : {}),
      current: true,
    });
  }

  return { rows, unrecorded: false, pending: false };
}
