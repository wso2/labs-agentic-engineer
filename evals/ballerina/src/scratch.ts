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
 * The throwaway package one attempt runs in, and the skill it can see.
 *
 * Every attempt gets its OWN directory, which is what makes concurrency safe
 * and repeats comparable: `target/` is an incremental cache, so a second
 * attempt reusing the first's directory would measure a warm build and call it
 * a cold one.
 *
 * What is deliberately NOT per-attempt is `~/.ballerina` — the central package
 * cache and the `bal library` disk cache both live there and are shared by
 * every attempt on the machine. That is correct rather than a compromise: a
 * real developer has a warm cache, and giving each attempt a cold one would
 * measure Central's latency instead of the agent's path. `warmCache` exists so
 * the first attempt of a sweep does not pay for all of them.
 */

import { execFileSync, execSync } from "node:child_process";
import { cpSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import type { EvalCase } from "./cases.js";

export interface Scratch {
  /** The Ballerina package root — the session's `cwd`. Outside the repo; see prepareScratch. */
  workspace: string;
  /** `.logs/` and metrics live here, OUTSIDE the workspace so a build never sees them. */
  runDir: string;
  /** Where the finished package is copied for a human to read. See `archive`. */
  archiveDir: string;
}

/**
 * Lay out one attempt: a real `bal new` package with the skill mirrored in.
 *
 * `bal new` rather than a hand-written `Ballerina.toml` because the scaffold is
 * what a real task starts from, down to the `main.bal` the skill tells the agent
 * to delete — and a case that starts from a shape no real project has would tune
 * the skill against a fiction.
 *
 * The package is staged in a TEMP DIRECTORY rather than under `.runs/`, and this
 * is the whole point of the function rather than an implementation detail. When
 * the workspace lived inside the repo, `cases/` was a few `..` from every
 * session's `cwd` — and in the 2026-08-16 sweep the `claims-fhir` attempt did
 * exactly what that permits: it ran `find … /evals/ballerina/cases -iname
 * '*claims*fhir*'`, read its own `expect.imports`, concluded that "the grading
 * only checks that the build succeeds and that the expected imports are present",
 * and REVERSED a design decision it had already made. Three other attempts in the
 * same sweep never looked, which is worse than if all of them had: the
 * contamination is per-attempt, so the runs are not comparable to each other.
 *
 * An attempt that can read its own answer key is not measuring the skill, and no
 * import assertion made against one means anything. `archive` copies the finished
 * package back under `.runs/` afterwards, so the artifact a human opens is
 * unchanged — only the session's reachable filesystem is.
 */
export function prepareScratch(root: string, evalCase: EvalCase, attempt: number, skillsDir: string): Scratch {
  const base = join(root, evalCase.suite, evalCase.name, `attempt-${attempt}`);
  const runDir = join(base, "run");
  mkdirSync(runDir, { recursive: true });

  const pkg = evalCase.packageName ?? evalCase.name.replace(/-/g, "_");
  const staging = mkdtempSync(join(tmpdir(), "bal-eval-"));
  execFileSync("bal", ["new", pkg], { cwd: staging, stdio: "pipe" });
  const workspace = join(staging, pkg);

  mirrorSkill(skillsDir, workspace);
  plantFixtures(evalCase, workspace);
  return { workspace, runDir, archiveDir: join(base, pkg) };
}

/**
 * Copy the finished package out of staging and into `.runs/`, then drop staging.
 *
 * Called after the verifying build, so what lands is the package as it was
 * scored — including `target/`, because the README promises a directory you can
 * `cd` into and `bal build` by hand.
 *
 * Failure here is deliberately swallowed by the caller rather than thrown: the
 * attempt has already been measured, and losing the archive copy is a worse
 * reason to fail a sweep than it is a thing to fail a sweep over.
 */
export function archive(scratch: Scratch): void {
  mkdirSync(dirname(scratch.archiveDir), { recursive: true });
  cpSync(scratch.workspace, scratch.archiveDir, { recursive: true });
  rmSync(dirname(scratch.workspace), { recursive: true, force: true });
}

/**
 * Copy the ballerina skill into the workspace the way the BFF mirrors an org's
 * skills into a clone: `<workspace>/.claude/skills/<name>/`.
 *
 * Copied rather than symlinked because the SDK resolves the skill's own
 * `references/` relative to the file, and because a symlink into the working
 * tree would let a session write to the real skill.
 */
export function mirrorSkill(skillsDir: string, workspace: string): void {
  const target = join(workspace, ".claude", "skills", "ballerina");
  mkdirSync(dirname(target), { recursive: true });
  cpSync(join(skillsDir, "ballerina"), target, { recursive: true });
}

/**
 * Copy each fixture in, so the agent's first move can be `bal openapi -i
 * openapi.yaml` against the very bytes the case ships.
 *
 * Copied per attempt rather than shared, because the session can write to the
 * package and a spec edited in place would leak into the next attempt's run.
 */
function plantFixtures(evalCase: EvalCase, workspace: string): void {
  for (const [relative, source] of Object.entries(evalCase.fixtures ?? {})) {
    // Fixture paths are authored, not agent-supplied, but a `../` here would
    // write outside the scratch tree and the cost of refusing is nil.
    const destination = resolve(workspace, relative);
    if (!destination.startsWith(resolve(workspace))) {
      throw new Error(`${evalCase.file}: fixture path escapes the workspace: ${relative}`);
    }
    mkdirSync(dirname(destination), { recursive: true });
    cpSync(source, destination);
  }
}

export interface VerifyResult {
  exitCode: number;
  output: string;
  /** Import paths found in the produced `.bal` files. */
  imports: string[];
}

/**
 * The harness's OWN build, run after the session stops.
 *
 * Separate from the agent's attempts on purpose. The agent's last `bal build`
 * may have been three edits ago, or may never have happened — and a session
 * that wrote code it never compiled must not be reported as green. This is the
 * number that decides `builds`.
 */
export function verify(workspace: string): VerifyResult {
  let exitCode = 0;
  let output = "";
  try {
    output = execSync("bal build", { cwd: workspace, encoding: "utf8", stdio: "pipe" });
  } catch (e) {
    const err = e as { status?: number; stdout?: string; stderr?: string };
    exitCode = err.status ?? 1;
    output = `${err.stdout ?? ""}${err.stderr ?? ""}`;
  }
  return { exitCode, output, imports: readImports(workspace) };
}

/**
 * Every `.bal` the agent wrote, concatenated — what a source assertion matches.
 *
 * Scoped to the package's OWN sources. `target/` is build output, and
 * `.claude/skills/` is the mirrored skill, which carries worked examples: a
 * pattern like `http:RetryConfig` appears in `code-rules.md` whether or not the
 * agent wrote it, so matching the skill would let every case assert itself.
 */
export function readSources(workspace: string): string {
  const parts: string[] = [];
  const walk = (dir: string): void => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (entry.name === "target" || entry.name === ".claude") continue;
        walk(join(dir, entry.name));
      } else if (entry.name.endsWith(".bal")) {
        parts.push(readFileSync(join(dir, entry.name), "utf8"));
      }
    }
  };
  try {
    walk(workspace);
  } catch {
    return "";
  }
  return parts.join("\n");
}

