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

// Authenticated `git clone` — the single owner of "clone a platform repo with
// the run's credential". The runner's one clone site, the project work tree
// (workspace.ts), goes through it. Skills are no longer a second clone: the
// BFF mirrors them into that same project clone at `.claude/skills/`, and
// skills_resolver.ts only reads pin names off the checked-out tree — see its
// module doc.
//
// The credential is supplied by the same credential helper that authenticates
// every later git operation (lib/credhelper.ts), wired in for this one command
// with `git -c credential.<origin>.helper=…`. No token is passed to git at all:
// not in the URL, not in argv, not in the environment. The helper mints one
// itself and hands it to git over its stdout pipe.
//
// Why not embed the token in the clone URL — the constraint that shapes all of
// this. `https://x-access-token:TOKEN@github.com/o/r` puts the credential in
// four places the runner cannot control:
//
//   - argv, visible in a `ps` listing for the clone's duration;
//   - the `Command failed: git clone '<url>' …` message that child_process
//     puts on every non-zero exit — which the runner logs and the BFF
//     forwards verbatim into the console build log;
//   - the cloned repo's `.git/config`, because git preserves URL userinfo, so
//     the credential would sit at rest in the work tree for the whole run;
//   - any later `git remote -v` the agent happens to run.
//
// An earlier version of this module avoided that with a GIT_ASKPASS shim and the
// token in the clone child's env. That worked, but it meant the clone
// authenticated through a *different* mechanism than the agent's own operations
// — and when the shared script's protocol dispatch was wrong, the clone kept
// working and masked the fact that nothing else could authenticate at all.
// Putting the helper on the clone collapses the two paths into one and makes a
// credential break fail provisioning, loudly, before the agent starts.
//
// `git -c <key>=<value>` (before the subcommand) applies to this command only
// and is NOT written into the cloned repo's config — unlike `git clone -c`,
// which persists. workspace.ts installs the durable helper itself, pointed at
// the helper's final path.

import { exec } from "node:child_process";
import { promisify } from "node:util";
import { shellQuote } from "./shell.js";

const execAsync = promisify(exec);

// Clone output can be large on a big repo; keep the generous buffer the two
// call sites used before they shared this module.
const CLONE_MAX_BUFFER = 16 * 1024 * 1024;

/**
 * How a clone authenticates. Both fields empty means an unauthenticated origin
 * (file:// in tests), which configures no helper at all so a genuinely missing
 * credential surfaces as git's own error rather than an empty password.
 *
 * `helperPath` is either:
 *   - an absolute path to the AEP credhelper script (reads AEP_BEARER_FILE), or
 *   - a git `!` shell helper, e.g. `!/usr/bin/gh auth git-credential` when a
 *     GITHUB_TOKEN/GH_TOKEN is mounted (see gh_git_auth.ts).
 */
export interface CloneAuth {
  /** Credential helper value for `credential.<scope>.helper`. */
  helperPath: string;
  /**
   * Absolute path to the platform bearer the AEP helper exchanges for a token.
   * Empty when using the gh shell helper (token comes from GITHUB_TOKEN/GH_TOKEN).
   */
  bearerFile: string;
}

export interface CloneOptions extends CloneAuth {
  /** Plain https clone URL — never carries credentials. */
  repoUrl: string;
  /** Destination directory; must not exist (git refuses a non-empty dir). */
  destDir: string;
  /** `--depth 1` for the skills repo, where history is not needed. */
  depth1?: boolean;
}

export interface CloneInvocation {
  cmd: string;
  env: NodeJS.ProcessEnv;
}

/**
 * The `credential.<scope>.helper` config scope for a clone URL — its scheme and
 * host, e.g. `https://github.com`. Returns undefined for a non-http(s) URL
 * (file:// origins in tests), which needs no credential at all.
 *
 * Exported because workspace.ts must derive the DURABLE config key the same way
 * it is derived here: a helper the clone honours but `.git/config` scopes to a
 * different host would authenticate provisioning and nothing afterwards.
 * Host-scoping rather than a bare `credential.helper` also means a redirect to
 * another host cannot draw the credential out of the helper.
 */
export function cloneCredentialScope(repoUrl: string): string | undefined {
  try {
    const u = new URL(repoUrl);
    if (u.protocol !== "http:" && u.protocol !== "https:") return undefined;
    return u.origin;
  } catch {
    return undefined;
  }
}

// buildCloneInvocation is the pure seam: it returns the exact command string and
// environment a clone runs with, so a test can assert that no credential appears
// in either, and that the helper is wired in when one is expected.
export function buildCloneInvocation(
  opts: CloneOptions & { baseEnv?: NodeJS.ProcessEnv },
): CloneInvocation {
  const depth = opts.depth1 ? " --depth 1" : "";
  const scope = cloneCredentialScope(opts.repoUrl);
  const authed = opts.helperPath !== "" && scope !== undefined;

  // An EMPTY credential.helper resets the inherited helper list, then the scoped
  // one is the only entry. Order matters and this is not belt-and-braces: git
  // consults helpers in config order and takes the FIRST that answers, so a
  // helper in system config (Homebrew's git ships `credential.helper=osxkeychain`
  // that way, and a base image could too) would authenticate the clone as
  // something else entirely — and would also be handed our token by git's
  // post-success `store`. Reset first so ours is the only credential source.
  const helperFlag = authed
    ? `-c credential.helper= -c ${shellQuote(`credential.${scope}.helper=${opts.helperPath}`)} `
    : "";
  const cmd = `git ${helperFlag}clone${depth} ${shellQuote(opts.repoUrl)} ${shellQuote(opts.destDir)}`;

  const env: NodeJS.ProcessEnv = {
    ...(opts.baseEnv ?? process.env),
    GIT_TERMINAL_PROMPT: "0",
  };
  // AEP credhelper reads the bearer from this path. It is a PATH, not a secret,
  // and it is set on a per-child env object rather than process.env: runner.ts
  // spreads process.env into the agent's child env, and provisioning's staged
  // bearer is gone by the time the agent starts. The gh shell helper does not
  // use it — GITHUB_TOKEN/GH_TOKEN is already in the inherited env.
  if (authed && opts.bearerFile !== "" && !opts.helperPath.startsWith("!")) {
    env.AEP_BEARER_FILE = opts.bearerFile;
  }
  return { cmd, env };
}

// cloneWithHelper runs the clone. Rejects with git's own error on failure — safe
// to log, since neither the command nor the message can contain a credential.
export async function cloneWithHelper(opts: CloneOptions): Promise<void> {
  const { cmd, env } = buildCloneInvocation(opts);
  await execAsync(cmd, { env, maxBuffer: CLONE_MAX_BUFFER });
}
