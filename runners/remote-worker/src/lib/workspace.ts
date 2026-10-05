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
// Authentication: the coding Job mounts the org's gitpat as GITHUB_TOKEN (gh
// also accepts GH_TOKEN). Clone and every later git op use `gh auth
// git-credential` (the helper `gh auth setup-git` installs), pinned to the real
// `gh` binary. Without a token provisioning fails before any network call.
//
// Layout inside the workspace:
//
//   <workspace>/
//     .git/                     ← cloned repo, default branch checked out
//     .gh-config/               ← gh's config dir (GH_CONFIG_DIR)
//     .aep/
//       bearer                  ← chmod 600 — publisher CC access token snapshot
//       gh                      ← chmod 755 — on PATH, execs the real gh
//
// The agent runs with cwd=<workspace> and PATH prefixed with <workspace>/.aep
// so `gh ...` resolves to the wrapper.

import { randomUUID } from "node:crypto";
import fs from "node:fs";
import { exec } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import { config } from "../config.js";
import {
  envHasGitHubToken,
  ghGitCredentialHelper,
  ghPassthroughScript,
  resolveRealGhPath,
} from "./gh_git_auth.js";
import { cloneCredentialScope, cloneWithHelper } from "./git_clone.js";
import { TASK_LOG_DIR } from "./logger.js";
import { shellQuote } from "./shell.js";

const execAsync = promisify(exec);

export interface WorkspaceLayout {
  workspace: string;
  ghConfigDir: string;
  bearerFile: string;
  aepDir: string;
  ghWrapper: string;
}

export interface ProvisionRequest {
  orgId: string;
  projectId: string;
  taskId: string;
  repoUrl: string;
  bearer: string;
  identity: { name: string; email: string; login?: string };
  correlationId?: string;
}

// writeBearerFile persists the platform access token for skill readers. Temp-file + rename so a concurrent `cat` never sees a truncated
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

const AEP_DIR = ".aep";
const GH_CONFIG_DIR = ".gh-config";

// computeLayout names every path the dispatch flow touches. Pure function
// so tests can verify the path layout without filesystem effects.
export function computeLayout(orgId: string, projectId: string, taskId: string): WorkspaceLayout {
  const workspace = path.join(config.workspaceBasePath, orgId, projectId, taskId);
  const aepDir = path.join(workspace, AEP_DIR);
  return {
    workspace,
    ghConfigDir: path.join(workspace, GH_CONFIG_DIR),
    bearerFile: path.join(aepDir, "bearer"),
    aepDir,
    ghWrapper: path.join(aepDir, "gh"),
  };
}

async function installCommitIdentity(
  workspace: string,
  identity: ProvisionRequest["identity"],
): Promise<void> {
  await execAsync(`git -C ${shellQuote(workspace)} config user.name ${shellQuote(identity.name)}`);
  await execAsync(`git -C ${shellQuote(workspace)} config user.email ${shellQuote(identity.email)}`);
}

// The crash artefacts a toolchain drops when it dies hard: a core dump from any
// process, and the JVM's two post-mortem logs (`bal build` runs one). Not a
// general-purpose ignore list — these are the files that are never wanted, in
// any component, in any language, and that nobody puts there on purpose.
//
// THE SHAPE OF EACH PATTERN IS THE WHOLE PROBLEM, because a name a crash picks
// is a name a person picks too. The list read `core.*` for a while, which is
// unanchored and extension-blind, so it ignored `core.ts`, `core.css` and
// `core.go` — and `src/authz/core.ts` is written into EVERY generated web app by
// the `thunder-authentication` skill, so it fired on every project. A bare
// `core` matches a DIRECTORY of that name as well as a file, and `src/core/` is
// about as common as a directory name gets. That combination cost a live run
// (`ae-demo/simplest-crud-blog`, 2026-09-19) two fully-built components: their
// sources were invisible to `git status` and unstageable by `git add -A`, and
// nothing anywhere said why.
//
// So each pattern now says what it means:
//   `core`         — a dump file at any depth. Unanchored deliberately: a JVM
//                    dumps where it was running, which is any component.
//   `!core/`       — …but a DIRECTORY named core is somebody's source. The
//                    trailing slash is what distinguishes the two, and the
//                    negation only has something to re-include because the line
//                    above it is unanchored. Do not "simplify" it away.
//   `core.[0-9]*`  — the kernel's `core.%p` / `core.%p.%t` shape, which is what
//                    a suffixed dump actually looks like. No source file's
//                    extension starts with a digit.
// Proven the only way that counts, against a real `git init` in
// `workspace.test.ts`: `core.ts`, `src/core/index.ts` and `session.ts` stay
// stageable, `core` and `core.4711` stay unstageable.
const CRASH_ARTEFACT_PATTERNS = ["core", "!core/", "core.[0-9]*", "hs_err_pid*.log", "replay_pid*.log"];

