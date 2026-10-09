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

/**
 * Retention for the DevTools capture file (`.devtools/generations.json`).
 *
 * The library caps nothing: it declares a `MAX_DB_BYTES` and never reads it, so
 * the file grows for as long as capture is enabled — observed at 80 MB beside a
 * 175 MB archive someone had already rotated by hand. Size is not a cosmetic
 * problem there: `saveDb` re-serialises the WHOLE db with `JSON.stringify(db,
 * null, 2)` and `writeFileSync`s it on EVERY step, so an 80 MB file means an
 * 80 MB synchronous write per step, on the event loop, while a turn streams.
 * That is why a big capture file reads as "the agent hung".
 *
 * WHEN this runs is the whole design. The library keeps the db in a
 * process-memory `dbCache`, lazily filled on its first write and flushed back
 * whole every time after; pruning the file underneath a running service would
 * simply be overwritten by the next step. So retention runs ONCE, at boot,
 * before the server accepts a request — which is also what keeps it off the
 * turn path. A `/start` turn never waits for it, because by the time any turn
 * can arrive the prune has already happened and the file is small.
 *
 * Best-effort by construction: a missing, unreadable or malformed file leaves
 * everything untouched and boots anyway. Trace capture is a debugging aid and
 * must never be able to stop the service — the same rule `nonFatalMiddleware`
 * enforces for the live path.
 */

import fs from "node:fs";
import path from "node:path";

/** A captured run. Only the fields retention needs are modelled. */
interface DevtoolsRun {
  id?: unknown;
  started_at?: unknown;
}

/** A captured step. Steps carry the bulk (raw_request/raw_response/raw_chunks). */
interface DevtoolsStep {
  run_id?: unknown;
}

export interface DevtoolsDb {
  runs: DevtoolsRun[];
  steps: DevtoolsStep[];
}

export interface PruneResult {
  db: DevtoolsDb;
  removedRuns: number;
  removedSteps: number;
}

/**
 * Drop every run started before `cutoff`, and every step belonging to one.
 *
 * Steps are pruned by OWNING RUN rather than by their own timestamp: a step is
 * what carries the raw request/response payloads, so orphans left behind by a
 * run-only prune would keep essentially all of the bytes and the file would
 * never shrink. A step whose `run_id` matches no surviving run is dropped for
 * the same reason — it can no longer be reached through the viewer.
 *
 * A run with an absent or unparseable `started_at` is KEPT. Retention deletes
 * data, so anything it cannot positively date is out of its remit.
 */
export function pruneDevtoolsDb(db: DevtoolsDb, cutoff: Date): PruneResult {
  const runs = Array.isArray(db.runs) ? db.runs : [];
  const steps = Array.isArray(db.steps) ? db.steps : [];
  const cutoffMs = cutoff.getTime();

  const keptRunIds = new Set<unknown>();
  const keptRuns = runs.filter((r) => {
    const started = typeof r?.started_at === "string" ? Date.parse(r.started_at) : Number.NaN;
    const keep = Number.isNaN(started) || started >= cutoffMs;
    if (keep) keptRunIds.add(r?.id);
    return keep;
  });

  const keptSteps = steps.filter((s) => keptRunIds.has(s?.run_id));

  return {
    db: { runs: keptRuns, steps: keptSteps },
    removedRuns: runs.length - keptRuns.length,
    removedSteps: steps.length - keptSteps.length,
  };
}

/**
 * Prune `<cwd>/.devtools/generations.json` in place, keeping `retentionDays` of
 * history. Returns a one-line summary for the boot log, or null when there was
 * nothing to do (no file, nothing old enough, or anything went wrong).
 *
 * Rewrites only when something was actually removed, so a warm boot on an
 * already-pruned file costs a read and no write.
 */
export function pruneDevtoolsFile(
  retentionDays: number,
  now: Date = new Date(),
  cwd: string = process.cwd(),
): string | null {
  const file = path.join(cwd, ".devtools", "generations.json");
  try {
    if (!fs.existsSync(file)) return null;
    const bytesBefore = fs.statSync(file).size;
    const parsed: unknown = JSON.parse(fs.readFileSync(file, "utf-8"));
    if (typeof parsed !== "object" || parsed === null) return null;

    const cutoff = new Date(now.getTime() - retentionDays * 24 * 60 * 60 * 1000);
    const { db, removedRuns, removedSteps } = pruneDevtoolsDb(parsed as DevtoolsDb, cutoff);
    if (removedRuns === 0 && removedSteps === 0) return null;

    // Match the library's own on-disk shape: it reads this file back with a
    // plain JSON.parse and re-writes it pretty-printed, so anything else here
    // would just be reformatted on the first step anyway.
    fs.writeFileSync(file, JSON.stringify(db, null, 2));
    const mb = (n: number): string => `${(n / 1024 / 1024).toFixed(1)}MB`;
    return (
      `@aep/ae-design-agent: devtools retention (${retentionDays}d) removed ${removedRuns} run(s) ` +
      `and ${removedSteps} step(s), ${mb(bytesBefore)} → ${mb(fs.statSync(file).size)}\n`
    );
  } catch {
    // Corrupt or unreadable capture is not a reason to fail a boot.
    return null;
  }
}
