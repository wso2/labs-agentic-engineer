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

import { useCallback, useRef, useSyncExternalStore } from "react";
import type * as Y from "yjs";
import { listDocPaths, readDocFile } from "@aep/collab-doc";

/**
 * The room's files under some prefixes, as text by path, kept current as the
 * room changes (an agent writing the design shows on the next render). Empty
 * while there is no doc.
 */
export function useRoomFiles(doc: Y.Doc | null, prefixes: readonly string[]): Readonly<Record<string, string>> {
  const cache = useRef<{ doc: Y.Doc | null; key: string; version: number; files: Record<string, string> } | null>(null);
  const version = useRef(0);
  const subscribe = useCallback(
    (fn: () => void) => {
      if (!doc) return () => undefined;
      const bump = () => {
        version.current += 1;
        fn();
      };
      doc.on("afterTransaction", bump);
      return () => doc.off("afterTransaction", bump);
    },
    [doc],
  );
  const key = prefixes.join("\n");
  const snapshot = useCallback(() => {
    // Read again when the room changed, and when the doc itself did (a doc that
    // arrives already seeded fires no transaction to count).
    let c = cache.current;
    if (!c || c.doc !== doc || c.key !== key || c.version !== version.current) {
      const files: Record<string, string> = {};
      if (doc) {
        for (const path of listDocPaths(doc)) {
          if (!key.split("\n").some((p) => path.startsWith(p))) continue;
          const text = readDocFile(doc, path);
          if (text !== undefined) files[path] = text;
        }
      }
      c = { doc, key, version: version.current, files };
      cache.current = c;
    }
    return c.files;
  }, [doc, key]);
  return useSyncExternalStore(subscribe, snapshot);
}
