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
 * The hooks a prototype's screens use. None reaches outside the frame:
 * navigation is the review's screen, role and display state are the
 * reviewer's choice, and the data is the in-memory mock store.
 */

import { useCallback, useSyncExternalStore } from "react";
import { useKit } from "./context.js";
import type { DataStore } from "./store.js";

export interface KitNav {
  /** The screen shown. */
  screen: string;
  /** Go to a screen, optionally with params the target reads with `useParams`. */
  go: (screenId: string, params?: Record<string, string>) => void;
}

export function useNav(): KitNav {
  const { view, go } = useKit();
  return { screen: view.screenId, go };
}

/**
 * The params the navigation to this screen carried. Empty when the reviewer
 * opened the screen from the preview's pickers, so every screen must render
 * without them (fall back to the first record).
 */
export function useParams(): Readonly<Record<string, string>> {
  return useKit().params;
}

/** The id of the role the reviewer is viewing as. */
export function useRole(): string {
  return useKit().view.roleId;
}

/** The id of the display state the reviewer chose (e.g. empty, loading, error). */
export function useDisplayState(): string {
  return useKit().view.stateId;
}

function useStore(): DataStore {
  const { store } = useKit();
  useSyncExternalStore(store.subscribe, store.version, store.version);
  return store;
}

/** A collection of mock records, and the ways to change it for every screen. */
export interface Collection<T extends { id: string }> {
  /** The records, in insertion order. */
  items: readonly T[];
  get: (id: string) => T | undefined;
  /** Adds a record and returns its new id, `${name}-${n}` with `n` past the highest number in use. */
  create: (record: Omit<T, "id">) => string;
  update: (id: string, patch: Partial<Omit<T, "id">>) => void;
  remove: (id: string) => void;
}

/** The collection `name` from `defineApp({ data })`: an array of records that each have a string `id`. */
export function useCollection<T extends { id: string }>(name: string): Collection<T> {
  const store = useStore();
  if (store.kind(name) !== "collection") {
    throw new Error(`useCollection: no collection named ${JSON.stringify(name)} in defineApp({ data }); a collection is an array of records that each have a string id`);
  }
  const items = store.items(name) as unknown as readonly T[];
  return {
    items,
    get: (id) => (store.items(name) as unknown as readonly T[]).find((r) => r.id === id),
    create: (record) => store.create(name, record as Record<string, unknown>),
    update: (id, patch) => store.update(name, id, patch as Record<string, unknown>),
    remove: (id) => store.remove(name, id),
  };
}

/** The single value `key` from `defineApp({ data })`, and a setter shared by every screen. */
export function useValue<T>(key: string): [T, (next: T | ((previous: T) => T)) => void] {
  const store = useStore();
  const kind = store.kind(key);
  if (kind !== "value") {
    const hint = kind === "collection" ? "; it is a collection, read it with useCollection" : "";
    throw new Error(`useValue: no value named ${JSON.stringify(key)} in defineApp({ data })${hint}`);
  }
  const set = useCallback(
    (next: T | ((previous: T) => T)) => {
      const previous = store.value(key) as T;
      store.set(key, typeof next === "function" ? (next as (p: T) => T)(previous) : next);
    },
    [store, key],
  );
  return [store.value(key) as T, set];
}

/** The fixed date every prototype treats as today (ISO `YYYY-MM-DD`). */
export const PROTOTYPE_TODAY = "2026-01-15";

/** Today's date, fixed (`PROTOTYPE_TODAY`): a prototype never reads the clock. */
export function useToday(): string {
  return PROTOTYPE_TODAY;
}
