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
 * The code run's pre-tag step: aep-api's derivation, spawned before the coding
 * agent (ADR-0003). The Go side has its own tests, including the production
 * oracle; these pin the playground's half — what it passes, how it reads the
 * answer, and where in `code` it runs — with the process injected.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { deriveDesign, type DeriveProcessResult } from "../src/engine/design-derive.js";
import { codeCommand } from "../src/commands.js";
import { renderTaskContextFile } from "../src/ports/issue-store.js";
import { listUndoSnapshots } from "../src/state/undo.js";
import { REPO_ROOT } from "../src/paths.js";

const answer = (result: Partial<DeriveProcessResult>) => async (): Promise<DeriveProcessResult> => ({
  code: 0,
  stdout: "",
  stderr: "",
  ...result,
});

test("deriveDesign passes the design dir, the project slug and the repo catalog", async () => {
  let seen: string[] = [];
  const outcome = await deriveDesign("/work/Track Each Hire", async (args) => {
    seen = args;
    return { code: 0, stdout: "derived: components/a/design.json\nderived: components/b/design.json\n", stderr: "" };
  });
  assert.deepEqual(seen, [
    "--design-dir",
    "/work/Track Each Hire/specs/design",
    "--project",
    "track-each-hire",
    "--resource-types",
    join(REPO_ROOT, "deployments", "single-cluster", "resource-types"),
  ]);
  assert.deepEqual(outcome, { ok: true, changed: ["components/a/design.json", "components/b/design.json"] });
});

test("deriveDesign: nothing to derive is success with no changes", async () => {
  assert.deepEqual(await deriveDesign("/p", answer({ stdout: "derived: no change\n" })), { ok: true, changed: [] });
});

test("deriveDesign: a refusal carries the derivation's own message", async () => {
  const stderr =
    'design-derive: resourceType is not installed on this cluster: receipts ("object-storage"); available: postgres-cnpg, thunder-app\n';
  const outcome = await deriveDesign("/p", answer({ code: 1, stderr }));
  assert.deepEqual(outcome, {
    ok: false,
    detail:
      'design refused: resourceType is not installed on this cluster: receipts ("object-storage"); available: postgres-cnpg, thunder-app',
  });
});

test("deriveDesign: no go on PATH is a clear refusal", async () => {
  const spawnError = Object.assign(new Error("spawn go ENOENT"), { code: "ENOENT" });
  const outcome = await deriveDesign("/p", answer({ code: null, spawnError }));
  assert.equal(outcome.ok, false);
  assert.match(outcome.ok ? "" : outcome.detail, /`go` is not on PATH/);
});

test("deriveDesign: any other failure refuses too, with the whole stderr", async () => {
  const outcome = await deriveDesign("/p", answer({ code: 2, stderr: "design-derive: read design: boom\n" }));
  assert.deepEqual(outcome, { ok: false, detail: "design derivation failed (exit 2): design-derive: read design: boom" });
});

function tempProject(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-derive-"));
  mkdirSync(join(dir, "issues"), { recursive: true });
  writeFileSync(
    join(dir, "issues", "1.md"),
    renderTaskContextFile({ issueNumber: 1, component: "orders-api", title: "Orders", dependsOn: [], body: "scope" }),
  );
  return dir;
}

test("code derives after the undo snapshot, and a refusal stops the run before the agent", async () => {
  const dir = tempProject();
  try {
    let snapshotsWhenDerived = -1;
    const outcome = await codeCommand(dir, {
      silent: true,
      yes: true,
      deriveDesign: async (projectDir) => {
        assert.equal(projectDir, dir);
        snapshotsWhenDerived = listUndoSnapshots(dir).length;
        return { ok: false, detail: "design refused: receipts" };
      },
    });
    assert.equal(snapshotsWhenDerived, 1, "the snapshot is taken before the design is derived");
    assert.deepEqual(outcome, { ok: false, detail: "design refused: receipts" });
    assert.equal(existsSync(join(dir, ".aep-playground", "runs")), false, "the coding agent never started");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
