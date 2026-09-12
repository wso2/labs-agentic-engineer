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
 * The engine loop's disk reconcile (`engine/turn.ts`): what the turn filter
 * hides is not the turn's to touch.
 *
 * The loop folds each streamed tool-call over the SERVER'S filtered view of the
 * snapshot and diff-writes it to disk. Seeding the "current disk state" side of
 * that diff from the RAW project read instead made every filtered-out path look
 * like a file the agent had just deleted: a refused op on such a path left the
 * bundle without it, the diff read `content → undefined`, and `reconcileFile`
 * rmSync'd a file the turn never wrote. A live chat turn lost
 * `specs/design/security.json` exactly this way — its only write was REFUSED
 * and the summary still printed `− specs/design/security.json`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { mockModel } from "@aep/agents/shared/mock-model";
import { openSession } from "../src/engine/session.js";
import { runSpecTurn } from "../src/engine/turn.js";
import { chatSpec } from "../src/engine/turn-spec.js";

/**
 * A path the turn filter hides: arbitrary `*.yaml` under specs/ is not one of
 * the two admitted OpenAPI shapes, so it is in the project folder and NOT in
 * the snapshot the agent sees.
 */
const HIDDEN_PATH = "specs/design/workload.yaml";
const HIDDEN_BODY = "kind: Workload\nname: api\n";

/**
 * A path the filter ADMITS and no write gate claims, so the agent can both edit
 * and remove it (a gated artifact like domain-model.md would refuse the edit for
 * its own reasons and prove nothing about the reconcile).
 */
const VISIBLE_PATH = "specs/requirements/notes.md";
const VISIBLE_BODY = "# Notes\n\n- login\n";

function seedProject(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-fold-"));
  mkdirSync(join(dir, "specs/requirements"), { recursive: true });
  writeFileSync(join(dir, "specs/requirements/prd.md"), "# Requirements\n\n- login\n");
  mkdirSync(join(dir, "specs/design"), { recursive: true });
  writeFileSync(join(dir, HIDDEN_PATH), HIDDEN_BODY);
  writeFileSync(join(dir, VISIBLE_PATH), VISIBLE_BODY);
  return dir;
}

/** An empty skill library — this asserts the reconcile, not skill loading. */
function emptySkills(): string {
  return mkdtempSync(join(tmpdir(), "aep-play-fold-skills-"));
}

test("a file the turn filter hides survives a turn that never touched it", async () => {
  const projectDir = seedProject();
  const skillsDir = emptySkills();
  // The agent cannot see workload.yaml, so this edit is REFUSED (NO_SUCH_FILE)
  // and the bundle stays byte-for-byte unchanged — the exact shape of the live
  // failure.
  const model = mockModel([
    {
      kind: "toolCall",
      toolCallId: "h1",
      toolName: "editFile",
      input: { path: HIDDEN_PATH, oldString: "kind: Workload", newString: "kind: Nope" },
    },
    { kind: "text", text: "I could not find that file." },
  ]);
  const session = await openSession(projectDir, { model, skillsDir });
  try {
    const result = await runSpecTurn(session, chatSpec("change the workload"));

    assert.ok(existsSync(join(projectDir, HIDDEN_PATH)), "a filtered-out file must not be deleted by a turn");
    assert.equal(readFileSync(join(projectDir, HIDDEN_PATH), "utf8"), HIDDEN_BODY, "its bytes are untouched");
    assert.deepEqual(result.changes, [], "a refused op on a hidden path is not a file change");
  } finally {
    await session.close();
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});

// The guard on the other side: narrowing the reconcile to the filtered view
// must not make the loop stop writing. A file the agent CAN see still gets its
// edit and its deletion folded to disk.
test("a visible file still takes its edit and its removal", async () => {
  const projectDir = seedProject();
  const skillsDir = emptySkills();
  const model = mockModel([
    {
      kind: "toolCall",
      toolCallId: "v1",
      toolName: "editFile",
      input: { path: VISIBLE_PATH, oldString: "- login", newString: "- login\n- logout" },
    },
    { kind: "toolCall", toolCallId: "v2", toolName: "removeFile", input: { path: VISIBLE_PATH } },
    { kind: "text", text: "Done." },
  ]);
  const session = await openSession(projectDir, { model, skillsDir });
  try {
    const result = await runSpecTurn(session, chatSpec("rewrite the notes"));

    assert.deepEqual(
      result.changes.map((c) => `${c.kind} ${c.path}`),
      [`edit ${VISIBLE_PATH}`, `remove ${VISIBLE_PATH}`],
    );
    assert.equal(existsSync(join(projectDir, VISIBLE_PATH)), false);
    // …and the hidden file beside it is still untouched.
    assert.equal(readFileSync(join(projectDir, HIDDEN_PATH), "utf8"), HIDDEN_BODY);
  } finally {
    await session.close();
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});
