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

import { exec } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";

const execAsync = promisify(exec);

/**
 * Cloud coding Jobs mount a long-lived GitHub PAT as GITHUB_TOKEN (gh also
 * accepts GH_TOKEN). When present, git authenticates through `gh auth
 * git-credential` — the same helper `gh auth setup-git` installs — instead of
 * the AEP credhelper → credentials/refresh path.
 *
 * Why not call `gh auth setup-git` itself: it writes --global config, and a bare
 * `!gh auth git-credential` resolves `gh` via PATH. The agent's PATH puts
 * `.aep/gh` first (the refresh wrapper), which would reintroduce the broken
 * refresh hop. We install the same helper value locally, pinned to the real
 * binary's absolute path.
 */
export function envHasGitHubToken(env: NodeJS.ProcessEnv = process.env): boolean {
  // Check each independently: `GITHUB_TOKEN=""` must not mask a set GH_TOKEN
  // (`??` would keep the empty string and skip the fallback).
  return (env.GITHUB_TOKEN ?? "") !== "" || (env.GH_TOKEN ?? "") !== "";
}

/** First absolute path from `which gh` stdout, or null. */
export function absoluteGhPathFromWhich(stdout: string): string | null {
  const p = stdout.trim().split(/\r?\n/, 1)[0]?.trim() ?? "";
  if (p === "" || !path.isAbsolute(p)) return null;
  return p;
}

/** Absolute path to the real `gh` binary (never the workspace `.aep/gh` wrapper). */
export async function resolveRealGhPath(): Promise<string> {
  try {
    const which = await execAsync("which gh");
    const p = absoluteGhPathFromWhich(which.stdout);
    if (p !== null) return p;
  } catch {
    // fall through
  }
  // Must be absolute: ghPassthroughScript quotes the value as one executable
  // (`exec "/usr/bin/env gh"`), which cannot run. Same requirement for the
  // durable `!/abs/path/gh auth git-credential` helper pin.
  throw new Error(
    "could not resolve an absolute path to `gh` (required for git credential helper and .aep/gh)",
  );
}

/**
 * Git credential.helper value equivalent to `gh auth setup-git`, using an
 * absolute binary so PATH wrappers cannot intercept.
 */
export function ghGitCredentialHelper(realGhPath: string): string {
  return `!${realGhPath} auth git-credential`;
}

/** Minimal wrapper: exec the real gh. Env GITHUB_TOKEN/GH_TOKEN is enough for auth. */
export function ghPassthroughScript(realGhPath: string): string {
  return `#!/usr/bin/env bash
# Env-token mode: exec the real gh binary. GitHub auth comes from
# GITHUB_TOKEN/GH_TOKEN in the environment — no platform token exchange.
exec ${JSON.stringify(realGhPath)} "$@"
`;
}
