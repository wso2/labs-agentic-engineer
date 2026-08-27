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
 * What has to be true before a sweep is worth running.
 *
 * Every check here exists because its absence produces a NUMBER rather than an
 * error — a sweep that runs happily and attributes its result to a change that
 * was never loaded. That is the expensive failure: three hours of runs whose
 * conclusion is wrong in a way nothing in the report can reveal.
 *
 * This runs ONCE per sweep, before the first session, and refuses rather than
 * warns. A warning at the top of a three-hour run is read after the run.
 */

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, statSync } from "node:fs";
import { basename, join } from "node:path";
import { PATHS, SKILL_NAME } from "./config.js";

export interface Preflight {
  /** Everything that must be fixed before running. */
  blockers: string[];
  /** Facts worth stamping into the report so a result can be placed later. */
  facts: Record<string, string>;
}

/** Takes no arguments: every path it checks is declared in `config.ts`. */
export function preflight(): Preflight {
  const blockers: string[] = [];
  const facts: Record<string, string> = {};

  const bal = which("bal");
  if (!bal) {
    blockers.push("`bal` is not on PATH — host mode runs the developer's own toolchain, so install Ballerina first");
  } else {
    facts.bal = version(bal);
  }

  // THE trap this file exists for. Host mode resolves `bal library` out of
  // ~/.ballerina, so a working-tree jar is invisible until install-local.sh
  // copies it in. A sweep against a stale tool reports the OLD CLI's numbers
  // under the new CLI's name, and nothing downstream can tell.
  const installed = installedToolJar();
  const built = workingTreeToolJars();
  if (!installed) {
    blockers.push(
      "`bal library` is not installed — the ballerina skill's lookups would all fail and the run would " +
        "measure the fallback path. Install it: packages/bal-library-tool/install-local.sh",
    );
  } else if (built.length > 1) {
    // REFUSED rather than skipped. Picking one would be a guess, and skipping
    // the comparison would drop the stale check without saying so — the silent
    // outcome this file is written to avoid.
    blockers.push(
      `more than one jar in ${PATHS.workingTreeToolLibs} (${built.map((jar) => basename(jar)).join(", ")}) — ` +
        "`gradlew :native:jar` does not clean, so a version bump leaves the old one behind and this sweep " +
        "cannot tell which build it would be measured against. Run `./gradlew clean :native:jar` in " +
        "packages/bal-library-tool, then re-run install-local.sh",
    );
  } else if (built.length === 1 && statSync(built[0]!).mtimeMs > statSync(installed).mtimeMs) {
    blockers.push(
      "`bal library` resolves to an installed jar OLDER than your working-tree build — this sweep would " +
        "measure the previous CLI. Re-run packages/bal-library-tool/install-local.sh",
    );
  }
  if (installed) facts.balLibraryJar = `${installed} (${new Date(statSync(installed).mtimeMs).toISOString()})`;

  // The skill is read from the working tree on every run, so its content is a
  // fact about the sweep rather than something to check. Stamped so a report
  // from last week can be placed against the skill it actually ran.
  //
  // The NEWEST file in the whole directory, not SKILL.md's own mtime. Measured
  // the moment this existed: a fix to `references/code-rules.md` moved a case
  // from 2 build cycles to 1, and the report still stamped the previous day —
  // provenance that misses the file you edited is worse than none, because it
  // reads as proof the run predates your change.
  const skillDir = join(PATHS.skillsDir, SKILL_NAME);
  if (!existsSync(join(skillDir, "SKILL.md"))) blockers.push(`the ballerina skill is missing at ${skillDir}`);
  else facts.skillMtime = new Date(newestMtime(skillDir)).toISOString();

  return { blockers, facts };
}

/** The most recent mtime anywhere under a directory. */
function newestMtime(dir: string): number {
  const listing = tryExec("find", [dir, "-type", "f"]);
  if (!listing) return 0;
  return listing
    .split("\n")
    .filter(Boolean)
    .reduce((newest, file) => Math.max(newest, statSync(file).mtimeMs), 0);
}

/**
 * The credential check, kept separate because it is about the ENVIRONMENT the
 * sessions will get rather than about the tools.
 *
 * Host mode here means `claude login` and nothing else. A key in the
 * environment is not an error to fix — it is stripped (see `hostEnv`) — so this
 * only reports what was removed, which is worth knowing when a developer
 * expected a key to be used.
 */
export function credentialNotes(env: NodeJS.ProcessEnv): string[] {
  const notes: string[] = [];
  if (env.ANTHROPIC_API_KEY) notes.push("ANTHROPIC_API_KEY present in the environment — withheld from every session");
  if (env.CLAUDE_CODE_OAUTH_TOKEN) {
    notes.push("CLAUDE_CODE_OAUTH_TOKEN present in the environment — withheld from every session");
  }
  return notes;
}

