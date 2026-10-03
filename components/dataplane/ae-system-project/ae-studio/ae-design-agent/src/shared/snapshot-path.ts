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
 * Where a turn's snapshots are (07 §12): the one place the lookup's answer
 * becomes paths. ae-studio-tools writes the snapshots on the shared
 * studio-data volume and answers their shas on the project lookup; this
 * container mounts that volume's `snapshots` dir read-only at
 * `AE_SNAPSHOTS_DIR`:
 *
 *   project: <AE_SNAPSHOTS_DIR>/projects/<project>/<headSha>/
 *   skills:  <AE_SNAPSHOTS_DIR>/skills/<skillsSha>/
 *
 * The pod serves one org, so there is no org segment and no id fence; the
 * shas are the lookup's. Each value is still checked to be exactly one
 * directory name (a DNS-label project, a full hex object name), so no input
 * can reach outside the root, and each dir is stat-checked to exist.
 */

import { statSync } from "node:fs";
import { join } from "node:path";

/** A snapshot path that is malformed or not on disk. */
export class SnapshotPathError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SnapshotPathError";
  }
}

/** A project name: a DNS label, as the platform names projects. */
const PROJECT_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
/** A full commit object name: SHA-1 or SHA-256. */
const SHA_RE = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;

/**
 * The project's snapshot at `headSha`. Throws `SnapshotPathError` when the
 * project or sha is malformed, or the dir does not exist.
 * @knipkeep wired in Task 3.12 (the turn start path reads the snapshot)
 */
export function projectSnapshotDir(root: string, project: string, headSha: string): string {
  if (!PROJECT_RE.test(project)) throw new SnapshotPathError("project is not a project name");
  checkSha(headSha, "headSha");
  return existingDir(join(root, "projects", project, headSha), `no snapshot of ${project} at ${headSha}`);
}

/**
 * The org skills snapshot at `skillsSha`. Throws `SnapshotPathError` when the
 * sha is malformed or the dir does not exist.
 * @knipkeep wired in Task 3.12 (the turn start path reads the skills)
 */
export function skillsSnapshotDir(root: string, skillsSha: string): string {
  checkSha(skillsSha, "skillsSha");
  return existingDir(join(root, "skills", skillsSha), `no skills snapshot at ${skillsSha}`);
}

function checkSha(sha: string, field: string): void {
  if (!SHA_RE.test(sha)) throw new SnapshotPathError(`${field} is not a commit sha`);
}

function existingDir(path: string, missing: string): string {
  let isDir = false;
  try {
    isDir = statSync(path).isDirectory();
  } catch {
    // absent or unreadable: not a snapshot this turn can read
  }
  if (!isDir) throw new SnapshotPathError(missing);
  return path;
}
