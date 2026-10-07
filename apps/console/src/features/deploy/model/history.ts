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
import type { EnvironmentColumn } from "./pipeline";

// The history under the board: what has been deployed where, newest first.
//
// The platform keeps no deployment record. What it does keep: the version
// ledger, whose every completed build deployed to the pipeline's first
// environment (a build lands there by itself), and each environment's current
// deployments, stamped when they were made. So the first environment's past
// is the ledger, and any other environment's is what it runs now. A build's
// time is when the build finished, so a row says so rather than passing it
// off as the deploy's. There are no promotions to list until there is a
// promote operation.

type BuildSummary = components["schemas"]["BuildSummary"];

export interface HistoryRow {
  key: string;
  /** The version, where a read names it. */
  version: string | null;
  environment: string;
  /** "Deployed after its build", "Deployed". */
  what: string;
  /** The stamp the row's time is, and what it is the time of. */
  at: { iso: string; of: "build" | "deploy" } | null;
  /** What the environment runs now. */
  current: boolean;
}

function newest(stamps: (string | undefined)[]): string | undefined {
  return stamps.filter((s): s is string => Boolean(s)).sort().at(-1);
}

/** The history, newest first. `builds` is the version ledger. */
export function deployHistory(columns: readonly EnvironmentColumn[], builds: readonly BuildSummary[]): HistoryRow[] {
  const rows: HistoryRow[] = [];
  const entry = columns.find((c) => c.entry);
  if (entry) {
    const completed = builds
      .filter((b) => b.status === "completed")
      .sort((a, b) => (b.completedAt ?? b.startedAt).localeCompare(a.completedAt ?? a.startedAt));
    for (const b of completed) {
      const current = entry.running && entry.version === b.tag;
      rows.push({
        key: `${entry.name}:${b.tag}`,
        version: b.tag,
        environment: entry.label,
        what: "Deployed after its build",
        at: b.completedAt ? { iso: b.completedAt, of: "build" } : null,
        current,
      });
    }
  }
  for (const column of columns) {
    if (column.entry || !column.running) continue;
    const at = newest(column.components.map((c) => c.deployment?.createdAt));
    rows.push({
      key: `${column.name}:current`,
      version: null,
      environment: column.label,
      what: "Deployed",
      at: at ? { iso: at, of: "deploy" } : null,
      current: true,
    });
  }
  return rows.sort((a, b) => (b.at?.iso ?? "").localeCompare(a.at?.iso ?? ""));
}
