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
import { Transform } from "@tiptap/pm/transform";
import type * as Y from "yjs";
import { updateYFragment, yXmlFragmentToProseMirrorRootNode } from "y-prosemirror";
import { parseLine } from "../model/ids";
import { blockingEntries } from "../model/questions";
import { docLines, type DocLine } from "./docLines";
import { specSchema } from "./specSchema";

// The edits the user makes to the spec by acting on what the agent left in it
// (a line to confirm, a question answered), as steps on a
// ProseMirror Transform. The editor runs them on its own transaction when the
// action is in the open file; `editFile` runs them on a file's fragment
// directly when it is not. Either way the change lands in the Y.Doc, so once
// the room is wired every peer sees it and the committer writes it.

/** Marks the user's edits made outside an editor. */
const USER_ORIGIN = "console:user";

/** Whitespace before the `*assumed*` tag goes with it. */
function tagRange(line: DocLine): { from: number; to: number } | null {
  const tag = parseLine(line.text, line.emphasis).assumed;
  if (!tag) return null;
  let start = tag.start;
  while (start > 0 && /\s/.test(line.text[start - 1]!)) start--;
  return { from: line.posAt(start), to: line.posAt(tag.end) };
}

/** Settle an assumed line: drop its `*assumed*` tag, keep its words. False when it has none. */
export function removeAssumedTag(tr: Transform, line: DocLine): boolean {
  const range = tagRange(line);
  if (!range) return false;
  tr.delete(range.from, range.to);
  return true;
}

/** Where the caret goes to edit an assumed line: after its words, before the tag. */
export function caretBeforeTag(line: DocLine): number {
  return tagRange(line)?.from ?? line.posAt(line.text.length);
}

/** Delete a line: its list item (the whole list, when it is the only item), or its block. */
export function deleteLine(tr: Transform, line: DocLine): void {
  const $line = tr.doc.resolve(line.lineFrom);
  const list = $line.parent;
  if (line.kind === "listItem" && list.childCount === 1 && $line.depth > 0) {
    tr.delete($line.before(), $line.after());
    return;
  }
  tr.delete(line.lineFrom, line.lineTo);
}

/** The line containing a document position. */
export function lineAt(doc: PmNode, pos: number): DocLine | null {
  return docLines(doc).find((l) => pos >= l.from && pos < l.to) ?? null;
}

/** The top-level heading `## <title>` and the block after it, by position. */
function section(doc: PmNode, title: string): { headingEnd: number; next: PmNode | null; nextPos: number } | null {
  let found: { headingEnd: number; next: PmNode | null; nextPos: number } | null = null;
  doc.forEach((node, offset, index) => {
    if (found || node.type.name !== "heading" || Number(node.attrs.level) > 2) return;
    if (node.textContent.trim().toLowerCase() !== title.toLowerCase()) return;
    const end = offset + node.nodeSize;
    found = { headingEnd: end, next: index + 1 < doc.childCount ? doc.child(index + 1) : null, nextPos: end };
  });
  return found;
}

/**
 * Add a settled line to the end of a section's list: the section is created
 * at the end of the document when missing, and its list after the heading.
 */
export function appendToSection(tr: Transform, title: string, text: string): void {
  const { nodes } = tr.doc.type.schema;
  const item = nodes.listItem!.create(null, nodes.paragraph!.create(null, tr.doc.type.schema.text(text)));
  const found = section(tr.doc, title);
  if (!found) {
    tr.insert(tr.doc.content.size, [
      nodes.heading!.create({ level: 2 }, tr.doc.type.schema.text(title)),
      nodes.bulletList!.create(null, item),
    ]);
    return;
  }
  if (found.next?.type.name === "bulletList") {
    tr.insert(found.nextPos + found.next.nodeSize - 1, item);
    return;
  }
  tr.insert(found.headingEnd, nodes.bulletList!.create(null, item));
}

/**
 * A markdown file's fragment, or null when the doc has no such file. Never
 * creates one: opening a file is not writing it.
 */
export function fileFragment(doc: Y.Doc, path: string): Y.XmlFragment | null {
  return doc.share.has(path) ? doc.getXmlFragment(path) : null;
}

/**
 * Run an edit on one markdown file of the doc, in one Yjs transaction. The
 * fragment is read as a document, the edit runs on it, and only what changed
 * is written back (y-prosemirror's minimal update, as the agents' writes in
 * @aep/collab-doc are), so an editor bound to the file keeps its caret and a
 * collaborator's concurrent typing survives. False when the file is not in the
 * doc or the edit changed nothing. The transaction carries `origin`: the
 * user's, unless the edit is someone else's.
 */
export function editFile(
  doc: Y.Doc,
  path: string,
  edit: (tr: Transform) => void,
  origin: unknown = USER_ORIGIN,
): boolean {
  const fragment = fileFragment(doc, path);
  if (!fragment) return false;
  const tr = new Transform(yXmlFragmentToProseMirrorRootNode(fragment, specSchema));
  edit(tr);
  if (!tr.docChanged) return false;
  doc.transact(() => {
    updateYFragment(doc, fragment, tr.doc, { mapping: new Map(), isOMark: new Map() });
  }, origin);
  return true;
}

/**
 * Settle an assumed line of a file from outside its editor (the chat's walk
 * of what an interview assumed): keep drops the `*assumed*` tag, remove
 * deletes the line, as the line's own buttons do in the editor. The line is
 * found by its words; false when no assumed line has them any more (it was
 * settled on the page meanwhile).
 */
export function settleAssumedLine(doc: Y.Doc, path: string, text: string, action: "keep" | "remove"): boolean {
  return editFile(doc, path, (tr) => {
    const line = docLines(tr.doc).find(
      (l) => l.text === text && parseLine(l.text, l.emphasis).assumed !== null,
    );
    if (!line) return;
    if (action === "keep") removeAssumedTag(tr, line);
    else deleteLine(tr, line);
  });
}

/**
 * Answer a feature's blocking question, in one Yjs transaction: its entry
 * (with the options nested under it) leaves Open Questions, and the answer is
 * a settled line in the feature's Decisions. The entry is found by its words;
 * false when no blocking question in the file has them any more (it was
 * answered or edited meanwhile), and then nothing is written.
 */
export function answerBlockingQuestion(doc: Y.Doc, path: string, question: string, answer: string): boolean {
  const words = answer.trim();
  if (!words) return false;
  return editFile(doc, path, (tr) => {
    const entry = blockingEntries(docLines(tr.doc)).find((e) => e.question === question);
    if (!entry) return;
    deleteLine(tr, entry.line);
    appendToSection(tr, "Decisions", words);
  });
}
