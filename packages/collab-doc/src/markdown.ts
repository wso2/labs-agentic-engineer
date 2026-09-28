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

// Headless markdown ↔ ProseMirror plumbing (#86 phase 6). Markdown files are
// shared as Y.XmlFragments (Tiptap's collaboration binding needs rich
// structure, not Y.Text); this module owns the conversion at the seams:
// parse on seed, serialize for committer/agent reads. The extension set MUST
// match the console editor's, or content drifts.

import { getSchema } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { MarkdownManager } from "@tiptap/markdown";
import {
  prosemirrorJSONToYXmlFragment,
  yXmlFragmentToProsemirrorJSON,
} from "y-prosemirror";
import type * as Y from "yjs";
import type { Node as ProseMirrorNode } from "prosemirror-model";
import { AgentInsertion } from "./agent-mark.js";

// AgentInsertion rides in the ONE shared schema (console editor, seeding,
// committer serialization, agent diff application) — its markdown render is
// a passthrough, so committed files never carry review state.
const extensions = [StarterKit, AgentInsertion];

const manager = new MarkdownManager({ extensions });
const schema = getSchema(extensions);

/** Parse markdown into a ProseMirror Node (the diff-apply input, #86 ph6). */
export function markdownToNode(markdown: string): ProseMirrorNode {
  return schema.nodeFromJSON(manager.parse(markdown));
}

/**
 * `.md` files that are NOT editable prose. A file listed here is shared as
 * plain text (byte-exact, like design.json and openapi.yaml) rather than as a
 * Y.XmlFragment.
 *
 * The test is not the extension — it is whether a markdown round-trip
 * (parse → ProseMirror → serialize) is lossless. It is, for prose. It is NOT
 * for a document whose `.md` wraps structured content: `agent.afm.md` carries
 * YAML front matter, and the round-trip escapes `_` as `\_`, turns `>` into
 * `&gt;`, and reflows `- type: webchat` into a bullet list — de-indenting
 * everything nested under it. The result is front matter that no longer
 * parses as YAML at all, committed to the project's repo.
 *
 * That is not hypothetical: it shipped, and the corruption reached a real
 * project's `specs/`. Add any future structured `.md` here.
 */
const NON_PROSE_MARKDOWN: ReadonlySet<string> = new Set(["agent.afm.md"]);

/** Whether a path is editable PROSE — rich-text in the console, XmlFragment here. */
export function isMarkdownPath(path: string): boolean {
  if (!path.endsWith(".md")) return false;
  return !NON_PROSE_MARKDOWN.has(path.slice(path.lastIndexOf("/") + 1));
}

/**
 * An EMPTY document is one empty paragraph, not zero blocks.
 *
 * That is what the editor itself holds for a document with nothing in it, and
 * `fragmentToMarkdown` serializes it back to "" — so the two agree and no
 * spurious write is produced. Parsing "" gives zero blocks instead, and the
 * difference is not cosmetic: a fragment with no children generates NO Yjs
 * update, so its share key never replicates to a joining client. `doc.share`
 * then answers "no such document" for a file the room was seeded with and git
 * really holds, which left an emptied markdown file permanently uneditable —
 * and emptying one is a supported action (the committer writes an emptied
 * fragment back as an empty file, since a top-level fragment cannot be deleted
 * from a Y.Doc).
 *
 * Normalizing here rather than at the call sites keeps every producer of a
 * fragment — the seed, and an agent writing a file — on the same shape.
 */
function withEmptyDocAsParagraph(json: Record<string, unknown>): Record<string, unknown> {
  const content = json.content;
  if (Array.isArray(content) && content.length > 0) return json;
  return { ...json, content: [{ type: "paragraph" }] };
}

/** Parse markdown into an (empty) Y.XmlFragment. */
export function markdownToFragment(
  markdown: string,
  fragment: Y.XmlFragment,
): void {
  prosemirrorJSONToYXmlFragment(
    schema,
    withEmptyDocAsParagraph(
      manager.parse(markdown) as unknown as Record<string, unknown>,
    ),
    fragment,
  );
}

/** Serialize a fragment back to markdown (committer + agent-read seam). */
export function fragmentToMarkdown(fragment: Y.XmlFragment): string {
  return manager.serialize(yXmlFragmentToProsemirrorJSON(fragment));
}
