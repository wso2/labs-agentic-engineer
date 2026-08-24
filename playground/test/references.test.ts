/*
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
 * Reference documents on a `/start` turn — the playground's half of what
 * aep-api lists server-side (`listReferenceDocs`, #384). The playground plays
 * the server role offline, so a kickoff here must carry the same facts a
 * production kickoff does, or the start skill's reference behaviour cannot be
 * exercised without the platform.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { REFERENCES_DIR, readReferences } from "../src/state/references.js";
import { startSpec } from "../src/engine/turn-spec.js";

function tempProject(): string {
  return mkdtempSync(join(tmpdir(), "aep-refs-test-"));
}

function writeRef(dir: string, name: string, body = "x"): void {
  mkdirSync(join(dir, REFERENCES_DIR), { recursive: true });
  writeFileSync(join(dir, REFERENCES_DIR, name), body);
}

test("readReferences lists the folder's files as repo-relative paths, sorted", () => {
  const dir = tempProject();
  try {
    writeRef(dir, "rfp.pdf");
    writeRef(dir, "glossary.md");
    assert.deepEqual(readReferences(dir), [
      "specs/requirements/references/glossary.md",
      "specs/requirements/references/rfp.pdf",
    ]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("no references folder is not an error — it is the ordinary case", () => {
  const dir = tempProject();
  try {
    assert.deepEqual(readReferences(dir), []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("startSpec carries the references, and omits the field when there are none", () => {
  const withRefs = startSpec("an expense tracker", ["specs/requirements/references/rfp.pdf"]);
  assert.deepEqual(withRefs, {
    kind: "start",
    idea: "an expense tracker",
    references: ["specs/requirements/references/rfp.pdf"],
  });

  // Absent and empty are the same turn as before this channel existed — the
  // key must not appear at all, matching Go's `omitempty`.
  assert.deepEqual(startSpec("an expense tracker", []), { kind: "start", idea: "an expense tracker" });
  assert.deepEqual(startSpec("an expense tracker"), { kind: "start", idea: "an expense tracker" });
  assert.deepEqual(startSpec(null, []), { kind: "start" });
});