/**
 * The environment a session gets: the developer's, minus both credentials.
 *
 * There is no opt-in. Not "host by default" — host is the only path, because a
 * harness whose credential can vary between sweeps cannot compare them.
 *
 * Deleting rather than never-adding is the load-bearing part. This package
 * loads no `.env` itself, but `make eval-bal` inherits the shell, and an
 * exported `ANTHROPIC_API_KEY` is ordinary on a developer machine here — the
 * playground reads one from `deployments/.env` for its own runs. Claude Code
 * ranks that key above the keychain, so a stray export would silently bill the
 * platform's key while the report claimed a subscription run. `credentialNotes`
 * says so when it happens rather than letting the removal be invisible.
 */
export function hostEnv(env: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const out = { ...env };
  delete out.ANTHROPIC_API_KEY;
  delete out.CLAUDE_CODE_OAUTH_TOKEN;
  return out;
}

/**
 * Is `bal library` reachable right now — and put it back if it is not.
 *
 * Called before EVERY attempt, not once per sweep, because the thing it guards
 * against happens inside a session. `bal tool pull openapi` — which the
 * `service/` cases legitimately run to generate their service from a contract —
 * rewrites `~/.ballerina/.config/bal-tools.toml` from `bal`'s own view of
 * installed tools, and the locally installed `library` entry carries
 * `repository = "local"`, which that view does not include. So the entry is
 * dropped, and every subsequent lookup in every concurrent attempt answers
 * `unknown command 'library'`.
 *
 * Measured on 2026-08-17: three sessions pulled openapi, `catalog-redis` then
 * failed four lookups, and one session tried to help itself by running
 * `bal tool pull library` — which installed the PUBLISHED tool over the
 * working-tree build, so the rest of the sweep measured a different tool under
 * the name of the one under test. That is worse than a crash: it produces
 * numbers.
 *
 * The repair re-runs the tool's own installer, which is a second or two when the
 * jar is already built. It is safe to do mid-sweep because it RESTORES the tool
 * the sweep was started against rather than changing it.
 *
 * @returns a note when a repair happened, so the report can say so
 */
export function ensureToolRegistered(): string | undefined {
  if (tryExec("bal", ["library", "--help"]) !== undefined) return undefined;
  const installer = join(PATHS.repoRoot, "packages", "bal-library-tool", "install-local.sh");
  if (!existsSync(installer)) {
    return `bal library is not registered and ${installer} is missing — attempts will measure nothing`;
  }
  tryExec(installer, []);
  return tryExec("bal", ["library", "--help"]) === undefined
    ? "bal library is not registered and re-installing it did not help — attempts will measure nothing"
    : "bal library had been dropped from bal-tools.toml (a `bal tool pull` in a session) and was re-installed";
}

function installedToolJar(): string | undefined {
  if (!existsSync(PATHS.installedToolDir)) return undefined;
  // <version>/any/tool/libs/*.jar — one version at a time, installed by the
  // tool's own script, so the first match found is the installed tool.
  const found = tryExec("find", [PATHS.installedToolDir, "-name", "*.jar", "-type", "f"]);
  return found?.split("\n").filter(Boolean)[0];
}

/**
 * Every jar the tool's own build has left behind, found by LOOKING rather than
 * by composing a version into a filename.
 *
 * ALL of them, not the single one: `gradlew :native:jar` does not clean, so a
 * version bump leaves the previous jar beside the new one. Answering `undefined`
 * to that — "no working-tree build" — would turn the ambiguity into a SILENTLY
 * skipped stale check, which is the failure this whole file exists to prevent.
 * The caller refuses on it instead. The playground's twin can return undefined
 * safely because there it only skips a bind-mount; here it would remove a guard.
 *
 * The directory is a parameter so a test can point it at a real one. Exported
 * for that test: `evals/*` is outside the knip gate (`knip.jsonc`), so this does
 * not read as dead code.
 */
export function workingTreeToolJars(libs: string = PATHS.workingTreeToolLibs): string[] {
  if (!existsSync(libs)) return [];
  return readdirSync(libs)
    .filter((entry) => entry.endsWith(".jar"))
    .sort()
    .map((entry) => join(libs, entry));
}

function which(cmd: string): string | undefined {
  return tryExec("which", [cmd])?.split("\n")[0];
}

function version(bal: string): string {
  return tryExec(bal, ["version"])?.split("\n")[0] ?? "unknown";
}

function tryExec(cmd: string, args: string[]): string | undefined {
  try {
    return execFileSync(cmd, args, { encoding: "utf8" }).trim() || undefined;
  } catch {
    return undefined;
  }
}
