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
 * One place for the eval framework's knobs: model choices, paths, and env.
 */

import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/** The repo checkout (evals/spec-agents/src → up 3). */
export const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

/** This package's root. */
export const PACKAGE_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/**
 * Where eval runs create their throwaway projects — under the ONE sanctioned
 * in-repo home for playground projects (gitignored; playground/src/paths.ts).
 */
export const PROJECTS_HOME = join(REPO_ROOT, "playground", ".projects", "spec-agent-evals");

/** Scenario YAML + captured fixtures (data, committed). */
export const SCENARIOS_DIR = join(PACKAGE_ROOT, "scenarios");
export const FIXTURES_DIR = join(SCENARIOS_DIR, "fixtures");

/** The human review queue: one markdown sheet per review-or-worse run. */
export const REVIEWS_DIR = join(PACKAGE_ROOT, "eval-reviews");

/**
 * Sim user + judge model (#354/#355). claude-sonnet-5-5 rejects a non-default
 * temperature/top_p/top_k with a 400, so none is passed; pass temperature 0 on
 * a model that accepts one.
 */
export const SIM_MODEL = "claude-sonnet-5-5";
export const JUDGE_MODEL = "claude-sonnet-5-5";

/** Default per-section agent-turn cap (#354); scenarios may override. */
export const DEFAULT_MAX_TURNS = 10;

/**
 * `EVAL_REPEATS=N` runs every scenario N times so the score SPREAD is visible
 * (#355: agent variance is signal, surfaced on demand — not suppressed).
 */
export function repeats(): number {
  const n = Number(process.env.EVAL_REPEATS ?? "1");
  return Number.isInteger(n) && n > 1 ? n : 1;
}

/**
 * The sim + judge read the key from the environment; the agent under eval gets
 * it via the playground session (which loads deployments/.env itself). Mirror
 * that fallback here so one exported variable serves all three.
 */
export function ensureAnthropicKey(): string {
  if (!process.env.ANTHROPIC_API_KEY) {
    const envFile = join(REPO_ROOT, "deployments", ".env");
    if (existsSync(envFile)) {
      const m = /^ANTHROPIC_API_KEY=(.+)$/m.exec(readFileSync(envFile, "utf8"));
      if (m?.[1]) process.env.ANTHROPIC_API_KEY = m[1].trim();
    }
  }
  const key = process.env.ANTHROPIC_API_KEY;
  if (!key) throw new Error("ANTHROPIC_API_KEY is not set (env or deployments/.env)");
  return key;
}
