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
 * What a `prototype.tsx` exports as its default: its screens, keyed by the
 * manifest's screen ids, and the mock data they share.
 */

import type { ComponentType } from "react";

const APP = Symbol.for("wso2.prototype-kit.app");

export interface PrototypeAppDefinition {
  /** One component per manifest screen, keyed by the screen's id. */
  screens: Record<string, ComponentType>;
  /**
   * The mock data the screens share, as JSON literals. An array of records
   * that each have a string `id` is a collection (`useCollection(name)`);
   * anything else is a single value (`useValue(key)`). Changes live until
   * Reset data (or a reload without `--persist`).
   */
  data?: Record<string, unknown>;
}

export interface PrototypeApp {
  readonly screens: Record<string, ComponentType>;
  readonly data: Record<string, unknown>;
  readonly [APP]: true;
}

/** Declare the prototype: `export default defineApp({ screens, data })`. */
export function defineApp(definition: PrototypeAppDefinition): PrototypeApp {
  if (typeof definition !== "object" || definition === null || typeof definition.screens !== "object" || definition.screens === null) {
    throw new Error("defineApp takes { screens, data? }");
  }
  for (const [id, screen] of Object.entries(definition.screens)) {
    if (typeof screen !== "function") throw new Error(`defineApp: screen ${JSON.stringify(id)} is not a component`);
  }
  const data = definition.data ?? {};
  for (const [key, value] of Object.entries(data)) assertJson(value, `data.${key}`);
  return { screens: definition.screens, data, [APP]: true };
}

/** Whether a module's default export came from `defineApp`. */
export function isPrototypeApp(value: unknown): value is PrototypeApp {
  return typeof value === "object" && value !== null && (value as Record<symbol, unknown>)[APP] === true;
}

/** Throws unless `value` is plain JSON: mock data crosses the bridge and is stored as JSON. */
function assertJson(value: unknown, path: string): void {
  if (value === null || typeof value === "string" || typeof value === "boolean") return;
  if (typeof value === "number") {
    if (Number.isFinite(value)) return;
    throw new Error(`defineApp: ${path} is not JSON (${String(value)})`);
  }
  if (Array.isArray(value)) {
    value.forEach((v, i) => assertJson(v, `${path}[${i}]`));
    return;
  }
  if (typeof value === "object" && Object.getPrototypeOf(value) === Object.prototype) {
    for (const [k, v] of Object.entries(value)) assertJson(v, `${path}.${k}`);
    return;
  }
  throw new Error(`defineApp: ${path} is not JSON; mock data is strings, numbers, booleans, null, arrays and plain objects`);
}
