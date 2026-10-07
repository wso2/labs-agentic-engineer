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
import type { ProjectBuild } from "../api/builds";
import type { ValidationOutcome } from "./validation";

// Build history, the project's version ledger: every version built, newest
// first, as today's ledger read lists them (list-project-builds), each with
// what the builds list adds (the features it built, the version a repair
// build fixes), when it ran, and its state in a few words: building,
// passing, N failing.

type BuildSummary = components["schemas"]["BuildSummary"];

export interface VersionRow {
  version: string;
  status: BuildSummary["status"];
  /** The version this repair build fixes; null for a version of its own. */
  fixes: string | null;
  /** The features it built, by name. */
  features: string[];
  /** The same features, by ID. */
  featureIds: string[];
  /** Failing scenarios its latest validation found that passed in the version before (B4). */
  regressions: number;
  startedAt: string;
  /** Null while it builds. */
  completedAt: string | null;
}

/** The ledger's rows, newest first, as the ledger read orders them. */
export function versionRows(
  summaries: Pick<BuildSummary, "tag" | "status" | "regressions" | "startedAt" | "completedAt">[],
  builds: Pick<ProjectBuild, "version" | "fixes" | "features">[],
): VersionRow[] {
  return summaries.map((s) => {
    const build = builds.find((b) => b.version === s.tag);
    return {
      version: s.tag,
      status: s.status,
      fixes: build?.fixes ?? null,
      features: build?.features.map((f) => f.name) ?? [],
      featureIds: build?.features.map((f) => f.id) ?? [],
      regressions: s.regressions ?? 0,
      startedAt: s.startedAt,
      completedAt: s.completedAt ?? null,
    };
  });
}

export function isBuilding(status: BuildSummary["status"]): boolean {
  return status === "started" || status === "in_progress";
}

/** The build that fixes this version, if one was started. */
export function fixedBy(rows: VersionRow[], version: string): VersionRow | null {
  return rows.find((r) => r.fixes === version) ?? null;
}

export type LedgerTone = "primary" | "success" | "warning" | "error";

/** A version's state in a few words, and its colour: never the colour alone. */
export function versionState(
  status: BuildSummary["status"],
  outcome: ValidationOutcome | null,
  regressions = 0,
): { label: string; tone: LedgerTone | null } {
  if (isBuilding(status)) return { label: "building", tone: "primary" };
  if (status === "failed") return { label: "failed", tone: "error" };
  if (status === "cancelled") return { label: "cancelled", tone: null };
  if (!outcome) return { label: "built", tone: null };
  if (outcome.failing.length > 0) {
    const regressed = regressions > 0 ? ` · ${regressions} regression${regressions === 1 ? "" : "s"}` : "";
    return { label: `${outcome.failing.length} failing${regressed}`, tone: "warning" };
  }
  return { label: "passing", tone: "success" };
}
