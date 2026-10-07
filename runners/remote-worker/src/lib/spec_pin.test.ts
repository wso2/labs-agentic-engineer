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

// B2: a coding run builds the VERSION it was dispatched for. The clone is
// main's tip (each task builds on merged code), but specs/ is the version's,
// and nothing the agent does with git brings main's specs/ back or commits
// the swap.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { pinSpecsToVersion } from "./workspace.js";

process.env.GIT_CONFIG_GLOBAL = "/dev/null";
process.env.GIT_CONFIG_NOSYSTEM = "1";

function git(dir: string, ...args: string[]): string {
  return execFileSync("git", ["-C", dir, "-c", "user.name=t", "-c", "user.email=t@e", ...args], { encoding: "utf8" });
}

function write(dir: string, rel: string, content: string): void {
  fs.mkdirSync(path.dirname(path.join(dir, rel)), { recursive: true });
  fs.writeFileSync(path.join(dir, rel), content);
}

function setup(): { origin: string; ws: string } {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "spec-pin-"));
  const origin = path.join(root, "origin");
  fs.mkdirSync(origin);
  git(origin, "init", "-q", "-b", "main");
  write(origin, "specs/design/design.cell", "v1 design\n");
  write(origin, "src/app.ts", "export {};\n");
  git(origin, "add", "-A");
  git(origin, "commit", "-q", "-m", "v1");
  git(origin, "tag", "-a", "v1", "-m", "spec version v1");
  // main moves on after the version: an edit, and a file v1 never had.
  write(origin, "specs/design/design.cell", "edited after v1\n");
  write(origin, "specs/requirements/features/F9-later.md", "# Later\n");
  git(origin, "add", "-A");
  git(origin, "commit", "-q", "-m", "after v1");
  const ws = path.join(root, "ws");
  execFileSync("git", ["clone", "-q", `file://${origin}`, ws]);
  return { origin, ws };
}

test("specs/ reads as the version, and the clone stays clean", async () => {
  const { ws } = setup();
  await pinSpecsToVersion(ws, "v1");
  assert.equal(fs.readFileSync(path.join(ws, "specs/design/design.cell"), "utf8"), "v1 design\n");
  assert.equal(fs.existsSync(path.join(ws, "specs/requirements/features/F9-later.md")), false);
  assert.equal(git(ws, "status", "--porcelain"), "");
});

test("the agent's commits and a rebase onto a newer main leave the version's specs/ alone", async () => {
  const { origin, ws } = setup();
  await pinSpecsToVersion(ws, "v1");
  git(ws, "checkout", "-q", "-b", "feat/task");
  write(ws, "src/app.ts", "export const x = 1;\n");
  git(ws, "add", "-A");
  git(ws, "commit", "-q", "-m", "work");
  assert.deepEqual(git(ws, "show", "--name-only", "--format=", "HEAD").trim().split("\n"), ["src/app.ts"]);

  write(origin, "specs/design/design.cell", "edited again on main\n");
  git(origin, "add", "-A");
  git(origin, "commit", "-q", "-m", "another spec edit");
  git(ws, "fetch", "-q", "origin");
  git(ws, "rebase", "-q", "origin/main");
  assert.equal(fs.readFileSync(path.join(ws, "specs/design/design.cell"), "utf8"), "v1 design\n");
  assert.equal(git(ws, "status", "--porcelain"), "");
});

test("a tag name that is not a ref is refused", async () => {
  const { ws } = setup();
  await assert.rejects(pinSpecsToVersion(ws, "v1..x"));
  await assert.rejects(pinSpecsToVersion(ws, "-v1"));
});