// installCrashArtefactExclude writes those patterns into the clone's
// `.git/info/exclude` — git's per-clone ignore file, which behaves exactly like
// `.gitignore` and is never committed.
//
// WHY `.git/info/exclude` AND NOT `.gitignore`: the repository belongs to the
// customer. `.gitignore` is a file THEY own and their agent edits, so writing
// into it would put a platform concern in their history, in a diff they did not
// ask for, in a file a later commit can reasonably rewrite. `info/exclude` is
// part of the clone, not part of the repository: it lives only in this
// one-shot pod, disappears with it, and cannot be undone by anything the agent
// commits.
//
// WHY THIS EXISTS WHEN `docker-entrypoint.sh` ALREADY SETS `ulimit -c 0`, and
// why NEITHER is redundant. The rlimit is the fix: it stops the dump being
// written at all, and it is enforced regardless of what any agent does. This is
// the belt to that braces — it holds for the artefacts an rlimit does not cover
// (the JVM writes `hs_err_pid*.log` itself, as an ordinary file), and for any
// path into this code that does not come through the image's entrypoint. A live
// run left a 26MB `core` in a clone, one `git add -A` from a customer's pull
// request, and only the lead's habit of staging by path kept it out.
//
// AND WHY `skills/aep/SKILL.md` STILL NAMES THESE PATTERNS TOO — that is not a
// third copy of the same guard, it is the only one a READER meets. This file is
// invisible from inside a session: an agent that wonders why `core` never shows
// up in `git status`, or that is authoring a `.gitignore` for a project people
// will later clone themselves, learns it from the skill. Deleting either half
// costs something the other does not provide.
// Exported for `workspace.test.ts`, which drives it against a real `git init`
// and asserts through `git add -A` that a dump stays unstageable AND that
// `core.ts` and a `src/core/` directory do not — the only assertions that prove
// the guarantee rather than the file's contents.
export async function installCrashArtefactExclude(workspace: string): Promise<void> {
  await appendCloneExclude(workspace, "crash artefacts", CRASH_ARTEFACT_PATTERNS);
}

// installRunLogExclude keeps the run's own log directory (`openTaskLog`: the
// transcript, the prompt appendix, the session-context record) out of anything
// the agent stages. Root-anchored, so a project's own `logs/` or a nested
// `.logs/` is untouched.
export async function installRunLogExclude(workspace: string): Promise<void> {
  await appendCloneExclude(workspace, "the runner's logs", [`/${TASK_LOG_DIR}/`]);
}

// installCredentialExclude keeps the credential directories provisionWorkspace
// drops inside the clone (the publisher bearer, the gh wrapper and gh's config) out of anything the agent stages: one `git add -A`
// would otherwise push the bearer into the customer's repository.
export async function installCredentialExclude(workspace: string): Promise<void> {
  await appendCloneExclude(workspace, "the runner's credentials", [
    `/${AEP_DIR}/`,
    `/${GH_CONFIG_DIR}/`,
  ]);
}

