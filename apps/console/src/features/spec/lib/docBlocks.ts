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

// One flattened block of a live markdown document, and the emphasised runs
// inside it.
//
// The shape is deliberately free of ProseMirror types — positions arrive as
// plain numbers — so the rules that read a document (which lens it offers, what
// a selection anchors to) stay testable without an editor. The walk that
// produces it needs the editor, and lives beside it in `collab/docBlocks.ts`.
//
// There is no PRD in it. The PRD's lenses were its first reader and it was
// named for them; aiming the agent at part of ANY markdown file (#666) is the
// second, and the shape did not have to change to serve it.

/** An emphasised (`*…*`) run inside a block, at its document positions. */
export interface EmphasisRun {
  text: string;
  from: number;
  to: number;
}

/** One textblock of a live document, flattened. */
export interface DocBlock {
  kind: "heading" | "listItem" | "paragraph";
  /** Heading depth; absent for everything else. */
  level?: number | undefined;
  /** The block's plain text. */
  text: string;
  emphasis: EmphasisRun[];
  /** Node start position. */
  from: number;
  /** Position just past the node. */
  to: number;
  /** Just inside the node's closing token — where a trailing widget anchors. */
  contentEnd: number;
}
