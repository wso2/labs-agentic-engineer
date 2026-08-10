/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Where the cache lives — decided as a pure function of the environment, so the
 * decision is testable without a filesystem and without setting `process.env`.
 *
 * `~/.cache/bal-library` is the default because it is the conventional location;
 * because `$HOME` in the runner is owned by the run user with nothing mounted
 * over it but the workspace; because it is not world-writable, so the
 * symlink-precreation hazard `/tmp` has does not arise; and because in
 * playground `--host` mode it lands in the developer's real `~/.cache`, which is
 * the only surface where a cache outlives a single run at all.
 *
 * Not beside the bundle, which the playground mounts read-only. Not in the
 * workspace, which is a git clone the platform commits and provisioning scrubs
 * per task.
 */

import { isAbsolute, join } from "node:path";

export interface Environment {
  readonly env: Readonly<Record<string, string | undefined>>;
  readonly homedir: string;
  readonly tmpdir: string;
  readonly uid: number;
}

export type CacheLocation =
  /** No cache at all: either asked for, or nowhere safe to put one. */
  | { readonly kind: "disabled"; readonly reason: string }
  | { readonly kind: "directory"; readonly root: string; readonly mode: number };

/** The environment variable that turns the cache off, spelled once. */
export const CACHE_OFF = "off";

/**
 * The precedence, in order:
 *
 *   1. `BAL_LIBRARY_CACHE=off`         explicit opt-out
 *   2. `BAL_LIBRARY_CACHE_DIR=<dir>`   explicit location
 *   3. `$XDG_CACHE_HOME/bal-library`   when absolute, per the spec
 *   4. `<homedir>/.cache/bal-library`  the default
 *   5. `<tmpdir>/bal-library-<uid>`    when $HOME is unusable, mode 0700
 *   6. disabled
 *
 * The `/tmp` fallback is mode 0700 with the uid in the directory name because
 * `/tmp` is world-writable and shared with the agent's own scratch files; a
 * shared name there is a directory another uid can pre-create.
 */
export function resolveCacheLocation(environment: Environment): CacheLocation {
  const { env, homedir, tmpdir, uid } = environment;

  if (env["BAL_LIBRARY_CACHE"] === CACHE_OFF) {
    return { kind: "disabled", reason: "BAL_LIBRARY_CACHE=off" };
  }

  const explicit = env["BAL_LIBRARY_CACHE_DIR"];
  if (explicit !== undefined && explicit.trim() !== "") {
    return { kind: "directory", root: explicit, mode: 0o700 };
  }

  const xdg = env["XDG_CACHE_HOME"];
  if (xdg !== undefined && xdg.trim() !== "" && isAbsolute(xdg)) {
    return { kind: "directory", root: join(xdg, "bal-library"), mode: 0o700 };
  }

  if (homedir.trim() !== "" && isAbsolute(homedir)) {
    return { kind: "directory", root: join(homedir, ".cache", "bal-library"), mode: 0o700 };
  }

  if (tmpdir.trim() !== "" && isAbsolute(tmpdir)) {
    return { kind: "directory", root: join(tmpdir, `bal-library-${uid}`), mode: 0o700 };
  }

  return { kind: "disabled", reason: "no writable location: neither $HOME nor a temp directory is usable" };
}
