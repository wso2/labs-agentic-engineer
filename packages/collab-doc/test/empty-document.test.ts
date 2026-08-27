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
 * AN EMPTY DOCUMENT IS ONE EMPTY PARAGRAPH, NOT ZERO BLOCKS.
 *
 * Emptying a markdown document is a supported action — a top-level fragment
 * cannot be deleted from a Y.Doc, so the committer writes an emptied one back
 * to git as an empty FILE. Parsing that file back to zero blocks made the
 * round trip lossy in a way that only showed up on the next reader: a fragment
 * with no children generates no Yjs update, so its share key never replicates,
 * and a joining client cannot tell the room's own seeded file from a file that
 * does not exist. With reads gated on `share.has` (ADR-0020) that left the
 * document permanently read-only — nobody could type the first character back.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import { fragmentToMarkdown, markdownToFragment } from "../src/index.js";

const PATH = "specs/design/security.md";

/** What a second client actually receives, rather than what the writer holds. */
function replicated(build: (doc: Y.Doc) => void): Y.Doc {
  const source = new Y.Doc();
  build(source);
  const joiner = new Y.Doc();
  Y.applyUpdate(joiner, Y.encodeStateAsUpdate(source));
  return joiner;
}

test("an emptied document still reaches a joining client", () => {
  for (const content of ["", "\n", "   "]) {
    const joiner = replicated((doc) =>
      markdownToFragment(content, doc.getXmlFragment(PATH)),
    );
    assert.equal(
      joiner.share.has(PATH),
      true,
      `an empty document (${JSON.stringify(content)}) never replicated its key`,
    );
  }
});

// The half that makes the fix safe: the shape is the EDITOR's empty document,
// so it serializes back to an empty file. Were it anything else, every empty
// file in git would take a spurious write on the first load after this shipped.
test("an empty document still serializes back to an empty file", () => {
  for (const content of ["", "\n", "   "]) {
    const doc = new Y.Doc();
    const fragment = doc.getXmlFragment(PATH);
    markdownToFragment(content, fragment);
    assert.equal(fragment.length, 1, "expected exactly one empty block");
    assert.equal(fragmentToMarkdown(fragment), "");
  }
});

test("a document with content is untouched by the normalization", () => {
  const doc = new Y.Doc();
  const fragment = doc.getXmlFragment(PATH);
  markdownToFragment("# Security\n\nNotes.", fragment);
  assert.equal(fragmentToMarkdown(fragment), "# Security\n\nNotes.");
});
