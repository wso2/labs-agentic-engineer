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
 * The report — median and spread per case × config, a baseline diff that says
 * `inconclusive` when a move is inside the noise, and one line per attempt
 * pointing at its archive (ADR-0005).
 *
 * Harness errors are counted but excluded from statistics; hard fails score 0
 * and count.
 */

import type { AttemptRecord } from "./attempt.js";

export interface Stat {
  median: number;
  min: number;
  max: number;
  /** max - min — the bar a claimed delta has to clear. */
  spread: number;
}

export interface Summary {
  /** `<case> × <config>`. */
  key: string;
  case: string;
  config: string;
  /** Attempts that count — everything but harness errors. */
  n: number;
  score: Stat;
  hardFails: number;
  harnessErrors: number;
  /** Median coding-run wall clock, minutes; null when no attempt got that far. */
  codingMinutes: number | null;
  /** Median coding cost (Claude Code's estimate), USD; null when none was reported. */
  codingCostUsd: number | null;
}

export function summaryKey(caseName: string, config: string): string {
  return `${caseName} × ${config}`;
}

/** Per case × config, without rewalks (they re-score an existing coding run). */
export function summarize(records: AttemptRecord[]): Summary[] {
  const groups = new Map<string, AttemptRecord[]>();
  for (const record of records.filter((r) => r.kind !== "rewalk")) {
    const key = summaryKey(record.case, record.config);
    groups.set(key, [...(groups.get(key) ?? []), record]);
  }
  return [...groups.entries()]
    .map(([key, group]) => {
      const counted = group.filter((r) => r.status !== "harness-error");
      const minutes = counted.flatMap((r) => (r.coding.minutes === null ? [] : [r.coding.minutes]));
      const costs = counted.flatMap((r) => (r.coding.costUsd === null ? [] : [r.coding.costUsd]));
      return {
        key,
        case: group[0]?.case ?? "",
        config: group[0]?.config ?? "",
        n: counted.length,
        score: stat(counted.map((r) => r.score ?? 0)),
        hardFails: group.filter((r) => r.status === "hard-fail").length,
        harnessErrors: group.length - counted.length,
        codingMinutes: minutes.length ? stat(minutes).median : null,
        codingCostUsd: costs.length ? stat(costs).median : null,
      };
    })
    .sort((a, b) => a.key.localeCompare(b.key));
}

export function stat(values: number[]): Stat {
  if (values.length === 0) return { median: 0, min: 0, max: 0, spread: 0 };
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  const median = sorted.length % 2 === 0 ? ((sorted[mid - 1] ?? 0) + (sorted[mid] ?? 0)) / 2 : (sorted[mid] ?? 0);
  const min = sorted[0] ?? 0;
  const max = sorted[sorted.length - 1] ?? 0;
  return { median: round(median), min: round(min), max: round(max), spread: round(max - min) };
}

/**
 * The baseline for ONE row: the most recent earlier sweep that ran the same
 * case × config with no harness errors in it (ADR-0005). A row with a harness
 * error is skipped because its n is not the n it claims to compare against.
 *
 * `candidates` are sweep ids; ids sort chronologically (ISO timestamps).
 */
export function pickBaseline(
  key: string,
  currentSweep: string,
  candidates: string[],
  read: (sweep: string) => Summary[] | undefined,
): { sweep: string; summary: Summary } | undefined {
  for (const sweep of [...candidates].sort().reverse()) {
    if (sweep >= currentSweep) continue;
    const row = read(sweep)?.find((s) => s.key === key);
    if (row && row.harnessErrors === 0 && row.n > 0) return { sweep, summary: row };
  }
  return undefined;
}

/** A delta, and whether it clears the noise. */
export function compare(now: Summary, before: Summary | undefined): string {
  if (!before) return "—";
  const delta = round(now.score.median - before.score.median);
  if (now.n < 2 || before.n < 2) return `${signed(delta)} (inconclusive — n=1 has no spread)`;
  if (delta === 0) return "unchanged";
  const bar = Math.max(now.score.spread, before.score.spread);
  return Math.abs(delta) <= bar ? `${signed(delta)} (inconclusive — spread is ${String(bar)})` : `${signed(delta)} (was ${String(before.score.median)})`;
}

