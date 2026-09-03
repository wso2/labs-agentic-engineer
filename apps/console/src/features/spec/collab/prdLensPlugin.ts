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

// The PRD's code lenses, as a ProseMirror decoration plugin (#579).
//
// The lens has to anchor to the LIVE collaborative document, not to a markdown
// snapshot: the user is typing into this fragment and the agent is streaming
// into it at the same time, so a line number from a committed file points at
// the wrong line within a keystroke. Decorations are derived from the doc on
// every change instead — a PRD is a few hundred nodes, so the rebuild is
// cheaper than mapping a stale parse forward and being subtly wrong.
//
// Where each lens goes is `lib/prdLenses.ts`; this file is the walk from doc to
// blocks and the DOM it renders.

import { Extension } from "@tiptap/core";
import type { Node as PmNode } from "@tiptap/pm/model";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { prdAffordances, type CommandPrompt, type PrdLens } from "../lib/prdLenses";
import { docBlocks } from "./docBlocks";
import { applyPrdEdit, type PrdEditLens } from "./prdEdits";

/**
 * What the console hands the editor to make the PRD's lenses live. Absent
 * entirely for every other file.
 */
export interface PrdLensBinding {
  /** Send the lens's command as the user's next message. */
  run: (command: string) => void;
  /**
   * Why a lens fired right now would be refused — an agent already holds the
   * turn — or `""` when the lenses are live. Doubles as the disabled tooltip.
   */
  busyReason: string;
}

/** How the plugin reaches the binding. Read at click time, never captured. */
export interface PrdLensOptions {
  run: (command: string) => void;
  isBusy: () => boolean;
  busyReason: () => string;
  /** Open the aim box on a block (#652's Discuss) — the editor's own surface. */
  discuss: (from: number, to: number) => void;
  /** Open the aim box to collect a prompted command's subject (#666). */
  compose: (command: string, prompt: CommandPrompt, at: number) => void;
}

/**
 * The lens as it stands NOW, for the block the user clicked.
 *
 * A widget whose key matches is REUSED by ProseMirror, DOM and click handler
 * both — so the lens a handler closed over may carry positions from before an
 * agent streamed a paragraph in above it. Commands do not care (they carry a
 * string), but an edit acts on positions, and acting on stale ones would edit
 * the wrong line. The block's text is the identity that survives; positions
 * are re-read from the live document at click time.
 */
function sameSignature(a: PrdLens, b: PrdLens): boolean {
  return (
    a.kind === b.kind &&
    (a.kind !== "edit" || a.edit === (b as PrdEditLens).edit) &&
    (a.kind === "command" ? false : a.block.text === (b as Extract<PrdLens, { kind: "edit" | "discuss" }>).block.text)
  );
}

/** Which occurrence of its signature this lens is, in document order. Two
 *  bullets with identical text are told apart by nothing else. */
function ordinalOf(lenses: PrdLens[], lens: PrdLens): number {
  let ordinal = 0;
  for (const other of lenses) {
    if (other === lens) return ordinal;
    if (sameSignature(other, lens)) ordinal += 1;
  }
  return ordinal;
}

function liveLens<L extends Extract<PrdLens, { kind: "edit" | "discuss" }>>(
  view: EditorView,
  captured: L,
  capturedOrdinal: number,
): L | undefined {
  const { lenses } = prdAffordances(docBlocks(view.state.doc));
  // The Nth duplicate stays the Nth duplicate: insertions and edits elsewhere
  // reorder nothing among blocks with identical text, so the ordinal is the
  // identity that survives where the text alone is ambiguous.
  const matches = lenses.filter((l): l is L => sameSignature(l, captured));
  return matches[capturedOrdinal] ?? matches.at(-1);
}

export const prdLensKey = new PluginKey<DecorationSet>("prdLenses");

