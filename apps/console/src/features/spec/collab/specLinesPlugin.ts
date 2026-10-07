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

// How a spec document reads, as ProseMirror decorations over the markdown it
// is: the markdown itself is never rewritten. A story's ID is drawn as a quiet
// mono ID at the start of its line, a line tagged `*assumed*` is highlighted
// and offers Keep, Remove and I'll edit (assumedLines.ts acts on them), a
// blocking question in Open Questions (model/questions.ts) is highlighted with
// its options, its tag marked (the box under the file answers it), a source
// and a `Needs:` or `Applies to:` clause are small tags, and every other ID is
// a quiet link (the editor's hover and click read `data-spec-id`). Text the
// agent has just written shows a green wash that fades as it lands: every
// edit is applied directly, and what the agent decided for the user carries
// `*assumed*`, so the wash only helps the eye follow the change. On the
// product page the Features list gives way to the feature rows, and the Fog's
// entries are drawn as the Fog. A line a design comment changed is marked as
// such, with the comment's number, until the comment is resolved.
//
// Decorations are rebuilt from the document on every change, and when the
// options change (setSpecLinesOptions), rather than mapped forward: the user
// types into these lines, and a spec file is a few dozen blocks.

import { Extension } from "@tiptap/core";
import type { Node as PmNode } from "@tiptap/pm/model";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { parseLine, type LineParts, type Span } from "../model/ids";
import { blockingEntries } from "../model/questions";
import { docLines, type DocLine } from "./docLines";

/** What an assumed line's buttons do; the button carries it as `data-assumed`. */
export type AssumedAction = "keep" | "remove" | "edit";

const ASSUMED_ACTIONS: { action: AssumedAction; label: string }[] = [
  { action: "keep", label: "Keep" },
  { action: "remove", label: "Remove" },
  { action: "edit", label: "I'll edit" },
];

/** The class of the slot the feature rows draw into. */
export const ROWS_SLOT = "aep-rows";

function rowsSlot(): HTMLElement {
  const el = document.createElement("div");
  el.className = ROWS_SLOT;
  return el;
}

export interface SpecLinesOptions {
  /** Draw a slot for the feature rows in place of the Features list. Product page only. */
  featureRows: boolean;
  /** Leave the Fog out: a small product shows its feature list alone. */
  hideFog: boolean;
  /** Lines a design comment changed, by line ID, and what their mark says. Feature pages only. */
  designChanged: ReadonlyMap<string, string> | null;
}

/** A decoration to draw, as data: what `build` turns into ProseMirror's. */
export type LineMark =
  | { kind: "line"; from: number; to: number; lineId: string | null; assumed: boolean; blocking: boolean }
  | { kind: "actions"; at: number; body: string }
  | { kind: "sid" | "was" | "source" | "clause" | "assumed" | "blocking" | "fresh"; from: number; to: number }
  | { kind: "ref"; from: number; to: number; id: string };

/** How long the agent's newly written text keeps its wash: long enough to see it land, and it fades within it. */
export const FRESH_MS = 6000;

/**
 * What one line draws: its highlight, its ID, its tags and its links, at its
 * end the actions on an assumed line, and a fading wash over text the agent
 * wrote less than FRESH_MS before `now`.
 * `blocking`: the line is a blocking question of the file's Open Questions;
 * a `*blocking*` tag anywhere else is only emphasis.
 */
export function lineMarks(
  line: Pick<DocLine, "lineFrom" | "lineTo" | "posAt" | "text" | "agentRuns">,
  parts: LineParts,
  blocking = false,
  now = Date.now(),
): LineMark[] {
  const marks: LineMark[] = [];
  const at = (span: Span) => ({ from: line.posAt(span.start), to: line.posAt(span.end) });
  if (parts.lead || parts.assumed || blocking) {
    marks.push({
      kind: "line",
      from: line.lineFrom,
      to: line.lineTo,
      lineId: parts.lead?.id ?? null,
      assumed: parts.assumed !== null,
      blocking,
    });
  }
  if (parts.lead) marks.push({ kind: "sid", ...at(parts.lead) });
  if (parts.was) marks.push({ kind: "was", ...at(parts.was) });
  for (const source of parts.sources) marks.push({ kind: "source", ...at(source) });
  for (const clause of parts.clauses) marks.push({ kind: "clause", ...at(clause) });
  if (parts.assumed) marks.push({ kind: "assumed", ...at(parts.assumed) });
  if (blocking && parts.blocking) marks.push({ kind: "blocking", ...at(parts.blocking) });
  for (const ref of parts.refs) marks.push({ kind: "ref", id: ref.id, ...at(ref) });
  for (const run of line.agentRuns) {
    const written = Date.parse(run.by.at);
    if (Number.isFinite(written) && now - written < FRESH_MS) marks.push({ kind: "fresh", ...at(run) });
  }
  if (parts.assumed) marks.push({ kind: "actions", at: line.posAt(line.text.length), body: parts.body });
  return marks;
}

function actionsDom(body: string): HTMLElement {
  const el = document.createElement("span");
  el.className = "aep-asm";
  el.contentEditable = "false";
  for (const { action, label } of ASSUMED_ACTIONS) {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.assumed = action;
    button.textContent = label;
    button.setAttribute("aria-label", `${label}: ${body}`);
    el.append(button);
  }
  return el;
}

