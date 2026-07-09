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
import type { SpecFile } from "./bff.js";
import { isMarkdownPath, markdownToFragment } from "./markdown.js";

// Doc model (#86 decision 2 + phase 6): one Y.Doc per project. Markdown
// files are Y.XmlFragments keyed by path (Tiptap's collaboration binding
// needs rich structure); everything else is a Y.Text in Y.Map('files').
//
// KEY SCHEME INVARIANT (#113 decision 2 / #114): doc keys are the repo path
// with the `specs/` prefix stripped (requirements/prd.md). The console's spec
// feature strips identically at its API boundary; any future peer that writes
// to rooms (agents, #86) must too. The strip lives in bff.ts (toRoomPath).
export const FILES_MAP = "files";

export function filesMap(doc: Y.Doc): Y.Map<Y.Text> {
  return doc.getMap<Y.Text>(FILES_MAP);
}

/**
 * Populate an (empty or partial) doc from a spec bundle. Existing entries are
 * never overwritten — a rejoin after eviction reseeds only what's missing, and
 * live peer content always wins over a late seed.
 */
export function seedDocument(doc: Y.Doc, files: SpecFile[]): void {
  const map = filesMap(doc);
  doc.transact(() => {
    for (const file of files) {
      if (isMarkdownPath(file.path)) {
        const fragment = doc.getXmlFragment(file.path);
        if (fragment.length === 0) {
          markdownToFragment(file.content, fragment);
        }
      } else if (!map.has(file.path)) {
        const text = new Y.Text();
        text.insert(0, file.content);
        map.set(file.path, text);
      }
    }
  });
}
