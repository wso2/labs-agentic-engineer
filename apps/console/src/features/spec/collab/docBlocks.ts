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

// The walk from a live ProseMirror document to flat `DocBlock`s.
//
// It has to read the LIVE document rather than a markdown snapshot: the user
// is typing into this fragment and an agent is streaming into it at the same
// time, so a line number from a committed file points at the wrong line within
// a keystroke. Both readers derive from the doc on every change instead — a
// spec document is a few hundred nodes, so the rebuild is cheaper than mapping
// a stale parse forward and being subtly wrong.
//
// Two readers: the PRD's lenses (`prdLensPlugin.ts`) and the selection anchor
// every markdown file carries (`lib/anchor.ts`, #666).

import type { Node as PmNode } from "@tiptap/pm/model";
import type { DocBlock } from "../lib/docBlocks";

/**
 * Flatten a live document's textblocks. A list entry is the paragraph INSIDE its
 * `listItem` — that is the textblock the marker decorates and the widget
 * anchors to — so the item-ness comes from the parent.
 */
export function docBlocks(doc: PmNode): DocBlock[] {
  const blocks: DocBlock[] = [];
  doc.descendants((node, pos, parent) => {
    if (!node.isTextblock) return true;
    const emphasis: DocBlock["emphasis"] = [];
    node.forEach((child, offset) => {
      if (!child.isText || !child.marks.some((m) => m.type.name === "italic")) return;
      const from = pos + 1 + offset;
      const to = from + child.nodeSize;
      // One `*assumed*` can arrive as several text nodes — an agent's streamed
      // write marks its insertions, and a run split across two marks stays
      // split. Rejoin the touching ones so the flag is read as the word it is.
      const previous = emphasis.at(-1);
      if (previous?.to === from) {
        previous.text += child.text ?? "";
        previous.to = to;
        return;
      }
      emphasis.push({ text: child.text ?? "", from, to });
    });
    blocks.push({
      kind:
        node.type.name === "heading"
          ? "heading"
          : parent?.type.name === "listItem"
            ? "listItem"
            : "paragraph",
      level: node.type.name === "heading" ? Number(node.attrs.level) : undefined,
      text: node.textContent,
      emphasis,
      from: pos,
      to: pos + node.nodeSize,
      contentEnd: pos + node.nodeSize - 1,
    });
    // A textblock's children are text and marks, never further blocks.
    return false;
  });
  return blocks;
}
