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
 * The mock data store a running prototype owns: collections with create,
 * update and remove, and single values. React-free; the hooks subscribe to
 * it. Every change reports a snapshot (the frame posts it to the host, which
 * may persist it); a store can start from such a snapshot instead of the seed.
 */

import { isCollection, type DataRecord, type DataSnapshot } from "../data.js";

export type DataKind = "collection" | "value";

export interface DataStore {
  subscribe(listener: () => void): () => void;
  /** Bumped on every change: what the hooks' `useSyncExternalStore` reads. */
  version(): number;
  kind(name: string): DataKind | undefined;
  items(name: string): readonly DataRecord[];
  value(key: string): unknown;
  /** Adds a record with the next id `${name}-${n}` and returns that id. */
  create(name: string, record: Record<string, unknown>): string;
  update(name: string, id: string, patch: Record<string, unknown>): void;
  remove(name: string, id: string): void;
  set(key: string, value: unknown): void;
  snapshot(): DataSnapshot;
}

function copy<T>(value: T): T {
  return value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T);
}

/** The largest number an id ends in (`contacts-3`, `1042`), 0 when none does. */
function highestNumber(records: readonly DataRecord[]): number {
  let highest = 0;
  for (const r of records) {
    const n = Number(/(\d+)$/.exec(r.id)?.[1] ?? 0);
    if (n > highest) highest = n;
  }
  return highest;
}

/**
 * A store seeded from `defineApp`'s data. `initial`, when given, is a
 * snapshot to start from instead: per key, a snapshot entry of the seed's
 * kind replaces the seed, and anything else in it is ignored.
 */
export function createDataStore(seed: DataSnapshot, initial: DataSnapshot | undefined, onChange: (snapshot: DataSnapshot) => void): DataStore {
  const kinds = new Map<string, DataKind>();
  const collections = new Map<string, DataRecord[]>();
  const values = new Map<string, unknown>();
  const counters = new Map<string, number>();
  const listeners = new Set<() => void>();
  let version = 0;

  for (const [key, seeded] of Object.entries(seed)) {
    const restored = initial !== undefined && Object.hasOwn(initial, key) ? initial[key] : undefined;
    if (isCollection(seeded)) {
      const items = isCollection(restored) ? restored : seeded;
      kinds.set(key, "collection");
      collections.set(key, copy(items));
      counters.set(key, Math.max(highestNumber(seeded), highestNumber(items)));
    } else {
      kinds.set(key, "value");
      values.set(key, copy(restored !== undefined && !isCollection(restored) ? restored : seeded));
    }
  }

  const snapshot = (): DataSnapshot => {
    const out: DataSnapshot = {};
    for (const [key, kind] of kinds) out[key] = copy(kind === "collection" ? collections.get(key) : values.get(key));
    return out;
  };
  const changed = () => {
    version += 1;
    for (const listener of listeners) listener();
    onChange(snapshot());
  };
  const collection = (name: string): DataRecord[] => {
    if (kinds.get(name) !== "collection") {
      throw new Error(`no collection named ${JSON.stringify(name)} in defineApp({ data }): a collection is an array of records that each have a string id`);
    }
    return collections.get(name) ?? [];
  };

  return {
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    version: () => version,
    kind: (name) => kinds.get(name),
    items: (name) => collection(name),
    value: (key) => values.get(key),
    create(name, record) {
      const items = collection(name);
      let n = (counters.get(name) ?? 0) + 1;
      while (items.some((r) => r.id === `${name}-${n}`)) n += 1;
      counters.set(name, n);
      const id = `${name}-${n}`;
      collections.set(name, [...items, { ...copy(record), id }]);
      changed();
      return id;
    },
    update(name, id, patch) {
      const items = collection(name);
      if (!items.some((r) => r.id === id)) return;
      collections.set(name, items.map((r) => (r.id === id ? { ...r, ...copy(patch), id } : r)));
      changed();
    },
    remove(name, id) {
      const items = collection(name);
      if (!items.some((r) => r.id === id)) return;
      collections.set(name, items.filter((r) => r.id !== id));
      changed();
    },
    set(key, value) {
      if (kinds.get(key) !== "value") throw new Error(`no value named ${JSON.stringify(key)} in defineApp({ data })`);
      values.set(key, copy(value));
      changed();
    },
    snapshot,
  };
}
