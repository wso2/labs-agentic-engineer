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
 * The report, which is the product — a sweep exists to change someone's mind
 * about a CLI flag or a skill line, and it can only do that if a reader can
 * tell a real move from noise.
 *
 * MEDIAN AND SPREAD, never a mean and never a bare number. The reason is
 * measured rather than statistical hygiene: between two real playground runs
 * the library-token total fell 32% while the answers got worse, and a single
 * figure per metric is what made that look like an improvement. A delta smaller
 * than the spread it sits inside is reported as inconclusive, in those words.
 */

import { type ReportedMetric } from "./config.js";
import type { Attempt } from "./sweep.js";

export interface Summary {
  key: string;
  suite: string;
  case: string;
  attempts: number;
  /** How many of the attempts produced a package that compiles. */
  green: number;
  /** Attempts with no expectation violations at all. */
  clean: number;
  stats: Record<string, Stat>;
  /** Every distinct violation seen, with how many attempts hit it. */
  violations: Record<string, number>;
}

export interface Stat {
  median: number;
  min: number;
  max: number;
  /** max - min. The number a claimed delta has to beat. */
  spread: number;
}

/**
 * How each column in `REPORTED_METRICS` is read off an attempt.
 *
 * The NAMES and their order live in `config.ts` with every other knob; the
 * accessors stay here because a closure over a type is code. `metricsMatchConfig`
 * in the tests pins the two together, so a column added to one and not the other
 * fails there rather than silently vanishing from a report.
 */
const METRICS: { key: ReportedMetric; label: string; of: (a: Attempt) => number }[] = [
  // The outcomes first: how much the agent had to read, and whether what it read
  // was right. These are what the tool is for.
  { key: "lookupTokens", label: "lookup tokens", of: (a) => a.path.lookupTokens },
  { key: "sigErrors", label: "signature errors", of: (a) => a.build.totalSignatureErrors },
  { key: "buildCycles", label: "agent build cycles", of: (a) => a.build.cycles },
  { key: "otherErrors", label: "other errors", of: (a) => a.build.totalErrors - a.build.totalSignatureErrors },
  // Then the diagnostics. TURNS and INVOCATIONS are different numbers and both are
  // printed: a chained `bal library a ; bal library b` is one round trip and two
  // questions, so a turn measures latency and context while an invocation measures
  // what was asked. One column labelled "calls" hid which of the two it meant.
  { key: "turns", label: "bal library turns", of: (a) => a.path.lookups },
  {
    key: "invocations",
    label: "…invocations",
    of: (a) => Object.values(a.path.byVerb).reduce((sum, n) => sum + n, 0),
  },
  { key: "toolMissing", label: "bal library MISSING", of: (a) => a.path.toolMissing },
  { key: "piped", label: "…piped", of: (a) => a.path.piped },
  { key: "truncated", label: "…piped AND cut", of: (a) => a.path.truncated },
  { key: "failed", label: "…failed", of: (a) => a.path.failed },
  { key: "sawNext", label: "…saw ## Next", of: (a) => a.path.sawNext },
  { key: "detourCalls", label: "worst detour (calls)", of: (a) => a.path.worstDetour.calls },
  { key: "costUsd", label: "cost (USD)", of: (a) => a.path.costUsd },
  { key: "durationSeconds", label: "duration (s)", of: (a) => Math.round(a.path.durationMs / 1000) },
];

/** The column keys this module can render — read by the test that pins it to config. */
export const METRIC_KEYS: readonly string[] = METRICS.map((m) => m.key);

export function summarize(attempts: Attempt[]): Summary[] {
  const groups = new Map<string, Attempt[]>();
  for (const a of attempts) {
    const key = `${a.suite}/${a.case}`;
    groups.set(key, [...(groups.get(key) ?? []), a]);
  }
  return [...groups.entries()]
    .map(([key, group]) => {
      const stats: Record<string, Stat> = {};
      for (const metric of METRICS) stats[metric.key] = stat(group.map(metric.of));
      const violations: Record<string, number> = {};
      for (const a of group) {
        for (const v of a.violations) violations[v] = (violations[v] ?? 0) + 1;
      }
      const first = group[0];
      return {
        key,
        suite: first?.suite ?? "",
        case: first?.case ?? "",
        attempts: group.length,
        green: group.filter((a) => a.verified.green).length,
        clean: group.filter((a) => a.violations.length === 0).length,
        stats,
        violations,
      };
    })
    .sort((a, b) => a.key.localeCompare(b.key));
}

export function stat(values: number[]): Stat {
  if (values.length === 0) return { median: 0, min: 0, max: 0, spread: 0 };
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  const median =
    sorted.length % 2 === 0 ? ((sorted[mid - 1] ?? 0) + (sorted[mid] ?? 0)) / 2 : (sorted[mid] ?? 0);
  const min = sorted[0] ?? 0;
  const max = sorted[sorted.length - 1] ?? 0;
  return { median: round(median), min: round(min), max: round(max), spread: round(max - min) };
}

export interface ReportInput {
  summaries: Summary[];
  facts: Record<string, string>;
  notes: string[];
  /** The previous sweep, when there is one to compare against. */
  baseline?: Summary[];
  concurrency: number;
}

