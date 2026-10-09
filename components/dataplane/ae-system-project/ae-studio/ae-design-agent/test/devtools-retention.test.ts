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
import { pruneDevtoolsDb, pruneDevtoolsFile } from "../src/shared/devtools-retention.js";

const NOW = new Date("2026-08-07T12:00:00.000Z");
const cutoff7d = new Date(NOW.getTime() - 7 * 24 * 60 * 60 * 1000);

const run = (id: string, started_at: string): { id: string; started_at: string } => ({
  id,
  started_at,
});

test("runs older than the cutoff go, with the steps that belong to them", () => {
  const db = {
    runs: [run("old", "2026-07-20T00:00:00.000Z"), run("fresh", "2026-08-06T00:00:00.000Z")],
    steps: [{ run_id: "old" }, { run_id: "old" }, { run_id: "fresh" }],
  };

  const { db: pruned, removedRuns, removedSteps } = pruneDevtoolsDb(db, cutoff7d);

  assert.deepEqual(
    pruned.runs.map((r) => r.id),
    ["fresh"],
  );
  assert.equal(removedRuns, 1);
  // Steps carry the raw payloads — leaving them behind would keep the bytes and
  // the file would never actually shrink.
  assert.equal(removedSteps, 2);
  assert.deepEqual(pruned.steps, [{ run_id: "fresh" }]);
});

test("a step whose run is gone is dropped even if the run was never listed", () => {
  const db = { runs: [run("fresh", "2026-08-06T00:00:00.000Z")], steps: [{ run_id: "ghost" }] };
  const { db: pruned, removedSteps } = pruneDevtoolsDb(db, cutoff7d);
  assert.equal(removedSteps, 1, "an unreachable step is dead weight");
  assert.deepEqual(pruned.steps, []);
});

test("a run that cannot be dated is KEPT — retention never guesses", () => {
  const db = {
    runs: [{ id: "undated" }, { id: "bad", started_at: "not-a-date" }],
    steps: [{ run_id: "undated" }, { run_id: "bad" }],
  };
  const { db: pruned, removedRuns, removedSteps } = pruneDevtoolsDb(db, cutoff7d);
  assert.equal(removedRuns, 0);
  assert.equal(removedSteps, 0);
  assert.equal(pruned.runs.length, 2);
});

test("the file is rewritten only when something was removed", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "devtools-retention-"));
  const dbDir = path.join(dir, ".devtools");
  fs.mkdirSync(dbDir);
  const file = path.join(dbDir, "generations.json");

  fs.writeFileSync(
    file,
    JSON.stringify({
      runs: [run("old", "2026-07-01T00:00:00.000Z"), run("fresh", "2026-08-06T00:00:00.000Z")],
      steps: [{ run_id: "old" }, { run_id: "fresh" }],
    }),
  );

  const summary = pruneDevtoolsFile(7, NOW, dir);
  assert.ok(summary && summary.includes("removed 1 run(s)"), `unexpected summary: ${summary}`);
  const after = JSON.parse(fs.readFileSync(file, "utf-8")) as { runs: { id: string }[] };
  assert.deepEqual(
    after.runs.map((r) => r.id),
    ["fresh"],
  );

  // Nothing left to prune: no rewrite, and the caller is told so.
  const before = fs.statSync(file).mtimeMs;
  assert.equal(pruneDevtoolsFile(7, NOW, dir), null);
  assert.equal(fs.statSync(file).mtimeMs, before, "a second prune must not rewrite the file");

  fs.rmSync(dir, { recursive: true, force: true });
});

test("a missing or corrupt capture is not a reason to fail a boot", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "devtools-retention-"));
  assert.equal(pruneDevtoolsFile(7, NOW, dir), null, "absent file");

  fs.mkdirSync(path.join(dir, ".devtools"));
  fs.writeFileSync(path.join(dir, ".devtools", "generations.json"), "{ not json");
  assert.equal(pruneDevtoolsFile(7, NOW, dir), null, "corrupt file");

  fs.rmSync(dir, { recursive: true, force: true });
});
