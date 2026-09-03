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

// What a selection in a markdown document becomes on the wire (#666).
//
// The anchor LOCATES; it never carries the selected content (console ADR-0024).
// The agent joins the spec room as a live peer, so between the selection and
// the turn starting the user may keep typing and a teammate may edit too —
// content captured here would be a photograph of a document that has since
// moved. What travels is a name the agent finds in the CURRENT document.
//
// Deliberately free of ProseMirror types, like `prdLenses.ts` beside it: it
// takes flat `DocBlock`s and plain positions, so the rules that decide what a
// selection means are testable without an editor.

import type { DocBlock } from "./docBlocks.js";

/**
 * How much of a block's text names it. A locator has to be UNIQUE, not
 * complete: the model receives the whole block regardless, because the agent
 * read it from the live file. Real spec blocks diverge from their neighbours
 * within the first thirty characters, so anything longer is carried content
 * arriving through the back door.
 */
export const EXCERPT_MAX = 80;

/** How a heading path reads in `context`, root-first. */
export const PATH_SEPARATOR = " › ";

/** One selected node — the name the agent resolves, and what the transcript shows. */
export interface AnchorNode {
  name: string;
  kind: string;
  context?: string;
}

/** A selection, ready for the wire. */
export interface Anchor {
  file: string;
  nodes: AnchorNode[];
}

/** The vocabulary markdown names its nodes with. Structural on purpose: this
 *  has to work for any `.md`, so a PRD-specific reading like "open question"
 *  would make the anchor know about one document type. */
const KIND: Record<DocBlock["kind"], string> = {
  heading: "heading",
  listItem: "list item",
  paragraph: "paragraph",
};

/**
 * A block's name: its RENDERED text, collapsed and bounded at a word boundary.
 *
 * Rendered rather than source, because the source carries `**bold**`,
 * `*assumed*`, links and hard wraps the reader never saw — a source-exact
 * excerpt would fail to match for most blocks, and exactness is a false comfort
 * anyway when one keystroke breaks it. The agent matches on prose.
 *
 * No ellipsis is appended: the name is a string the agent searches for, and a
 * "…" in it would be a character the document does not contain. Truncation
 * SHOWS as an ellipsis in the transcript chip, which is a render concern.
 */
export function excerpt(text: string, max: number = EXCERPT_MAX): string {
  const collapsed = text.replace(/\s+/g, " ").trim();
  if (collapsed.length <= max) return collapsed;
  const cut = collapsed.slice(0, max);
  const lastSpace = cut.lastIndexOf(" ");
  // Only honour a word boundary that is not most of the way back to the start —
  // one very long word should be cut mid-word rather than yield a stub.
  const kept = lastSpace > max * 0.6 ? cut.slice(0, lastSpace) : cut;
  return kept.replace(/[\s,;:.—-]+$/, "");
}

/** Whether a block overlaps `[from, to]`, a collapsed caret counting as inside. */
function covers(block: DocBlock, from: number, to: number): boolean {
  return from === to
    ? // Exclusive at the end: a caret AT `block.to` sits between blocks, and an
      // inclusive check would hand the anchor both neighbours.
      block.from <= from && block.to > from
    : // A `to` sitting exactly on the next block's start is the end of THIS
      // block, not the start of that one — a trailing newline in the drag.
      block.from < to && block.to > from;
}

/**
 * The section a heading owns: itself, plus everything under it until the next
 * heading at the same level or shallower. Selecting a heading alone means the
 * section, which is what a user dragging across a title is pointing at.
 */
function section(blocks: DocBlock[], index: number): DocBlock[] {
  const head = blocks[index];
  if (!head || head.kind !== "heading") return head ? [head] : [];
  const level = head.level ?? 1;
  const out = [head];
  for (let i = index + 1; i < blocks.length; i += 1) {
    const block = blocks[i]!;
    if (block.kind === "heading" && (block.level ?? 1) <= level) break;
    out.push(block);
  }
  return out;
}

/**
 * The headings enclosing `index`, root-first. A heading's own path excludes
 * itself — a heading is named by its text, not by repeating it as context.
 */
export function headingPath(blocks: DocBlock[], index: number): string {
  const self = blocks[index];
  const ceiling = self?.kind === "heading" ? (self.level ?? 1) : Number.MAX_SAFE_INTEGER;
  const stack: DocBlock[] = [];
  for (let i = 0; i < index; i += 1) {
    const block = blocks[i]!;
    if (block.kind !== "heading") continue;
    const level = block.level ?? 1;
    while (stack.length > 0 && (stack.at(-1)!.level ?? 1) >= level) stack.pop();
    stack.push(block);
  }
  return stack
    .filter((h) => (h.level ?? 1) < ceiling)
    .map((h) => excerpt(h.text))
    .join(PATH_SEPARATOR);
}

/**
 * The blocks a selection resolves to. A partial paragraph becomes the whole
 * paragraph — a character range never reaches the agent, since the agent
 * streams into this same document and a range ending mid-sentence would hand
 * the model a fragment it cannot act on.
 */
export function selectedBlocks(blocks: DocBlock[], from: number, to: number): DocBlock[] {
  const indexes = blocks
    .map((block, i) => (covers(block, from, to) ? i : -1))
    .filter((i) => i >= 0);
  const first = indexes[0];
  if (first === undefined) return [];
  return indexes.length === 1 && blocks[first]!.kind === "heading"
    ? section(blocks, first)
    : indexes.map((i) => blocks[i]!);
}

/**
 * Build the anchor for a selection, or null when it resolves to nothing —
 * an empty document, or a range past the end.
 *
 * A block with no text is dropped: it names nothing the agent could find, and
 * an empty chip in the transcript would be worse than no chip.
 */
export function anchorFor(
  file: string,
  blocks: DocBlock[],
  from: number,
  to: number,
): Anchor | null {
  const selected = selectedBlocks(blocks, from, to);
  const nodes: AnchorNode[] = [];
  for (const block of selected) {
    const name = excerpt(block.text);
    if (name === "") continue;
    const context = headingPath(blocks, blocks.indexOf(block));
    nodes.push({ name, kind: KIND[block.kind], ...(context ? { context } : {}) });
  }
  return nodes.length > 0 ? { file, nodes } : null;
}
