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

// The wash over the blocks an aimed turn would actually receive (#666).
//
// Why this exists at all: the native selection is painted by the BROWSER, and
// the browser stops painting it the moment focus leaves the editor — which is
// exactly what opening the ask box does. Without a decoration the user loses
// sight of what they are changing at the one moment they are typing the
// instruction for it, which is the worst possible time to lose it.
//
// It also tells a truer story than the character range does. The range is what
// the user dragged; these are the whole blocks the agent will be pointed at
// after the snap. While the editor still has focus the two are visible at once
// — the crisp native range sitting inside the softer block wash — which is what
// makes the snap legible rather than surprising.

import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { docBlocks } from "./docBlocks";
import { selectedBlocks } from "../lib/anchor";

/** The class the editor styles; see SpecMdEditor's `sx`. */
export const AIM_SELECTED_CLASS = "aim-selected";

interface AimRange {
  from: number;
  to: number;
}

export const aimHighlightKey = new PluginKey<DecorationSet>("aimHighlight");

/**
 * Paint (or clear) the wash. Held OUTSIDE the document — a highlight is console
 * state, not something a co-editor should receive over Yjs.
 *
 * The range is mapped forward by ProseMirror on every transaction, so the wash
 * follows a concurrent edit instead of drifting off the text it claims to
 * cover — the same reason the anchor itself is snapshotted at send rather than
 * at selection.
 */
export function setAimHighlight(view: EditorView, range: AimRange | null): void {
  view.dispatch(view.state.tr.setMeta(aimHighlightKey, range));
}

function build(doc: Parameters<typeof docBlocks>[0], range: AimRange | null): DecorationSet {
  if (!range) return DecorationSet.empty;
  const blocks = selectedBlocks(docBlocks(doc), range.from, range.to);
  return DecorationSet.create(
    doc,
    blocks.map((b) => Decoration.node(b.from, b.to, { class: AIM_SELECTED_CLASS })),
  );
}

/** The wash, as a Tiptap extension. Every markdown document carries it. */
export const AimHighlight = Extension.create({
  name: "aimHighlight",

  addProseMirrorPlugins() {
    return [
      new Plugin<DecorationSet>({
        key: aimHighlightKey,
        state: {
          init: () => DecorationSet.empty,
          apply(tr, current) {
            const meta = tr.getMeta(aimHighlightKey) as AimRange | null | undefined;
            if (meta !== undefined) return build(tr.doc, meta);
            // No instruction this transaction: keep what is painted, mapped
            // through the change so it stays on its text.
            return tr.docChanged ? current.map(tr.mapping, tr.doc) : current;
          },
        },
        props: {
          decorations(state) {
            return aimHighlightKey.getState(state);
          },
        },
      }),
    ];
  },
});
