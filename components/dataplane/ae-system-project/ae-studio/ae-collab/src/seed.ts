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

import * as Y from "yjs";
import { filesMap, isMarkdownPath, markdownToFragment } from "@aep/collab-doc";
import type { SpecFile } from "./bff.js";

// Doc model (#86 decision 2 + phase 6): one Y.Doc per project — the model
// itself (Y.Map('files') + md fragments) lives in @aep/collab-doc, shared
// with the agents service's live-peer path (#86 phase 4).
//
// KEY SCHEME INVARIANT: doc keys are the FULL repo-relative path, verbatim
// (specs/requirements/prd.md). The console's spec feature, the committer, and
// agent peers (#86 phase 4) all use this one scheme — no strip/re-add. (Retires
// #113 decision 2's stripped keys, which double-prefixed agent-created files.)
export { FILES_MAP, filesMap } from "@aep/collab-doc";

/**
 * Reference documents (#383) are user uploads under this folder — inputs to the
 * spec, never collaboratively-edited spec content. They stay out of the room in
 * BOTH directions: never seeded (a binary file seeded as text renders as
 * garbage), and never flushed (writing one back as text destroys the file — a
 * real PDF in git became its own base64 this way).
 *
 * Still needed after ADR-0017 took references out of git, for two reasons that
 * do not overlap. The repo's specs/.gitignore covers a WORKING TREE; this
 * committer builds writes from the ROOM, so no ignore rule reaches it. And
 * projects created under the feature's v1 really do have these paths committed
 * — this is what keeps a room seeded from one of them from flushing a PDF back
 * as text.
 */
export const REFERENCE_DOCS_PREFIX = "specs/requirements/references/";

/** True for a path inside the reference-documents folder. */
export function isReferenceDocPath(path: string): boolean {
  return path.startsWith(REFERENCE_DOCS_PREFIX);
}

/**
 * SEEDING IS IDEMPOTENT ACROSS PROCESSES, and that is the whole point of this
 * module rather than an incidental nicety.
 *
 * A room's document is seeded from git on every LOAD, and a load happens far
 * more often than a room is "new": `unloadImmediately` discards the server's
 * document the moment the last peer leaves, while a browser keeps ONE Y.Doc for
 * as long as the spec view stays mounted. Any reconnect — a redeploy, a laptop
 * waking, a dropped socket — therefore puts a client holding a full copy of the
 * room in front of a server that believes the room is empty.
 *
 * Written the obvious way, that merge DOUBLES the document. Yjs identifies every
 * insertion by (clientID, clock) and merges by union: two independently-built
 * copies of identical text are two disjoint sets of items, so the room ends up
 * holding the file twice, and the committer faithfully writes twice as much to
 * git. Observed in production as 374 → 750 → 1502 → 3006 → 6014 → 12030 → 24062
 * → 48126 lines, one doubling per session.
 *
 * The fix is to give the seed a STABLE IDENTITY: build it in a scratch document
 * whose clientID is a pure function of the content, then transfer it as an
 * update. Re-seeding the same bytes reproduces byte-identical items, and Yjs
 * skips items it already has — so the client's retained copy and the fresh seed
 * collapse into one. A client's own EDITS carry that client's own id, so they
 * are untouched by the dedupe and still merge normally.
 */
function seedClientId(path: string, content: string): number {
  // FNV-1a over path + content. Any stable 32-bit hash would do; what matters
  // is that it depends on BOTH — on the path so two files never share a clock
  // line, and on the content so that changed bytes get a fresh identity rather
  // than colliding with the old item ids at the same clocks. The NUL separator
  // keeps the fields from running together, so ("ab", "c") and ("a", "bc") hash
  // differently; it is spelled as an escape because a raw NUL in the source
  // makes the whole file binary to git.
  let hash = 0x811c9dc5;
  const input = `${path}\u0000${content}`;
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  // 0 is avoided only to keep the id visibly non-default in a debugger; Yjs
  // ascribes it no special meaning.
  return hash === 0 ? 1 : hash;
}

/**
 * The update that puts ONE file into a room, built under that file's stable
 * identity. Separate scratch docs per file (rather than one for the batch) keep
 * each file's item ids independent: editing one file must not renumber the
 * others, or every unchanged file would fail to dedupe on the next rejoin.
 */
function seedUpdateFor(file: SpecFile): Uint8Array {
  const scratch = new Y.Doc();
  scratch.clientID = seedClientId(file.path, file.content);
  if (isMarkdownPath(file.path)) {
    markdownToFragment(file.content, scratch.getXmlFragment(file.path));
  } else {
    const text = new Y.Text();
    filesMap(scratch).set(file.path, text);
    text.insert(0, file.content);
  }
  return Y.encodeStateAsUpdate(scratch);
}

/** Whether `doc` is still missing `file` — live content always wins over a seed. */
function needsSeeding(doc: Y.Doc, file: SpecFile): boolean {
  if (isMarkdownPath(file.path)) {
    return doc.getXmlFragment(file.path).length === 0;
  }
  return !filesMap(doc).has(file.path);
}

/** Whether the seed for `file` actually materialized in `doc`. */
function isPresent(doc: Y.Doc, file: SpecFile): boolean {
  return !needsSeeding(doc, file);
}

/**
 * Populate an (empty or partial) doc from a spec bundle. Existing entries are
 * never overwritten — a rejoin after eviction reseeds only what's missing, and
 * live peer content always wins over a late seed.
 *
 * Safe to run against a room a client is about to sync its own copy into: see
 * the note on `seedClientId` for why the two collapse rather than stack.
 *
 * `onAnomaly` reports the one failure this scheme can have — two files in one
 * bundle whose (path, content) hash collides, which would make the second file's
 * items look already-applied and silently drop them. Odds are ~1e-8 for a
 * ten-file bundle, but silent data loss is not something to leave unobserved, so
 * a dropped file is re-seeded WITHOUT a stable identity (content present, at the
 * cost of that one file doubling on a future rejoin) and reported.
 */
export function seedDocument(
  doc: Y.Doc,
  files: SpecFile[],
  onAnomaly?: (message: string) => void,
): void {
  const seeded: SpecFile[] = [];
  doc.transact(() => {
    for (const file of files) {
      if (!needsSeeding(doc, file)) continue;
      Y.applyUpdate(doc, seedUpdateFor(file));
      seeded.push(file);
    }
  });

  const dropped = seeded.filter((file) => !isPresent(doc, file));
  if (dropped.length === 0) return;
  doc.transact(() => {
    for (const file of dropped) {
      onAnomaly?.(
        `seed identity collision on ${file.path} — re-seeded without a stable id`,
      );
      if (isMarkdownPath(file.path)) {
        markdownToFragment(file.content, doc.getXmlFragment(file.path));
      } else {
        const text = new Y.Text();
        filesMap(doc).set(file.path, text);
        text.insert(0, file.content);
      }
    }
  });
}
