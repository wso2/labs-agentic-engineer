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

// The PRD's direct edit (#652): Agree, the one verdict on an `*assumed*` run
// that changes the document without an agent turn.
//
// No model is involved, and that is the point. An assumption is a decision the
// agent already made; agreeing with it is the user's verdict, and the marker
// leaving the document is the signal the agent reads on its next round. So it
// runs instantly, and it stays live while an agent holds the turn — the moment
// a reviewer is most likely to be reading flagged lines.
//
// One transaction, so one undo, and it goes through the collab sync like any
// keystroke.

import type { Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import type { PrdLens } from "../lib/prdLenses";

export type PrdEditLens = Extract<PrdLens, { kind: "edit" }>;

/** Apply the lens's verdict to the view's current document. */
export function applyPrdEdit(view: EditorView, lens: PrdEditLens): void {
  const tr = view.state.tr;
  stripFlag(tr, lens);
  if (tr.docChanged) view.dispatch(tr.scrollIntoView());
}

/**
 * Delete the `*assumed*` run, and the one space that went with it, so
 * "slot *assumed* — nobody" reads "slot — nobody" and a trailing flag leaves no
 * dangling space behind.
 */
function stripFlag(tr: Transaction, lens: PrdEditLens): void {
  const { from, to } = lens.run;
  const contentStart = lens.block.from + 1;
  const contentEnd = lens.block.contentEnd;
  const before = from > contentStart ? tr.doc.textBetween(from - 1, from) : "";
  const after = to < contentEnd ? tr.doc.textBetween(to, to + 1) : "";
  tr.delete(from, to);
  // After the delete, the character that followed the run now sits at `from`.
  if (before === " " && after === " ") tr.delete(from, from + 1);
  else if (before === " " && after === "") tr.delete(from - 1, from);
  else if (before === "" && after === " ") tr.delete(from, from + 1);
}
