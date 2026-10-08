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

import { useEffect, useState } from "react";
import type * as Y from "yjs";
import { yXmlFragmentToProseMirrorRootNode } from "y-prosemirror";
import { isMarkdownPath, listDocPaths } from "@aep/collab-doc";
import type { LineBlock } from "../model/ids";
import { docLines } from "./docLines";
import { specSchema } from "./specSchema";

/** Every markdown file's lines, by room path, read from the doc as it is now. */
export function readSpecLines(doc: Y.Doc): Map<string, LineBlock[]> {
  const lines = new Map<string, LineBlock[]>();
  for (const path of listDocPaths(doc)) {
    if (!isMarkdownPath(path)) continue;
    lines.set(path, docLines(yXmlFragmentToProseMirrorRootNode(doc.getXmlFragment(path), specSchema)));
  }
  return lines;
}

/**
 * The spec's lines, re-read on every change to the doc: the ID index, the
 * lines to confirm and the Fog follow the user's typing (and, once the room
 * is wired, the agent's). A spec is a handful of short files, so re-reading
 * all of them is cheaper than tracking which one changed.
 */
export function useSpecLines(doc: Y.Doc | null): ReadonlyMap<string, LineBlock[]> | null {
  const [lines, setLines] = useState<{ doc: Y.Doc; lines: ReadonlyMap<string, LineBlock[]> } | null>(null);
  useEffect(() => {
    if (!doc) return;
    const read = () => setLines({ doc, lines: readSpecLines(doc) });
    read();
    doc.on("update", read);
    return () => doc.off("update", read);
  }, [doc]);
  // Lines read from a previous doc (another project) are never served.
  return lines && lines.doc === doc ? lines.lines : null;
}
