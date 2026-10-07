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
 * The mock data a prototype runs on, as it crosses the bridge: `defineApp`'s
 * `data`, keyed by name. An array of records that each have a string `id` is
 * a collection; anything else is a single value. JSON only.
 */

export type DataSnapshot = Record<string, unknown>;

export interface DataRecord {
  id: string;
  [field: string]: unknown;
}

export function isCollection(value: unknown): value is DataRecord[] {
  return (
    Array.isArray(value) &&
    value.every((r) => typeof r === "object" && r !== null && !Array.isArray(r) && typeof (r as { id?: unknown }).id === "string")
  );
}

/** Deep enough for any mock data. */
const MAX_JSON_DEPTH = 64;
/** Structured clone keeps shared references, so a small hostile value can be a huge tree; the walk stops at this many values. */
const MAX_JSON_NODES = 100_000;

/** Walks `value` once, counting into `budget`; an object met again while still being walked (a cycle) is not JSON. */
function isJsonValue(value: unknown, depth: number, ancestors: Set<object>, budget: { left: number }): boolean {
  if (--budget.left < 0) return false;
  if (value === null || typeof value === "string" || typeof value === "boolean") return true;
  if (typeof value === "number") return Number.isFinite(value);
  if (depth >= MAX_JSON_DEPTH || typeof value !== "object" || ancestors.has(value)) return false;
  if (!Array.isArray(value)) {
    const proto: unknown = Object.getPrototypeOf(value);
    if (proto !== Object.prototype && proto !== null) return false;
  }
  ancestors.add(value);
  const ok = Object.values(value).every((v) => isJsonValue(v, depth + 1, ancestors, budget));
  ancestors.delete(value);
  return ok;
}

/**
 * Whether `value` is a snapshot a host may keep and hand back to the frame: an
 * object whose every key holds a collection (an array of records with a
 * string `id`) or any other JSON value. A frame is untrusted, so its snapshot
 * is checked before it is stored.
 */
export function isDataSnapshot(value: unknown): value is DataSnapshot {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  return isJsonValue(value, 0, new Set(), { left: MAX_JSON_NODES });
}
