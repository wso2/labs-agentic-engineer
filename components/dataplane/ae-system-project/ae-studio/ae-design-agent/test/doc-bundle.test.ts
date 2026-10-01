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
 * DocFileBundle mirrors bundle mutations onto the live collab doc. The
 * MARK decision is by operation intent, not fragment-emptiness:
 *  - addFile → UNMARKED: a newly-created file is accept-by-default; nothing to
 *    review. (Load-bearing for streaming — a marked write re-renders the
 *    highlighted tail on every line flush → the "requirements flicker" bug.)
 *  - editFile → MARKED: an edit to already-committed content is reviewable.
 */
import { test } from "node:test";
import assert from "node:assert/strict";
import type { RoomPeer } from "../src/collab/room-peer.js";
import { DocFileBundle } from "../src/collab/doc-bundle.js";

class SpyPeer implements RoomPeer {
  sets: { path: string; content: string; mark: boolean }[] = [];
  deletes: string[] = [];
  files(): Record<string, string> {
    return {};
  }
  set(path: string, content: string, mark: boolean): void {
    this.sets.push({ path, content, mark });
  }
  delete(path: string): void {
    this.deletes.push(path);
  }
  leave(): void {}
}

const MD = "specs/requirements/prd.md";

test("addFile mirrors the new file UNMARKED (accept-by-default, no flicker)", () => {
  const peer = new SpyPeer();
  const bundle = new DocFileBundle(peer, {});

  const res = bundle.addFile(MD, "# Todo\n\nManage todo items.\n");

  assert.equal(res.ok, true);
  assert.deepEqual(peer.sets, [
    { path: MD, content: "# Todo\n\nManage todo items.\n", mark: false },
  ]);
});

test("editFile mirrors the change MARKED (reviewable edit to committed content)", () => {
  const peer = new SpyPeer();
  const bundle = new DocFileBundle(peer, { [MD]: "# Todo\n\nManage todo items.\n" });

  const res = bundle.editFile(MD, "Manage todo items.", "Manage and filter todo items.");

  assert.equal(res.ok, true);
  assert.equal(peer.sets.length, 1);
  assert.equal(peer.sets[0]!.path, MD);
  assert.equal(peer.sets[0]!.mark, true, "an edit must be a reviewable (marked) write");
});
