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

// Per-cycle workspace provisioning.
//
// On dispatch the BFF creates an OpenChoreo coding-agent Job Component and
// the dataplane schedules an ephemeral pod whose entrypoint (src/oneshot.ts)
// calls this function. We clone the project's repo on its **default
// branch** into $WORKSPACE_BASE_PATH/<orgId>/<projectId>/<taskId>/ and
// configure `.git/config` + `gh` so the agent can git/gh against GitHub.
// The agent itself creates the feature branch and opens the PR with
// `Closes #<issueNumber>` — see skills/aep/SKILL.md.
//
// Authentication — two modes, chosen by whether GITHUB_TOKEN/GH_TOKEN is set:
//
//   1. Env token (cloud Jobs mount a PAT as GITHUB_TOKEN): clone and every later
//      git op use `gh auth git-credential` (same helper `gh auth setup-git`
//      installs), pinned to the real `gh` binary. No credentials/refresh call.
//   2. Otherwise: AEP credhelper (lib/credhelper.ts) exchanges the publisher CC
//      token for a GitHub token via credentials/refresh — used when no env token
//      is mounted.
//
// Layout inside the workspace:
//
//   <workspace>/
//     .git/                     ← cloned repo, default branch checked out
//     .gh-config/hosts.yml      ← gh's auth config (refresh-wrapper mode only)
//     .aep/
//       bearer                  ← chmod 600 — publisher CC access token snapshot
//       credhelper.sh           ← chmod 700 — only in refresh mode
//       gh                      ← chmod 755 — on PATH (passthrough or refresh wrap)
//
// The agent runs with cwd=<workspace> and PATH prefixed with <workspace>/.aep
// so `gh ...` resolves to the wrapper.

import { randomUUID } from "node:crypto";
import fs from "node:fs";
import { exec } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import { config } from "../config.js";
import { CREDHELPER_FILE, credHelperScript, ghWrapperScript } from "./credhelper.js";
import {
  envHasGitHubToken,
  ghGitCredentialHelper,
  ghPassthroughScript,
  resolveRealGhPath,
} from "./gh_git_auth.js";
import { cloneCredentialScope, cloneWithHelper } from "./git_clone.js";
import { shellQuote } from "./shell.js";

const execAsync = promisify(exec);

export interface WorkspaceLayout {
  workspace: string;
  ghConfigDir: string;
  bearerFile: string;
  aepDir: string;
  helperBin: string;
  ghWrapper: string;
}

export interface ProvisionRequest {
  orgId: string;
  projectId: string;
  taskId: string;
  repoUrl: string;
  bearer: string;
  identity: { name: string; email: string; login?: string };
  gitServiceUrl: string;
  correlationId?: string;
  // WS2.6 — full refresh URL, set by oneshot.ts to the path-scoped
  // `${platformUrl}/internal/v1/executions/{executionId}/credentials/refresh`
  // (publisher CC; taskId carries the execution id, §9.2). Falls back to a path-scoped URL built from
  // gitServiceUrl below when unset. Only used when GITHUB_TOKEN/GH_TOKEN is unset.
  refreshUrl?: string;
}

// writeBearerFile persists the platform access token for credhelper/skill
// readers. Temp-file + rename so a concurrent `cat` never sees a truncated
// file. No-op when the value is unchanged (MCP proxy calls this per request).
export async function writeBearerFile(file: string, token: string, previous?: string): Promise<string> {
  if (previous !== undefined && token === previous) {
    return token;
  }
  const tmp = `${file}.${randomUUID()}.tmp`;
  try {
    await fs.promises.writeFile(tmp, token, { mode: 0o600 });
    await fs.promises.rename(tmp, file);
  } finally {
    await fs.promises.unlink(tmp).catch(() => undefined);
  }
  return token;
}

// computeLayout names every path the dispatch flow touches. Pure function
// so tests can verify the path layout without filesystem effects.
export function computeLayout(orgId: string, projectId: string, taskId: string): WorkspaceLayout {
  const workspace = path.join(config.workspaceBasePath, orgId, projectId, taskId);
  const aepDir = path.join(workspace, ".aep");
  return {
    workspace,
    ghConfigDir: path.join(workspace, ".gh-config"),
    bearerFile: path.join(aepDir, "bearer"),
    aepDir,
    helperBin: path.join(aepDir, CREDHELPER_FILE),
    ghWrapper: path.join(aepDir, "gh"),
  };
}

// resolveRefreshUrl is the one owner of the credentials/refresh endpoint URL.
// It is baked into the helper script at provisioning time, so the clone and
// every later git operation cannot end up pointed at different endpoints.
//
// WS2.6 — req.refreshUrl (set by oneshot.ts from AEP_PLATFORM_URL) is already
// the path-scoped endpoint. The fallback builds the same path-scoped URL from
// gitServiceUrl for the rare case oneshot didn't set it.
function resolveRefreshUrl(req: ProvisionRequest): string {
  if (req.refreshUrl && req.refreshUrl !== "") return req.refreshUrl;
  const url = new URL(req.gitServiceUrl);
  if (!url.pathname.endsWith("/")) url.pathname += "/";
  url.pathname += `internal/v1/executions/${encodeURIComponent(req.taskId)}/credentials/refresh`;
  return url.toString();
}