function tagDom(className: string, text: string): HTMLElement {
  const el = document.createElement("span");
  el.className = className;
  el.contentEditable = "false";
  el.textContent = text;
  return el;
}

function lineClass(mark: { assumed: boolean; blocking: boolean }): string {
  if (mark.assumed) return "aep-line aep-line--assumed";
  return mark.blocking ? "aep-line aep-line--blocking" : "aep-line";
}

function toDecoration(mark: LineMark): Decoration {
  switch (mark.kind) {
    case "line":
      return Decoration.node(mark.from, mark.to, {
        class: lineClass(mark),
        ...(mark.lineId ? { "data-line-id": mark.lineId } : {}),
      });
    // The buttons are ProseMirror's to ignore (stopEvent): the editor's click
    // handler reads `data-assumed` and acts on the line.
    case "actions":
      return Decoration.widget(mark.at, () => actionsDom(mark.body), {
        key: `asm:${mark.body}`,
        side: 1,
        ignoreSelection: true,
        stopEvent: () => true,
      });
    // IDs, sources and clauses are not words: no spelling squiggles under them.
    case "ref":
      return Decoration.inline(mark.from, mark.to, { class: "aep-ref", "data-spec-id": mark.id, spellcheck: "false" });
    default:
      return Decoration.inline(mark.from, mark.to, { class: `aep-${mark.kind}`, spellcheck: "false" });
  }
}

/** The product page's sections that draw differently: Features and Fog. */
function sectionDecorations(doc: PmNode, opts: SpecLinesOptions): Decoration[] {
  const out: Decoration[] = [];
  let section: string | null = null;
  doc.forEach((node, offset) => {
    const end = offset + node.nodeSize;
    if (node.type.name === "heading" && Number(node.attrs.level) <= 2) {
      section = node.textContent.trim().toLowerCase();
      if (section === "features" && opts.featureRows) {
        // An empty slot, fresh per widget view; the editor finds the one in
        // the live DOM and renders the rows into it (SpecEditor.tsx). Handing
        // ProseMirror one shared element instead lets two widget views hold
        // the same node while it redraws, and its child sync never settles.
        out.push(
          Decoration.widget(end, () => rowsSlot(), {
            key: "feature-rows",
            side: -1,
            ignoreSelection: true,
            stopEvent: () => true,
          }),
        );
      }
      if (section === "fog") {
        out.push(Decoration.node(offset, end, opts.hideFog ? { class: "aep-hidden" } : { "data-anchor": "fog" }));
      }
      return;
    }
    if (section === "features" && opts.featureRows && node.type.name === "bulletList") {
      out.push(Decoration.node(offset, end, { class: "aep-hidden" }));
    }
    if (section === "fog") {
      out.push(Decoration.node(offset, end, { class: opts.hideFog ? "aep-hidden" : "aep-fog" }));
    }
  });
  return out;
}

function build(doc: PmNode, opts: SpecLinesOptions): DecorationSet {
  const decorations = sectionDecorations(doc, opts);
  const lines = docLines(doc);
  const blocking = new Set(blockingEntries(lines).map((e) => e.line));
  for (const line of lines) {
    const parts = parseLine(line.text, line.emphasis);
    for (const mark of lineMarks(line, parts, blocking.has(line))) decorations.push(toDecoration(mark));
    const changed = parts.lead ? opts.designChanged?.get(parts.lead.id) : undefined;
    if (changed) {
      decorations.push(Decoration.node(line.lineFrom, line.lineTo, { class: "aep-line--design" }));
      decorations.push(
        Decoration.widget(line.to - 1, () => tagDom("aep-dtag", changed), {
          key: `design:${changed}`,
          side: 1,
          ignoreSelection: true,
        }),
      );
    }
  }
  return DecorationSet.create(doc, decorations);
}

interface SpecLinesState {
  opts: SpecLinesOptions;
  decorations: DecorationSet;
}

const specLinesKey = new PluginKey<SpecLinesState>("specLines");

/**
 * Draw the open document with new options. They change the drawing only, so
 * they go in as a transaction rather than a new editor: an editor rebuilt for
 * them would lose its undo history and the cursor.
 */
export function setSpecLinesOptions(view: EditorView, opts: SpecLinesOptions): void {
  const current = specLinesKey.getState(view.state)?.opts;
  if (
    current &&
    current.featureRows === opts.featureRows &&
    current.hideFog === opts.hideFog &&
    current.designChanged === opts.designChanged
  ) {
    return;
  }
  view.dispatch(view.state.tr.setMeta(specLinesKey, opts));
}

export const SpecLines = Extension.create<SpecLinesOptions>({
  name: "specLines",
  addOptions() {
    return { featureRows: false, hideFog: false, designChanged: null };
  },
  addProseMirrorPlugins() {
    const initial = this.options;
    return [
      new Plugin<SpecLinesState>({
        key: specLinesKey,
        state: {
          init: (_, state) => ({ opts: initial, decorations: build(state.doc, initial) }),
          apply: (tr, old) => {
            const opts = (tr.getMeta(specLinesKey) as SpecLinesOptions | undefined) ?? old.opts;
            if (opts === old.opts && !tr.docChanged) return old;
            return { opts, decorations: build(tr.doc, opts) };
          },
        },
        props: {
          decorations: (state) => specLinesKey.getState(state)?.decorations,
        },
      }),
    ];
  },
});
