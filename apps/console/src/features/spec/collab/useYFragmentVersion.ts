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

/**
 * A counter that bumps whenever a live markdown document changes.
 *
 * `useYTextString` covers a `Y.Text` — a plain-text file streaming in. A
 * markdown document is a `Y.XmlFragment` instead, and its content cannot be
 * surfaced as a primitive the way a string can: converting it to markdown on
 * every keystroke, to hand React something comparable, would do that work
 * whether or not anyone needed the result.
 *
 * So this reports only THAT it changed and leaves the caller to derive what it
 * needs behind a memo keyed on the count. The rail's assumption and
 * open-question counts read the live document this way, which is what makes an
 * alert clear the moment a flag is deleted rather than when the collab server
 * next commits — the committed copy can be a flush behind, and on the agent's
 * own edits that lag was long enough to look broken.
 *
 * `observeDeep`, because the flags live inside the document's blocks rather
 * than on the fragment itself: a shallow observer would miss a word being
 * deleted from a line, which is precisely the edit that clears an assumption.
 *
 * A counter rather than `useSyncExternalStore`: there is no cheap snapshot to
 * compare. The fragment's own length counts top-level blocks and does not move
 * when a word inside one is removed, and anything finer means doing the
 * conversion this hook exists to defer.
 */
export function useYFragmentVersion(fragment: Y.XmlFragment | null): number {
  const [version, setVersion] = useState(0);

  useEffect(() => {
    if (!fragment) return;
    const bump = () => setVersion((n) => n + 1);
    fragment.observeDeep(bump);
    // Bump on bind too: a fragment that arrives already populated (the room
    // seeded from git before this mounted) fires no event of its own, and the
    // caller would sit on a stale derivation until the next keystroke.
    bump();
    return () => fragment.unobserveDeep(bump);
  }, [fragment]);

  return version;
}
