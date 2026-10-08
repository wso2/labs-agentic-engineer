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

import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, TextSelection, type EditorState, type Transaction } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { caretBeforeTag, deleteLine, lineAt, removeAssumedTag } from "./specEdits";
import type { AssumedAction } from "./specLinesPlugin";

// Settling the lines the agent assumed, in the editor: Keep drops the tag,
// Remove deletes the line, I'll edit puts the caret in it and drops the tag
// when the user leaves it changed. A line the user settles is marked "by you"
// for a few seconds. Every change is an editor transaction, so it lands in
// the Y.Doc through the collaboration binding like any keystroke.
//
// The state holds document positions (the line being edited, the lines just
// settled) mapped through every transaction; one whose line is replaced
// under it (a remote rewrite of the file) is dropped.

/** How long a settled line shows "by you". */
const BY_YOU_MS = 4000;

interface Editing {
  /** The start of the line's textblock. */
  pos: number;
  /** Its text when editing began: a changed line is settled on leaving it. */
  original: string;
}

interface AssumedState {
  editing: Editing | null;
  byYou: { id: number; pos: number }[];
}

/** What a transaction tells the plugin; positions are in the document before its steps. */
interface Meta {
  /** Start editing a line. */
  edit?: Editing;
  /** The editor lost focus: whatever line is being edited is left. */
  blurred?: true;
  /** Stop editing (set by the plugin itself once it has handled leaving). */
  leave?: true;
  /** Mark the line starting here "by you". */
  settled?: number;
  /** A "by you" mark's time is up. */
  expire?: number;
}

const assumedKey = new PluginKey<AssumedState>("assumedLines");

let nextId = 0;

function metaOf(tr: Transaction): Meta {
  return (tr.getMeta(assumedKey) as Meta | undefined) ?? {};
}

function mapPos(tr: Transaction, pos: number): number | null {
  if (!tr.docChanged) return pos;
  const result = tr.mapping.mapResult(pos, 1);
  return result.deleted ? null : result.pos;
}

function apply(tr: Transaction, prev: AssumedState): AssumedState {
  const meta = metaOf(tr);
  let editing = meta.leave ? null : (meta.edit ?? prev.editing);
  if (editing) {
    const pos = mapPos(tr, editing.pos);
    editing = pos === null ? null : { ...editing, pos };
  }
  const byYou = [...prev.byYou];
  if (meta.settled !== undefined) byYou.push({ id: ++nextId, pos: meta.settled });
  return {
    editing,
    byYou: byYou.flatMap((b) => {
      if (b.id === meta.expire) return [];
      const pos = mapPos(tr, b.pos);
      return pos === null ? [] : [{ ...b, pos }];
    }),
  };
}

/**
 * After a transaction: when the user has left the line they chose to edit
 * (the caret moved out of it, or the editor lost focus), stop editing it, and
 * drop its tag if they changed it. An unchanged line stays assumed.
 */
function leaveEditedLine(transactions: readonly Transaction[], _old: EditorState, state: EditorState): Transaction | null {
  const editing = assumedKey.getState(state)?.editing;
  if (!editing) return null;
  const blurred = transactions.some((tr) => metaOf(tr).blurred);
  const line = lineAt(state.doc, editing.pos);
  const { from, to } = state.selection;
  if (line && !blurred && from >= line.from && to <= line.to) return null;
  const tr = state.tr;
  const settled = line !== null && line.text !== editing.original && removeAssumedTag(tr, line);
  return tr.setMeta(assumedKey, { leave: true, ...(settled ? { settled: line.from } : {}) } satisfies Meta);
}

/**
 * The transaction that acts on the assumed line at `pos` (any position in it:
 * a button's `posAtDOM`), or null when there is no such line to act on.
 */
export function assumedTransaction(state: EditorState, pos: number, action: AssumedAction): Transaction | null {
  const line = lineAt(state.doc, pos);
  if (!line) return null;
  const tr = state.tr;
  if (action === "keep") {
    return removeAssumedTag(tr, line) ? tr.setMeta(assumedKey, { settled: line.from } satisfies Meta) : null;
  }
  if (action === "remove") {
    deleteLine(tr, line);
    return tr;
  }
  return tr
    .setSelection(TextSelection.create(tr.doc, caretBeforeTag(line)))
    .setMeta(assumedKey, { edit: { pos: line.from, original: line.text } } satisfies Meta)
    .scrollIntoView();
}

/** Act on an assumed line from its button; editing it takes the focus there. */
export function actOnAssumed(view: EditorView, pos: number, action: AssumedAction): void {
  const tr = assumedTransaction(view.state, pos, action);
  if (!tr) return;
  view.dispatch(tr);
  if (action === "edit") view.focus();
}

/** The user left the editor: the line being edited, if any, is left too. */
export function leaveAssumedEdit(view: EditorView): void {
  if (!assumedKey.getState(view.state)?.editing) return;
  view.dispatch(view.state.tr.setMeta(assumedKey, { blurred: true } satisfies Meta));
}

function byYouDecorations(state: EditorState): DecorationSet {
  const byYou = assumedKey.getState(state)?.byYou ?? [];
  const decorations: Decoration[] = [];
  for (const { id, pos } of byYou) {
    const line = lineAt(state.doc, pos);
    if (!line) continue;
    decorations.push(Decoration.node(line.lineFrom, line.lineTo, { class: "aep-line--by-you" }));
    decorations.push(
      Decoration.widget(line.to - 1, () => {
        const el = document.createElement("span");
        el.className = "aep-you";
        el.contentEditable = "false";
        el.textContent = "by you";
        return el;
      }, { key: `by-you:${id}`, side: 2, ignoreSelection: true }),
    );
  }
  return DecorationSet.create(state.doc, decorations);
}

/** The plugin behind the extension, on its own so a bare EditorState can run it. */
export function assumedLinesPlugin(): Plugin<AssumedState> {
  return new Plugin<AssumedState>({
    key: assumedKey,
    state: {
      init: () => ({ editing: null, byYou: [] }),
      apply,
    },
    appendTransaction: leaveEditedLine,
    props: { decorations: byYouDecorations },
    // "by you" is brief: each mark expires on its own timer.
    view: () => {
      const timers = new Map<number, number>();
      return {
        update(view) {
          for (const { id } of assumedKey.getState(view.state)?.byYou ?? []) {
            if (timers.has(id)) continue;
            timers.set(
              id,
              window.setTimeout(() => {
                timers.delete(id);
                if (!view.isDestroyed) view.dispatch(view.state.tr.setMeta(assumedKey, { expire: id } satisfies Meta));
              }, BY_YOU_MS),
            );
          }
        },
        destroy() {
          for (const timer of timers.values()) window.clearTimeout(timer);
        },
      };
    },
  });
}

export const AssumedLines = Extension.create({
  name: "assumedLines",
  addProseMirrorPlugins: () => [assumedLinesPlugin()],
});
