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

// Undo keeps the reader's place (#666).
//
// y-prosemirror applies EVERY Yjs-origin change — an undo included — with
// `tr.scrollIntoView()` whenever the local cursor was in view (sync-plugin
// :639), and an undo lands as a whole-document replace whose restored
// selection can clamp to the end. So pressing Undo threw the reader to the
// bottom of the document they were looking at.
//
// ProseMirror's designed hook for this is `handleScrollToSelection`: it is
// consulted before the default scroll, and returning true suppresses it. The
// guard flags an undo/redo transaction as it is applied and swallows the one
// scroll it requests; every other scroll — typing, the aim box, an agent's
// write — proceeds untouched.

import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import type { EditorState, Transaction } from "@tiptap/pm/state";

/**
 * The y-sync plugin's key, read off the STATE's own plugin list. Imported, the
 * key can be a different y-prosemirror instance's (Vite pre-bundles the
 * Collaboration extension's copy) and never match — the same trap the
 * follow-the-edit listener documents in SpecMdEditor.
 */
function ySyncKeyOf(state: EditorState): string | undefined {
  return state.plugins
    .map((plugin) => (plugin as unknown as { key?: unknown }).key)
    .find((key): key is string => typeof key === "string" && key.startsWith("y-sync$"));
}

function isUndoRedo(tr: Transaction, state: EditorState): boolean {
  const key = ySyncKeyOf(state);
  const meta = key
    ? (tr.getMeta(key) as { isUndoRedoOperation?: boolean } | undefined)
    : undefined;
  return meta?.isUndoRedoOperation === true;
}

export const UndoScrollGuard = Extension.create({
  name: "undoScrollGuard",

  addProseMirrorPlugins() {
    // One pending suppression at most. Armed when an undo/redo is applied,
    // spent by the scroll that transaction requests — and disarmed by the next
    // transaction of any other kind, so a scroll-less undo cannot leak the
    // suppression onto a later, legitimate scroll.
    let suppress = false;
    return [
      new Plugin({
        filterTransaction(tr, state) {
          suppress = isUndoRedo(tr, state);
          return true;
        },
        props: {
          handleScrollToSelection() {
            if (!suppress) return false;
            suppress = false;
            return true;
          },
        },
      }),
    ];
  },
});
