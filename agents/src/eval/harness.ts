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
 * Shared eval harness — the cross-cutting bits every agent eval reuses:
 * build a model from a plain env key (no org resolver), a transport-free
 * sink, programmatic scoring types, sampling, aggregation, and a committed
 * report. Per-agent `.eval.ts` files own their fixtures, run, and checks.
 *
 * These are EVALS (paid, non-deterministic generation) — fenced from the
 * deterministic `.test.ts` unit suite by the `.eval.ts` suffix and the
 * separate `npm run eval` script, so CI / `npm test` never spend tokens.
 */

import { readdir, readFile, mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { createModel } from "../shared/model.js";
import type { LanguageModel } from "ai";

/** Transport-free sink: tools push progress here; evals just collect it. */
export interface EvalSink {
  send(event: string, data: unknown): void;
  isClosed(): boolean;
}

export function makeCollectingSink(): {
  sink: EvalSink;
  frames: { event: string; data: unknown }[];
} {
  const frames: { event: string; data: unknown }[] = [];
  return {
    sink: {
      send: (event, data) => frames.push({ event, data }),
      isClosed: () => false,
    },
    frames,
  };
}

/**
 * Build a model from `ANTHROPIC_API_KEY` (+ optional `AGENT_MODEL`), bypassing
 * the per-org resolver. Returns null when no key is set, so evals can skip
 * cleanly instead of throwing.
 */
export function modelFromEnv(): LanguageModel | null {
  const apiKey = process.env.ANTHROPIC_API_KEY;
  if (!apiKey) return null;
  return createModel({ apiKey, model: process.env.AGENT_MODEL });
}

/** Number of samples per fixture (`EVAL_SAMPLES`, default 3). */
export function sampleCount(): number {
  return Math.max(1, parseInt(process.env.EVAL_SAMPLES || "3", 10));
}

export interface Check {
  name: string;
  pass: boolean;
  detail?: string;
}

export interface SampleResult {
  fixture: string;
  difficulty?: string;
  sample: number;
  pass: boolean;
  checks: Check[];
  usage?: { inputTokens: number; outputTokens: number };
  error?: string;
  ms: number;
}

/** A run passes iff every check passed. Convenience for `.eval.ts` files. */
export function allPass(checks: Check[]): boolean {
  return checks.every((c) => c.pass);
}

/**
 * Load + JSON-parse every fixture in an agent's `__fixtures__/` dir, sorted by
 * filename. `dirUrl` is the eval file's `import.meta.url`.
 */
export async function loadFixtures<T>(
  dirUrl: string,
  subdir = "__fixtures__",
): Promise<T[]> {
  const base = resolve(new URL(".", dirUrl).pathname, subdir);
  const names = (await readdir(base)).filter((n) => n.endsWith(".json")).sort();
  const out: T[] = [];
  for (const n of names) {
    out.push(JSON.parse(await readFile(join(base, n), "utf8")) as T);
  }
  return out;
}

export interface FixtureSummary {
  fixture: string;
  difficulty?: string;
  passed: number;
  total: number;
  passRate: number;
}

export interface Aggregate {
  totalSamples: number;
  passed: number;
  passRate: number;
  perFixture: FixtureSummary[];
}

export function aggregate(results: SampleResult[]): Aggregate {
  const byFixture = new Map<string, SampleResult[]>();
  for (const r of results) {
    const arr = byFixture.get(r.fixture) ?? [];
    arr.push(r);
    byFixture.set(r.fixture, arr);
  }
  const perFixture: FixtureSummary[] = [];
  for (const [fixture, arr] of byFixture) {
    const passed = arr.filter((r) => r.pass).length;
    perFixture.push({
      fixture,
      difficulty: arr[0]?.difficulty,
      passed,
      total: arr.length,
      passRate: arr.length ? passed / arr.length : 0,
    });
  }
  const passed = results.filter((r) => r.pass).length;
  return {
    totalSamples: results.length,
    passed,
    passRate: results.length ? passed / results.length : 0,
    perFixture,
  };
}

const pct = (n: number) => `${Math.round(n * 100)}%`;

/** Human summary block for stdout. */
export function formatSummary(
  agent: string,
  model: string,
  agg: Aggregate,
): string {
  const lines = [
    ``,
    `── eval: ${agent} (model=${model}) ───────────────────────────`,
    `overall: ${agg.passed}/${agg.totalSamples} (${pct(agg.passRate)})`,
    ``,
  ];
  for (const f of agg.perFixture) {
    const tier = f.difficulty ? ` [${f.difficulty}]` : "";
    lines.push(
      `  ${f.fixture}${tier}: ${f.passed}/${f.total} (${pct(f.passRate)})`,
    );
  }
  lines.push(``);
  return lines.join("\n");
}

/**
 * Write `<agent>-<stamp>.{json,md}` into `agents/eval-results/` (committed, so
 * runs are diffable and numbers are liftable into the talk). Returns paths.
 */
export async function writeReport(
  agent: string,
  model: string,
  results: SampleResult[],
  agg: Aggregate,
): Promise<{ jsonPath: string; mdPath: string }> {
  const dir = resolve(process.cwd(), "eval-results");
  await mkdir(dir, { recursive: true });
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const jsonPath = join(dir, `${agent}-${stamp}.json`);
  const mdPath = join(dir, `${agent}-${stamp}.md`);

  await writeFile(
    jsonPath,
    JSON.stringify({ agent, model, at: stamp, aggregate: agg, results }, null, 2),
  );

  const md = [
    `# Eval: ${agent}`,
    ``,
    `- model: \`${model}\``,
    `- at: ${stamp}`,
    `- overall: **${agg.passed}/${agg.totalSamples} (${pct(agg.passRate)})**`,
    ``,
    `| fixture | tier | pass rate |`,
    `|---|---|---|`,
    ...agg.perFixture.map(
      (f) =>
        `| ${f.fixture} | ${f.difficulty ?? ""} | ${f.passed}/${f.total} (${pct(f.passRate)}) |`,
    ),
    ``,
    `## Failed checks`,
    ``,
    ...failedCheckLines(results),
    ``,
  ].join("\n");
  await writeFile(mdPath, md);

  return { jsonPath, mdPath };
}

function failedCheckLines(results: SampleResult[]): string[] {
  const fails = results.filter((r) => !r.pass);
  if (fails.length === 0) return ["_none_"];
  return fails.map((r) => {
    const failed = r.error
      ? `error: ${r.error}`
      : r.checks
          .filter((c) => !c.pass)
          .map((c) => `${c.name}${c.detail ? ` (${c.detail})` : ""}`)
          .join(", ");
    return `- \`${r.fixture}\` #${r.sample}: ${failed}`;
  });
}
