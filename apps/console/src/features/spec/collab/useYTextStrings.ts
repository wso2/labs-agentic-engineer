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

/**
 * `useYTextString` for SEVERAL documents at once, returning their strings in
 * the order given (`null` for a path the room has not delivered).
 *
 * It exists because one rail entry can stand for many documents — the
 * Acceptance criteria entry is every `specs/validation/acceptance/*.feature` — and the
 * single-text hook cannot be called in a loop. One subscription per text,
 * one `useSyncExternalStore`.
 *
 * THE SNAPSHOT MUST BE CACHED. `texts.map((t) => t.toString())` builds a fresh
 * array on every call, React compares snapshots with `Object.is`, and the
 * result is not a subtle bug but an immediate render loop ("The result of
 * getSnapshot should be cached to avoid an infinite loop"). So the previous
 * array is kept and returned unless some document's content actually changed.
 * The single-text hook needs none of this: a string is a primitive and compares
 * by value.
 */
export function useYTextStrings(
  texts: readonly (Y.Text | null)[],
): readonly (string | null)[] {
  const cache = useRef<readonly (string | null)[]>([]);

  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      const bound = texts.filter((t): t is Y.Text => t !== null);
      for (const t of bound) t.observe(onStoreChange);
      return () => {
        for (const t of bound) t.unobserve(onStoreChange);
      };
    },
    // The array is rebuilt by the caller on every render, so identity would
    // resubscribe every time; its contents are what decide.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [texts.length, ...texts],
  );

  const getSnapshot = useCallback(() => {
    const next = texts.map((t) => (t === null ? null : t.toString()));
    const prev = cache.current;
    const same =
      prev.length === next.length && next.every((value, i) => prev[i] === value);
    if (!same) cache.current = next;
    return cache.current;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [texts.length, ...texts]);

  return useSyncExternalStore(subscribe, getSnapshot);
}
