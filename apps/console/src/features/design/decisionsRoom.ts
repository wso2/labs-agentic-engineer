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

import type * as Y from "yjs";
import { docLines } from "../spec/collab/docLines";
import { readFragment } from "../spec/collab/readFragment";
import { editFile, fileFragment, removeTag } from "../spec/collab/specEdits";
import type { LineBlock } from "../spec/model/ids";
import { decisionTag } from "./model/decisions";
import { DECISIONS_PATH } from "./model/designFiles";

// The technical decisions in the room. Their lines are read from the file's
// fragment, as the spec's are, so a line is known by the same words that Keep
// finds it by. Keep is a minimal edit of that fragment (editFile), so it lands
// while a design run writes the same file.

/** The decisions file's lines; empty while Design has not written it. */
export function readDecisionLines(doc: Y.Doc): LineBlock[] {
  const fragment = fileFragment(doc, DECISIONS_PATH);
  return fragment ? docLines(readFragment(fragment).doc) : [];
}

/**
 * Keep an assumed decision: its `*assumed*` tag comes off and its words stay.
 * The line is found by its words; false when no assumed decision has them any
 * more (a run rewrote it, or a teammate kept it), and then nothing is written.
 */
export function keepDecision(doc: Y.Doc, text: string): boolean {
  return editFile(doc, DECISIONS_PATH, (tr) => {
    const line = docLines(tr.doc).find((l) => l.text === text && decisionTag(l) !== null);
    const tag = line ? decisionTag(line) : null;
    if (line && tag) removeTag(tr, line, tag);
  });
}
