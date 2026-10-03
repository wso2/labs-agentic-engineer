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
 * The pod's snapshot paths (07 §12): the lookup answers the shas, and this is
 * the one place they become paths under `AE_SNAPSHOTS_DIR`. No id fence: the
 * shas are the lookup's, the project name is checked to be one directory name.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { projectSnapshotDir, skillsSnapshotDir, SnapshotPathError } from "../src/shared/snapshot-path.js";

const HEAD = "a".repeat(40);
const SKILLS = "b".repeat(40);

function withRoot(fn: (root: string) => void): void {
  const root = mkdtempSync(join(tmpdir(), "ae-snapshots-"));
  try {
    mkdirSync(join(root, "projects", "greeter", HEAD), { recursive: true });
    mkdirSync(join(root, "skills", SKILLS), { recursive: true });
    fn(root);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

function assertRefused(fn: () => unknown, label: string): void {
  assert.throws(fn, (err: unknown) => err instanceof SnapshotPathError, label);
}

test("project and skills snapshots resolve under AE_SNAPSHOTS_DIR by the lookup's shas", () => {
  withRoot((root) => {
    assert.equal(projectSnapshotDir(root, "greeter", HEAD), join(root, "projects", "greeter", HEAD));
    assert.equal(skillsSnapshotDir(root, SKILLS), join(root, "skills", SKILLS));
  });
});

test("a snapshot that is not on disk is refused (stat-checked)", () => {
  withRoot((root) => {
    assertRefused(() => projectSnapshotDir(root, "greeter", "c".repeat(40)), "unknown head");
    assertRefused(() => projectSnapshotDir(root, "other", HEAD), "unknown project");
    assertRefused(() => skillsSnapshotDir(root, "d".repeat(40)), "unknown skills sha");
    writeFileSync(join(root, "projects", "greeter", "e".repeat(40)), "not a dir");
    assertRefused(() => projectSnapshotDir(root, "greeter", "e".repeat(40)), "a file is not a snapshot");
  });
});

test("a project name that is not one directory name is refused before any stat", () => {
  withRoot((root) => {
    for (const project of ["../x", "..", ".", "a/b", "a\\b", "", ".hidden", "Greeter", "x\0y", "-x", "a".repeat(64)]) {
      assertRefused(() => projectSnapshotDir(root, project, HEAD), `project ${JSON.stringify(project)}`);
    }
  });
});

test("a sha that is not a full hex object name is refused", () => {
  withRoot((root) => {
    for (const sha of ["", "abc1234", "../" + HEAD, HEAD.toUpperCase(), HEAD + "/x", "g".repeat(40)]) {
      assertRefused(() => projectSnapshotDir(root, "greeter", sha), `head ${JSON.stringify(sha)}`);
      assertRefused(() => skillsSnapshotDir(root, sha), `skills ${JSON.stringify(sha)}`);
    }
  });
});

test("a SHA-256 object name resolves too", () => {
  withRoot((root) => {
    const sha256 = "f".repeat(64);
    mkdirSync(join(root, "skills", sha256));
    assert.equal(skillsSnapshotDir(root, sha256), join(root, "skills", sha256));
  });
});
