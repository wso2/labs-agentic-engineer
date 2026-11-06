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

import * as Y from "yjs";
import type { Transform } from "@tiptap/pm/transform";
import { readDocFile, setDocFile } from "@aep/collab-doc";
import { docLines } from "../spec/collab/docLines";
import { readFragment } from "../spec/collab/readFragment";
import { editFile, fileFragment, removeTag } from "../spec/collab/specEdits";
import type { LineBlock } from "../spec/model/ids";
import { decisionTag, DECISIONS_PATH } from "./model/decisions";

// The technical decisions in the room. Their lines are read from the file's
// fragment, as the spec's are, so a line is known by the same words that Keep
// finds it by. Keep is a minimal edit of that fragment (editFile), so it lands
// while a design run writes the same file. The file's Checked hash is taken of
// it as it reads once every decision is kept, made by Keep's own edit, so a
// Keep can never change that hash (ADR-0006).

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

/** Keep every assumed decision of a file: each tag comes off as Keep takes it off, the last line first so the earlier positions hold. */
function keepAll(tr: Transform): void {
  for (const line of docLines(tr.doc).reverse()) {
    const tag = decisionTag(line);
    if (tag) removeTag(tr, line, tag);
  }
}

/**
 * The decisions file as it reads once every decision is kept, as the room
 * writes it out: what its Checked hash is taken of. Worked out on a scratch
 * copy of the file, never the room's.
 */
export function keptMarkdown(markdown: string): string {
  const scratch = new Y.Doc();
  setDocFile(scratch, DECISIONS_PATH, markdown);
  editFile(scratch, DECISIONS_PATH, keepAll);
  return readDocFile(scratch, DECISIONS_PATH) ?? markdown;
}