async function appendCloneExclude(workspace: string, what: string, patterns: readonly string[]): Promise<void> {
  // `.git/info/` is not created by every clone (a worktree or a `--separate-git-dir`
  // layout puts the real git dir elsewhere), so resolve it from git rather than
  // assuming `<workspace>/.git/info`.
  const { stdout } = await execAsync(`git -C ${shellQuote(workspace)} rev-parse --git-dir`);
  const gitDir = path.resolve(workspace, stdout.trim());
  const infoDir = path.join(gitDir, "info");
  await fs.promises.mkdir(infoDir, { recursive: true, mode: 0o755 });
  // Appended, never overwritten: a clone may already carry an exclude file, and
  // replacing one would silently drop whatever it said.
  await fs.promises.appendFile(
    path.join(infoDir, "exclude"),
    `\n# AEP: ${what}. Written per clone by the runner, never committed.\n${patterns.join("\n")}\n`,
  );
}

async function installScopedCredentialHelper(workspace: string, scope: string, helper: string): Promise<void> {
  // Empty value first: reset any helper list inherited from system/global.
  // Git takes the FIRST helper that answers.
  await execAsync(`git -C ${shellQuote(workspace)} config credential.helper ""`);
  await execAsync(
    `git -C ${shellQuote(workspace)} config ${shellQuote(`credential.${scope}.helper`)} ${shellQuote(helper)}`,
  );
}

// provisionWorkspace clones the default branch and wires git/gh to the mounted
// GITHUB_TOKEN. It throws, before any network call, when no token is mounted.
// Idempotent: it removes any existing workspace first (§12.1 step 5
// resume-safety: a crash mid-clone leaves DispatchedAt=null, the resume
// sweep re-enters this step, which begins with rm -rf).
//
// Order matters: `git clone <url> <dir>` refuses to write into an existing
// non-empty directory, so the workspace path must not exist when we clone; the
// .aep/ and .gh-config/ directories are dropped inside the cloned tree after.
export async function provisionWorkspace(req: ProvisionRequest): Promise<WorkspaceLayout> {
  if (!envHasGitHubToken()) {
    throw new Error("GITHUB_TOKEN (or GH_TOKEN) is required: the coding Job mounts the org's gitpat");
  }
  const layout = computeLayout(req.orgId, req.projectId, req.taskId);

  // Wipe any prior workspace. Don't pre-create it — git clone will materialise it.
  await fs.promises.rm(layout.workspace, { recursive: true, force: true });
  await fs.promises.mkdir(path.dirname(layout.workspace), { recursive: true, mode: 0o755 });

  const realGhPath = await resolveRealGhPath();
  const ghHelper = ghGitCredentialHelper(realGhPath);

  // No --branch: clone the remote's default branch (HEAD). The agent creates its
  // own feature branch via `git checkout -b ...` once it starts working, per the
  // aep skill workflow. Clone and later push share the gh credential helper.
  await cloneWithHelper({ repoUrl: req.repoUrl, destDir: layout.workspace, helperPath: ghHelper });

  // Materialise the runtime layout inside the cloned tree.
  await fs.promises.mkdir(layout.aepDir, { recursive: true, mode: 0o755 });
  await fs.promises.mkdir(layout.ghConfigDir, { recursive: true, mode: 0o755 });
  if (req.bearer !== "") {
    await writeBearerFile(layout.bearerFile, req.bearer);
  }
  await fs.promises.writeFile(layout.ghWrapper, ghPassthroughScript(realGhPath), { mode: 0o755 });

  await installCommitIdentity(layout.workspace, req.identity);
  await installCrashArtefactExclude(layout.workspace);
  await installRunLogExclude(layout.workspace);
  await installCredentialExclude(layout.workspace);

  const scope = cloneCredentialScope(req.repoUrl);
  if (scope) {
    await installScopedCredentialHelper(layout.workspace, scope, ghHelper);
  }

  return layout;
}
