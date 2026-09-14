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

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { installCrashArtefactExclude, writeBearerFile } from "./workspace.js";

test("writeBearerFile: concurrent writers do not share a tmp path", async () => {
  const dir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-bearer-"));
  const file = path.join(dir, "bearer");
  try {
    const tokens = Array.from({ length: 20 }, (_, i) => `tok-${i}-${"x".repeat(32)}`);
    await Promise.all(tokens.map((t) => writeBearerFile(file, t)));
    const got = await fs.promises.readFile(file, "utf8");
    assert.ok(tokens.includes(got), `final token ${got} was not one of the writers`);
    const leftovers = (await fs.promises.readdir(dir)).filter((n) => n.includes(".tmp"));
    assert.deepEqual(leftovers, []);
  } finally {
    await fs.promises.rm(dir, { recursive: true, force: true });
  }
});

// Driven against a REAL git repository, and asserted through `git add -A`
// rather than by reading the file back: what matters is not that four patterns
// were written, it is that the staging command an agent is most likely to reach
// for cannot pick a crash artefact up. A test that asserted the file's contents
// would still pass if the patterns were written somewhere git never reads.
test("installCrashArtefactExclude: a core dump cannot be staged, even by `git add -A`", async () => {
  const dir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-exclude-"));
  const git = (...args: string[]): string =>
    execFileSync("git", ["-C", dir, ...args], { encoding: "utf8" });
  try {
    git("init", "--quiet");
    await installCrashArtefactExclude(dir);

    // What a crash leaves behind, beside a file the run legitimately authored.
    await fs.promises.writeFile(path.join(dir, "core"), "not really a core");
    await fs.promises.writeFile(path.join(dir, "hs_err_pid417.log"), "# A fatal error");
    await fs.promises.mkdir(path.join(dir, "expense-webapp"));
    await fs.promises.writeFile(path.join(dir, "expense-webapp", "core.1234"), "not really a core");
    await fs.promises.writeFile(path.join(dir, "expense-webapp", "App.tsx"), "export default null;");

    // The symptom the lead saw: nothing unfamiliar in a status skim.
    assert.equal(git("status", "--porcelain", "--untracked-files=all").includes("core"), false);

    git("add", "-A");
    const staged = git("diff", "--cached", "--name-only").trim().split("\n").filter(Boolean);
    assert.deepEqual(staged, ["expense-webapp/App.tsx"]);
  } finally {
    await fs.promises.rm(dir, { recursive: true, force: true });
  }
});