export interface ReportInput {
  sweepId: string;
  facts: Record<string, string>;
  notes: string[];
  summaries: Summary[];
  records: AttemptRecord[];
  /** Rewalks of this sweep's attempts — listed, never counted. */
  rewalks: AttemptRecord[];
  baselines: Map<string, { sweep: string; summary: Summary }>;
}

export function renderReport(input: ReportInput): string {
  const lines: string[] = [`# Codegen eval sweep ${input.sweepId}`, ""];
  lines.push("| | |", "|---|---|");
  for (const [k, v] of Object.entries(input.facts)) lines.push(`| ${k} | ${v} |`);
  lines.push("");
  for (const note of input.notes) lines.push(`> ${note}`, "");

  lines.push(
    "| case × config | n | median | min | max | spread | hard fails | harness errors | coding min (median) | coding $ (median) | vs baseline |",
    "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|",
  );
  for (const s of input.summaries) {
    const base = input.baselines.get(s.key);
    lines.push(
      `| ${s.key} | ${String(s.n)} | ${String(s.score.median)} | ${String(s.score.min)} | ${String(s.score.max)} | ` +
        `${String(s.score.spread)} | ${String(s.hardFails)} | ${String(s.harnessErrors)} | ${fmt(s.codingMinutes)} | ` +
        `${fmt(s.codingCostUsd)} | ${base ? `${compare(s, base.summary)} vs ${base.sweep}` : "—"} |`,
    );
  }
  lines.push(
    "",
    "Harness errors are counted but excluded from n and every statistic; hard fails score 0 and are included. " +
      "Each failed attempt below names its phase and cause: `app` (the generated app failed — a hard fail) or " +
      "`environment` (docker, the runner, the provider, the browser, the harness — a harness error).",
    "",
  );

  lines.push("## Attempts", "");
  for (const r of input.records.filter((record) => record.kind !== "rewalk")) lines.push(attemptLine(r));
  lines.push("");
  if (input.rewalks.length > 0) {
    lines.push(
      "## Rewalks — excluded from every statistic above",
      "",
      "An attempt's archived code, wired and walked again against the checklist as it stood at rewalk time.",
      "",
    );
    for (const r of input.rewalks) lines.push(attemptLine(r));
    lines.push("");
  }
  return lines.join("\n");
}

/** One attempt, one line: what happened, the worst of what failed, where to look. */
export function attemptLine(r: AttemptRecord): string {
  const rewalk = r.kind === "rewalk" ? ` rewalk-${String(r.rewalk ?? 0)}` : "";
  const head = `- **${r.case} × ${r.config} #${String(r.attempt)}${rewalk}** ${r.status}`;
  const score = r.score === null ? "" : ` · ${String(r.score)} ${r.band ?? ""}`;
  const why = failureText(r);
  const symptom = why ? ` · ${why}` : "";
  const top = r.failing.slice(0, 3).map((f) => f.id);
  const failing = top.length ? ` · failing: ${top.join(", ")}${r.failing.length > 3 ? ` (+${String(r.failing.length - 3)})` : ""}` : "";
  return `${head}${score}${symptom}${failing} — \`${r.archive}\``;
}

/**
 * Why a record did not score, as one line: the phase, whose failure it was
 * (`app` is the generated app's, `environment` everything else's), and the
 * reason — then any note on the record.
 */
export function failureText(r: AttemptRecord): string {
  const parts: string[] = [];
  if (r.failure) parts.push(`${r.failure.phase} failed (${r.failure.cause}): ${r.failure.reason}`);
  if (r.symptom) parts.push(r.symptom);
  return parts.join("; ");
}

function signed(n: number): string {
  return n > 0 ? `+${String(n)}` : String(n);
}

function fmt(n: number | null): string {
  return n === null ? "—" : String(round(n));
}

function round(n: number): number {
  return Math.round(n * 100) / 100;
}
