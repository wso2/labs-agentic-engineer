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

/**
 * Reading a SET of sibling spec files, room first and git second — the same
 * two sources, in the same order, that every single-file read in Spec already
 * uses.
 *
 * It is its own module because its responsibility is not the Security page's:
 * "give me the current text of these paths, wherever it lives" is the shape any
 * cross-file check needs, and the Security page is only the first to need it.
 *
 * The room half is the interesting one. A spec file's live text is a `Y.Text`,
 * which changes without React knowing, so this subscribes to the ROOM DOC
 * rather than to each file: one subscription covers every path, including a
 * path the room has not delivered yet.
 */

import { useCallback, useMemo, useRef, useSyncExternalStore } from "react";
import type * as Y from "yjs";

import type { SecurityReferenceContext } from "@aep/agent-stream";

import type { SpecFileEntry } from "../api/mapping";
import { useSpecFileContents } from "../api/queries";
import type { CollabSpec } from "../collab/useCollabSpec";

/** What a set of spec files reads as: a path answers text, or nothing. */
export interface SpecReferences {
  read(path: string): string | undefined;
}

// This IS the shape `@aep/agent-stream`'s referential rules read their sibling
// files through, and the Security page hands one straight to
// `securityReferenceFindings`. Asserted rather than aliased so the name here
// stays about spec files in general, and so a change on either side is a
// compile error rather than a surprise at the one call site.
const _isReferenceContext: SecurityReferenceContext = {
  read: () => undefined,
} satisfies SpecReferences;
void _isReferenceContext;

/**
 * The current text of `paths`, room first, committed copy second.
 *
 * A path the room does not hold is fetched from git ONLY when the committed
 * tree actually lists it, so a document naming a component whose specs were
 * never written costs no request and answers `undefined` — which is exactly
 * what the cross-check rules expect for a file they cannot see.
 *
 * The returned object's identity is stable while its answers are, so a caller
 * may put it in a dependency array and memoise the work it feeds.
 */
export function useSpecReferences({
  projectName,
  paths,
  files,
  collab,
  active,
}: {
  projectName: string;
  /** The paths to resolve. */
  paths: string[];
  /** The committed tree, for the git fallback. */
  files: SpecFileEntry[];
  collab: CollabSpec;
  /** False when nothing is looking — every read is then skipped. */
  active: boolean;
}): SpecReferences {
  const roomText = useRoomTexts(active ? collab : null, paths);

  // Fetch only what the room does not already hold AND git actually has: no
  // 404-probing for a path that was never committed, and no request for a file
  // the room is already streaming.
  const wanted = useMemo(() => {
    if (!active) return [];
    return files.filter(
      (f) => paths.includes(f.path) && roomText.get(f.path) === undefined,
    );
  }, [active, files, paths, roomText]);

  const committed = useSpecFileContents(projectName, wanted);

  return useMemo(
    () => ({ read: (path: string) => roomText.get(path) ?? committed[path] }),
    [roomText, committed],
  );
}

/**
 * The room's text for each of `paths`, re-read whenever the doc changes.
 *
 * `useSyncExternalStore` compares snapshots with `Object.is`, so `getSnapshot`
 * must not build a fresh Map on every call or the store loops forever. The map
 * is therefore cached and rebuilt only when the doc has actually updated or the
 * path list has changed — a revision counter, not a deep compare, because the
 * snapshot is read on every render and the files here run to tens of kilobytes.
 */
function useRoomTexts(
  collab: CollabSpec | null,
  paths: string[],
): ReadonlyMap<string, string> {
  const revision = useRef(0);
  const cache = useRef<{
    revision: number;
    key: string | null;
    texts: ReadonlyMap<string, string>;
  }>({ revision: -1, key: null, texts: EMPTY });

  const doc = collab?.doc ?? null;
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      if (!doc) return () => {};
      const bump = () => {
        revision.current++;
        onStoreChange();
      };
      // `update` fires for local and remote edits alike, and — because this
      // watches the doc rather than one Y.Text — also for a file arriving in
      // the room for the first time, which is the case a per-file observer
      // cannot see.
      doc.on("update", bump);
      return () => doc.off("update", bump);
    },
    [doc],
  );

  const getFileText = collab?.getFileText;
  // The path list is keyed by CONTENT, not identity: a caller that rebuilds the
  // array each render would otherwise miss the cache every time, hand
  // `useSyncExternalStore` a fresh Map every time, and spin forever.
  const key = paths.join("\n");
  const getSnapshot = useCallback(() => {
    const current = cache.current;
    if (current.revision === revision.current && current.key === key) {
      return current.texts;
    }
    const texts = new Map<string, string>();
    if (getFileText) {
      for (const path of key === "" ? [] : key.split("\n")) {
        const ytext: Y.Text | null = getFileText(path);
        if (ytext) texts.set(path, ytext.toString());
      }
    }
    cache.current = { revision: revision.current, key, texts };
    return texts;
  }, [getFileText, key]);

  return useSyncExternalStore(subscribe, getSnapshot);
}

const EMPTY: ReadonlyMap<string, string> = new Map();