function lensButton(
  view: EditorView,
  lens: PrdLens,
  ordinal: number,
  busyReason: string,
  opts: PrdLensOptions,
): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `prd-lens prd-lens--${lens.placement} prd-lens--${lens.kind}`;
  el.textContent = lens.label;
  el.contentEditable = "false";
  // A direct edit needs no agent, so it never goes inert: a reviewer reading
  // flagged lines while the agent works is exactly who these are for.
  const inert = lens.kind !== "edit" && busyReason !== "";
  el.disabled = inert;
  el.title = inert ? busyReason : lens.title;
  // The button lives inside a contenteditable, so the browser would otherwise
  // move the caret into it before the click ever lands.
  el.addEventListener("mousedown", (e) => e.preventDefault());
  el.addEventListener("click", (e) => {
    e.preventDefault();
    e.stopPropagation();
    switch (lens.kind) {
      case "command":
        if (opts.isBusy()) return;
        // A lens with a subject to collect opens the box; the rest fire as
        // they are — their subject is the line they sit on, or nothing.
        if (lens.prompt) opts.compose(lens.command, lens.prompt, lens.at);
        else opts.run(lens.command);
        return;
      case "edit": {
        const live = liveLens(view, lens, ordinal);
        if (live) applyPrdEdit(view, live);
        return;
      }
      case "discuss": {
        if (opts.isBusy()) return;
        const live = liveLens(view, lens, ordinal);
        if (live) opts.discuss(live.block.from, live.block.to);
        return;
      }
    }
  });
  return el;
}

/** The identity a lens's DOM survives a rebuild under — never a position. The
 *  ordinal keeps two identical bullets from sharing one key (and one DOM). */
function lensKey(lens: PrdLens, ordinal: number, busyReason: string): string {
  const what =
    lens.kind === "command"
      ? lens.command
      : `${lens.label}:${lens.block.text.replace(/\s+/g, " ").trim()}`;
  return `${lens.kind}:${what}#${ordinal}@${lens.placement}@${busyReason}`;
}

function build(doc: PmNode, opts: PrdLensOptions): DecorationSet {
  const { lenses, flags } = prdAffordances(docBlocks(doc));
  const busyReason = opts.isBusy() ? opts.busyReason() : "";
  const decorations: Decoration[] = [];
  for (const flag of flags) {
    decorations.push(
      // Node decorations for the whole-entry flags, inline for the `*assumed*`
      // run — the flag covers exactly the thing that is unsettled.
      flag.kind === "assumed"
        ? Decoration.inline(flag.from, flag.to, { class: "prd-flag prd-flag--assumed" })
        : Decoration.node(flag.from, flag.to, { class: `prd-flag prd-flag--${flag.kind}` }),
    );
  }
  for (const lens of lenses) {
    const ordinal = ordinalOf(lenses, lens);
    decorations.push(
      Decoration.widget(lens.at, (view) => lensButton(view, lens, ordinal, busyReason, opts), {
        side: 1,
        // `side: 1` keeps the widget after the text it follows; the key makes
        // an unchanged lens survive a rebuild without its DOM being replaced,
        // so hovering one does not flicker while the agent streams elsewhere.
        //
        // A matching key short-circuits ProseMirror's widget comparison and the
        // DOM is REUSED, factory and all — so anything the button renders has
        // to be in the key, or it freezes at whatever the first build said.
        // That is exactly what `refreshPrdLenses` exists to change.
        key: lensKey(lens, ordinal, busyReason),
        ignoreSelection: true,
      }),
    );
  }
  return DecorationSet.create(doc, decorations);
}

/**
 * Rebuild the lenses even though the document did not change — the busy state
 * lives outside ProseMirror, and it decides whether a lens is clickable.
 */
export function refreshPrdLenses(view: EditorView): void {
  view.dispatch(view.state.tr.setMeta(prdLensKey, true));
}

/**
 * The lens surface, as a Tiptap extension. Absent from every editor but the
 * PRD's: the affordances are the PRD's own, and a design or feature file would
 * only be offered commands that do not apply to it.
 */
export const PrdLenses = Extension.create<PrdLensOptions>({
  name: "prdLenses",

  addOptions() {
    return {
      run: () => {},
      isBusy: () => false,
      busyReason: () => "",
      discuss: () => {},
      compose: () => {},
    };
  },

  addProseMirrorPlugins() {
    const opts = this.options;
    return [
      new Plugin<DecorationSet>({
        key: prdLensKey,
        state: {
          init: (_config, state) => build(state.doc, opts),
          apply: (tr, current, _old, next) =>
            tr.docChanged || tr.getMeta(prdLensKey) ? build(next.doc, opts) : current,
        },
        props: {
          decorations(state) {
            return prdLensKey.getState(state);
          },
        },
      }),
    ];
  },
});
