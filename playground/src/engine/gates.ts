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
 * Phase preconditions (docs/design/playground.md §5). These are
 * playground-side UX conveniences — production has no server gate on the
 * console's spec paths; the tasks gate is a subset of the production build
 * gate (no v<N> tag exists locally). A blocked phase reports WHY.
 */

import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { checkComponentDesign } from "@aep/agent-stream";

export interface GateResult {
  ok: boolean;
  /** Human-readable reason when blocked. */
  reason?: string;
}

const ok: GateResult = { ok: true };
const blocked = (reason: string): GateResult => ({ ok: false, reason });

export function requirementsGate(): GateResult {
  return ok; // phase 1 has no precondition — the idea prompt seeds it
}

export function designGate(projectDir: string): GateResult {
  const file = join(projectDir, "specs/requirements/prd.md");
  if (!existsSync(file)) return blocked("specs/requirements/prd.md is missing — run the requirements phase first");
  if (readFileSync(file, "utf8").trim() === "") return blocked("specs/requirements/prd.md is empty");
  return ok;
}

/** Component names with a design dir (what justifies a Task). */
export function listComponents(projectDir: string): string[] {
  const dir = join(projectDir, "specs/design/components");
  if (!existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isDirectory())
    .map((e) => e.name)
    .sort();
}

/**
 * Key-flow files under specs/design/flows/ — one `.md` per flow, regular
 * files only, non-blank (an empty placeholder is not a flow). The single
 * definition the eval scorer and runner share.
 *
 * @knipkeep consumed by evals/spec-agents (structural scorer + runner), which is outside knip's workspaces
 */
export function listFlows(projectDir: string): string[] {
  const dir = join(projectDir, "specs/design/flows");
  if (!existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && e.name.endsWith(".md") && readFileSync(join(dir, e.name), "utf8").trim() !== "")
    .map((e) => e.name)
    .sort();
}

export function tasksGate(projectDir: string): GateResult {
  if (!existsSync(join(projectDir, "specs/design/design.cell"))) {
    return blocked("specs/design/design.cell is missing — run the design phase first");
  }
  const components = listComponents(projectDir);
  if (components.length === 0) return blocked("no components under specs/design/components/");
  let valid = 0;
  const problems: string[] = [];
  for (const name of components) {
    const rel = `specs/design/components/${name}/design.json`;
    const abs = join(projectDir, rel);
    if (!existsSync(abs)) continue;
    const problem = checkComponentDesign(rel, readFileSync(abs, "utf8"));
    if (problem === null) valid += 1;
    else problems.push(`${rel}: ${problem.message}`);
  }
  if (valid === 0) {
    return blocked(problems[0] ?? "no component has a design.json — regenerate the design");
  }
  return ok;
}