async function installCommitIdentity(
  workspace: string,
  identity: ProvisionRequest["identity"],
): Promise<void> {
  await execAsync(`git -C ${shellQuote(workspace)} config user.name ${shellQuote(identity.name)}`);
  await execAsync(`git -C ${shellQuote(workspace)} config user.email ${shellQuote(identity.email)}`);
}

async function installScopedCredentialHelper(workspace: string, scope: string, helper: string): Promise<void> {
  // Empty value first: reset any helper list inherited from system/global.
  // Git takes the FIRST helper that answers.
  await execAsync(`git -C ${shellQuote(workspace)} config credential.helper ""`);
  await execAsync(
    `git -C ${shellQuote(workspace)} config ${shellQuote(`credential.${scope}.helper`)} ${shellQuote(helper)}`,
  );
}

// provisionWorkspace clones the feature branch and writes credentials.
// Idempotent: it removes any existing workspace first (§12.1 step 5
// resume-safety: a crash mid-clone leaves DispatchedAt=null, the resume
// sweep re-enters this step, which begins with rm -rf).
//
// Order matters: `git clone <url> <dir>` refuses to write into an existing
// non-empty directory. So we stage auth material in a sibling tmp dir, clone
// into the workspace path (which must not exist yet), and only then drop the
// .aep/ and .gh-config/ directories inside the cloned tree.
export async function provisionWorkspace(req: ProvisionRequest): Promise<WorkspaceLayout> {
  const layout = computeLayout(req.orgId, req.projectId, req.taskId);
  const stageDir = layout.workspace + ".stage";
  const useGhToken = envHasGitHubToken();

  // Wipe both the target and any prior stage. Don't pre-create the workspace
  // dir — git clone will materialise it.
  await fs.promises.rm(layout.workspace, { recursive: true, force: true });
  await fs.promises.rm(stageDir, { recursive: true, force: true });
  await fs.promises.mkdir(path.dirname(layout.workspace), { recursive: true, mode: 0o755 });
  await fs.promises.mkdir(stageDir, { recursive: true, mode: 0o700 });

  const realGhPath = await resolveRealGhPath();
  const ghHelper = ghGitCredentialHelper(realGhPath);

  try {
    // No --branch: clone the remote's default branch (HEAD). The agent
    // creates its own feature branch via `git checkout -b ...` once it
    // starts working, per the aep skill workflow.
    if (useGhToken) {
      // GITHUB_TOKEN/GH_TOKEN in the process env: authenticate via gh's
      // credential helper (setup-git equivalent). Clone and later push share
      // this path — credentials/refresh is never consulted for git.
      await cloneWithHelper({
        repoUrl: req.repoUrl,
        destDir: layout.workspace,
        helperPath: ghHelper,
        bearerFile: "",
      });
    } else {
      // AEP credhelper: stage bearer + helper outside the not-yet-existing
      // workspace; clone through `git -c`; install the same helper durably below.
      const helperBody = credHelperScript({
        taskId: req.taskId,
        workspaceDir: layout.workspace,
        refreshUrl: resolveRefreshUrl(req),
      });
      const stageBearer = path.join(stageDir, "bearer");
      const stageHelper = path.join(stageDir, CREDHELPER_FILE);
      await fs.promises.writeFile(stageBearer, req.bearer, { mode: 0o600 });
      await fs.promises.writeFile(stageHelper, helperBody, { mode: 0o700 });
      await cloneWithHelper({
        repoUrl: req.repoUrl,
        destDir: layout.workspace,
        helperPath: stageHelper,
        bearerFile: stageBearer,
      });
      await fs.promises.mkdir(layout.aepDir, { recursive: true, mode: 0o755 });
      await fs.promises.writeFile(layout.helperBin, helperBody, { mode: 0o700 });
    }

    // Materialise the runtime layout inside the cloned tree.
    await fs.promises.mkdir(layout.aepDir, { recursive: true, mode: 0o755 });
    await fs.promises.mkdir(layout.ghConfigDir, { recursive: true, mode: 0o755 });
    if (req.bearer !== "") {
      await writeBearerFile(layout.bearerFile, req.bearer);
    }

    // gh on PATH: passthrough when env token auth is active; otherwise the
    // refresh wrapper that rewrites hosts.yml from credhelper.
    await fs.promises.writeFile(
      layout.ghWrapper,
      useGhToken ? ghPassthroughScript(realGhPath) : ghWrapperScript(realGhPath),
      { mode: 0o755 },
    );

    await installCommitIdentity(layout.workspace, req.identity);

    const scope = cloneCredentialScope(req.repoUrl);
    if (scope) {
      if (useGhToken) {
        await installScopedCredentialHelper(layout.workspace, scope, ghHelper);
      } else {
        await installScopedCredentialHelper(layout.workspace, scope, layout.helperBin);
      }
    }
  } finally {
    await fs.promises.rm(stageDir, { recursive: true, force: true });
  }

  return layout;
}