export function renderReport(input: ReportInput): string {
  const lines: string[] = ["# Ballerina eval sweep", ""];

  lines.push("| | |", "|---|---|");
  for (const [k, v] of Object.entries(input.facts)) lines.push(`| ${k} | ${v} |`);
  lines.push(`| concurrency | ${input.concurrency} |`);
  lines.push("");
  if (input.concurrency > 1) {
    // Stated rather than left for a reader to work out: the wall-clock column
    // is the one number a concurrent sweep cannot compare against a serial one.
    lines.push(
      "> Ran " +
        input.concurrency +
        " attempts at a time, so **duration is not comparable** against a sweep at a different " +
        "concurrency. Call counts, token counts and error counts are unaffected.",
      "",
    );
  }
  for (const note of input.notes) lines.push(`> ${note}`, "");

  for (const summary of input.summaries) {
    lines.push(`## ${summary.key}`, "");
    lines.push(
      `${summary.green}/${summary.attempts} built · ${summary.clean}/${summary.attempts} met every expectation`,
      "",
    );
    // ABOVE the table, because it says the table is not evidence. A `bal tool pull`
    // inside a session can drop the locally installed `library` registration, and
    // every lookup after that answers `unknown command 'library'` — see
    // PathMetrics.toolMissing. Numbers from such an attempt describe a run with no
    // tool in it, and they average in silently with runs that had one.
    const missing = summary.stats.toolMissing;
    if (missing && missing.max > 0) {
      lines.push(
        `> **NOT EVIDENCE — \`bal library\` was missing for ${missing.min}–${missing.max} lookups.** ` +
          "Something in the session rewrote `~/.ballerina/.config/bal-tools.toml` and dropped the local " +
          "tool. Re-install it (`packages/bal-library-tool/install-local.sh`) and re-run this case; " +
          "disregard every number below.",
        "",
      );
    }
    const base = input.baseline?.find((b) => b.key === summary.key);
    lines.push(base ? "| metric | median | spread | vs baseline |" : "| metric | median | spread |");
    lines.push(base ? "|---|---:|---:|---|" : "|---|---:|---:|");
    for (const metric of METRICS) {
      const s = summary.stats[metric.key];
      if (!s) continue;
      const row = [`| ${metric.label} | ${s.median} | ${s.min}–${s.max} `];
      if (base) row.push(`| ${compare(s, base.stats[metric.key])} `);
      lines.push(`${row.join("")}|`);
    }
    lines.push("");
    if (Object.keys(summary.violations).length > 0) {
      lines.push("Violations:", "");
      for (const [v, n] of Object.entries(summary.violations)) {
        lines.push(`- ${v} (${n}/${summary.attempts})`);
      }
      lines.push("");
    }
  }
  return lines.join("\n");
}

/**
 * Which previous sweep the baseline column compares against: the most recent
 * one that ran EVERY case this sweep ran.
 *
 * Newest-directory-wins is the obvious rule and it is wrong — measured wrong. A
 * one-case debug re-run of `catalog-redis` sat between two full sweeps, so the
 * full sweep after it would have dashed six cases and diffed the seventh
 * against a run made under different conditions: six phantom regressions and
 * one number nobody could interpret, in the column a reader trusts most.
 *
 * Requiring coverage means every row in the column answers the same question,
 * or there is no column. A narrower sweep still baselines against a wider run
 * that contains it, which is the case that matters when iterating on one suite.
 * An unreadable summary is skipped rather than fatal: an older format should
 * cost the comparison, never the run.
 */
export function pickBaseline(
  candidates: string[],
  currentKeys: string[],
  read: (dir: string) => Summary[] | undefined,
): Summary[] | undefined {
  for (const dir of [...candidates].sort().reverse()) {
    const summaries = read(dir);
    if (!summaries) continue;
    // A sweep that ran without `bal library` describes conditions that did not include
    // the thing under test, so it cannot be a baseline. Measured: a contaminated sweep
    // reported telemetry-kafka at 16 lookup tokens, and the next clean one printed
    // "+6484" against it — the tool coming back, rendered as a 400x regression.
    //
    // ONE spoiled case disqualifies the whole sweep, not just its own row: the attempts
    // ran concurrently against one `bal-tools.toml`, so a neighbour's clean row is not
    // evidence that the neighbour was unaffected.
    if (summaries.some((s) => (s.stats.toolMissing?.max ?? 0) > 0)) continue;
    const covered = new Set(summaries.map((s) => s.key));
    if (currentKeys.every((k) => covered.has(k))) return summaries;
  }
  return undefined;
}

/**
 * A delta, and whether it is big enough to mean anything.
 *
 * The bar is the WIDER of the two spreads: a change that moves the median less
 * than the run-to-run variation of either sweep has not been shown to move
 * anything, and saying "inconclusive" is the whole reason repeats exist.
 */
export function compare(now: Stat, before: Stat | undefined): string {
  if (!before) return "—";
  const delta = round(now.median - before.median);
  if (delta === 0) return "unchanged";
  const bar = Math.max(now.spread, before.spread);
  const sign = delta > 0 ? "+" : "";
  return Math.abs(delta) <= bar
    ? `${sign}${delta} (inconclusive — spread is ${bar})`
    : `${sign}${delta} (was ${before.median})`;
}

function round(n: number): number {
  return Math.round(n * 1000) / 1000;
}
