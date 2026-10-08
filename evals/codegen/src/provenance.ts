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
 * What an attempt ran AGAINST, written at its start so a result can be placed
 * later: the commit, which harness-relevant paths were uncommitted, the exact
 * skill diff, the runner image's ID, every model id in play, and the
 * `agent-browser` version the walker drove.
 *
 * Paths and ids only: no env value is ever recorded.
 *
 * Best-effort throughout: a git or docker command that fails leaves its field
 * null rather than failing an attempt that has not started spending yet.
 */

import { spawnSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { runnerImage } from "@aep/playground/src/engine/runner-image.js";
import type { RunConfig } from "./case.js";
import { MODELS, PATHS, PROVENANCE } from "./config.js";
import { walkerAgentBrowserVersion } from "./walker.js";

export interface Provenance {
  writtenAt: string;
  git: { sha: string | null; branch: string | null; dirty: string[] };
  runnerImage: { name: string; id: string | null };
  coding: { config: string; runtime: string; model: string };
  models: { planner: string; walker: string; judge: string };
  /** `agent-browser --version` as the walk resolves it — the pinned copy, which must match the runner image's. */
  walkerAgentBrowser: string | null;
}

/**
 * Paths from `git status --porcelain` (v1) that fall under one of `roots`.
 */
export function dirtyPaths(porcelain: string, roots: readonly string[]): string[] {
  const out: string[] = [];
  for (const line of porcelain.split("\n")) {
    if (line.length < 4) continue;
    let path = line.slice(3);
    const arrow = path.indexOf(" -> ");
    if (arrow >= 0) path = path.slice(arrow + 4);
    if (path.startsWith('"') && path.endsWith('"')) path = path.slice(1, -1);
    if (roots.some((root) => path.startsWith(root))) out.push(path);
  }
  return out.sort();
}

export function writeProvenance(dir: string, config: RunConfig): Provenance {
  const image = runnerImage(config.runtime);
  const provenance: Provenance = {
    writtenAt: new Date().toISOString(),
    git: {
      sha: run("git", ["rev-parse", "HEAD"]),
      branch: run("git", ["rev-parse", "--abbrev-ref", "HEAD"]),
      dirty: dirtyPaths(run("git", ["status", "--porcelain"], false) ?? "", PROVENANCE.dirtyRoots),
    },
    runnerImage: { name: image, id: run("docker", ["image", "inspect", "--format", "{{.Id}}", image]) },
    coding: { config: config.id, runtime: config.runtime, model: config.model },
    models: { planner: MODELS.planner, walker: MODELS.walker, judge: MODELS.judge },
    walkerAgentBrowser: walkerAgentBrowserVersion(),
  };
  writeFileSync(join(dir, "provenance.json"), JSON.stringify(provenance, null, 2));
  writeFileSync(join(dir, "skills.diff"), skillsDiff());
  return provenance;
}

/**
 * `git diff HEAD -- skills/`, plus every UNTRACKED file under `skills/` as a
 * new-file hunk — a skill reference added in the edit loop is invisible to
 * `git diff` and would otherwise be the one change the record misses.
 * Empty when the library is clean.
 */
function skillsDiff(): string {
  const tracked = run("git", ["diff", "HEAD", "--", PROVENANCE.diffRoot], false) ?? "";
  const untracked = (run("git", ["ls-files", "--others", "--exclude-standard", "--", PROVENANCE.diffRoot], false) ?? "")
    .split("\n")
    .filter(Boolean);
  // `--no-index` exits 1 when the files differ, which they always do here.
  const added = untracked.map((file) => run("git", ["diff", "--no-index", "--", "/dev/null", file], false, [0, 1]) ?? "");
  return [tracked, ...added].filter(Boolean).join("");
}

function run(cmd: string, args: string[], trim = true, okCodes: number[] = [0]): string | null {
  const result = spawnSync(cmd, args, { cwd: PATHS.repoRoot, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  if (result.error || result.status === null || !okCodes.includes(result.status)) return null;
  return trim ? result.stdout.trim() || null : result.stdout;
}