/** Every `import org/name...;` across the package's own sources. */
export function readImports(workspace: string): string[] {
  let listing = "";
  try {
    // -h keeps the filename off the match, --include keeps `target/` and the
    // mirrored skill's markdown out of it.
    listing = execSync(`grep -rhoE "^import [a-z0-9_.]+/[a-z0-9_.]+" --include="*.bal" . || true`, {
      cwd: workspace,
      encoding: "utf8",
      stdio: "pipe",
    });
  } catch {
    return [];
  }
  const found = new Set<string>();
  for (const line of listing.split("\n")) {
    const match = /^import\s+(\S+)/.exec(line.trim());
    if (match?.[1]) found.add(match[1]);
  }
  return [...found].sort();
}

/**
 * Pull the packages a sweep is about to need, once, before the fan-out.
 *
 * Without this the first attempt of each package pays a cold Central fetch
 * (measured at 4.9-6.6s per invocation in the tool's own ADR-0001) and every
 * later attempt does not, so attempt 1 looks slower than attempts 2 and 3 for a
 * reason that has nothing to do with what changed. Concurrent cold fetches of
 * the same coordinate also race in one shared cache directory.
 *
 * Best-effort: a package that will not resolve is the case's problem to report,
 * not a reason to refuse the sweep.
 */
export function warmCache(packages: string[]): void {
  for (const pkg of packages) {
    try {
      execFileSync("bal", ["library", "overview", pkg], { stdio: "pipe", timeout: 60_000 });
    } catch {
      // See above.
    }
  }
}
