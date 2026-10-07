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

import type { Node as PmNode } from "@tiptap/pm/model";
import { AGENT_INSERTION } from "@aep/collab-doc";
import type { AgentWriter } from "../api/specModel";
import type { LineBlock } from "../model/ids";

// The walk from a live ProseMirror document to its lines (after the old console's
// docBlocks). A list entry is the paragraph INSIDE its `listItem`: that is the
// textblock that carries the words, so its item-ness comes from the parent.
// Text the agent wrote carries the agentInsertion mark (@aep/collab-doc) with
// when it wrote it, so a fresh write can be highlighted as it lands. A list
// entry's depth counts the lists around it, so an entry's nested bullets (a
// blocking question's options) are told from its siblings.

const LISTS = new Set(["bulletList", "orderedList"]);

/** A run of the agent's text, as offsets into the line's text. */
export interface AgentRun {
  start: number;
  end: number;
  by: AgentWriter;
}

/** A line of a live document, with the positions a decoration needs. */
export interface DocLine extends LineBlock {
  /** The textblock's range. */
  from: number;
  to: number;
  /** The range a whole-line highlight covers: its list item when it is one, else the block. */
  lineFrom: number;
  lineTo: number;
  /** The agent's text in the line, in order. */
  agentRuns: AgentRun[];
  /** The document position of an offset into `text`. */
  posAt: (offset: number) => number;
}

interface Segment {
  /** Where the text node starts in the line's text, and in the document. */
  offset: number;
  pos: number;
  length: number;
}

interface Parent {
  node: PmNode;
  pos: number;
  /** The textblock is the parent's first child. */
  first: boolean;
  /** How many lists the parent is inside. */
  lists: number;
}

function line(node: PmNode, pos: number, parent: Parent): DocLine {
  let text = "";
  const segments: Segment[] = [];
  const emphasis: LineBlock["emphasis"] = [];
  const agentRuns: AgentRun[] = [];
  node.forEach((child, childOffset) => {
    if (!child.isText) return;
    const start = text.length;
    const value = child.text ?? "";
    segments.push({ offset: start, pos: pos + 1 + childOffset, length: value.length });
    text += value;
    const agent = child.marks.find((m) => m.type.name === AGENT_INSERTION);
    if (agent) {
      const by = { agent: String(agent.attrs.agent ?? ""), at: String(agent.attrs.at ?? "") };
      const last = agentRuns.at(-1);
      if (last?.end === start && last.by.agent === by.agent && last.by.at === by.at) last.end = text.length;
      else agentRuns.push({ start, end: text.length, by });
    }
    if (!child.marks.some((m) => m.type.name === "italic")) return;
    // One `*assumed*` can arrive as several text nodes (an agent's streamed
    // write marks its insertions); rejoin the touching runs.
    const previous = emphasis.at(-1);
    if (previous?.end === start) previous.end = text.length;
    else emphasis.push({ start, end: text.length });
  });
  const listItem = parent.node.type.name === "listItem";
  const inItem = listItem && parent.first;
  return {
    kind: node.type.name === "heading" ? "heading" : listItem ? "listItem" : "paragraph",
    ...(node.type.name === "heading" ? { level: Number(node.attrs.level) } : {}),
    ...(listItem ? { depth: parent.lists } : {}),
    text,
    emphasis,
    agentRuns,
    from: pos,
    to: pos + node.nodeSize,
    lineFrom: inItem ? parent.pos : pos,
    lineTo: inItem ? parent.pos + parent.node.nodeSize : pos + node.nodeSize,
    posAt: (offset) => {
      const segment = segments.find((s) => offset <= s.offset + s.length) ?? segments.at(-1);
      return segment ? segment.pos + Math.min(offset - segment.offset, segment.length) : pos + 1;
    },
  };
}

/** Every textblock of the document, in order. */
export function docLines(doc: PmNode): DocLine[] {
  const out: DocLine[] = [];
  const walk = (node: PmNode, contentStart: number, lists: number) => {
    node.forEach((child, offset) => {
      const pos = contentStart + offset;
      if (child.isTextblock) {
        out.push(line(child, pos, { node, pos: contentStart - 1, first: offset === 0, lists }));
      } else if (!child.isLeaf) {
        walk(child, pos + 1, lists + (LISTS.has(child.type.name) ? 1 : 0));
      }
    });
  };
  walk(doc, 0, 0);
  return out;
}
